// Package deploy holds the database steps a deployment runs and the development
// bootstrap reuses: applying the schema migrations and reconciling the role
// configuration into the permission engine's policy store; and the bootstrap's own
// data steps, seeding the demo world and emptying a database for the next seed.
//
// Demonstrates: impulse.bootstrapped, auth.two-populations.
package deploy

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/access"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource/lodestar/pkg/config"
	"github.com/cccteam/ccc/resource/lodestar/pkg/router"
	initiator "github.com/cccteam/db-initiator"
	"github.com/go-playground/errors/v5"
	"github.com/golang-migrate/migrate/v4"
)

// MigrationsSource is where the schema migrations live, relative to the module root.
const MigrationsSource = "file://schema/migrations"

// DevSeedSource is the demo world (the sectors, the fleet, the missions a persona signs in
// to), applied by cmd/bootstrap only as a data migration, relative to the module root.
const DevSeedSource = "file://schema/devseed"

// MigrateSchema connects to the existing database and applies every pending schema
// migration. Creating the database is not its business: a deployment's database exists
// before its first migration runs, and cmd/bootstrap creates the emulator's.
func MigrateSchema(ctx context.Context, settings config.SpannerSettings) error {
	migrator, err := initiator.NewSpannerMigrator(ctx, settings.ProjectID, settings.InstanceID, settings.DatabaseName)
	if err != nil {
		return errors.Wrapf(err, "initiator.NewSpannerMigrator(): %s", settings.DatabasePath())
	}
	defer migrator.Close()

	// A database already at the latest migration is not a failure: the migrator
	// reports it as migrate.ErrNoChange.
	if err := migrator.MigrateUpSchema(ctx, MigrationsSource); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return errors.Wrap(err, "initiator.SpannerMigrator.MigrateUpSchema()")
	}

	return nil
}

// SeedDevelopmentData applies the demo world to the database as a data migration. The
// sectors it seeds are the domain universe MigrateRoles reconciles across, so it runs
// before the roles.
func SeedDevelopmentData(ctx context.Context, settings config.SpannerSettings) error {
	migrator, err := initiator.NewSpannerMigrator(ctx, settings.ProjectID, settings.InstanceID, settings.DatabaseName)
	if err != nil {
		return errors.Wrapf(err, "initiator.NewSpannerMigrator(): %s", settings.DatabasePath())
	}
	defer migrator.Close()

	if err := migrator.MigrateUpData(ctx, DevSeedSource); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return errors.Wrap(err, "initiator.SpannerMigrator.MigrateUpData()")
	}

	return nil
}

// MigrateRoles reconciles one auth's committed role configuration (its roles file) into
// its policy store (its user manager), validated against the generated permission
// collection, across the given tenant domains (none for a global-only application).
func MigrateRoles(ctx context.Context, manager access.UserManager, rolesPath string, domains ...accesstypes.Domain) error {
	roles, err := loadRoles(rolesPath)
	if err != nil {
		return err
	}

	if err := access.MigrateRoles(ctx, manager, router.Collection(), roles, domains...); err != nil {
		return errors.Wrap(err, "access.MigrateRoles()")
	}

	return nil
}

func loadRoles(path string) (*access.RoleConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.Wrapf(err, "os.ReadFile(%q)", path)
	}

	var roles access.RoleConfig
	if err := json.Unmarshal(raw, &roles); err != nil {
		return nil, errors.Wrapf(err, "json.Unmarshal(%q)", path)
	}

	return &roles, nil
}

// schemaMigrationsTable is where db-initiator's migrator records the schema versions it
// applied (its default name). A data reset leaves it alone: the schema stays current.
const schemaMigrationsTable = "SchemaMigrations"

// insertInto finds the tables a migration script writes rows into.
var insertInto = regexp.MustCompile("(?i)\\bINSERT\\s+INTO\\s+`?([A-Za-z_][A-Za-z0-9_]*)`?")

// ResetDevelopmentData empties every table the schema migrations do not populate, so
// the bootstrap can seed the world again on a database whose schema is current: the demo
// world, the logins and their sessions, both auths' roles and grants, the change events,
// and the data-migration bookkeeping, so the seed applies afresh. The rows the schema
// migrations themselves insert (the enumeration tables) stay, and so does the
// schema-migration bookkeeping. Tables empty one commit at a time, children first: Spanner
// refuses to empty a parent while an interleaved child under ON DELETE NO ACTION still
// holds rows, whether or not the child's delete is in the same commit. No DDL runs, which
// on a real instance is the point: schema changes there are slow and rate-limited, a data
// reset is neither.
//
// Demonstrates: bootstrap.reset.
func ResetDevelopmentData(ctx context.Context, client *spanner.Client, migrationsSource string) error {
	owned, err := schemaOwnedTables(migrationsSource)
	if err != nil {
		return err
	}

	tables, err := deleteOrder(ctx, client)
	if err != nil {
		return err
	}

	for _, table := range tables {
		if table == schemaMigrationsTable || owned[table] {
			continue
		}
		if _, err := client.Apply(ctx, []*spanner.Mutation{spanner.Delete(table, spanner.AllKeys())}); err != nil {
			return errors.Wrapf(err, "spanner.Client.Apply(): emptying %s", table)
		}
	}

	return nil
}

