package resource

// These tests pin the cursor's copy of a sort key (decided 2026-09-15, on the
// positional mechanism of 2026-09-11): a paged statement selects a second time,
// under the reserved alias, every sort key whose value the row data will not
// carry — a key outside the columns projection, the primary key included, and
// a positional key whose cell arrives as the masked filler — and the cursor
// reads its boundary key from that copy. A concealing key's copy is its
// visible-projection CASE, NULL where the cell is masked; every other copy is
// the raw column. The projection itself is never widened.

import (
	"math/big"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/civil"
	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/google/go-cmp/cmp"
	"github.com/shopspring/decimal"
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

// wantCursorColumn is the shape a rendered statement's cursor column is
// checked against: the field it copies and whether the copy is a CASE.
type wantCursorColumn struct {
	field    accesstypes.Field
	nullable bool
}

// checkCursorColumns compares a statement's cursor columns with the expected
// list, in order: field, alias under the reserved prefix, the field's Go type,
// and the nullable flag.
func checkCursorColumns(t *testing.T, dbType DBType, stmt *Statement, want []wantCursorColumn) {
	t.Helper()

	if len(stmt.cursorColumns) != len(want) {
		t.Fatalf("stmt(%s) cursor columns = %+v, want %d: %+v", dbType, stmt.cursorColumns, len(want), want)
	}
	dbFields := NewMetadata[projectionResource]().dbFieldMap(dbType)
	for i, column := range stmt.cursorColumns {
		dbField := dbFields[want[i].field]
		if column.field != want[i].field || column.alias != cursorColumnPrefix+dbField.ColumnName || column.fieldType != dbField.fieldType || column.nullable != want[i].nullable {
			t.Errorf("stmt(%s) cursor column %d = %+v, want %s under %s%s as %s, nullable %t", dbType, i, column, want[i].field, cursorColumnPrefix, dbField.ColumnName, dbField.fieldType, want[i].nullable)
		}
	}
}

// renderPaged decodes a list request against the given request type with the
// stubbed decisions and renders its statement for the database.
func renderPaged[Request any](t *testing.T, dbType DBType, target string, decisions accesstypes.Decisions, cur *cursor) *Statement {
	t.Helper()

	resSet, err := NewSet[projectionResource, Request](accesstypes.List)
	if err != nil {
		t.Fatalf("NewSet() error = %v", err)
	}
	decoder, err := NewQueryDecoder[projectionResource, Request](resSet)
	if err != nil {
		t.Fatalf("NewQueryDecoder() error = %v", err)
	}
	decoder.collection = projectionCollection(t)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, http.NoBody)
	qSet, err := decoder.Decode(req, renderStubPermissions{decisions: decisions}, testScope)
	if err != nil {
		t.Fatalf("QueryDecoder.Decode() error = %v", err)
	}
	qSet.cursor = cur

	if err := qSet.checkPermissions(t.Context(), dbType); err != nil {
		t.Fatalf("checkPermissions(%s) error = %v", dbType, err)
	}

	stmt, err := qSet.stmt(dbType)
	if err != nil {
		t.Fatalf("stmt(%s) error = %v", dbType, err)
	}

	return stmt
}

