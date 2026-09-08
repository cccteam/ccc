package resource

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
)

// TestQuerySet_Order pins the total order a list is read in: the request's sort,
// or the declared default when the request states none, then the primary key
// ascending unless the caller already named it; a hand-built QuerySet with no
// keys keeps exactly the sort it was given.
func TestQuerySet_Order(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		sortFields   []SortField
		defaultOrder []SortField
		keyFields    []accesstypes.Field
		wantOrder    []SortField
		wantSpanner  string
		wantPostgres string
	}{
		{
			name:         "no sort, no declaration: primary-key order",
			keyFields:    []accesstypes.Field{"ID"},
			wantOrder:    []SortField{{Field: "ID", Direction: SortAscending}},
			wantSpanner:  "ORDER BY `Id` ASC",
			wantPostgres: `ORDER BY "Id" ASC`,
		},
		{
			name:         "no sort: the declared order, then the key",
			defaultOrder: []SortField{{Field: "Date", Direction: SortDescending}},
			keyFields:    []accesstypes.Field{"ID"},
			wantOrder:    []SortField{{Field: "Date", Direction: SortDescending}, {Field: "ID", Direction: SortAscending}},
			wantSpanner:  "ORDER BY `Date` DESC, `Id` ASC",
			wantPostgres: `ORDER BY "Date" DESC, "Id" ASC`,
		},
		{
			name:         "a request sort replaces the declared order",
			sortFields:   []SortField{{Field: "Name", Direction: SortAscending}},
			defaultOrder: []SortField{{Field: "Date", Direction: SortDescending}},
			keyFields:    []accesstypes.Field{"ID"},
			wantOrder:    []SortField{{Field: "Name", Direction: SortAscending}, {Field: "ID", Direction: SortAscending}},
			wantSpanner:  "ORDER BY `Name` ASC, `Id` ASC",
			wantPostgres: `ORDER BY "Name" ASC, "Id" ASC`,
		},
		{
			name:         "a key the caller named keeps its direction and is not repeated",
			sortFields:   []SortField{{Field: "ID", Direction: SortDescending}},
			keyFields:    []accesstypes.Field{"ID"},
			wantOrder:    []SortField{{Field: "ID", Direction: SortDescending}},
			wantSpanner:  "ORDER BY `Id` DESC",
			wantPostgres: `ORDER BY "Id" DESC`,
		},
		{
			name:         "a nullable column states its NULL placement, ascending",
			sortFields:   []SortField{{Field: "Note", Direction: SortAscending}},
			keyFields:    []accesstypes.Field{"ID"},
			wantOrder:    []SortField{{Field: "Note", Direction: SortAscending}, {Field: "ID", Direction: SortAscending}},
			wantSpanner:  "ORDER BY `Note` ASC NULLS LAST, `Id` ASC",
			wantPostgres: `ORDER BY "Note" ASC NULLS LAST, "Id" ASC`,
		},
		{
			name:         "a nullable column states its NULL placement, descending",
			sortFields:   []SortField{{Field: "Note", Direction: SortDescending}, {Field: "Name", Direction: SortDescending}},
			keyFields:    []accesstypes.Field{"ID"},
			wantOrder:    []SortField{{Field: "Note", Direction: SortDescending}, {Field: "Name", Direction: SortDescending}, {Field: "ID", Direction: SortAscending}},
			wantSpanner:  "ORDER BY `Note` DESC NULLS FIRST, `Name` DESC, `Id` ASC",
			wantPostgres: `ORDER BY "Note" DESC NULLS FIRST, "Name" DESC, "Id" ASC`,
		},
		{
			name:         "a hand-built QuerySet keeps exactly its sort",
			sortFields:   []SortField{{Field: "Name", Direction: SortAscending}},
			wantOrder:    []SortField{{Field: "Name", Direction: SortAscending}},
			wantSpanner:  "ORDER BY `Name` ASC",
			wantPostgres: `ORDER BY "Name" ASC`,
		},
		{
			name:      "a hand-built QuerySet with no sort has no order",
			wantOrder: []SortField{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			qSet := NewQuerySet(NewMetadata[SortTestResource]())
			qSet.AddField("ID")
			qSet.SetSortFields(tt.sortFields)
			qSet.defaultOrder = tt.defaultOrder
			qSet.keyFields = tt.keyFields

			if got := qSet.Order(); !slices.Equal(got, tt.wantOrder) {
				t.Errorf("QuerySet.Order() = %v, want %v", got, tt.wantOrder)
			}

			for _, db := range []struct {
				dbType DBType
				want   string
			}{{SpannerDBType, tt.wantSpanner}, {PostgresDBType, tt.wantPostgres}} {
				got, err := qSet.buildOrderByClause(db.dbType, nil)
				if err != nil {
					t.Fatalf("buildOrderByClause(%s) error = %v", db.dbType, err)
				}
				if got != db.want {
					t.Errorf("buildOrderByClause(%s) = %q, want %q", db.dbType, got, db.want)
				}
			}
		})
	}
}

