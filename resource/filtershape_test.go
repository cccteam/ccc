package resource

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/google/go-cmp/cmp"
	"github.com/shopspring/decimal"
)

// boardRow is the computed-resource fixture the in-memory query tests run over:
// one column of each comparable shape, a nullable one, and a nested one the
// query never reaches.
type boardRow struct {
	ID         ccc.UUID
	ShipName   string
	Subsystem  string
	Worst      float64
	Count      int64
	RecordedAt time.Time
	Fee        decimal.Decimal
	Note       *string
	Recent     []boardReading
}

type boardReading struct {
	Value float64
}

func (boardRow) Resource() accesstypes.Resource { return "boardRows" }

func (boardRow) DefaultConfig() Config { return Config{} }

type boardRequest struct {
	ID         ccc.UUID        `json:"id"         perm:"-"`
	ShipName   string          `json:"shipName"   allow_filter:"true"`
	Subsystem  string          `json:"subsystem"  allow_filter:"true"`
	Worst      float64         `json:"worst"      allow_filter:"true"`
	Count      int64           `json:"count"      allow_filter:"true"`
	RecordedAt time.Time       `json:"recordedAt" allow_filter:"true"`
	Fee        decimal.Decimal `json:"fee"        allow_filter:"true"`
	Note       *string         `json:"note"       allow_filter:"true"`
	Recent     []boardReading  `json:"recent"`
}

// decodeBoard decodes a computed request over the fixture without permissions.
func decodeBoard(t *testing.T, target string, paging Paging) *QuerySet[boardRow] {
	t.Helper()

	resSet, err := NewSet[boardRow, boardRequest](accesstypes.List)
	if err != nil {
		t.Fatalf("NewSet() error = %v", err)
	}
	decoder, err := NewQueryDecoder[boardRow, boardRequest](resSet)
	if err != nil {
		t.Fatalf("NewQueryDecoder() error = %v", err)
	}
	decoder.WithPaging(paging).WithCursorKey(mustCursorKey(t, testCookieKey))
	qSet, err := decoder.DecodeWithoutPermissions(httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, http.NoBody))
	if err != nil {
		t.Fatalf("DecodeWithoutPermissions(%s) error = %v", target, err)
	}
	qSet.scope = testScope

	return qSet
}

var (
	boardT0 = time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	boardT1 = boardT0.Add(time.Hour)
	boardT2 = boardT0.Add(2 * time.Hour)
)

func boardRows() []*boardRow {
	return []*boardRow{
		{ID: mustUUIDFromString("00000000-0000-4000-8000-000000000001"), ShipName: "Kingfisher", Subsystem: "hull", Worst: 0.95, Count: 3, RecordedAt: boardT2, Fee: decimal.RequireFromString("10.5"), Note: strPtr("watch")},
		{ID: mustUUIDFromString("00000000-0000-4000-8000-000000000002"), ShipName: "Kingfisher", Subsystem: "reactor", Worst: 0.55, Count: 1, RecordedAt: boardT1, Fee: decimal.RequireFromString("2")},
		{ID: mustUUIDFromString("00000000-0000-4000-8000-000000000003"), ShipName: "Lantern", Subsystem: "hull", Worst: 0.20, Count: 7, RecordedAt: boardT0, Fee: decimal.RequireFromString("10.5"), Note: strPtr("ok")},
	}
}

func shipNames(rows []*boardRow) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.ShipName+"/"+r.Subsystem)
	}

	return out
}