// TestQuerySet_stmt_cursorColumns pins the selection rule on the concealing
// fixture: a copy for each sort key outside the projection, the primary key
// included, as the raw column on a plain resource and as the visible
// projection under an unpruned condition; nothing extra when every sort key
// is selected.
func TestQuerySet_stmt_cursorColumns(t *testing.T) {
	t.Parallel()

	feeOwner := conditionalOn(projectedResource+".fee", "owner = subject")

	tests := []struct {
		name         string
		target       string
		decisions    accesstypes.Decisions
		wantSpanner  string
		wantPostgres string
		wantParams   map[string]any
		wantColumns  []wantCursorColumn
	}{
		{
			name:   "a plain resource copies each unselected sort key raw, in order",
			target: "/?sort=fee,note&columns=id",
			wantSpanner: "SELECT Id, Fee AS zzCursorFee, Note AS zzCursorNote FROM projectionResources WHERE (`projectionResources`.`Station` = @domain) " +
				"ORDER BY `Fee` ASC, `Note` ASC, `Id` ASC LIMIT 51",
			wantPostgres: `SELECT "Id", "Fee" AS "zzCursorFee", "Note" AS "zzCursorNote" FROM projectionResources WHERE ("projectionResources"."Station" = @domain) ` +
				`ORDER BY "Fee" ASC, "Note" ASC, "Id" ASC LIMIT 51`,
			wantParams:  map[string]any{"domain": "testDomain"},
			wantColumns: []wantCursorColumn{{field: "Fee"}, {field: "Note"}},
		},
		{
			name:      "a concealing key under a condition copies its visible projection, NULL where masked",
			target:    "/?sort=fee&columns=id,name",
			decisions: accesstypes.Decisions{projectedResource + ".fee": feeOwner},
			wantSpanner: "SELECT Id, Name, CASE WHEN `projectionResources`.`Owner` = @subject THEN `Fee` END AS zzCursorFee " +
				"FROM projectionResources WHERE (`projectionResources`.`Station` = @domain) " +
				"ORDER BY CASE WHEN `projectionResources`.`Owner` = @subject THEN `Fee` END ASC, `Id` ASC LIMIT 51",
			wantPostgres: `SELECT "Id", "Name", CASE WHEN "projectionResources"."Owner" = @subject THEN "Fee" END AS "zzCursorFee" ` +
				`FROM projectionResources WHERE ("projectionResources"."Station" = @domain) ` +
				`ORDER BY CASE WHEN "projectionResources"."Owner" = @subject THEN "Fee" END ASC, "Id" ASC LIMIT 51`,
			wantParams:  map[string]any{"subject": "u1", "domain": "testDomain"},
			wantColumns: []wantCursorColumn{{field: "Fee", nullable: true}},
		},
		{
			name:   "the primary key omitted from the projection is copied too",
			target: "/?sort=name&columns=name",
			wantSpanner: "SELECT Name, Id AS zzCursorId FROM projectionResources WHERE (`projectionResources`.`Station` = @domain) " +
				"ORDER BY `Name` ASC, `Id` ASC LIMIT 51",
			wantPostgres: `SELECT "Name", "Id" AS "zzCursorId" FROM projectionResources WHERE ("projectionResources"."Station" = @domain) ` +
				`ORDER BY "Name" ASC, "Id" ASC LIMIT 51`,
			wantParams:  map[string]any{"domain": "testDomain"},
			wantColumns: []wantCursorColumn{{field: "ID"}},
		},
		{
			name:   "a sort key named twice is copied once",
			target: "/?sort=fee,fee:desc&columns=id",
			wantSpanner: "SELECT Id, Fee AS zzCursorFee FROM projectionResources WHERE (`projectionResources`.`Station` = @domain) " +
				"ORDER BY `Fee` ASC, `Fee` DESC, `Id` ASC LIMIT 51",
			wantPostgres: `SELECT "Id", "Fee" AS "zzCursorFee" FROM projectionResources WHERE ("projectionResources"."Station" = @domain) ` +
				`ORDER BY "Fee" ASC, "Fee" DESC, "Id" ASC LIMIT 51`,
			wantParams:  map[string]any{"domain": "testDomain"},
			wantColumns: []wantCursorColumn{{field: "Fee"}},
		},
		{
			name:   "a statement selecting every sort key is unchanged",
			target: "/?sort=fee&columns=id,fee",
			wantSpanner: "SELECT Id, Fee FROM projectionResources WHERE (`projectionResources`.`Station` = @domain) " +
				"ORDER BY `Fee` ASC, `Id` ASC LIMIT 51",
			wantPostgres: `SELECT "Id", "Fee" FROM projectionResources WHERE ("projectionResources"."Station" = @domain) ` +
				`ORDER BY "Fee" ASC, "Id" ASC LIMIT 51`,
			wantParams: map[string]any{"domain": "testDomain"},
		},
		{
			name:      "a concealing key the projection shows under its CASE needs no copy: masked reads as NULL, unmasked as the cell",
			target:    "/?sort=fee&columns=id,name,fee",
			decisions: accesstypes.Decisions{projectedResource + ".fee": feeOwner},
			wantSpanner: "SELECT Id, Name, CASE WHEN `projectionResources`.`Owner` = @subject THEN Fee ELSE @_c1 END AS Fee, " +
				"IF(`projectionResources`.`Owner` = @subject, ARRAY<STRING>[], ['fee']) AS zzMaskedFields " +
				"FROM projectionResources WHERE (`projectionResources`.`Station` = @domain) " +
				"ORDER BY CASE WHEN `projectionResources`.`Owner` = @subject THEN `Fee` END ASC, `Id` ASC LIMIT 51",
			wantPostgres: `SELECT "Id", "Name", CASE WHEN "projectionResources"."Owner" = @subject THEN "Fee" ELSE @_c1 END AS "Fee", ` +
				`ARRAY_REMOVE(ARRAY[CASE WHEN "projectionResources"."Owner" = @subject THEN NULL ELSE 'fee' END], NULL) AS "zzMaskedFields" ` +
				`FROM projectionResources WHERE ("projectionResources"."Station" = @domain) ` +
				`ORDER BY CASE WHEN "projectionResources"."Owner" = @subject THEN "Fee" END ASC, "Id" ASC LIMIT 51`,
			wantParams: map[string]any{"subject": "u1", "domain": "testDomain", "_c1": int64(0)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			for _, dbType := range []DBType{SpannerDBType, PostgresDBType} {
				stmt := renderPaged[projectionRequest](t, dbType, tt.target, tt.decisions, nil)

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
				checkCursorColumns(t, dbType, stmt, tt.wantColumns)
			}
		})
	}
}

