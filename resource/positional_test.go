package resource

// These tests pin positional masking (decided 2026-09-11): a field declared
// masking:"positional" keeps its select CASE and mask term, so the cell stays
// hidden, but ORDER BY, the cursor predicate, and the filter run on the raw
// column; a paged statement selects a positional sort key's raw value a second
// time under the reserved alias, and the cursor carries that value where a
// concealing key would carry NULL.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/google/go-cmp/cmp"
)

// positionalRequest is projectionRequest with the fee declared positional; the
// note stays concealing.
type positionalRequest struct {
	ID      string  `json:"id"   perm:"-"`
	Name    string  `json:"name" index:"true"`
	Fee     int64   `json:"fee"  index:"true"        masking:"positional"`
	Note    *string `json:"note" allow_filter:"true"`
	Station string  `json:"-"`
}

func TestQuerySet_stmt_positionalMasking(t *testing.T) {
	t.Parallel()

	const id = "0193e2a7-522c-708f-bfd0-4adf33486bb1"
	owner := "owner = subject"
	feeOwner := conditionalOn(projectedResource+".fee", owner)
	noteOwner := conditionalOn(projectedResource+".note", "owner = subject OR priority = 3")

	tests := []struct {
		name         string
		target       string
		decisions    accesstypes.Decisions
		cursor       *cursor
		wantSpanner  string
		wantPostgres string
		wantParams   map[string]any
		wantKeys     int
	}{
		{
			name:      "a sort on a positional field orders the raw column, keeps the CASE, and selects the raw value for the cursor",
			target:    "/?sort=fee",
			decisions: accesstypes.Decisions{projectedResource + ".fee": feeOwner},
			wantSpanner: "SELECT Id, Name, CASE WHEN `projectionResources`.`Owner` = @subject THEN Fee ELSE @_c1 END AS Fee, Note, " +
				"IF(`projectionResources`.`Owner` = @subject, ARRAY<STRING>[], ['fee']) AS zzMaskedFields, Fee AS zzPositionalFee " +
				"FROM projectionResources WHERE (`projectionResources`.`Station` = @domain) " +
				"ORDER BY `Fee` ASC, `Id` ASC LIMIT 51",
			wantPostgres: `SELECT "Id", "Name", CASE WHEN "projectionResources"."Owner" = @subject THEN "Fee" ELSE @_c1 END AS "Fee", "Note", ` +
				`ARRAY_REMOVE(ARRAY[CASE WHEN "projectionResources"."Owner" = @subject THEN NULL ELSE 'fee' END], NULL) AS "zzMaskedFields", "Fee" AS "zzPositionalFee" ` +
				`FROM projectionResources WHERE ("projectionResources"."Station" = @domain) ` +
				`ORDER BY "Fee" ASC, "Id" ASC LIMIT 51`,
			wantParams: map[string]any{"subject": "u1", "domain": "testDomain", "_c1": int64(0)},
			wantKeys:   1,
		},
		{
			name:      "a positional sort key outside the projection has no CASE and needs no raw select",
			target:    "/?sort=fee:desc&columns=id,name",
			decisions: accesstypes.Decisions{projectedResource + ".fee": feeOwner},
			wantSpanner: "SELECT Id, Name FROM projectionResources WHERE (`projectionResources`.`Station` = @domain) " +
				"ORDER BY `Fee` DESC, `Id` ASC LIMIT 51",
			wantPostgres: `SELECT "Id", "Name" FROM projectionResources WHERE ("projectionResources"."Station" = @domain) ` +
				`ORDER BY "Fee" DESC, "Id" ASC LIMIT 51`,
			// Nothing lowers: the positional key renders no override, and no CASE
			// is projected, so the condition's parameter never binds.
			wantParams: map[string]any{"domain": "testDomain"},
		},
		{
			name:      "a filter on a positional field compares the raw column, so a masked row can match",
			target:    "/?filter=fee:gt:5&columns=id",
			decisions: accesstypes.Decisions{projectedResource + ".fee": feeOwner, projectedResource + ".note": noteOwner},
			wantSpanner: "SELECT Id FROM projectionResources " +
				"WHERE `Fee` > @_p1 AND (`projectionResources`.`Station` = @domain) " +
				"ORDER BY `Id` ASC LIMIT 51",
			wantPostgres: `SELECT "Id" FROM projectionResources ` +
				`WHERE "Fee" > @_p1 AND ("projectionResources"."Station" = @domain) ` +
				`ORDER BY "Id" ASC LIMIT 51`,
			wantParams: map[string]any{"domain": "testDomain", "_p1": 5},
		},
		{
			name:      "the cursor predicate compares the raw column, with no NULL region for a non-null positional key",
			target:    "/?sort=fee&columns=id,name,fee",
			decisions: accesstypes.Decisions{projectedResource + ".fee": feeOwner},
			cursor:    &cursor{Direction: pageNext, Keys: []*string{strPtr("7"), strPtr(id)}},
			wantSpanner: "SELECT Id, Name, CASE WHEN `projectionResources`.`Owner` = @subject THEN Fee ELSE @_c1 END AS Fee, " +
				"IF(`projectionResources`.`Owner` = @subject, ARRAY<STRING>[], ['fee']) AS zzMaskedFields, Fee AS zzPositionalFee " +
				"FROM projectionResources WHERE (`projectionResources`.`Station` = @domain) AND (`Fee` > @_c2 OR (`Fee` = @_c2 AND `Id` > @_c3)) " +
				"ORDER BY `Fee` ASC, `Id` ASC LIMIT 51",
			wantPostgres: `SELECT "Id", "Name", CASE WHEN "projectionResources"."Owner" = @subject THEN "Fee" ELSE @_c1 END AS "Fee", ` +
				`ARRAY_REMOVE(ARRAY[CASE WHEN "projectionResources"."Owner" = @subject THEN NULL ELSE 'fee' END], NULL) AS "zzMaskedFields", "Fee" AS "zzPositionalFee" ` +
				`FROM projectionResources WHERE ("projectionResources"."Station" = @domain) AND ("Fee" > @_c2 OR ("Fee" = @_c2 AND "Id" > @_c3)) ` +
				`ORDER BY "Fee" ASC, "Id" ASC LIMIT 51`,
			wantParams: map[string]any{"subject": "u1", "domain": "testDomain", "_c1": int64(0), "_c2": int64(7), "_c3": id},
			wantKeys:   1,
		},
		{
			name:      "a concealing sibling keeps its CASE beside a positional key",
			target:    "/?sort=fee,note&columns=id",
			decisions: accesstypes.Decisions{projectedResource + ".fee": feeOwner, projectedResource + ".note": noteOwner},
			wantSpanner: "SELECT Id FROM projectionResources WHERE (`projectionResources`.`Station` = @domain) " +
				"ORDER BY `Fee` ASC, CASE WHEN (`projectionResources`.`Owner` = @subject OR `projectionResources`.`Priority` = @_c1) THEN `Note` END ASC, `Id` ASC LIMIT 51",
			wantPostgres: `SELECT "Id" FROM projectionResources WHERE ("projectionResources"."Station" = @domain) ` +
				`ORDER BY "Fee" ASC, CASE WHEN ("projectionResources"."Owner" = @subject OR "projectionResources"."Priority" = @_c1) THEN "Note" END ASC, "Id" ASC LIMIT 51`,
			wantParams: map[string]any{"subject": "u1", "domain": "testDomain", "_c1": int64(3)},
		},
		{
			name:      "a pruned positional key needs no raw select: its cell is shown on every row",
			target:    "/?sort=fee&columns=id,fee",
			decisions: accesstypes.Decisions{projectedResource + ".fee": feeOwner, projectedResource + ".name": conditionalOn(projectedResource+".name", owner)},
			wantSpanner: "SELECT Id, Fee FROM projectionResources WHERE (`projectionResources`.`Station` = @domain) AND (`projectionResources`.`Owner` = @subject) " +
				"ORDER BY `Fee` ASC, `Id` ASC LIMIT 51",
			wantPostgres: `SELECT "Id", "Fee" FROM projectionResources WHERE ("projectionResources"."Station" = @domain) AND ("projectionResources"."Owner" = @subject) ` +
				`ORDER BY "Fee" ASC, "Id" ASC LIMIT 51`,
			wantParams: map[string]any{"subject": "u1", "domain": "testDomain"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			for _, dbType := range []DBType{SpannerDBType, PostgresDBType} {
				resSet, err := NewSet[projectionResource, positionalRequest](accesstypes.List)
				if err != nil {
					t.Fatalf("NewSet() error = %v", err)
				}
				decoder, err := NewQueryDecoder[projectionResource, positionalRequest](resSet)
				if err != nil {
					t.Fatalf("NewQueryDecoder() error = %v", err)
				}
				decoder.collection = projectionCollection(t)

				req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tt.target, http.NoBody)
				qSet, err := decoder.Decode(req, renderStubPermissions{decisions: tt.decisions}, testScope)
				if err != nil {
					t.Fatalf("QueryDecoder.Decode() error = %v", err)
				}
				qSet.cursor = tt.cursor

				if err := qSet.checkPermissions(t.Context(), dbType); err != nil {
					t.Fatalf("checkPermissions(%s) error = %v", dbType, err)
				}

				stmt, err := qSet.stmt(dbType)
				if err != nil {
					t.Fatalf("stmt(%s) error = %v", dbType, err)
				}

				want := tt.wantSpanner
				if dbType == PostgresDBType {
					want = tt.wantPostgres
				}
				if got := normalizeSQL(stmt.SQL); got != want {
					t.Errorf("stmt(%s) SQL =\n%s\nwant\n%s", dbType, got, want)
				}
				if diff := cmp.Diff(tt.wantParams, stmt.Params); diff != "" {
					t.Errorf("stmt(%s) params mismatch (-want +got):\n%s", dbType, diff)
				}
				if len(stmt.positionalKeys) != tt.wantKeys {
					t.Errorf("stmt(%s) positional keys = %d, want %d", dbType, len(stmt.positionalKeys), tt.wantKeys)
				}
				for _, key := range stmt.positionalKeys {
					if key.field != "Fee" || key.alias != positionalKeyColumnPrefix+"Fee" || key.fieldType.Kind().String() != "int64" {
						t.Errorf("stmt(%s) positional key = %+v, want Fee under %sFee as int64", dbType, key, positionalKeyColumnPrefix)
					}
				}
			}
		})
	}
}