// schemaOwnedTables reads the schema migrations at a file:// source and returns the
// tables their up scripts insert rows into: schema-owned data, which a reset keeps. A
// source that cannot be read is an error, never an empty set, because an empty set would
// let the reset empty those tables too.
func schemaOwnedTables(migrationsSource string) (map[string]bool, error) {
	dir, ok := strings.CutPrefix(migrationsSource, "file://")
	if !ok {
		return nil, errors.Newf("schema migrations source %q is not a file:// URL", migrationsSource)
	}

	files, err := filepath.Glob(filepath.Join(dir, "*.up.sql"))
	if err != nil {
		return nil, errors.Wrapf(err, "filepath.Glob(%q)", dir)
	}
	if len(files) == 0 {
		return nil, errors.Newf("no *.up.sql schema migrations under %q", dir)
	}

	owned := map[string]bool{}
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			return nil, errors.Wrapf(err, "os.ReadFile(%q)", file)
		}
		for _, match := range insertInto.FindAllStringSubmatch(string(raw), -1) {
			owned[match[1]] = true
		}
	}

	return owned, nil
}

// deleteOrder lists the database's tables in an order where every table can be emptied
// in a commit of its own: interleaved children before their parents, and referencing
// tables before the tables their foreign keys point at. Both relations come from the
// information schema, so a new table needs no registration here.
func deleteOrder(ctx context.Context, client *spanner.Client) ([]string, error) {
	parents, err := tableParents(ctx, client)
	if err != nil {
		return nil, err
	}
	references, err := foreignKeyReferences(ctx, client)
	if err != nil {
		return nil, err
	}

	// An edge runs from the table that must empty first to the one that follows.
	indegree := make(map[string]int, len(parents))
	for table := range parents {
		indegree[table] = 0
	}
	followers := map[string][]string{}
	edge := func(first, then string) {
		if first == then {
			return
		}
		if _, ok := indegree[first]; !ok {
			return
		}
		if _, ok := indegree[then]; !ok {
			return
		}
		followers[first] = append(followers[first], then)
		indegree[then]++
	}
	for child, parent := range parents {
		if parent != "" {
			edge(child, parent)
		}
	}
	for _, r := range references {
		edge(r.referencing, r.referenced)
	}

	var ready []string
	for table, n := range indegree {
		if n == 0 {
			ready = append(ready, table)
		}
	}
	sort.Strings(ready)

	order := make([]string, 0, len(indegree))
	for len(ready) > 0 {
		table := ready[0]
		ready = ready[1:]
		order = append(order, table)
		for _, next := range followers[table] {
			indegree[next]--
			if indegree[next] == 0 {
				at, _ := slices.BinarySearch(ready, next)
				ready = slices.Insert(ready, at, next)
			}
		}
	}
	if len(order) != len(indegree) {
		return nil, errors.New("the tables' interleaving and foreign keys form a cycle; no delete order empties them one by one")
	}

	return order, nil
}

// tableParents maps every table to the table it is interleaved in, "" for a root.
func tableParents(ctx context.Context, client *spanner.Client) (map[string]string, error) {
	stmt := spanner.Statement{SQL: `SELECT TABLE_NAME, PARENT_TABLE_NAME FROM INFORMATION_SCHEMA.TABLES
		WHERE TABLE_CATALOG = '' AND TABLE_SCHEMA = '' AND TABLE_TYPE = 'BASE TABLE'`}

	parents := map[string]string{}
	if err := client.Single().Query(ctx, stmt).Do(func(row *spanner.Row) error {
		var name string
		var parent spanner.NullString
		if err := row.Columns(&name, &parent); err != nil {
			return errors.Wrap(err, "spanner.Row.Columns()")
		}
		parents[name] = parent.StringVal

		return nil
	}); err != nil {
		return nil, errors.Wrap(err, "spanner.RowIterator.Do(): INFORMATION_SCHEMA.TABLES")
	}

	return parents, nil
}

// foreignKeyReference is one foreign key: the table holding it and the table it points at.
type foreignKeyReference struct {
	referencing string
	referenced  string
}

// foreignKeyReferences lists every foreign key as a pair of tables.
func foreignKeyReferences(ctx context.Context, client *spanner.Client) ([]foreignKeyReference, error) {
	stmt := spanner.Statement{SQL: `SELECT tc.TABLE_NAME, uc.TABLE_NAME
		FROM INFORMATION_SCHEMA.REFERENTIAL_CONSTRAINTS rc
		JOIN INFORMATION_SCHEMA.TABLE_CONSTRAINTS tc
		  ON tc.CONSTRAINT_NAME = rc.CONSTRAINT_NAME AND tc.CONSTRAINT_SCHEMA = rc.CONSTRAINT_SCHEMA
		JOIN INFORMATION_SCHEMA.TABLE_CONSTRAINTS uc
		  ON uc.CONSTRAINT_NAME = rc.UNIQUE_CONSTRAINT_NAME AND uc.CONSTRAINT_SCHEMA = rc.UNIQUE_CONSTRAINT_SCHEMA
		WHERE rc.CONSTRAINT_CATALOG = '' AND rc.CONSTRAINT_SCHEMA = ''`}

	var references []foreignKeyReference
	if err := client.Single().Query(ctx, stmt).Do(func(row *spanner.Row) error {
		var r foreignKeyReference
		if err := row.Columns(&r.referencing, &r.referenced); err != nil {
			return errors.Wrap(err, "spanner.Row.Columns()")
		}
		references = append(references, r)

		return nil
	}); err != nil {
		return nil, errors.Wrap(err, "spanner.RowIterator.Do(): INFORMATION_SCHEMA.REFERENTIAL_CONSTRAINTS")
	}

	return references, nil
}
