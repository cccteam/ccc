// migrations.go checks the schema migrations directory: the sequence the migrate command
// applies must be complete and unambiguous, so no migration is skipped and every one
// applies in the documented order.

package check

import (
	"fmt"
	"os"
	"path"
	"regexp"
	"sort"
	"strconv"

	"github.com/go-playground/errors/v5"
)

// MigrationFinding is one problem with the migrations directory.
type MigrationFinding struct {
	// Path is the file, or the directory for a problem of the sequence as a whole.
	Path string
	// Problem says what is wrong, the way a person reads it.
	Problem string
}

// migrationNameRE is a migration file name: a six-digit index, an underscore, a name,
// and .up.sql or .down.sql.
var migrationNameRE = regexp.MustCompile(`^(\d{6})_[A-Za-z0-9_-]+\.(up|down)\.sql$`)

// seedDir is the seed directory's name beside the schema migrations directory.
const seedDir = "devseed"

// scanMigrations reads the migrations directory and reports every file that is not a
// migration file, every index with two up files or a down file without an up, and every
// gap: the indexes run contiguously from the lowest present to the highest, one up file
// each, so a consolidated history that starts above 000001 passes and a skipped number
// does not. A directory that does not exist has nothing to check. Findings name their
// files under rel, the directory as the application names it.
func scanMigrations(dir, rel string) ([]MigrationFinding, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}

		return nil, errors.Wrapf(err, "os.ReadDir(): %s", dir)
	}
	var findings []MigrationFinding
	ups := map[int][]string{}
	downs := map[int][]string{}
	for _, e := range entries {
		if e.IsDir() || path.Ext(e.Name()) != ".sql" {
			continue
		}
		m := migrationNameRE.FindStringSubmatch(e.Name())
		if m == nil {
			findings = append(findings, MigrationFinding{Path: path.Join(rel, e.Name()), Problem: "not a migration file name (NNNNNN_name.up.sql or NNNNNN_name.down.sql)"})

			continue
		}
		idx, _ := strconv.Atoi(m[1])
		if m[2] == "up" {
			ups[idx] = append(ups[idx], e.Name())
		} else {
			downs[idx] = append(downs[idx], e.Name())
		}
	}
	indexes := make([]int, 0, len(ups)+len(downs))
	for idx := range ups {
		indexes = append(indexes, idx)
	}
	for idx := range downs {
		if _, ok := ups[idx]; !ok {
			indexes = append(indexes, idx)
		}
	}
	sort.Ints(indexes)
	for _, idx := range indexes {
		if len(ups[idx]) > 1 {
			for _, name := range ups[idx] {
				findings = append(findings, MigrationFinding{Path: path.Join(rel, name), Problem: fmt.Sprintf("index %06d has %d up files; one migration per index", idx, len(ups[idx]))})
			}
		}
		if len(downs[idx]) > 1 {
			for _, name := range downs[idx] {
				findings = append(findings, MigrationFinding{Path: path.Join(rel, name), Problem: fmt.Sprintf("index %06d has %d down files; at most one", idx, len(downs[idx]))})
			}
		}
		if _, ok := ups[idx]; !ok {
			for _, name := range downs[idx] {
				findings = append(findings, MigrationFinding{Path: path.Join(rel, name), Problem: fmt.Sprintf("index %06d has a down file and no up file", idx)})
			}
		}
	}
	if len(indexes) > 0 {
		for idx := indexes[0]; idx <= indexes[len(indexes)-1]; idx++ {
			if _, ok := ups[idx]; !ok {
				if _, down := downs[idx]; down {
					continue // reported above
				}
				findings = append(findings, MigrationFinding{Path: rel, Problem: fmt.Sprintf("gap: no migration %06d between %06d and %06d; the sequence is contiguous so nothing is skipped", idx, indexes[0], indexes[len(indexes)-1])})
			}
		}
	}

	return findings, nil
}