// TestQuerySet_stmt_positionalMasking pins the positional shape: a field
// declared masking:"positional" keeps its select CASE and mask term, so the
// cell stays hidden, but ORDER BY, the cursor predicate, and the filter run on
// the raw column, and its cursor copy is always the raw column — beside the
// CASE when the key is projected, alone when it is not.
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
		wantColumns  []wantCursorColumn
	}{
		{
			name:      "a sort on a positional field orders the raw column, keeps the CASE, and copies the raw value for the cursor",
			target:    "/?sort=fee",
			decisions: accesstypes.Decisions{projectedResource + ".fee": feeOwner},
			wantSpanner: "SELECT Id, Name, CASE WHEN `projectionResources`.`Owner` = @subject THEN Fee ELSE @_c1 END AS Fee, Note, " +
				"IF(`projectionResources`.`Owner` = @subject, ARRAY<STRING>[], ['fee']) AS zzMaskedFields, Fee AS zzCursorFee " +
				"FROM projectionResources WHERE (`projectionResources`.`Station` = @domain) " +
				"ORDER BY `Fee` ASC, `Id` ASC LIMIT 51",
			wantPostgres: `SELECT "Id", "Name", CASE WHEN "projectionResources"."Owner" = @subject THEN "Fee" ELSE @_c1 END AS "Fee", "Note", ` +
				`ARRAY_REMOVE(ARRAY[CASE WHEN "projectionResources"."Owner" = @subject THEN NULL ELSE 'fee' END], NULL) AS "zzMaskedFields", "Fee" AS "zzCursorFee" ` +
				`FROM projectionResources WHERE ("projectionResources"."Station" = @domain) ` +
				`ORDER BY "Fee" ASC, "Id" ASC LIMIT 51`,
			wantParams:  map[string]any{"subject": "u1", "domain": "testDomain", "_c1": int64(0)},
			wantColumns: []wantCursorColumn{{field: "Fee"}},
		},
		{
			name:      "a positional sort key outside the projection has no CASE and copies the raw column",
			target:    "/?sort=fee:desc&columns=id,name",
			decisions: accesstypes.Decisions{projectedResource + ".fee": feeOwner},
			wantSpanner: "SELECT Id, Name, Fee AS zzCursorFee FROM projectionResources WHERE (`projectionResources`.`Station` = @domain) " +
				"ORDER BY `Fee` DESC, `Id` ASC LIMIT 51",
			wantPostgres: `SELECT "Id", "Name", "Fee" AS "zzCursorFee" FROM projectionResources WHERE ("projectionResources"."Station" = @domain) ` +
				`ORDER BY "Fee" DESC, "Id" ASC LIMIT 51`,
			// Nothing lowers: the positional key renders no override, and no CASE
			// is projected, so the condition's parameter never binds.
			wantParams:  map[string]any{"domain": "testDomain"},
			wantColumns: []wantCursorColumn{{field: "Fee"}},
		},
		{
			name:      "a filter on a positional field compares the raw column, so a masked row can match",
			target:    "/?filter=fee:gt:5&columns=id&limit=all",
			decisions: accesstypes.Decisions{projectedResource + ".fee": feeOwner, projectedResource + ".note": noteOwner},
			// No sort and no declared order, so the whole list: the statement carries
			// no ORDER BY and no LIMIT.
			wantSpanner: "SELECT Id FROM projectionResources " +
				"WHERE `Fee` > @_p1 AND (`projectionResources`.`Station` = @domain)",
			wantPostgres: `SELECT "Id" FROM projectionResources ` +
				`WHERE "Fee" > @_p1 AND ("projectionResources"."Station" = @domain)`,
			wantParams: map[string]any{"domain": "testDomain", "_p1": 5},
		},
		{
			name:      "the cursor predicate compares the raw column, with no NULL region for a non-null positional key",
			target:    "/?sort=fee&columns=id,name,fee",
			decisions: accesstypes.Decisions{projectedResource + ".fee": feeOwner},
			cursor:    &cursor{Direction: pageNext, Keys: []*string{strPtr("7"), strPtr(id)}},
			wantSpanner: "SELECT Id, Name, CASE WHEN `projectionResources`.`Owner` = @subject THEN Fee ELSE @_c1 END AS Fee, " +
				"IF(`projectionResources`.`Owner` = @subject, ARRAY<STRING>[], ['fee']) AS zzMaskedFields, Fee AS zzCursorFee " +
				"FROM projectionResources WHERE (`projectionResources`.`Station` = @domain) AND (`Fee` > @_c2 OR (`Fee` = @_c2 AND `Id` > @_c3)) " +
				"ORDER BY `Fee` ASC, `Id` ASC LIMIT 51",
			wantPostgres: `SELECT "Id", "Name", CASE WHEN "projectionResources"."Owner" = @subject THEN "Fee" ELSE @_c1 END AS "Fee", ` +
				`ARRAY_REMOVE(ARRAY[CASE WHEN "projectionResources"."Owner" = @subject THEN NULL ELSE 'fee' END], NULL) AS "zzMaskedFields", "Fee" AS "zzCursorFee" ` +
				`FROM projectionResources WHERE ("projectionResources"."Station" = @domain) AND ("Fee" > @_c2 OR ("Fee" = @_c2 AND "Id" > @_c3)) ` +
				`ORDER BY "Fee" ASC, "Id" ASC LIMIT 51`,
			wantParams:  map[string]any{"subject": "u1", "domain": "testDomain", "_c1": int64(0), "_c2": int64(7), "_c3": id},
			wantColumns: []wantCursorColumn{{field: "Fee"}},
		},
		{
			name:      "a concealing sibling outside the projection copies its CASE beside the positional key's raw column",
			target:    "/?sort=fee,note&columns=id",
			decisions: accesstypes.Decisions{projectedResource + ".fee": feeOwner, projectedResource + ".note": noteOwner},
			wantSpanner: "SELECT Id, Fee AS zzCursorFee, CASE WHEN (`projectionResources`.`Owner` = @subject OR `projectionResources`.`Priority` = @_c1) THEN `Note` END AS zzCursorNote " +
				"FROM projectionResources WHERE (`projectionResources`.`Station` = @domain) " +
				"ORDER BY `Fee` ASC, CASE WHEN (`projectionResources`.`Owner` = @subject OR `projectionResources`.`Priority` = @_c1) THEN `Note` END ASC, `Id` ASC LIMIT 51",
			wantPostgres: `SELECT "Id", "Fee" AS "zzCursorFee", CASE WHEN ("projectionResources"."Owner" = @subject OR "projectionResources"."Priority" = @_c1) THEN "Note" END AS "zzCursorNote" ` +
				`FROM projectionResources WHERE ("projectionResources"."Station" = @domain) ` +
				`ORDER BY "Fee" ASC, CASE WHEN ("projectionResources"."Owner" = @subject OR "projectionResources"."Priority" = @_c1) THEN "Note" END ASC, "Id" ASC LIMIT 51`,
			wantParams:  map[string]any{"subject": "u1", "domain": "testDomain", "_c1": int64(3)},
			wantColumns: []wantCursorColumn{{field: "Fee"}, {field: "Note", nullable: true}},
		},
		{
			name:      "a pruned positional key the projection shows needs no copy: its cell is shown on every row",
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
				stmt := renderPaged[positionalRequest](t, dbType, tt.target, tt.decisions, tt.cursor)

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
				checkCursorColumns(t, dbType, stmt, tt.wantColumns)
			}
		})
	}
}

