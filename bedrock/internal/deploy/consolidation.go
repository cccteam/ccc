// consolidation.go reads a schema migrations directory for the one change that removes
// committed migrations: a consolidation (bedrock migration consolidate), the committed
// sequence folded into one file at the index where it ended. The migration guard accepts
// the shape on a pull request and reads each environment's migration version before the
// merge; the migrate step reads the database in front of it before the fold runs.

package deploy

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/migration"
)

// migrationDatabasesSub names the application's database in every other environment,
// env=resource name (the first generation's; a restore puts a database into the next
// generation, which the reader probes), for the guard's read on a consolidation.
const migrationDatabasesSub = "_MIGRATION_DATABASES"

// consolidation is a directory read as a fold: the index its one file carries.
type consolidation struct {
	Dir   string
	Index int
}

// consolidatedNames are the folded file's names at an index: the up file, and the down
// file a sequence with any down file gets.
func consolidatedNames(index int) (up, down string) {
	stem := fmt.Sprintf("%06d_%s", index, migration.ConsolidatedName)

	return stem + ".up.sql", stem + ".down.sql"
}

// consolidationShape reads a directory's names as a consolidation: exactly the folded
// file at index, with or without its down file, and nothing else. It answers the index,
// or 0 when the names are not that shape.
func consolidationShape(names []string) int {
	var sqls []string
	for _, n := range names {
		if strings.HasSuffix(n, ".sql") {
			sqls = append(sqls, n)
		}
	}
	if len(sqls) == 0 || len(sqls) > 2 {
		return 0
	}
	m := migration.NameRE.FindStringSubmatch(sqls[0])
	if m == nil {
		return 0
	}
	index, _ := strconv.Atoi(m[1])
	up, down := consolidatedNames(index)
	sort.Strings(sqls)
	switch {
	case len(sqls) == 1 && sqls[0] == up:
		return index
	case len(sqls) == 2 && sqls[0] == down && sqls[1] == up:
		return index
	default:
		return 0
	}
}

// consolidationOf reads a pull request's schema directory against the files it branched
// from: a consolidation when every committed file is gone, none kept, and the tree holds
// the folded file at the committed sequence's highest index alone. nil otherwise.
func consolidationOf(dir string, committed, tree []string) *consolidation {
	if len(committed) == 0 {
		return nil
	}
	highest := 0
	for _, name := range committed {
		if slices.Contains(tree, name) {
			return nil
		}
		if m := migration.NameRE.FindStringSubmatch(name); len(m) > 3 && m[3] == "up" {
			index, _ := strconv.Atoi(m[1])
			highest = max(highest, index)
		}
	}
	if index := consolidationShape(tree); index != 0 && index == highest {
		return &consolidation{Dir: dir, Index: index}
	}

	return nil
}

// environmentVersion reads the live database's migration version in one environment:
// the first generation's name is probed first, then the generations a restore made
// (-2, -3, ...) until one is missing; the newest that exists is live, since no earlier
// generation is ever put back into service. role is the database role the read goes
// through, "" for the identity's own grant.
func environmentVersion(ctx context.Context, reader Versions, database, role string) (MigrationVersion, string, error) {
	live := ""
	var version MigrationVersion
	for generation := 1; ; generation++ {
		name := database
		if generation > 1 {
			name = fmt.Sprintf("%s-%d", database, generation)
		}
		v, err := reader.Version(ctx, name, role)
		switch {
		case isNotFound(err) && live != "":
			return version, live, nil
		case isForbidden(err) && live != "":
			// Spanner answers a database name the caller holds no grant on with 403, a
			// name no database has included, and the stack grants the identity on every
			// generation it makes: a denial past a readable generation names no database.
			return version, live, nil
		case isNotFound(err):
			return MigrationVersion{}, "", errors.Wrapf(err, "no database %s", database)
		case err != nil:
			return MigrationVersion{}, "", err
		}
		live, version = name, v
	}
}

// pairsSorted lists a pairs map's keys in order.
func pairsSorted(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	return keys
}
