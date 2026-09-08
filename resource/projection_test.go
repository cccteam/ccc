package resource

// These tests pin the visible-projection rule (decided 2026-09-08): a sort or
// filter over a conditionally granted field runs over CASE WHEN <condition>
// THEN column END, so a masked cell is NULL for ordering, for the cursor
// predicate, and for the filter; a pruned CASE uses the raw column; a field the
// query names outside the projection renders its override alone.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/google/go-cmp/cmp"
)

// projectionResource is the two-database fixture: a key, a text, a number the
// conditions guard, and a nullable note.
type projectionResource struct {
	ID      string  `spanner:"Id"      postgres:"Id"`
	Name    string  `spanner:"Name"    postgres:"Name"`
	Fee     int64   `spanner:"Fee"     postgres:"Fee"`
	Note    *string `spanner:"Note"    postgres:"Note"`
	Station string  `spanner:"Station" postgres:"Station"`
}

func (projectionResource) Resource() accesstypes.Resource { return projectedResource }

const projectedResource = accesstypes.Resource("projectionResources")

type projectionRequest struct {
	ID      string  `json:"id"   perm:"-"`
	Name    string  `json:"name" index:"true"`
	Fee     int64   `json:"fee"  index:"true"`
	Note    *string `json:"note" allow_filter:"true"`
	Station string  `json:"-"`
}

func projectionCollection(t *testing.T) *GeneratedCollection {
	t.Helper()

	g, err := NewGeneratedCollection(CollectionData{Resources: []CollectionResource{{
		Name:        projectedResource,
		Scope:       accesstypes.DomainPermissionScope,
		Permissions: []accesstypes.Permission{accesstypes.List},
		Attributes: []AttributeData{
			{Name: "owner", Column: "Owner", Type: AttributeTypeString},
			{Name: "priority", Column: "Priority", Type: AttributeTypeNumber},
		},
		Domain: &DomainBindingData{Column: "Station"},
	}}})
	if err != nil {
		t.Fatalf("NewGeneratedCollection() error = %v", err)
	}

	return g
}

