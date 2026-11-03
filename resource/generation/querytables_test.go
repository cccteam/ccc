package generation

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// Test_queryTables pins the walk over a query's tokens, one shape per case: the tables
// come from the positions after FROM, JOIN and the commas of a FROM list, a CTE shadows
// a table after its body, and a shape the walk cannot classify refuses with its place.
func Test_queryTables(t *testing.T) {
	t.Parallel()

	tables := tableNames("Berths", "Docks", "Visits", "Harbors", "Tides")
	tests := []struct {
		name    string
		sql     string
		want    []string
		wantErr string
	}{
		{name: "FROM and JOIN", sql: "SELECT b.Slot FROM Berths b JOIN Docks d ON d.Id = b.DockId", want: []string{"Berths", "Docks"}},
		{name: "comma joins", sql: "SELECT 1 FROM Berths b, Docks d, Visits v WHERE v.BerthDockId = b.DockId", want: []string{"Berths", "Docks", "Visits"}},
		{name: "a comma join after a JOIN ... ON", sql: "SELECT 1 FROM Berths b JOIN Docks d ON d.Id = b.DockId AND d.Open, Visits v", want: []string{"Berths", "Docks", "Visits"}},
		{name: "WHERE ends the FROM list, so a later comma introduces no table", sql: "SELECT 1 FROM Berths b WHERE b.Slot IN (1, 2) ORDER BY b.Slot, Docks", want: []string{"Berths"}},
		{name: "GROUP BY and HAVING end it", sql: "SELECT COUNT(*) FROM Berths GROUP BY Slot, Docks HAVING COUNT(*) > 1", want: []string{"Berths"}},
		{name: "LIMIT and OFFSET end it", sql: "SELECT 1 FROM Berths LIMIT 10 OFFSET 20", want: []string{"Berths"}},
		{name: "UNION ends the FROM list and the next SELECT opens its own", sql: "SELECT Id FROM Berths UNION ALL SELECT Id, Docks FROM Visits", want: []string{"Berths", "Visits"}},
		{name: "INTERSECT and EXCEPT end it too", sql: "SELECT Id FROM Berths INTERSECT DISTINCT SELECT Id FROM Docks EXCEPT DISTINCT SELECT Id FROM Visits", want: []string{"Berths", "Docks", "Visits"}},
		{name: "QUALIFY and WINDOW end it", sql: "SELECT Id FROM Berths WINDOW w AS (PARTITION BY DockId) QUALIFY ROW_NUMBER() OVER w = 1", want: []string{"Berths"}},
		{name: "a subquery after FROM", sql: "SELECT * FROM (SELECT Id FROM Berths) b JOIN Docks d ON d.Id = b.Id", want: []string{"Berths", "Docks"}},
		{name: "a subquery in the select list and one in WHERE", sql: "SELECT (SELECT COUNT(*) FROM Visits) AS n, b.Id FROM Berths b WHERE EXISTS (SELECT 1 FROM Docks d WHERE d.Id = b.DockId)", want: []string{"Berths", "Docks", "Visits"}},
		{name: "UNNEST and a table-valued function are functions, not tables", sql: "SELECT * FROM Berths b, UNNEST(b.Tags) AS tag WITH OFFSET AS n, read_docks(1, 2) r, Visits v", want: []string{"Berths", "Visits"}},
		{name: "UNNEST of an ARRAY subquery", sql: "SELECT * FROM Berths b, UNNEST(ARRAY(SELECT Id FROM Docks)) AS dockId", want: []string{"Berths", "Docks"}},
		{name: "a table hint and aliases with and without AS", sql: "SELECT * FROM Berths@{FORCE_INDEX=BerthsBySlot} AS b JOIN@{FORCE_JOIN_ORDER=TRUE} Docks d USING (DockId) LEFT JOIN @{JOIN_METHOD=HASH_JOIN} Visits ON Visits.Id = b.Id", want: []string{"Berths", "Docks", "Visits"}},
		{name: "a dotted schema name and backticks", sql: "SELECT 1 FROM Harbor.Berths b JOIN `Harbor`.`Docks` d ON TRUE, `Visits`", want: []string{"Berths", "Docks", "Visits"}},
		{name: "a column named like a table, in the select list and in WHERE, is not a table", sql: "SELECT b.Docks, Visits FROM Berths b WHERE Docks > 1 AND b.Visits = Docks", want: []string{"Berths"}},
		{name: "an alias equal to a table name is an alias", sql: "SELECT Docks.Id FROM Berths AS Docks WHERE Docks.Slot = 1", want: []string{"Berths"}},
		{name: "a CTE named like a table shadows it and reads another", sql: "WITH Docks AS (SELECT Id FROM Visits) SELECT * FROM Docks", want: []string{"Visits"}},
		{name: "a CTE named like the table it reads: the table, once", sql: "WITH Docks AS (SELECT Id FROM Docks WHERE Open) SELECT * FROM Docks d JOIN Berths b ON b.DockId = d.Id", want: []string{"Berths", "Docks"}},
		{name: "a later CTE reads an earlier one", sql: "WITH open AS (SELECT Id FROM Berths), paired AS (SELECT * FROM open o JOIN Docks d ON d.Id = o.Id) SELECT * FROM paired", want: []string{"Berths", "Docks"}},
		{name: "a WITH inside a subquery keeps its names to itself", sql: "SELECT * FROM (WITH Docks AS (SELECT Id FROM Visits) SELECT * FROM Docks) x JOIN Docks d ON d.Id = x.Id", want: []string{"Docks", "Visits"}},
		{name: "a WITH name is out of scope outside its subquery", sql: "SELECT * FROM (WITH t AS (SELECT Id FROM Visits) SELECT * FROM t) x JOIN t ON t.Id = x.Id", wantErr: "names t at line 1, column 74 in a table position"},
		{name: "string literals and comments naming tables are not tables", sql: "SELECT 'Docks' AS Label, /* Visits */ b.Id -- Docks\nFROM Berths b WHERE b.Name = \"Visits\"", want: []string{"Berths"}},
		{name: "a parameter is not a table", sql: "SELECT 1 FROM Berths WHERE Slot > @docks", want: []string{"Berths"}},
		{name: "EXTRACT's FROM introduces no table", sql: "SELECT EXTRACT(YEAR FROM b.ArrivedAt) FROM Berths b", want: []string{"Berths"}},
		{name: "a correlated array reference leaves from an alias", sql: "SELECT * FROM Berths b, b.Cargo c, Docks d", want: []string{"Berths", "Docks"}},
		{name: "a parenthesized join", sql: "SELECT * FROM (Berths b JOIN Docks d ON d.Id = b.DockId) JOIN Visits v ON v.BerthDockId = b.DockId", want: []string{"Berths", "Docks", "Visits"}},
		{name: "a column spelled like a clause keyword after a dot ends nothing", sql: "SELECT * FROM Berths b JOIN Docks d ON d.offset = b.offset, Visits v", want: []string{"Berths", "Docks", "Visits"}},
		{name: "an array subscript's OFFSET ends nothing", sql: "SELECT * FROM Berths b JOIN Docks d ON d.Ids[OFFSET(0)] = b.Id, Visits v", want: []string{"Berths", "Docks", "Visits"}},
		{name: "TABLESAMPLE and FOR SYSTEM_TIME follow the table", sql: "SELECT * FROM Berths TABLESAMPLE RESERVOIR (10 ROWS) JOIN Docks FOR SYSTEM_TIME AS OF @t ON TRUE", want: []string{"Berths", "Docks"}},
		{name: "set operations in parentheses", sql: "(SELECT Id FROM Berths) UNION DISTINCT (SELECT Id FROM Docks)", want: []string{"Berths", "Docks"}},
		{name: "every join form, with USING and ON", sql: "SELECT 1 FROM Berths b INNER JOIN Docks d USING (DockId) RIGHT OUTER JOIN Visits v ON v.Id = b.Id FULL JOIN Harbors h ON h.Id = d.HarborId CROSS JOIN Tides t", want: []string{"Berths", "Docks", "Harbors", "Tides", "Visits"}},
		{name: "columns spelled like keywords in backticks", sql: "SELECT b.`Key`, b.`Type`, b.`Date`, b.`Role`, b.`Group`, b.`Window`, b.`Order` FROM Berths b WHERE b.`Group` = 1 AND b.`Order` > 2", want: []string{"Berths"}},
		{name: "window functions, in the select list and under QUALIFY", sql: "SELECT b.Id, ROW_NUMBER() OVER (PARTITION BY b.DockId ORDER BY b.Slot) AS rn, SUM(b.Slot) OVER w FROM Berths b JOIN Docks d ON d.Id = b.DockId WINDOW w AS (PARTITION BY b.DockId) QUALIFY ROW_NUMBER() OVER (PARTITION BY d.Id ORDER BY b.Slot) = 1", want: []string{"Berths", "Docks"}},
		{name: "STRUCT and ARRAY literals, a typed array, an ARRAY subquery", sql: "SELECT STRUCT(b.Id AS id, b.Slot AS slot) AS berth, [1, 2, 3] AS numbers, ARRAY<STRING>['Docks', 'Visits'] AS names, ARRAY(SELECT Id FROM Docks) AS dockIds FROM Berths b", want: []string{"Berths", "Docks"}},
		{name: "typed literals naming tables are literals", sql: "SELECT DATE '2024-01-01', TIMESTAMP '2024-01-01T00:00:00Z', JSON '{\"docks\": 1}', NUMERIC '1.5', b'Visits', r'Docks' FROM Berths", want: []string{"Berths"}},
		{name: "SAFE_CAST and a function named like a table in the select list", sql: "SELECT SAFE_CAST(b.Slot AS STRING), Docks(b.Id), SAFE.Visits(b.Id) FROM Berths b", want: []string{"Berths"}},
		{name: "a table-valued function named like a table after a comma is a function", sql: "SELECT * FROM Berths b, Docks(b.Id) d", want: []string{"Berths"}},
		{name: "full-text search functions", sql: "SELECT b.Id FROM Berths b WHERE SEARCH(b.Name_Tokens, @q) AND SCORE(b.Name_Tokens, @q) > 0.5 ORDER BY SCORE(b.Name_Tokens, @q) DESC", want: []string{"Berths"}},
		{name: "UNNEST of a parameter in WHERE", sql: "SELECT b.Id FROM Berths b WHERE b.DockId IN UNNEST(@dockIds)", want: []string{"Berths"}},
		{name: "DISTINCT and AS STRUCT", sql: "SELECT DISTINCT b.DockId FROM Berths b UNION ALL SELECT AS STRUCT d.Id FROM Docks d", want: []string{"Berths", "Docks"}},
		{name: "case is not significant", sql: "select * from berths", want: []string{"Berths"}},
		{name: "nothing named", sql: "SELECT 1", want: nil},
		{name: "a table position naming neither a table nor a CTE is refused", sql: "SELECT * FROM Berths b JOIN Quays q ON q.Id = b.Id", wantErr: "names Quays at line 1, column 29 in a table position (after FROM, JOIN or a comma of a FROM list), and that is neither a table in the schema nor a CTE in scope"},
		{name: "the refusal's position is the token's own, on its line", sql: "SELECT *\nFROM Berths b\n  JOIN sch.Quays q ON q.Id = b.Id", wantErr: "names sch.Quays at line 3, column 8"},
		{name: "a recursive WITH is refused", sql: "WITH RECURSIVE t AS (SELECT 1 AS n UNION ALL SELECT n + 1 FROM t) SELECT * FROM Berths", wantErr: "opens WITH RECURSIVE, which the derivation does not follow"},
		{name: "a text the lexer refuses is refused with its position", sql: "SELECT * FROM Berths WHERE Name = 'open", wantErr: "cannot be read: the lexer stopped at line 1, column 35 (unclosed string literal)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := queryTables(tt.sql, tables)
			if (err != nil) != (tt.wantErr != "") {
				t.Fatalf("queryTables() error = %v, wantErr %v", err, tt.wantErr != "")
			}
			if err != nil {
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("queryTables() error = %v, want it to contain %q", err, tt.wantErr)
				}

				return
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("queryTables() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