// TestFilterShape_Match pins the in-memory evaluator against the SQL grammar's
// meaning: every operator, literal typing per field type, NULL semantics, and
// the AND/OR/group structure.
func TestFilterShape_Match(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		filter string
		want   []string
	}{
		{name: "eq on text", filter: "shipName:eq:Lantern", want: []string{"Lantern/hull"}},
		{name: "ne on text", filter: "shipName:ne:Lantern", want: []string{"Kingfisher/hull", "Kingfisher/reactor"}},
		{name: "gt on a float", filter: "worst:gt:0.5", want: []string{"Kingfisher/hull", "Kingfisher/reactor"}},
		{name: "lte on an integer", filter: "count:lte:3", want: []string{"Kingfisher/hull", "Kingfisher/reactor"}},
		{name: "gte on a timestamp", filter: "recordedAt:gte:2026-09-04T13:00:00Z", want: []string{"Kingfisher/hull", "Kingfisher/reactor"}},
		{name: "eq on a decimal by value", filter: "fee:eq:10.50", want: []string{"Kingfisher/hull", "Lantern/hull"}},
		{name: "in over a list", filter: "subsystem:in:(reactor,drive)", want: []string{"Kingfisher/reactor"}},
		{name: "notin over a list", filter: "subsystem:notin:(reactor,drive)", want: []string{"Kingfisher/hull", "Lantern/hull"}},
		{name: "isnull matches only NULL", filter: "note:isnull", want: []string{"Kingfisher/reactor"}},
		{name: "isnotnull", filter: "note:isnotnull", want: []string{"Kingfisher/hull", "Lantern/hull"}},
		{name: "a comparison never matches NULL", filter: "note:ne:watch", want: []string{"Lantern/hull"}},
		{name: "and", filter: "shipName:eq:Kingfisher,worst:lt:0.9", want: []string{"Kingfisher/reactor"}},
		{name: "or", filter: "subsystem:eq:reactor|shipName:eq:Lantern", want: []string{"Kingfisher/reactor", "Lantern/hull"}},
		{name: "group", filter: "worst:gt:0.1,(subsystem:eq:reactor|shipName:eq:Lantern)", want: []string{"Kingfisher/reactor", "Lantern/hull"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			qSet := decodeBoard(t, "/?filter="+tt.filter, Paging{})
			var got []string
			for _, row := range boardRows() {
				ok, err := qSet.Filter().Match(row)
				if err != nil {
					t.Fatalf("Match() error = %v", err)
				}
				if ok {
					got = append(got, row.ShipName+"/"+row.Subsystem)
				}
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("Match() kept %v, want %v", got, tt.want)
			}
		})
	}
}

// TestFilterShape_Take pins the pushdown rule: a conjunction's conditions can be
// taken by field and leave the handler the rest; an OR takes nothing.
func TestFilterShape_Take(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		filter          string
		take            []string
		wantTaken       []Condition
		wantConjunction bool
		wantResidual    bool // whether anything is left for the handler
		wantMatched     []string
	}{
		{
			name:            "no filter: nothing to take, everything matches",
			wantConjunction: false,
			wantMatched:     []string{"Kingfisher/hull", "Kingfisher/reactor", "Lantern/hull"},
		},
		{
			name:            "take one condition of a conjunction, the handler keeps the other",
			filter:          "shipName:eq:Kingfisher,worst:lt:0.9",
			take:            []string{"ShipName"},
			wantTaken:       []Condition{{Field: "ShipName", Operator: "eq", Value: "Kingfisher"}},
			wantConjunction: true,
			wantResidual:    true,
			wantMatched:     []string{"Kingfisher/reactor", "Lantern/hull"},
		},
		{
			name:            "take every condition: nothing is left",
			filter:          "shipName:eq:Kingfisher,worst:lt:0.9",
			take:            []string{"ShipName", "Worst"},
			wantTaken:       []Condition{{Field: "ShipName", Operator: "eq", Value: "Kingfisher"}, {Field: "Worst", Operator: "lt", Value: 0.9}},
			wantConjunction: true,
			wantMatched:     []string{"Kingfisher/hull", "Kingfisher/reactor", "Lantern/hull"},
		},
		{
			name:            "a field the filter does not touch takes nothing",
			filter:          "shipName:eq:Kingfisher",
			take:            []string{"Subsystem"},
			wantConjunction: true,
			wantResidual:    true,
			wantMatched:     []string{"Kingfisher/hull", "Kingfisher/reactor"},
		},
		{
			name:            "an OR takes nothing and the handler evaluates the whole tree",
			filter:          "shipName:eq:Lantern|subsystem:eq:reactor",
			take:            []string{"ShipName"},
			wantConjunction: false,
			wantResidual:    true,
			wantMatched:     []string{"Kingfisher/reactor", "Lantern/hull"},
		},
		{
			name:            "a grouped conjunction is still a conjunction",
			filter:          "(shipName:eq:Kingfisher),(subsystem:eq:hull)",
			take:            []string{"ShipName", "Subsystem"},
			wantTaken:       []Condition{{Field: "ShipName", Operator: "eq", Value: "Kingfisher"}, {Field: "Subsystem", Operator: "eq", Value: "hull"}},
			wantConjunction: true,
			wantMatched:     []string{"Kingfisher/hull", "Kingfisher/reactor", "Lantern/hull"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			target := "/"
			if tt.filter != "" {
				target = "/?filter=" + tt.filter
			}
			qSet := decodeBoard(t, target, Paging{})
			filter := qSet.Filter()
			if filter.IsConjunction() != tt.wantConjunction {
				t.Errorf("IsConjunction() = %v, want %v", filter.IsConjunction(), tt.wantConjunction)
			}
			taken := filter.Take(tt.take...)
			if diff := cmp.Diff(tt.wantTaken, taken); diff != "" {
				t.Errorf("Take() mismatch (-want +got):\n%s", diff)
			}
			if filter.residualEmpty() == tt.wantResidual {
				t.Errorf("residualEmpty() = %v, want %v", filter.residualEmpty(), !tt.wantResidual)
			}
			var matched []string
			for _, row := range boardRows() {
				ok, err := filter.Match(row)
				if err != nil {
					t.Fatalf("Match() error = %v", err)
				}
				if ok {
					matched = append(matched, row.ShipName+"/"+row.Subsystem)
				}
			}
			if !slices.Equal(matched, tt.wantMatched) {
				t.Errorf("Match() after Take kept %v, want %v", matched, tt.wantMatched)
			}
		})
	}
}