// TestQuerySet_stmt_cursorColumnsUnpaged pins that a statement that does not
// page selects no copy: a hand-built or armed query issues no cursor, whether
// or not its sort key is projected.
func TestQuerySet_stmt_cursorColumnsUnpaged(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		fields []accesstypes.Field
	}{
		{name: "the sort key outside the projection", fields: []accesstypes.Field{"ID", "Name"}},
		{name: "the positional sort key under its CASE", fields: []accesstypes.Field{"ID", "Name", "Fee"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			resSet, err := NewSet[projectionResource, positionalRequest](accesstypes.List)
			if err != nil {
				t.Fatalf("NewSet() error = %v", err)
			}
			qSet := NewQuerySet(NewMetadata[projectionResource]())
			for _, field := range tt.fields {
				qSet.AddField(field)
			}
			qSet.SetSortFields([]SortField{{Field: "Fee", Direction: SortAscending}})
			qSet.EnableUserPermissionEnforcement(resSet, renderStubPermissions{decisions: accesstypes.Decisions{projectedResource + ".fee": conditionalOn(projectedResource+".fee", "owner = subject")}}, testScope, accesstypes.List)
			qSet.collection = projectionCollection(t)
			if err := qSet.checkPermissions(t.Context(), SpannerDBType); err != nil {
				t.Fatalf("checkPermissions() error = %v", err)
			}
			stmt, err := qSet.stmt(SpannerDBType)
			if err != nil {
				t.Fatalf("stmt() error = %v", err)
			}
			if strings.Contains(stmt.SQL, cursorColumnPrefix) || len(stmt.cursorColumns) != 0 {
				t.Errorf("unpaged statement selects a cursor column:\n%s", stmt.SQL)
			}
			if !strings.Contains(stmt.SQL, "ORDER BY `Fee` ASC") {
				t.Errorf("unpaged statement does not order on the raw column:\n%s", stmt.SQL)
			}
		})
	}
}

