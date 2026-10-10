// Package deploy holds the database steps a deployment runs and the development
// bootstrap reuses: applying the schema migrations and checking the release's role policy
// against what the permission engine's store holds.
package deploy

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/cccteam/access"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/tenanted/pkg/config"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/tenanted/pkg/resources"
	"github.com/cccteam/ccc/resource"
	initiator "github.com/cccteam/db-initiator"
	"github.com/go-playground/errors/v5"
)

// MigrationsSource is where the schema migrations live, relative to the module root.
const MigrationsSource = "file://schema/migrations"

// DevSeedSource is the development data (the tenants a developer signs in to), relative
// to the module root: applied by cmd/bootstrap in development and by the migrate
// command with -seed in test environments (the pipeline passes it there and never in
// production), as data migrations tracked apart from the schema, so a seeded database
// takes nothing twice and a new seed file reaches it.
const DevSeedSource = "file://schema/devseed"

// MigrateSchema connects to the existing database and applies every pending schema
// migration. Creating the database is not its business: a deployment's database exists
// before its first migration runs, and cmd/bootstrap creates the emulator's.
func MigrateSchema(ctx context.Context, settings config.DatabaseSettings) error {
	migrator, err := initiator.NewSpannerMigrator(ctx, settings.ProjectID, settings.InstanceID, settings.DatabaseName)
	if err != nil {
		return errors.Wrapf(err, "initiator.NewSpannerMigrator(): %s", settings.DatabasePath())
	}
	defer migrator.Close()

	// A database already at the latest migration is not a failure: the migrator has
	// nothing to apply and returns nil.
	if err := migrator.MigrateUpSchema(ctx, MigrationsSource); err != nil {
		return errors.Wrap(err, "initiator.SpannerMigrator.MigrateUpSchema()")
	}

	return nil
}

// SeedDevelopmentData applies the development seed to the database as data migrations.
// An application without a seed directory, or with an empty one, has nothing to apply.
func SeedDevelopmentData(ctx context.Context, settings config.DatabaseSettings) error {
	files, err := filepath.Glob(filepath.Join(strings.TrimPrefix(DevSeedSource, "file://"), "*.up.sql"))
	if err != nil {
		return errors.Wrap(err, "filepath.Glob()")
	}
	if len(files) == 0 {
		return nil
	}
	migrator, err := initiator.NewSpannerMigrator(ctx, settings.ProjectID, settings.InstanceID, settings.DatabaseName)
	if err != nil {
		return errors.Wrapf(err, "initiator.NewSpannerMigrator(): %s", settings.DatabasePath())
	}
	defer migrator.Close()

	if err := migrator.MigrateUpData(ctx, DevSeedSource); err != nil {
		return errors.Wrap(err, "initiator.SpannerMigrator.MigrateUpData()")
	}

	return nil
}

// Table names one of the two migrations tables the migrate command reports and forces:
// the schema migrations' and the data migrations'.
type Table string

// The two tables, as the command's flags and output name them.
const (
	SchemaTable Table = "schema"
	DataTable   Table = "data"
)

// Versions reads what the schema and data migrations tables say about the database: no
// version, clean at a version, or dirty at one with the progress the runner recorded.
// Nothing is applied.
func Versions(ctx context.Context, settings config.DatabaseSettings) (schema, data initiator.Version, err error) {
	migrator, err := initiator.NewSpannerMigrator(ctx, settings.ProjectID, settings.InstanceID, settings.DatabaseName)
	if err != nil {
		return initiator.Version{}, initiator.Version{}, errors.Wrapf(err, "initiator.NewSpannerMigrator(): %s", settings.DatabasePath())
	}
	defer migrator.Close()

	schema, err = migrator.SchemaVersion(ctx)
	if err != nil {
		return initiator.Version{}, initiator.Version{}, errors.Wrap(err, "initiator.SpannerMigrator.SchemaVersion()")
	}
	data, err = migrator.DataVersion(ctx)
	if err != nil {
		return initiator.Version{}, initiator.Version{}, errors.Wrap(err, "initiator.SpannerMigrator.DataVersion()")
	}

	return schema, data, nil
}

// Force sets one migrations table to a version, clean, with no progress recorded; -1
// leaves the table with no version. It is for the states the runner refuses to guess at
// (a database the old library left dirty with no progress recorded, an in-flight
// operation Spanner no longer has, a file changed in its applied part): a person reads
// the database's state, decides what it really holds, and forces that version, and the
// next run continues from it.
func Force(ctx context.Context, settings config.DatabaseSettings, table Table, version int) error {
	migrator, err := initiator.NewSpannerMigrator(ctx, settings.ProjectID, settings.InstanceID, settings.DatabaseName)
	if err != nil {
		return errors.Wrapf(err, "initiator.NewSpannerMigrator(): %s", settings.DatabasePath())
	}
	defer migrator.Close()

	switch table {
	case SchemaTable:
		if err := migrator.ForceSchema(ctx, version); err != nil {
			return errors.Wrap(err, "initiator.SpannerMigrator.ForceSchema()")
		}
	case DataTable:
		if err := migrator.ForceData(ctx, version); err != nil {
			return errors.Wrap(err, "initiator.SpannerMigrator.ForceData()")
		}
	default:
		return errors.Newf("table %q is neither %s nor %s", table, SchemaTable, DataTable)
	}

	return nil
}

// CheckRoles reports what the deploy should hear about the named auth's role policy
// before the release takes traffic: the warnings its role file raises, and what its
// store holds that this release cannot use as written (a grant it skips, a custom role a
// default role of the same name shadows, memberships naming a role nothing defines). Each
// is printed on its own "Warning:" line. Nothing is written: the default roles travel with
// the release, which validated them when it opened the engine, so there is nothing to
// reconcile; a store that cannot be read is the error.
func CheckRoles(ctx context.Context, client *access.Client, name string) error {
	warnings, err := client.CheckPolicy(ctx)
	if err != nil {
		return errors.Wrapf(err, "access.Client.CheckPolicy(): the %s auth", name)
	}
	for _, w := range warnings {
		fmt.Printf("Warning: %s\n", w)
	}

	return nil
}

// MigrateFeatures brings the FeatureFlags table to the release's declarations
// (resources.Features): a flag declared for the first time is inserted off, a known
// flag keeps its state and takes the release's description, and a flag the release no
// longer declares is deleted. It runs after the schema migration, so the table exists,
// and after the role check, before the release takes traffic, so every instance's copy
// lists the same flags; the development seed's rows, applied before it, decide a flag's
// state in development and the test environments.
func MigrateFeatures(ctx context.Context, client resource.Client) error {
	declared := resources.Features()
	if err := resource.MigrateFeatures(ctx, client, declared); err != nil {
		return errors.Wrap(err, "resource.MigrateFeatures()")
	}
	fmt.Printf("Migrated the feature flags: %d declared\n", len(declared))

	return nil
}
