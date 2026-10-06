package integration

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
)

// TestSortieExpenseMemo_formerlyNote drives the renamed field both ways a console speaks
// it: a console built before the rename still says note, and one built after it says
// memo. The column is still Note; the wire name moved to memo with @formerly(Note) on the
// field, so a row carries both keys, a request naming either in a body, in columns or in
// sort is read as the one field under its permission checks (the grants here name memo
// alone, as the role file does), a body naming both is refused, the permission digest
// of the shipped roles carries the quartermaster's entry under both names, and the generated
// TypeScript knows only memo. Memo is not a filterable field (no allow_filter), so the
// filter alias is not driven here; the resource package's own tests cover it. The reads
// use the pod sortie's tow gear line and the writes the convoy sortie's lines, so the rows
// run in parallel.
//
// Demonstrates: api.formerly.
func TestSortieExpenseMemo_formerlyNote(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}
	fields := withFields("SortieExpenses", "sortieId", "category", "amount", "memo")
	h := newTestApp(db, grants{accesstypes.List: fields, accesstypes.Read: fields, accesstypes.Create: fields, accesstypes.Update: fields})

	const towGear = "Grapple replacement" // the pod sortie's seeded tow gear line

	tests := []struct {
		name       string
		method     string
		target     string
		body       string
		wantStatus int
		// wantRow is the row id whose memo and note the body (a row, or every row of a
		// list) must both carry as wantMemo; empty skips the check.
		wantRow  string
		wantMemo string
		// wantColumn is what the Note column must hold afterwards, for a write; the
		// row is wantRow, or the created row when wantCreated is set.
		wantColumn  string
		wantCreated bool
	}{
		{name: "a row carries both keys", method: http.MethodGet, target: sectorPath(anvil, "sortie-expenses/"+expensePodTowGearID), wantStatus: http.StatusOK, wantRow: expensePodTowGearID, wantMemo: towGear},
		{name: "columns naming the former name select the field, and the rows carry both keys", method: http.MethodGet, target: sectorPath(anvil, "sortie-expenses?columns=note&filter=id:eq:"+expensePodTowGearID), wantStatus: http.StatusOK, wantRow: expensePodTowGearID, wantMemo: towGear},
		{name: "columns naming the current name do the same", method: http.MethodGet, target: sectorPath(anvil, "sortie-expenses?columns=memo&filter=id:eq:"+expensePodTowGearID), wantStatus: http.StatusOK, wantRow: expensePodTowGearID, wantMemo: towGear},
		{name: "a sort by the former name is accepted", method: http.MethodGet, target: sectorPath(anvil, "sortie-expenses?sort=note"), wantStatus: http.StatusOK},
		{name: "a sort by the current name is accepted", method: http.MethodGet, target: sectorPath(anvil, "sortie-expenses?sort=memo"), wantStatus: http.StatusOK},
		{name: "a patch naming the former name writes the column", method: http.MethodPatch, target: "/console/api/resources", body: fmt.Sprintf(`[{"op":"patch","path":%q,"value":{"note":"Reaction mass, both legs"}}]`, opPath(anvil, "sortie-expenses/"+expenseConvoyFuelID)), wantStatus: http.StatusOK, wantRow: expenseConvoyFuelID, wantColumn: "Reaction mass, both legs"},
		{name: "a patch naming the current name writes the same column", method: http.MethodPatch, target: "/console/api/resources", body: fmt.Sprintf(`[{"op":"patch","path":%q,"value":{"memo":"Field dressings"}}]`, opPath(anvil, "sortie-expenses/"+expenseConvoyMedID)), wantStatus: http.StatusOK, wantRow: expenseConvoyMedID, wantColumn: "Field dressings"},
		{name: "a body naming both is refused", method: http.MethodPatch, target: "/console/api/resources", body: fmt.Sprintf(`[{"op":"patch","path":%q,"value":{"memo":"one","note":"two"}}]`, opPath(anvil, "sortie-expenses/"+expenseConvoyFuelID)), wantStatus: http.StatusBadRequest},
		{name: "a create naming the former name writes the column", method: http.MethodPatch, target: "/console/api/resources", body: fmt.Sprintf(`[{"op":"add","path":%q,"value":{"sortieId":%q,"category":"supplies","amount":12,"note":"Booked under the old name"}}]`, opPath(anvil, "sortie-expenses"), sortieConvoyID), wantStatus: http.StatusOK, wantCreated: true, wantColumn: "Booked under the old name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status, body := doRequestAs(t, h, "purser", tt.method, tt.target, tt.body)
			assertStatus(t, status, tt.wantStatus, body)
			if status != http.StatusOK {
				return
			}

			if tt.wantMemo != "" {
				var rows []map[string]any
				if tt.method == http.MethodGet && filepath.Base(tt.target) != expensePodTowGearID {
					rows = decodeRows(t, body)
				} else {
					rows = []map[string]any{decodeRow(t, body)}
				}
				if len(rows) != 1 {
					t.Fatalf("rows = %d, want the one row %s: %s", len(rows), tt.wantRow, body)
				}
				if got := cell[string](t, rows[0], "memo"); got != tt.wantMemo {
					t.Errorf("memo = %q, want %q", got, tt.wantMemo)
				}
				if got := cell[string](t, rows[0], "note"); got != tt.wantMemo {
					t.Errorf("note = %q, want %q (the former name beside the current one)", got, tt.wantMemo)
				}
			}

			if tt.wantColumn != "" {
				row := tt.wantRow
				if tt.wantCreated {
					ids, _ := decodeRow(t, body)["sortieExpenses"].([]any)
					if len(ids) != 1 {
						t.Fatalf("created ids = %v, want one expense id: %s", ids, body)
					}
					row, _ = ids[0].(string)
				}
				got := readColumn[spanner.NullString](ctx, t, db, "SortieExpenses", spanner.Key{row}, "Note")
				if !got.Valid || got.StringVal != tt.wantColumn {
					t.Errorf("Note column = %v, want %q", got, tt.wantColumn)
				}
			}
		})
	}

	// The permission digest carries the field under both names: the shipped roles name
	// memo alone, and the digest mirrors the quartermaster's entry under note with the
	// same states, so a console built before the rename keeps its column.
	t.Run("the permission digest carries the field under both names", func(t *testing.T) {
		t.Parallel()

		_, world, _ := sharedWorld(t)
		status, body := doRequestAs(t, world, "quartermaster", http.MethodGet, consoleAPI+"/permission-digest?domain="+anvil, "")
		assertStatus(t, status, http.StatusOK, body)

		var digest accesstypes.PermissionDigest
		if err := json.Unmarshal(body, &digest); err != nil {
			t.Fatalf("unmarshaling digest %s: %v", body, err)
		}
		memo, ok := digest["SortieExpenses.memo"]
		if !ok {
			t.Fatalf("digest carries no SortieExpenses.memo: %s", body)
		}
		if note := digest["SortieExpenses.note"]; !maps.Equal(memo, note) {
			t.Errorf("digest[SortieExpenses.note] = %v, want memo's states %v mirrored under the former name", note, memo)
		}
	})

	// The generated TypeScript knows only the current name: the console's expense type
	// declares memo and no note, so a console built from it sends and reads memo.
	t.Run("the TypeScript knows only the current name", func(t *testing.T) {
		t.Parallel()

		source, err := os.ReadFile(filepath.Join("..", "..", "web", "console", "src", "app", "core", "service", "zz_gen_resources.ts"))
		if err != nil {
			t.Fatal(err)
		}
		if !regexp.MustCompile(`(?m)^\s*memo\?: string;`).Match(source) {
			t.Error("zz_gen_resources.ts declares no memo field")
		}
		if regexp.MustCompile(`(?m)^\s*note\?: string;`).Match(source) {
			t.Error("zz_gen_resources.ts still declares note, the former name")
		}
	})
}