// TestScanEnvelopeRow_cursorColumns pins the scan contract for the reserved
// alias on the row data: the copy lands in the envelope, typed as the field,
// and the row data keeps the masked filler or no cell at all.
func TestScanEnvelopeRow_cursorColumns(t *testing.T) {
	t.Parallel()

	feeType := NewMetadata[projectionResource]().dbFieldMap(SpannerDBType)["Fee"].fieldType
	idType := NewMetadata[projectionResource]().dbFieldMap(SpannerDBType)["ID"].fieldType

	tests := []struct {
		name     string
		columns  []string
		values   []any
		stmt     *Statement
		wantRaw  int64
		wantFill int64
		wantID   string
	}{
		{
			name:     "a masked positional cell carries the filler while the alias carries the value",
			columns:  []string{"Id", "Name", "Fee", maskedNamesColumnName, cursorColumnPrefix + "Fee"},
			values:   []any{"a", "n", int64(0), []string{"fee"}, int64(42)},
			stmt:     &Statement{maskedNamesColumn: maskedNamesColumnName, cursorColumns: []cursorColumn{{field: "Fee", alias: cursorColumnPrefix + "Fee", fieldType: feeType}}},
			wantRaw:  42,
			wantFill: 0,
			wantID:   "a",
		},
		{
			name:     "an unmasked cell and its alias agree",
			columns:  []string{"Id", "Name", "Fee", maskedNamesColumnName, cursorColumnPrefix + "Fee"},
			values:   []any{"a", "n", int64(42), []string{}, int64(42)},
			stmt:     &Statement{maskedNamesColumn: maskedNamesColumnName, cursorColumns: []cursorColumn{{field: "Fee", alias: cursorColumnPrefix + "Fee", fieldType: feeType}}},
			wantRaw:  42,
			wantFill: 42,
			wantID:   "a",
		},
		{
			name:    "an unselected key arrives in the envelope only: the row data has no cell for it",
			columns: []string{"Id", "Name", cursorColumnPrefix + "Fee"},
			values:  []any{"a", "n", int64(42)},
			stmt:    &Statement{cursorColumns: []cursorColumn{{field: "Fee", alias: cursorColumnPrefix + "Fee", fieldType: feeType}}},
			wantRaw: 42,
			wantID:  "a",
		},
		{
			name:     "the primary key outside the projection arrives in the envelope only",
			columns:  []string{"Name", "Fee", cursorColumnPrefix + "Id"},
			values:   []any{"n", int64(42), "k"},
			stmt:     &Statement{cursorColumns: []cursorColumn{{field: "ID", alias: cursorColumnPrefix + "Id", fieldType: idType}}},
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
			if !envelopeScan(tt.stmt) {
				t.Fatal("envelopeScan() = false for a statement carrying cursor columns")
			}
			row, err := scanEnvelopeRow[projectionResource](spannerRow, tt.stmt)
			if err != nil {
				t.Fatalf("scanEnvelopeRow() error = %v", err)
			}
			if row.Data.Fee != tt.wantFill {
				t.Errorf("Data.Fee = %d, want %d", row.Data.Fee, tt.wantFill)
			}
			if row.Data.ID != tt.wantID {
				t.Errorf("Data.ID = %q, want %q", row.Data.ID, tt.wantID)
			}
			for _, column := range tt.stmt.cursorColumns {
				copied, ok := row.cursorValues[column.field]
				if !ok {
					t.Fatalf("cursorValues[%s] absent", column.field)
				}
				switch column.field {
				case "Fee":
					if copied.Int() != tt.wantRaw {
						t.Errorf("cursorValues[Fee] = %v, want %d", copied, tt.wantRaw)
					}
				case "ID":
					if copied.String() != "k" {
						t.Errorf("cursorValues[ID] = %v, want k", copied)
					}
				}
			}
		})
	}
}

