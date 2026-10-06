// migrations.go checks the schema migrations directory: the sequence the migrate command
// applies must be complete and unambiguous, so no migration is skipped and every one
// applies in the documented order.

package check

import (
	"os"
	"path"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/migration"
)

// MigrationFinding is one problem with the migrations directory.
type MigrationFinding struct {
	// Path is the file, or the directory for a problem of the sequence as a whole.
	Path string
	// Problem says what is wrong, the way a person reads it.
	Problem string
}

// scanMigrations reads the migrations directory by the sequence rule (migration.Sequence)
// and reports every file that is not a migration file, every index with two up files or a
// down file without an up, and every gap: the indexes run contiguously from the lowest
// present to the highest, one up file each, so a consolidated history that starts above
// 000001 passes and a skipped number does not. A directory that does not exist has nothing
// to check. Findings name their files under rel, the directory as the application names it.
func scanMigrations(dir, rel string) ([]MigrationFinding, error) {
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}

		return nil, errors.Wrapf(err, "os.ReadDir(): %s", dir)
	}
	entries := make([]migration.Entry, 0, len(dirEntries))
	for _, e := range dirEntries {
		if !e.IsDir() {
			entries = append(entries, migration.Entry{Name: e.Name()})
		}
	}
	problems, _ := migration.Sequence(entries)
	findings := make([]MigrationFinding, 0, len(problems))
	for _, p := range problems {
		where := rel
		if p.File != "" {
			where = path.Join(rel, p.File)
		}
		findings = append(findings, MigrationFinding{Path: where, Problem: p.Text})
	}

	return findings, nil
}