// TestQuerySet_stmt_positionalUnpaged pins that a statement that does not page
// selects no raw value: a hand-built or armed query issues no cursor.
func TestQuerySet_stmt_positionalUnpaged(t *testing.T) {
	t.Parallel()

	resSet, err := NewSet[projectionResource, positionalRequest](accesstypes.List)
	if err != nil {
		t.Fatalf("NewSet() error = %v", err)
	}
	qSet := NewQuerySet(NewMetadata[projectionResource]())
	qSet.SetSortFields([]SortField{{Field: "Fee", Direction: SortAscending}})
	qSet.ReturnAccessibleFields(true)
	qSet.EnableUserPermissionEnforcement(resSet, renderStubPermissions{decisions: accesstypes.Decisions{projectedResource + ".fee": conditionalOn(projectedResource+".fee", "owner = subject")}}, testScope, accesstypes.List)
	qSet.collection = projectionCollection(t)
	if err := qSet.checkPermissions(t.Context(), SpannerDBType); err != nil {
		t.Fatalf("checkPermissions() error = %v", err)
	}
	stmt, err := qSet.stmt(SpannerDBType)
	if err != nil {
		t.Fatalf("stmt() error = %v", err)
	}
	if strings.Contains(stmt.SQL, positionalKeyColumnPrefix) || len(stmt.positionalKeys) != 0 {
		t.Errorf("unpaged statement selects a positional key:\n%s", stmt.SQL)
	}
	if !strings.Contains(stmt.SQL, "ORDER BY `Fee` ASC") {
		t.Errorf("unpaged statement does not order on the raw column:\n%s", stmt.SQL)
	}
}