// feeCents, code, and ratio are named variants of base kinds, the shapes a
// resource may declare for a column (type HazardLevel int64, type Status
// string); the scan matrix covers them beside the base kinds.
type (
	feeCents int64
	code     string
	ratio    float64
)

// TestScanEnvelopeRow_nullableCursorColumn pins the scan of a concealing key's
// copy — the visible-projection CASE, which is NULL where the cell is masked —
// for every kind the sortable rule admits: a value arrives typed as the field
// behind a pointer, NULL arrives as a nil pointer, and the cursor writes the
// NULL key for it. A field type that is nullable itself scans as it is.
//
// The copy is read as a generic column value and decoded into the field's own
// type, so a named variant of a base kind (feeCents, code, ratio) arrives typed
// as the field and encodes exactly as its cell would, where a scan into a
// pointer to a pointer to it is refused by the client
// (TestSpannerClient_pointerToNamedVariantGap). A type the client cannot
// decode at all still fails the scan with the client's error.
func TestScanEnvelopeRow_nullableCursorColumn(t *testing.T) {
	t.Parallel()

	uid, err := ccc.NewUUID()
	if err != nil {
		t.Fatalf("ccc.NewUUID() error = %v", err)
	}
	at := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		fieldType reflect.Type
		value     any
		null      any
		wantText  string
		wantErr   string
	}{
		{name: "text", fieldType: reflect.TypeFor[string](), value: "x", null: spanner.NullString{}, wantText: "x"},
		{name: "integer", fieldType: reflect.TypeFor[int64](), value: int64(42), null: spanner.NullInt64{}, wantText: "42"},
		{name: "boolean", fieldType: reflect.TypeFor[bool](), value: true, null: spanner.NullBool{}, wantText: "true"},
		{name: "float", fieldType: reflect.TypeFor[float64](), value: 1.5, null: spanner.NullFloat64{}, wantText: "1.5"},
		{name: "time", fieldType: reflect.TypeFor[time.Time](), value: at, null: spanner.NullTime{}, wantText: "2026-09-15T12:00:00Z"},
		{name: "date", fieldType: reflect.TypeFor[civil.Date](), value: civil.Date{Year: 2026, Month: time.September, Day: 15}, null: spanner.NullDate{}, wantText: "2026-09-15"},
		{name: "decimal", fieldType: reflect.TypeFor[decimal.Decimal](), value: big.NewRat(3, 2), null: spanner.NullNumeric{}, wantText: "1.5"},
		{name: "UUID", fieldType: reflect.TypeFor[ccc.UUID](), value: uid.String(), null: spanner.NullString{}, wantText: uid.String()},
		{name: "a named variant of an integer decodes into the field's type", fieldType: reflect.TypeFor[feeCents](), value: int64(7), null: spanner.NullInt64{}, wantText: "7"},
		{name: "a named variant of a text decodes into the field's type", fieldType: reflect.TypeFor[code](), value: "x", null: spanner.NullString{}, wantText: "x"},
		{name: "a named variant of a float decodes into the field's type", fieldType: reflect.TypeFor[ratio](), value: 1.5, null: spanner.NullFloat64{}, wantText: "1.5"},
		{name: "a type the client cannot decode at all fails with the client's error", fieldType: reflect.TypeFor[struct{ A int }](), value: int64(7), null: spanner.NullInt64{}, wantErr: "type *struct { A int } cannot be used for decoding INT64"},
		{name: "a field type nullable already scans as it is", fieldType: reflect.TypeFor[*string](), value: "x", null: spanner.NullString{}, wantText: "x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			stmt := &Statement{cursorColumns: []cursorColumn{{field: "Fee", alias: cursorColumnPrefix + "Fee", fieldType: tt.fieldType, nullable: true}}}

			valueRow, err := spanner.NewRow([]string{"Id", cursorColumnPrefix + "Fee"}, []any{"a", tt.value})
			if err != nil {
				t.Fatalf("spanner.NewRow(value) error = %v", err)
			}
			row, err := scanEnvelopeRow[projectionResource](valueRow, stmt)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("scanEnvelopeRow(value) error = %v, want containing %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("scanEnvelopeRow(value) error = %v", err)
			}
			copied := row.cursorValues["Fee"]
			if !isNullableType(tt.fieldType) && (copied.Kind() != reflect.Pointer || copied.Type().Elem() != tt.fieldType) {
				t.Fatalf("copy of %s = %s, want a pointer to it", tt.fieldType, copied.Type())
			}
			text, err := cursorText(copied)
			if err != nil {
				t.Fatalf("cursorText() error = %v", err)
			}
			if text == nil || *text != tt.wantText {
				t.Errorf("cursor key = %v, want %q", text, tt.wantText)
			}

			nullRow, err := spanner.NewRow([]string{"Id", cursorColumnPrefix + "Fee"}, []any{"a", tt.null})
			if err != nil {
				t.Fatalf("spanner.NewRow(null) error = %v", err)
			}
			row, err = scanEnvelopeRow[projectionResource](nullRow, stmt)
			if err != nil {
				t.Fatalf("scanEnvelopeRow(null) error = %v", err)
			}
			copied = row.cursorValues["Fee"]
			if !copied.IsNil() {
				t.Fatalf("copy of NULL = %v, want nil", copied)
			}
			text, err = cursorText(copied)
			if err != nil {
				t.Fatalf("cursorText(null) error = %v", err)
			}
			if text != nil {
				t.Errorf("cursor key for NULL = %q, want the NULL key", *text)
			}
		})
	}
}

