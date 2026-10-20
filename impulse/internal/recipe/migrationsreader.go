package recipe

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/app"
	"github.com/cccteam/ccc/impulse/internal/check"
	"github.com/cccteam/ccc/impulse/internal/transition"
)

// MigrationsReader gives an application whose schema sequence predates the
// migrations_reader database role a migration that creates it: the role, with select on
// the schema migrations table alone, which the pipeline's migration guard reads each
// environment's migration version through on a consolidation. The skeleton's first
// migration carries the two statements; here they go into a migration of their own at
// the sequence's next free index, written only where the detector finds neither
// statement run, so the recipe is safe to run twice. A released sequence takes the file
// at its end, under the freeze; an unreleased one may fold it into its first file by
// hand instead.
type MigrationsReader struct{}

// Name is the recipe's short name.
func (MigrationsReader) Name() string { return "migrations-reader" }

// Meaning says what the change means in this framework.
func (MigrationsReader) Meaning() string {
	return "The pipeline's migration guard reads each environment's migration version through the migrations_reader database role, which holds select on the schema migrations table alone; the sequence creates and grants it once, and the environment layer grants the test environment's deploy identity the role on the other environments' databases."
}

// Detect names each schema sequence that runs neither statement.
func (MigrationsReader) Detect(_ context.Context, a *app.App) ([]string, error) {
	var found []string
	for _, dir := range migrationDirs(a) {
		missing, next, err := check.MigrationsReaderMissing(a, dir)
		if err != nil {
			return nil, err
		}
		if next == 0 || len(missing) == 0 {
			continue
		}
		found = append(found, fmt.Sprintf("%s: no migration runs %s", dir, strings.Join(missing, " and ")))
	}

	return found, nil
}

// Apply writes the migration at the next free index of each sequence the detector names.
func (r MigrationsReader) Apply(_ context.Context, a *app.App, _ check.Execer) (*transition.Change, error) {
	ch := &transition.Change{}
	for _, dir := range migrationDirs(a) {
		missing, next, err := check.MigrationsReaderMissing(a, dir)
		if err != nil {
			return nil, err
		}
		if next == 0 || len(missing) == 0 {
			continue
		}
		stem := fmt.Sprintf("%06d%s", next, check.MigrationsReaderSuffix)
		up := filepath.ToSlash(filepath.Join(dir, stem+".up.sql"))
		down := filepath.ToSlash(filepath.Join(dir, stem+".down.sql"))
		mode, err := siblingMode(a.Abs(dir))
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(a.Abs(up), []byte(migrationsReaderUp), mode); err != nil {
			return nil, errors.Wrap(err, "os.WriteFile()")
		}
		if err := os.WriteFile(a.Abs(down), []byte(migrationsReaderDown), mode); err != nil {
			return nil, errors.Wrap(err, "os.WriteFile()")
		}
		ch.Did = append(ch.Did, fmt.Sprintf("wrote %s and %s: the %s database role, created with select on %s, which the pipeline's migration guard reads this database's migration version through", up, down, check.MigrationsReaderRole, check.SchemaMigrationsTable))
	}

	return ch, nil
}

// siblingMode is the permission of the migrations already in dir, which the written
// pair takes (the directory holds one at least: the recipe writes at a sequence's next
// free index, never a first).
func siblingMode(dir string) (fs.FileMode, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, errors.Wrap(err, "os.ReadDir()")
	}
	for _, e := range entries {
		if e.Type().IsRegular() && strings.HasSuffix(e.Name(), ".sql") {
			info, err := e.Info()
			if err != nil {
				return 0, errors.Wrap(err, "os.DirEntry.Info()")
			}

			return info.Mode().Perm(), nil
		}
	}

	return 0, errors.Newf("%s: no migration to take the file mode from", dir)
}

// migrationDirs lists the schema migration directories the application's site
// generators read, root-relative, each once.
func migrationDirs(a *app.App) []string {
	var dirs []string
	seen := map[string]bool{}
	for i := range a.Profile().Sites {
		for _, src := range a.Profile().Sites[i].Generator.MigrationSources {
			dir, ok := strings.CutPrefix(src, "file://")
			if !ok || seen[dir] {
				continue
			}
			seen[dir] = true
			dirs = append(dirs, dir)
		}
	}

	return dirs
}

// The migration's files: the statements the skeleton's first migration carries, with
// the same note.
var (
	migrationsReaderUp = "-- The pipeline's migration guard reads this database's migration version through the\n" +
		"-- migrations_reader database role, which holds select on the migrations table alone,\n" +
		"-- and the environment layer grants the test environment's deploy identity the role.\n" +
		"-- Created once, by this migration, after the runner has made the table. The emulator\n" +
		"-- accepts the statements and keeps no role.\n" +
		strings.Join(check.MigrationsReaderStatements, ";\n") + ";\n"
	migrationsReaderDown = strings.Join(check.MigrationsReaderDown, ";\n") + ";\n"
)
