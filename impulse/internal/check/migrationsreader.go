package check

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/app"
)

// The database role the pipeline's migration guard reads an environment's migration
// version through, and the two statements the schema sequence must carry to create it:
// the role, with select on the schema migrations table alone. A migration in the
// sequence runs them once, after the runner has made the table; the emulator accepts
// them and keeps no role.
const (
	MigrationsReaderRole   = "migrations_reader"
	SchemaMigrationsTable  = "SchemaMigrations"
	migrationsReaderName   = "migrations-reader"
	MigrationsReaderSuffix = "_MigrationsReader"
)

// MigrationsReaderStatements are the statements a sequence must carry, in the order a
// migration runs them.
var MigrationsReaderStatements = []string{
	"CREATE ROLE " + MigrationsReaderRole,
	"GRANT SELECT ON TABLE " + SchemaMigrationsTable + " TO ROLE " + MigrationsReaderRole,
}

// MigrationsReaderDown reverses the statements, for the migration's down file.
var MigrationsReaderDown = []string{
	"REVOKE SELECT ON TABLE " + SchemaMigrationsTable + " FROM ROLE " + MigrationsReaderRole,
	"DROP ROLE " + MigrationsReaderRole,
}

// migrationIndexRE reads a migration file's six-digit index.
var migrationIndexRE = regexp.MustCompile(`^(\d{6})_.*\.(up|down)\.sql$`)

// migrationsReader verifies that the schema sequence creates the migrations_reader
// database role and grants it select on the schema migrations table, which the
// pipeline's migration guard reads each environment's migration version through on a
// consolidation. The skeleton's first migration carries the two statements; an
// application whose sequence predates them gets a migration that adds them from
// impulse upgrade.
type migrationsReader struct{}

func (migrationsReader) Name() string { return migrationsReaderName }

func (migrationsReader) Describe() string {
	return "the schema sequence creates the migrations_reader database role with select on the SchemaMigrations table, which the pipeline's migration guard reads each environment's version through"
}

func (migrationsReader) Meaning() string {
	return "The pipeline's migration guard reads each environment's migration version through the migrations_reader database role, which holds select on the schema migrations table alone: a migration in the sequence creates and grants it once, and the environment layer grants the test environment's deploy identity the role on the other environments' databases."
}

func (c migrationsReader) Run(_ context.Context, env *Env) Result {
	a := env.App
	p := a.Profile()
	if len(p.Sites) == 0 {
		return skip(c.Name(), "no site generator")
	}
	var details, read []string
	for _, dir := range migrationSourceDirs(p) {
		missing, next, err := MigrationsReaderMissing(a, dir)
		if err != nil {
			return fail(c.Name(), err.Error())
		}
		if next == 0 {
			continue // no sequence there; the schema checks report an absent directory
		}
		read = append(read, dir)
		if len(missing) == 0 {
			continue
		}
		details = append(details, fmt.Sprintf("%s: no migration runs %s, so the pipeline's migration guard cannot read this database's migration version through the %s role; add %s/%06d%s.up.sql with %s (and a down file reversing it), which impulse upgrade writes", dir, backticked(missing), MigrationsReaderRole, dir, next, MigrationsReaderSuffix, backticked(MigrationsReaderStatements)))
	}
	if len(details) > 0 {
		return fail(c.Name(), fmt.Sprintf("%d schema sequence(s) without the %s role", len(details), MigrationsReaderRole), details...)
	}
	if len(read) == 0 {
		return skip(c.Name(), "no schema migrations directory")
	}

	return pass(c.Name(), fmt.Sprintf("the %s role is created and granted select on %s in %s", MigrationsReaderRole, SchemaMigrationsTable, strings.Join(read, ", ")))
}

// migrationSourceDirs lists the file:// migration directories the site generators read,
// root-relative, each once.
func migrationSourceDirs(p app.Profile) []string {
	var dirs []string
	seen := map[string]bool{}
	for i := range p.Sites {
		for _, src := range p.Sites[i].Generator.MigrationSources {
			if !strings.HasPrefix(src, fileScheme) {
				continue
			}
			dir := strings.TrimPrefix(src, fileScheme)
			if seen[dir] {
				continue
			}
			seen[dir] = true
			dirs = append(dirs, dir)
		}
	}

	return dirs
}

// MigrationsReaderMissing reads the up migrations under a root-relative directory
// statement by statement and names the reader role's statements none of them runs, in
// order, with the next free index of the sequence (0 when the directory holds no
// migration, or does not exist).
func MigrationsReaderMissing(a *app.App, dir string) (missing []string, next int, err error) {
	entries, err := os.ReadDir(a.Abs(dir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, errors.Wrap(err, "os.ReadDir()")
	}
	found := map[string]bool{}
	highest := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		m := migrationIndexRE.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		idx, _ := strconv.Atoi(m[1])
		highest = max(highest, idx)
		if m[2] != "up" {
			continue
		}
		data, err := os.ReadFile(a.Abs(filepath.ToSlash(filepath.Join(dir, e.Name()))))
		if err != nil {
			return nil, 0, errors.Wrap(err, "os.ReadFile()")
		}
		for stmt := range strings.SplitSeq(stripSQLComments(string(data)), ";") {
			normalized := normalizeSQL(stmt)
			for _, want := range MigrationsReaderStatements {
				if strings.EqualFold(normalized, want) {
					found[want] = true
				}
			}
		}
	}
	if highest == 0 {
		return nil, 0, nil
	}
	for _, want := range MigrationsReaderStatements {
		if !found[want] {
			missing = append(missing, want)
		}
	}

	return missing, highest + 1, nil
}

// backticked spells statements for a message, each in backticks.
func backticked(statements []string) string {
	q := make([]string, len(statements))
	for i, s := range statements {
		q[i] = "`" + s + "`"
	}

	return strings.Join(q, " and ")
}