// TestSpannerClient_pointerToNamedVariantGap pins the client gap the generic
// read routes around: spanner.Row.ColumnByName decodes an INT64 into a pointer
// to a pointer to the base kind and refuses the same shape over a named variant
// of it. The reader never asks the client for that shape; this test only keeps
// the reason on record. When its refusal row fails, the client has learned the
// shape, and the generic read is a choice rather than a necessity.
func TestSpannerClient_pointerToNamedVariantGap(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		dest    any
		wantErr string
	}{
		{name: "a pointer to a pointer to the base kind decodes", dest: new(*int64)},
		{name: "a pointer to a pointer to a named variant is refused", dest: new(*feeCents), wantErr: "type **resource.feeCents cannot be used for decoding INT64"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			spannerRow, err := spanner.NewRow([]string{cursorColumnPrefix + "Fee"}, []any{int64(7)})
			if err != nil {
				t.Fatalf("spanner.NewRow() error = %v", err)
			}
			err = spannerRow.ColumnByName(cursorColumnPrefix+"Fee", tt.dest)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("spanner.Row.ColumnByName(%T) error = %v", tt.dest, err)
				}

				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("spanner.Row.ColumnByName(%T) error = %v, want containing %q", tt.dest, err, tt.wantErr)
			}
		})
	}
}
