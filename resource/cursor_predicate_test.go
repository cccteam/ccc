package resource

import (
	"math/big"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/httpio"
	"github.com/google/go-cmp/cmp"
	"github.com/shopspring/decimal"
)

// cursorTestResource carries one column of each shape the predicate renderer
// distinguishes: the key, a number, a timestamp, a nullable text, and a decimal.
type cursorTestResource struct {
	ID       string          `spanner:"Id"       postgres:"Id"`
	Hazard   int64           `spanner:"Hazard"   postgres:"Hazard"`
	Deadline time.Time       `spanner:"Deadline" postgres:"Deadline"`
	Note     *string         `spanner:"Note"     postgres:"Note"`
	Fee      decimal.Decimal `spanner:"Fee"      postgres:"Fee"`
}

func (cursorTestResource) Resource() accesstypes.Resource { return "cursorTestResources" }

func (cursorTestResource) DefaultConfig() Config { return Config{} }

// TestQuerySet_stmt_cursorPredicate is the two-database matrix for the cursor
// predicate: one, two, and three sort columns, mixed directions, a nullable column
// at each position with a value and in the NULL region, the previous-page flip,
// and the refusals. The Spanner and PostgreSQL renderings differ only in quoting.
func TestQuerySet_stmt_cursorPredicate(t *testing.T) {
	t.Parallel()

	const (
		id       = "0193e2a7-522c-708f-bfd0-4adf33486bb1"
		deadline = "2027-01-15T09:00:00.000000000Z"
	)
	stamp := time.Date(2027, 1, 15, 9, 0, 0, 0, time.UTC)

	tests := []struct {
		name        string
		sort        []SortField
		cursor      *cursor
		wantSpanner string // WHERE … ORDER BY …, Spanner quoting; postgresRendering derives the PostgreSQL rendering
		wantParams  map[string]any
		wantErr     string
	}{
		{
			name:        "key only",
			cursor:      &cursor{Direction: pageNext, Keys: []*string{strPtr(id)}},
			wantSpanner: "WHERE (`Id` > @_c1) ORDER BY `Id` ASC",
			wantParams:  map[string]any{"_c1": id},
		},
		{
			name:        "two columns, mixed directions",
			sort:        []SortField{{Field: "Hazard", Direction: SortDescending}},
			cursor:      &cursor{Direction: pageNext, Keys: []*string{strPtr("3"), strPtr(id)}},
			wantSpanner: "WHERE (`Hazard` < @_c1 OR (`Hazard` = @_c1 AND `Id` > @_c2)) ORDER BY `Hazard` DESC, `Id` ASC",
			wantParams:  map[string]any{"_c1": int64(3), "_c2": id},
		},
		{
			name:   "three columns, typed values",
			sort:   []SortField{{Field: "Hazard", Direction: SortDescending}, {Field: "Deadline", Direction: SortAscending}},
			cursor: &cursor{Direction: pageNext, Keys: []*string{strPtr("3"), strPtr(deadline), strPtr(id)}},
			wantSpanner: "WHERE (`Hazard` < @_c1 OR (`Hazard` = @_c1 AND `Deadline` > @_c2) OR (`Hazard` = @_c1 AND `Deadline` = @_c2 AND `Id` > @_c3)) " +
				"ORDER BY `Hazard` DESC, `Deadline` ASC, `Id` ASC",
			wantParams: map[string]any{"_c1": int64(3), "_c2": stamp, "_c3": id},
		},
		{
			name:        "decimal boundary binds as NUMERIC",
			sort:        []SortField{{Field: "Fee", Direction: SortAscending}},
			cursor:      &cursor{Direction: pageNext, Keys: []*string{strPtr("24000.125"), strPtr(id)}},
			wantSpanner: "WHERE (`Fee` > @_c1 OR (`Fee` = @_c1 AND `Id` > @_c2)) ORDER BY `Fee` ASC, `Id` ASC",
			wantParams:  map[string]any{"_c1": new(big.Rat).SetFrac64(24000125, 1000), "_c2": id},
		},
		{
			name:        "nullable ascending after a value admits the NULL region",
			sort:        []SortField{{Field: "Note", Direction: SortAscending}},
			cursor:      &cursor{Direction: pageNext, Keys: []*string{strPtr("m"), strPtr(id)}},
			wantSpanner: "WHERE ((`Note` > @_c1 OR `Note` IS NULL) OR (`Note` = @_c1 AND `Id` > @_c2)) ORDER BY `Note` IS NULL, `Note` ASC, `Id` ASC",
			wantParams:  map[string]any{"_c1": "m", "_c2": id},
		},
		{
			name:        "nullable ascending in the NULL region: only the key advances",
			sort:        []SortField{{Field: "Note", Direction: SortAscending}},
			cursor:      &cursor{Direction: pageNext, Keys: []*string{nil, strPtr(id)}},
			wantSpanner: "WHERE ((`Note` IS NULL AND `Id` > @_c1)) ORDER BY `Note` IS NULL, `Note` ASC, `Id` ASC",
			wantParams:  map[string]any{"_c1": id},
		},
		{
			name:        "nullable descending after a value",
			sort:        []SortField{{Field: "Note", Direction: SortDescending}},
			cursor:      &cursor{Direction: pageNext, Keys: []*string{strPtr("m"), strPtr(id)}},
			wantSpanner: "WHERE (`Note` < @_c1 OR (`Note` = @_c1 AND `Id` > @_c2)) ORDER BY `Note` IS NULL DESC, `Note` DESC, `Id` ASC",
			wantParams:  map[string]any{"_c1": "m", "_c2": id},
		},
		{
			name:        "nullable descending in the NULL region admits every value",
			sort:        []SortField{{Field: "Note", Direction: SortDescending}},
			cursor:      &cursor{Direction: pageNext, Keys: []*string{nil, strPtr(id)}},
			wantSpanner: "WHERE (`Note` IS NOT NULL OR (`Note` IS NULL AND `Id` > @_c1)) ORDER BY `Note` IS NULL DESC, `Note` DESC, `Id` ASC",
			wantParams:  map[string]any{"_c1": id},
		},
		{
			name:   "nullable in the middle position, in the NULL region",
			sort:   []SortField{{Field: "Hazard", Direction: SortDescending}, {Field: "Note", Direction: SortAscending}},
			cursor: &cursor{Direction: pageNext, Keys: []*string{strPtr("3"), nil, strPtr(id)}},
			wantSpanner: "WHERE (`Hazard` < @_c1 OR (`Hazard` = @_c1 AND `Note` IS NULL AND `Id` > @_c2)) " +
				"ORDER BY `Hazard` DESC, `Note` IS NULL, `Note` ASC, `Id` ASC",
			wantParams: map[string]any{"_c1": int64(3), "_c2": id},
		},
		{
			name:        "the previous page flips every comparison and the order",
			sort:        []SortField{{Field: "Hazard", Direction: SortDescending}},
			cursor:      &cursor{Direction: pagePrev, Keys: []*string{strPtr("3"), strPtr(id)}},
			wantSpanner: "WHERE (`Hazard` > @_c1 OR (`Hazard` = @_c1 AND `Id` < @_c2)) ORDER BY `Hazard` ASC, `Id` DESC",
			wantParams:  map[string]any{"_c1": int64(3), "_c2": id},
		},
		{
			name:        "the previous page of a nullable ascending column reads the NULL region first",
			sort:        []SortField{{Field: "Note", Direction: SortAscending}},
			cursor:      &cursor{Direction: pagePrev, Keys: []*string{strPtr("m"), strPtr(id)}},
			wantSpanner: "WHERE (`Note` < @_c1 OR (`Note` = @_c1 AND `Id` < @_c2)) ORDER BY `Note` IS NULL DESC, `Note` DESC, `Id` DESC",
			wantParams:  map[string]any{"_c1": "m", "_c2": id},
		},
		{
			name:    "a cursor with the wrong number of values is refused",
			sort:    []SortField{{Field: "Hazard", Direction: SortDescending}},
			cursor:  &cursor{Direction: pageNext, Keys: []*string{strPtr(id)}},
			wantErr: "invalid cursor",
		},
		{
			name:    "a NULL boundary on a column that cannot be NULL is refused",
			sort:    []SortField{{Field: "Hazard", Direction: SortDescending}},
			cursor:  &cursor{Direction: pageNext, Keys: []*string{nil, strPtr(id)}},
			wantErr: "invalid cursor",
		},
		{
			name:    "a boundary value the column cannot hold is refused",
			sort:    []SortField{{Field: "Hazard", Direction: SortDescending}},
			cursor:  &cursor{Direction: pageNext, Keys: []*string{strPtr("three"), strPtr(id)}},
			wantErr: "invalid cursor",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			for _, dbType := range []DBType{SpannerDBType, PostgresDBType} {
				qSet := NewQuerySet(NewMetadata[cursorTestResource]())
				qSet.AddField("ID")
				qSet.keyFields = []accesstypes.Field{"ID"}
				qSet.SetSortFields(tt.sort)
				qSet.cursor = tt.cursor

				stmt, err := qSet.stmt(dbType)
				if tt.wantErr != "" {
					if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
						t.Fatalf("stmt(%s) error = %v, want error containing %q", dbType, err, tt.wantErr)
					}
					if !httpio.HasBadRequest(err) {
						t.Errorf("stmt(%s) error = %v, want a 400", dbType, err)
					}

					continue
				}
				if err != nil {
					t.Fatalf("stmt(%s) error = %v", dbType, err)
				}

				want := tt.wantSpanner
				if dbType == PostgresDBType {
					want = postgresRendering(want)
				}
				got := collapseWhitespace.ReplaceAllString(stmt.SQL, " ")
				if i := strings.Index(got, "WHERE"); i >= 0 {
					got = strings.TrimSpace(got[i:])
				}
				if got != want {
					t.Errorf("stmt(%s) SQL =\n%s\nwant\n%s", dbType, got, want)
				}
				if diff := cmp.Diff(tt.wantParams, stmt.Params, cmp.Comparer(func(a, b *big.Rat) bool { return a.Cmp(b) == 0 })); diff != "" {
					t.Errorf("stmt(%s) params mismatch (-want +got):\n%s", dbType, diff)
				}
			}
		})
	}
}

var (
	spannerNullsLast  = regexp.MustCompile(`(\S+) IS NULL, (\S+) ASC`)
	spannerNullsFirst = regexp.MustCompile(`(\S+) IS NULL DESC, (\S+) DESC`)
)

// postgresRendering derives the PostgreSQL statement from the Spanner one: double
// quotes for backticks, and the stated NULL placement for Spanner's IS NULL sort
// key.
func postgresRendering(spanner string) string {
	pg := strings.ReplaceAll(spanner, "`", `"`)
	pg = spannerNullsLast.ReplaceAllString(pg, "$2 ASC NULLS LAST")
	pg = spannerNullsFirst.ReplaceAllString(pg, "$2 DESC NULLS FIRST")

	return pg
}