func TestQuerySet_stmt_visibleProjection(t *testing.T) {
	t.Parallel()

	const id = "0193e2a7-522c-708f-bfd0-4adf33486bb1"
	feeOwner := conditionalOn(projectedResource+".fee", "owner = subject")

	tests := []struct {
		name         string
		target       string
		decisions    accesstypes.Decisions
		cursor       *cursor
		wantSpanner  string
		wantPostgres string
		wantParams   map[string]any
		wantErr      string
	}{
		{
			name:      "sort on an unpruned conditional field orders its CASE with NULLS LAST",
			target:    "/?sort=fee",
			decisions: accesstypes.Decisions{projectedResource + ".fee": feeOwner},
			wantSpanner: "SELECT Id, Name, CASE WHEN `projectionResources`.`Owner` = @subject THEN Fee ELSE @_c1 END AS Fee, Note, " +
				"IF(`projectionResources`.`Owner` = @subject, ARRAY<STRING>[], ['fee']) AS zzMaskedFields " +
				"FROM projectionResources WHERE (`projectionResources`.`Station` = @domain) " +
				"ORDER BY CASE WHEN `projectionResources`.`Owner` = @subject THEN `Fee` END ASC NULLS LAST, `Id` ASC LIMIT 51",
			wantPostgres: `SELECT "Id", "Name", CASE WHEN "projectionResources"."Owner" = @subject THEN "Fee" ELSE @_c1 END AS "Fee", "Note", ` +
				`ARRAY_REMOVE(ARRAY[CASE WHEN "projectionResources"."Owner" = @subject THEN NULL ELSE 'fee' END], NULL) AS "zzMaskedFields" ` +
				`FROM projectionResources WHERE ("projectionResources"."Station" = @domain) ` +
				`ORDER BY CASE WHEN "projectionResources"."Owner" = @subject THEN "Fee" END ASC NULLS LAST, "Id" ASC LIMIT 51`,
			wantParams: map[string]any{"subject": "u1", "domain": "testDomain", "_c1": int64(0)},
		},
		{
			name:      "descending puts the masked rows first",
			target:    "/?sort=fee:desc&columns=id,name",
			decisions: accesstypes.Decisions{projectedResource + ".fee": feeOwner},
			wantSpanner: "SELECT Id, Name FROM projectionResources WHERE (`projectionResources`.`Station` = @domain) " +
				"ORDER BY CASE WHEN `projectionResources`.`Owner` = @subject THEN `Fee` END DESC NULLS FIRST, `Id` ASC LIMIT 51",
			wantPostgres: `SELECT "Id", "Name" FROM projectionResources WHERE ("projectionResources"."Station" = @domain) ` +
				`ORDER BY CASE WHEN "projectionResources"."Owner" = @subject THEN "Fee" END DESC NULLS FIRST, "Id" ASC LIMIT 51`,
			wantParams: map[string]any{"subject": "u1", "domain": "testDomain"},
		},
		{
			name:   "a pruned CASE sorts and filters on the raw column",
			target: "/?sort=fee&filter=fee:gt:5",
			decisions: accesstypes.Decisions{
				projectedResource + ".name": conditionalOn(projectedResource+".name", "owner = subject"),
				projectedResource + ".fee":  feeOwner,
				projectedResource + ".note": conditionalOn(projectedResource+".note", "owner = subject"),
			},
			wantSpanner: "SELECT Id, Name, Fee, Note FROM projectionResources " +
				"WHERE `Fee` > @_p1 AND (`projectionResources`.`Station` = @domain) AND (`projectionResources`.`Owner` = @subject) " +
				"ORDER BY `Fee` ASC, `Id` ASC LIMIT 51",
			wantPostgres: `SELECT "Id", "Name", "Fee", "Note" FROM projectionResources ` +
				`WHERE "Fee" > @_p1 AND ("projectionResources"."Station" = @domain) AND ("projectionResources"."Owner" = @subject) ` +
				`ORDER BY "Fee" ASC, "Id" ASC LIMIT 51`,
			wantParams: map[string]any{"subject": "u1", "domain": "testDomain", "_p1": 5},
		},
		{
			name:   "a filter on an unpruned field compares its CASE, so a masked row never matches",
			target: "/?filter=fee:gt:5&columns=id,name",
			decisions: accesstypes.Decisions{
				projectedResource + ".fee":  feeOwner,
				projectedResource + ".note": conditionalOn(projectedResource+".note", "owner = subject OR priority = 3"),
			},
			wantSpanner: "SELECT Id, Name FROM projectionResources " +
				"WHERE CASE WHEN `projectionResources`.`Owner` = @subject THEN `Fee` END > @_p1 AND (`projectionResources`.`Station` = @domain) " +
				"ORDER BY `Id` ASC LIMIT 51",
			wantPostgres: `SELECT "Id", "Name" FROM projectionResources ` +
				`WHERE CASE WHEN "projectionResources"."Owner" = @subject THEN "Fee" END > @_p1 AND ("projectionResources"."Station" = @domain) ` +
				`ORDER BY "Id" ASC LIMIT 51`,
			wantParams: map[string]any{"subject": "u1", "domain": "testDomain", "_p1": 5},
		},
		{
			name:      "isnull on an unpruned field matches the masked rows",
			target:    "/?filter=fee:isnull&columns=id",
			decisions: accesstypes.Decisions{projectedResource + ".fee": feeOwner},
			wantSpanner: "SELECT Id FROM projectionResources " +
				"WHERE CASE WHEN `projectionResources`.`Owner` = @subject THEN `Fee` END IS NULL AND (`projectionResources`.`Station` = @domain) " +
				"ORDER BY `Id` ASC LIMIT 51",
			wantPostgres: `SELECT "Id" FROM projectionResources ` +
				`WHERE CASE WHEN "projectionResources"."Owner" = @subject THEN "Fee" END IS NULL AND ("projectionResources"."Station" = @domain) ` +
				`ORDER BY "Id" ASC LIMIT 51`,
			wantParams: map[string]any{"subject": "u1", "domain": "testDomain"},
		},
		{
			name:      "a NULL boundary key on the masked sort column positions the cursor in the NULL region",
			target:    "/?sort=fee&columns=id",
			decisions: accesstypes.Decisions{projectedResource + ".fee": feeOwner},
			cursor:    &cursor{Direction: pageNext, Keys: []*string{nil, strPtr(id)}},
			wantSpanner: "SELECT Id FROM projectionResources " +
				"WHERE (`projectionResources`.`Station` = @domain) AND ((CASE WHEN `projectionResources`.`Owner` = @subject THEN `Fee` END IS NULL AND `Id` > @_c1)) " +
				"ORDER BY CASE WHEN `projectionResources`.`Owner` = @subject THEN `Fee` END ASC NULLS LAST, `Id` ASC LIMIT 51",
			wantPostgres: `SELECT "Id" FROM projectionResources ` +
				`WHERE ("projectionResources"."Station" = @domain) AND ((CASE WHEN "projectionResources"."Owner" = @subject THEN "Fee" END IS NULL AND "Id" > @_c1)) ` +
				`ORDER BY CASE WHEN "projectionResources"."Owner" = @subject THEN "Fee" END ASC NULLS LAST, "Id" ASC LIMIT 51`,
			wantParams: map[string]any{"subject": "u1", "domain": "testDomain", "_c1": id},
		},
		{
			name:      "a value boundary on the masked sort column admits the NULL region after it",
			target:    "/?sort=fee&columns=id",
			decisions: accesstypes.Decisions{projectedResource + ".fee": feeOwner},
			cursor:    &cursor{Direction: pageNext, Keys: []*string{strPtr("7"), strPtr(id)}},
			wantSpanner: "SELECT Id FROM projectionResources " +
				"WHERE (`projectionResources`.`Station` = @domain) AND (" +
				"(CASE WHEN `projectionResources`.`Owner` = @subject THEN `Fee` END > @_c1 OR CASE WHEN `projectionResources`.`Owner` = @subject THEN `Fee` END IS NULL) OR " +
				"(CASE WHEN `projectionResources`.`Owner` = @subject THEN `Fee` END = @_c1 AND `Id` > @_c2)) " +
				"ORDER BY CASE WHEN `projectionResources`.`Owner` = @subject THEN `Fee` END ASC NULLS LAST, `Id` ASC LIMIT 51",
			wantPostgres: `SELECT "Id" FROM projectionResources ` +
				`WHERE ("projectionResources"."Station" = @domain) AND (` +
				`(CASE WHEN "projectionResources"."Owner" = @subject THEN "Fee" END > @_c1 OR CASE WHEN "projectionResources"."Owner" = @subject THEN "Fee" END IS NULL) OR ` +
				`(CASE WHEN "projectionResources"."Owner" = @subject THEN "Fee" END = @_c1 AND "Id" > @_c2)) ` +
				`ORDER BY CASE WHEN "projectionResources"."Owner" = @subject THEN "Fee" END ASC NULLS LAST, "Id" ASC LIMIT 51`,
			wantParams: map[string]any{"subject": "u1", "domain": "testDomain", "_c1": int64(7), "_c2": id},
		},
		{
			name:      "a denied field is refused",
			target:    "/?sort=fee",
			decisions: accesstypes.Decisions{projectedResource + ".fee": accesstypes.Denied()},
			wantErr:   "cannot sort or filter on fee: (List) on projectionResources.fee is denied",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			for _, dbType := range []DBType{SpannerDBType, PostgresDBType} {
				resSet, err := NewSet[projectionResource, projectionRequest](accesstypes.List)
				if err != nil {
					t.Fatalf("NewSet() error = %v", err)
				}
				decoder, err := NewQueryDecoder[projectionResource, projectionRequest](resSet)
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

				err = qSet.checkPermissions(t.Context(), dbType)
				if tt.wantErr != "" {
					if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
						t.Fatalf("checkPermissions(%s) error = %v, want containing %q", dbType, err, tt.wantErr)
					}

					continue
				}
				if err != nil {
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
			}
		})
	}
}