func TestSortRows(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		order []SortField
		want  []string
	}{
		{name: "text ascending, key tiebreak", order: []SortField{{Field: "ShipName", Direction: SortAscending}, {Field: "ID", Direction: SortAscending}}, want: []string{"Kingfisher/hull", "Kingfisher/reactor", "Lantern/hull"}},
		{name: "float descending", order: []SortField{{Field: "Worst", Direction: SortDescending}}, want: []string{"Kingfisher/hull", "Kingfisher/reactor", "Lantern/hull"}},
		{name: "integer ascending", order: []SortField{{Field: "Count", Direction: SortAscending}}, want: []string{"Kingfisher/reactor", "Kingfisher/hull", "Lantern/hull"}},
		{name: "timestamp ascending", order: []SortField{{Field: "RecordedAt", Direction: SortAscending}}, want: []string{"Lantern/hull", "Kingfisher/reactor", "Kingfisher/hull"}},
		{name: "decimal by value, then key", order: []SortField{{Field: "Fee", Direction: SortDescending}, {Field: "ID", Direction: SortAscending}}, want: []string{"Kingfisher/hull", "Lantern/hull", "Kingfisher/reactor"}},
		{name: "nullable ascending puts NULL last", order: []SortField{{Field: "Note", Direction: SortAscending}}, want: []string{"Lantern/hull", "Kingfisher/hull", "Kingfisher/reactor"}},
		{name: "nullable descending puts NULL first", order: []SortField{{Field: "Note", Direction: SortDescending}}, want: []string{"Kingfisher/reactor", "Kingfisher/hull", "Lantern/hull"}},
		{name: "no order keeps the yielded order", want: []string{"Kingfisher/hull", "Kingfisher/reactor", "Lantern/hull"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rows := boardRows()
			if err := SortRows(rows, tt.order); err != nil {
				t.Fatalf("SortRows() error = %v", err)
			}
			if got := shipNames(rows); !slices.Equal(got, tt.want) {
				t.Errorf("SortRows() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSortRows_unorderableField(t *testing.T) {
	t.Parallel()

	err := SortRows(boardRows(), []SortField{{Field: "Recent", Direction: SortAscending}})
	if err == nil || !strings.Contains(err.Error(), "cannot be ordered") {
		t.Errorf("SortRows() error = %v, want the unorderable refusal", err)
	}
}

// TestQuerySet_Collect pins the handler's application of the query over a body's
// rows: filter, then count, then sort, then the cursor position, then the page,
// with the same headers a table page writes.
func TestQuerySet_Collect(t *testing.T) {
	t.Parallel()

	byWorst := []SortField{{Field: "Worst", Direction: SortDescending}}
	seal := func(t *testing.T, qSet *QuerySet[boardRow], direction pageDirection, row *boardRow) string {
		t.Helper()

		keys, err := qSet.boundaryKeys(row)
		if err != nil {
			t.Fatal(err)
		}
		token, err := qSet.cursorKey.seal(cursor{Query: qSet.queryHash(testScope), Direction: direction, Keys: keys})
		if err != nil {
			t.Fatal(err)
		}

		return token
	}

	tests := []struct {
		name       string
		target     func(t *testing.T) string
		paging     Paging
		takeSort   bool
		takeFilter []string
		wantRows   []string
		wantMore   bool
		wantTotal  *int64
		wantRels   []string
	}{
		{
			name:     "no query: every row in primary-key order",
			target:   func(*testing.T) string { return "/" },
			wantRows: []string{"Kingfisher/hull", "Kingfisher/reactor", "Lantern/hull"},
		},
		{
			name:     "filter then sort then page",
			target:   func(*testing.T) string { return "/?filter=worst:gt:0.1&sort=worst&limit=2" },
			wantRows: []string{"Lantern/hull", "Kingfisher/reactor"},
			wantMore: true,
			wantRels: []string{"next"},
		},
		{
			name:      "count is the filtered total, not the page",
			target:    func(*testing.T) string { return "/?filter=shipName:eq:Kingfisher&sort=worst:desc&limit=1&count=true" },
			wantRows:  []string{"Kingfisher/hull"},
			wantMore:  true,
			wantTotal: new(int64(2)),
			wantRels:  []string{"next"},
		},
		{
			name:     "the declared order pages a sort-less request",
			target:   func(*testing.T) string { return "/?limit=2" },
			paging:   Paging{Order: byWorst},
			wantRows: []string{"Kingfisher/hull", "Kingfisher/reactor"},
			wantMore: true,
			wantRels: []string{"next"},
		},
		{
			name: "a cursor positions the page after its boundary row",
			target: func(t *testing.T) string {
				first := decodeBoard(t, "/?sort=worst:desc&limit=1", Paging{})

				return "/?sort=worst:desc&limit=1&cursor=" + seal(t, first, pageNext, boardRows()[0])
			},
			wantRows: []string{"Kingfisher/reactor"},
			wantMore: true,
			wantRels: []string{"next", "prev"},
		},
		{
			name: "a backward cursor reads the reversed order; the handler reverses",
			target: func(t *testing.T) string {
				first := decodeBoard(t, "/?sort=worst:desc&limit=1", Paging{})

				return "/?sort=worst:desc&limit=1&cursor=" + seal(t, first, pagePrev, boardRows()[2])
			},
			wantRows: []string{"Kingfisher/reactor"},
			wantMore: true,
			wantRels: []string{"next", "prev"},
		},
		{
			name:     "a body that took the sort is trusted for order",
			target:   func(*testing.T) string { return "/?sort=worst&limit=5" },
			takeSort: true,
			wantRows: []string{"Kingfisher/hull", "Kingfisher/reactor", "Lantern/hull"},
		},
		{
			name:       "a taken condition is not applied again; the rest is",
			target:     func(*testing.T) string { return "/?filter=shipName:eq:Lantern,worst:lt:0.9" },
			takeFilter: []string{"ShipName"},
			wantRows:   []string{"Kingfisher/reactor", "Lantern/hull"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			qSet := decodeBoard(t, tt.target(t), tt.paging)
			if err := qSet.bindCursor(testScope); err != nil {
				t.Fatalf("bindCursor() error = %v", err)
			}
			if tt.takeSort {
				qSet.TakeSort()
			}
			if len(tt.takeFilter) > 0 {
				qSet.Filter().Take(tt.takeFilter...)
			}

			page, err := qSet.Collect(func(yield func(*boardRow, error) bool) {
				for _, row := range boardRows() {
					if !yield(row, nil) {
						return
					}
				}
			})
			if err != nil {
				t.Fatalf("Collect() error = %v", err)
			}
			rows := page.Rows()
			if page.Reversed() {
				rows = slices.Clone(rows)
				slices.Reverse(rows)
			}
			if got := shipNames(rows); !slices.Equal(got, tt.wantRows) {
				t.Errorf("Collect() rows = %v, want %v", got, tt.wantRows)
			}
			if page.more != tt.wantMore {
				t.Errorf("more = %v, want %v", page.more, tt.wantMore)
			}
			if (page.total == nil) != (tt.wantTotal == nil) || (page.total != nil && *page.total != *tt.wantTotal) {
				t.Errorf("total = %v, want %v", page.total, tt.wantTotal)
			}

			w := httptest.NewRecorder()
			if err := page.WriteHeaders(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, tt.target(t), http.NoBody)); err != nil {
				t.Fatalf("WriteHeaders() error = %v", err)
			}
			link := w.Header().Get(LinkHeader)
			for _, rel := range tt.wantRels {
				if !strings.Contains(link, `rel="`+rel+`"`) {
					t.Errorf("Link = %q, want %s", link, rel)
				}
			}
			if strings.Count(link, "rel=") != len(tt.wantRels) {
				t.Errorf("Link = %q, want %d relations", link, len(tt.wantRels))
			}
		})
	}
}

// TestQuerySet_TakePage pins when a body may page for itself: only with nothing
// left to filter, the sort taken, a bounded page, and no count.
func TestQuerySet_TakePage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		target     string
		takeSort   bool
		takeFilter []string
		wantOK     bool
		wantFetch  uint64
	}{
		{name: "sort taken, no filter", target: "/?sort=worst&limit=4", takeSort: true, wantOK: true, wantFetch: 5},
		{name: "sort not taken", target: "/?sort=worst&limit=4", wantOK: false},
		{name: "a filter condition remains for the handler", target: "/?sort=worst&filter=shipName:eq:Lantern", takeSort: true, wantOK: false},
		{name: "the filter fully taken", target: "/?sort=worst&filter=shipName:eq:Lantern", takeSort: true, takeFilter: []string{"ShipName"}, wantOK: true, wantFetch: DefaultPageSize + 1},
		{name: "count asked: the handler needs every row", target: "/?sort=worst&count=true", takeSort: true, wantOK: false},
		{name: "every row asked: nothing to page", target: "/?sort=worst&limit=all", takeSort: true, wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			qSet := decodeBoard(t, tt.target, Paging{})
			if tt.takeSort {
				if order := qSet.TakeSort(); order[len(order)-1].Field != "ID" {
					t.Errorf("TakeSort() = %v, want the key last", order)
				}
			}
			if len(tt.takeFilter) > 0 {
				qSet.Filter().Take(tt.takeFilter...)
			}
			bounds, ok := qSet.TakePage()
			if ok != tt.wantOK {
				t.Fatalf("TakePage() ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && bounds.Fetch() != tt.wantFetch {
				t.Errorf("Fetch() = %d, want %d", bounds.Fetch(), tt.wantFetch)
			}
			if ok && bounds.Boundary() != nil {
				t.Errorf("Boundary() = %v on a first page, want nil", bounds.Boundary())
			}
		})
	}
}

func TestQuerySet_TakePage_boundaryValues(t *testing.T) {
	t.Parallel()

	first := decodeBoard(t, "/?sort=worst:desc&limit=1", Paging{})
	keys, err := first.boundaryKeys(boardRows()[0])
	if err != nil {
		t.Fatal(err)
	}
	token, err := first.cursorKey.seal(cursor{Query: first.queryHash(testScope), Direction: pageNext, Keys: keys})
	if err != nil {
		t.Fatal(err)
	}

	qSet := decodeBoard(t, "/?sort=worst:desc&limit=1&cursor="+token, Paging{})
	if err := qSet.bindCursor(testScope); err != nil {
		t.Fatalf("bindCursor() error = %v", err)
	}
	qSet.TakeSort()
	bounds, ok := qSet.TakePage()
	if !ok {
		t.Fatal("TakePage() ok = false, want true")
	}
	want := []any{0.95, boardRows()[0].ID}
	if diff := cmp.Diff(want, bounds.Boundary()); diff != "" {
		t.Errorf("Boundary() mismatch (-want +got):\n%s", diff)
	}
	if got := bounds.Order(); got[0].Direction != SortDescending || got[1].Field != "ID" {
		t.Errorf("Order() = %v, want worst desc then the key", got)
	}
}

func TestQueryDecoder_refusesUnsortableField(t *testing.T) {
	t.Parallel()

	resSet, err := NewSet[boardRow, boardRequest](accesstypes.List)
	if err != nil {
		t.Fatal(err)
	}
	decoder, err := NewQueryDecoder[boardRow, boardRequest](resSet)
	if err != nil {
		t.Fatal(err)
	}
	_, err = decoder.DecodeWithoutPermissions(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/?sort=recent", http.NoBody))
	if err == nil || !strings.Contains(err.Error(), "cannot be sorted by") {
		t.Errorf("DecodeWithoutPermissions() error = %v, want the unsortable refusal naming the field", err)
	}
}