// TestQueryDecoder_stampsOrder pins what the decoder hands the QuerySet: the
// request type's primary-key markers as the key fields and the declared paging
// contract's order as the default.
func TestQueryDecoder_stampsOrder(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		target    string
		paging    Paging
		wantOrder []SortField
	}{
		{
			name:      "no sort, no declaration: the primary key alone",
			target:    "/",
			wantOrder: []SortField{{Field: "ID", Direction: SortAscending}},
		},
		{
			name:      "no sort: the declared order, then the key",
			target:    "/",
			paging:    Paging{Order: []SortField{{Field: "Public", Direction: SortDescending}}},
			wantOrder: []SortField{{Field: "Public", Direction: SortDescending}, {Field: "ID", Direction: SortAscending}},
		},
		{
			name:      "a request sort replaces the declared order",
			target:    "/?sort=tagged",
			paging:    Paging{Order: []SortField{{Field: "Public", Direction: SortDescending}}},
			wantOrder: []SortField{{Field: "Tagged", Direction: SortAscending}, {Field: "ID", Direction: SortAscending}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			resSet, err := NewSet[enforcementResource, enforcementQueryRequest](accesstypes.List)
			if err != nil {
				t.Fatalf("NewSet() error = %v", err)
			}
			decoder, err := NewQueryDecoder[enforcementResource, enforcementQueryRequest](resSet)
			if err != nil {
				t.Fatalf("NewQueryDecoder() error = %v", err)
			}
			decoder.WithPaging(tt.paging)

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tt.target, http.NoBody)
			qSet, err := decoder.DecodeWithoutPermissions(req)
			if err != nil {
				t.Fatalf("DecodeWithoutPermissions() error = %v", err)
			}
			if got := qSet.Order(); !slices.Equal(got, tt.wantOrder) {
				t.Errorf("QuerySet.Order() = %v, want %v", got, tt.wantOrder)
			}
		})
	}
}

func TestQueryDecoder_WithPaging_unknownOrderField(t *testing.T) {
	t.Parallel()

	resSet, err := NewSet[enforcementResource, enforcementQueryRequest](accesstypes.List)
	if err != nil {
		t.Fatalf("NewSet() error = %v", err)
	}
	decoder, err := NewQueryDecoder[enforcementResource, enforcementQueryRequest](resSet)
	if err != nil {
		t.Fatalf("NewQueryDecoder() error = %v", err)
	}

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("WithPaging() expected a panic on an order field the request type lacks")
		}
		if msg, ok := r.(string); !ok || !strings.Contains(msg, `"Missing"`) {
			t.Errorf("WithPaging() panic = %v, want it to name the field", r)
		}
	}()
	decoder.WithPaging(Paging{Order: []SortField{{Field: "Missing", Direction: SortAscending}}})
}