// TestScanEnvelopeRow_positionalKeys pins the scan contract for the reserved
// positional alias: the raw value lands in the envelope, typed as the field, and
// the row data keeps the masked filler.
func TestScanEnvelopeRow_positionalKeys(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		columns  []string
		values   []any
		wantRaw  int64
		wantFill int64
	}{
		{
			name:     "a masked cell carries the filler while the alias carries the value",
			columns:  []string{"Id", "Name", "Fee", maskedNamesColumnName, positionalKeyColumnPrefix + "Fee"},
			values:   []any{"a", "n", int64(0), []string{"fee"}, int64(42)},
			wantRaw:  42,
			wantFill: 0,
		},
		{
			name:     "an unmasked cell and its alias agree",
			columns:  []string{"Id", "Name", "Fee", maskedNamesColumnName, positionalKeyColumnPrefix + "Fee"},
			values:   []any{"a", "n", int64(42), []string{}, int64(42)},
			wantRaw:  42,
			wantFill: 42,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			spannerRow, err := spanner.NewRow(tt.columns, tt.values)
			if err != nil {
				t.Fatalf("spanner.NewRow() error = %v", err)
			}
			stmt := &Statement{
				maskedNamesColumn: maskedNamesColumnName,
				positionalKeys:    []positionalKey{{field: "Fee", alias: positionalKeyColumnPrefix + "Fee", fieldType: NewMetadata[projectionResource]().dbFieldMap(SpannerDBType)["Fee"].fieldType}},
			}
			if !envelopeScan(stmt) {
				t.Fatal("envelopeScan() = false for a statement carrying positional keys")
			}
			row, err := scanEnvelopeRow[projectionResource](spannerRow, stmt)
			if err != nil {
				t.Fatalf("scanEnvelopeRow() error = %v", err)
			}
			if row.Data.Fee != tt.wantFill {
				t.Errorf("Data.Fee = %d, want %d", row.Data.Fee, tt.wantFill)
			}
			raw, ok := row.positional["Fee"]
			if !ok || raw.Int() != tt.wantRaw {
				t.Errorf("positional[Fee] = %v (present %t), want %d", raw, ok, tt.wantRaw)
			}
		})
	}
}
