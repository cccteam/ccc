package resource

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
)

// pageRows builds a QuerySet the way the decoder does for a paged list over the
// cursor fixture, with the given page and cursor, and runs the given rows through
// a Page.
func pageOver(t *testing.T, sort, defaultOrder []SortField, page pageRequest, cur *cursor, ids ...string) (*Page[cursorTestResource], *QuerySet[cursorTestResource]) {
	t.Helper()

	qSet := NewQuerySet(NewMetadata[cursorTestResource]())
	qSet.keyFields = []accesstypes.Field{"ID"}
	qSet.defaultOrder = defaultOrder
	qSet.SetSortFields(sort)
	qSet.page = &page
	qSet.cursor = cur
	qSet.cursorKey = mustCursorKey(t, testCookieKey)
	qSet.scope = testScope

	p := qSet.Page()
	for _, id := range ids {
		row := &cursorTestResource{ID: id, Hazard: int64(len(id))}
		if !p.Add(row) {
			break
		}
	}

	return p, qSet
}

// TestPage_headers pins the headers a page writes from the rows it saw: the
// more-exists row past the page size, the prev and next relations by direction,
// Total-Count on request, Page-More on a primary-key-only order, and nothing for
// limit=all.
func TestPage_headers(t *testing.T) {
	t.Parallel()

	hazard := []SortField{{Field: "Hazard", Direction: SortDescending}}

	tests := []struct {
		name         string
		sort         []SortField
		defaultOrder []SortField
		page         pageRequest
		cursor       *cursor
		total        *int64
		ids          []string
		wantKept     uint64
		wantRels     []string
		wantTotal    string
		wantMore     string
		wantNoLink   bool
	}{
		{
			name:     "first page with more rows: next only",
			sort:     hazard,
			page:     pageRequest{size: 2},
			ids:      []string{"a", "bb", "ccc"},
			wantKept: 2,
			wantRels: []string{"next"},
		},
		{
			name:       "first page that fits: no links",
			sort:       hazard,
			page:       pageRequest{size: 3},
			ids:        []string{"a", "bb", "ccc"},
			wantKept:   3,
			wantNoLink: true,
		},
		{
			name:     "forward page with more rows: prev and next",
			sort:     hazard,
			page:     pageRequest{size: 2},
			cursor:   &cursor{Direction: pageNext, Keys: []*string{strPtr("9"), strPtr("z")}},
			ids:      []string{"a", "bb", "ccc"},
			wantKept: 2,
			wantRels: []string{"next", "prev"},
		},
		{
			name:     "last forward page: prev only",
			sort:     hazard,
			page:     pageRequest{size: 2},
			cursor:   &cursor{Direction: pageNext, Keys: []*string{strPtr("9"), strPtr("z")}},
			ids:      []string{"a", "bb"},
			wantKept: 2,
			wantRels: []string{"prev"},
		},
		{
			name:     "backward page with more rows: next and prev",
			sort:     hazard,
			page:     pageRequest{size: 2},
			cursor:   &cursor{Direction: pagePrev, Keys: []*string{strPtr("1"), strPtr("a")}},
			ids:      []string{"a", "bb", "ccc"},
			wantKept: 2,
			wantRels: []string{"next", "prev"},
		},
		{
			name:     "backward page reaching the start: next only",
			sort:     hazard,
			page:     pageRequest{size: 2},
			cursor:   &cursor{Direction: pagePrev, Keys: []*string{strPtr("1"), strPtr("a")}},
			ids:      []string{"a", "bb"},
			wantKept: 2,
			wantRels: []string{"next"},
		},
		{
			name:         "the declared order pages a sort-less request",
			defaultOrder: hazard,
			page:         pageRequest{size: 1},
			ids:          []string{"a", "bb"},
			wantKept:     1,
			wantRels:     []string{"next"},
		},
		{
			name:       "primary-key order issues no cursor, only the more signal",
			page:       pageRequest{size: 2},
			ids:        []string{"a", "bb", "ccc"},
			wantKept:   2,
			wantNoLink: true,
			wantMore:   "true",
		},
		{
			name:       "primary-key order that fits: nothing",
			page:       pageRequest{size: 3},
			ids:        []string{"a", "bb"},
			wantKept:   2,
			wantNoLink: true,
		},
		{
			name:      "count answers in Total-Count",
			sort:      hazard,
			page:      pageRequest{size: 2, count: true},
			total:     new(int64(41)),
			ids:       []string{"a", "bb", "ccc"},
			wantKept:  2,
			wantRels:  []string{"next"},
			wantTotal: "41",
		},
		{
			name:       "limit=all keeps every row and writes nothing",
			sort:       hazard,
			page:       pageRequest{all: true},
			ids:        []string{"a", "bb", "ccc", "dddd"},
			wantKept:   4,
			wantNoLink: true,
		},
		{
			name:       "an empty page writes no links",
			sort:       hazard,
			page:       pageRequest{size: 2},
			cursor:     &cursor{Direction: pageNext, Keys: []*string{strPtr("9"), strPtr("z")}},
			wantNoLink: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			page, _ := pageOver(t, tt.sort, tt.defaultOrder, tt.page, tt.cursor, tt.ids...)
			page.total = tt.total
			if page.kept != tt.wantKept {
				t.Errorf("kept = %d, want %d", page.kept, tt.wantKept)
			}

			w := httptest.NewRecorder()
			r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/things?sort=hazard:desc&limit=2&count=true", http.NoBody)
			if err := page.WriteHeaders(w, r); err != nil {
				t.Fatalf("WriteHeaders() error = %v", err)
			}

			if got := w.Header().Get(TotalCountHeader); got != tt.wantTotal {
				t.Errorf("Total-Count = %q, want %q", got, tt.wantTotal)
			}
			if got := w.Header().Get(PageMoreHeader); got != tt.wantMore {
				t.Errorf("Page-More = %q, want %q", got, tt.wantMore)
			}

			link := w.Header().Get(LinkHeader)
			if tt.wantNoLink {
				if link != "" {
					t.Errorf("Link = %q, want none", link)
				}

				return
			}
			for _, rel := range tt.wantRels {
				if !strings.Contains(link, `; rel="`+rel+`"`) {
					t.Errorf("Link = %q, want a %s relation", link, rel)
				}
			}
			if got := strings.Count(link, "rel="); got != len(tt.wantRels) {
				t.Errorf("Link = %q carries %d relations, want %d", link, got, len(tt.wantRels))
			}
			// Every relation is the request URL with the cursor set and count removed.
			for _, part := range strings.Split(link, ", ") {
				raw := part[1:strings.Index(part, ">")]
				u, err := url.Parse(raw)
				if err != nil {
					t.Fatalf("Link URL %q: %v", raw, err)
				}
				if u.Path != "/api/things" {
					t.Errorf("Link path = %q, want /api/things", u.Path)
				}
				q := u.Query()
				if q.Get("sort") != "hazard:desc" || q.Get("limit") != "2" {
					t.Errorf("Link query = %q, want the request's sort and limit", u.RawQuery)
				}
				if q.Has("count") {
					t.Errorf("Link query = %q carries count; later pages carry no count", u.RawQuery)
				}
				if !strings.HasPrefix(q.Get("cursor"), "v4.local.") {
					t.Errorf("Link cursor = %q, want a sealed token", q.Get("cursor"))
				}
			}
		})
	}
}

// TestPage_linksRoundTrip pins that the cursor a page issues opens to the boundary
// row's values in the list's total order, keyed for the same query, and that a
// backward page swaps which row bounds which relation.
func TestPage_linksRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		cursor   *cursor
		wantNext []string
		wantPrev []string
	}{
		{
			name:     "forward: the last row kept bounds next, the first bounds prev",
			cursor:   &cursor{Direction: pageNext, Keys: []*string{strPtr("9"), strPtr("z")}},
			wantNext: []string{"2", "bb"},
			wantPrev: []string{"1", "a"},
		},
		{
			name:     "backward: rows arrive reversed, so the first read bounds next",
			cursor:   &cursor{Direction: pagePrev, Keys: []*string{strPtr("0"), strPtr("0")}},
			wantNext: []string{"1", "a"},
			wantPrev: []string{"2", "bb"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sort := []SortField{{Field: "Hazard", Direction: SortDescending}}
			page, qSet := pageOver(t, sort, nil, pageRequest{size: 2}, tt.cursor, "a", "bb", "ccc")

			w := httptest.NewRecorder()
			r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/things?sort=hazard:desc&limit=2", http.NoBody)
			if err := page.WriteHeaders(w, r); err != nil {
				t.Fatalf("WriteHeaders() error = %v", err)
			}

			for _, part := range strings.Split(w.Header().Get(LinkHeader), ", ") {
				u, err := url.Parse(part[1:strings.Index(part, ">")])
				if err != nil {
					t.Fatal(err)
				}
				c, err := qSet.cursorKey.open(u.Query().Get("cursor"))
				if err != nil {
					t.Fatalf("open() error = %v", err)
				}
				if c.Query != qSet.queryHash(testScope) {
					t.Errorf("cursor query = %q, want this query's hash %q", c.Query, qSet.queryHash(testScope))
				}
				want := tt.wantNext
				if strings.Contains(part, `rel="prev"`) {
					want = tt.wantPrev
					if c.Direction != pagePrev {
						t.Errorf("prev cursor direction = %v", c.Direction)
					}
				} else if c.Direction != pageNext {
					t.Errorf("next cursor direction = %v", c.Direction)
				}
				got := make([]string, len(c.Keys))
				for i, k := range c.Keys {
					got[i] = deref(k)
				}
				if strings.Join(got, ",") != strings.Join(want, ",") {
					t.Errorf("%s keys = %v, want %v", part, got, want)
				}
			}
		})
	}
}

// TestQuerySet_countStmt pins the count statement: the same WHERE as the page
// query, no order, no page, no cursor.
func TestQuerySet_countStmt(t *testing.T) {
	t.Parallel()

	qSet := NewQuerySet(NewMetadata[cursorTestResource]())
	qSet.AddField("ID")
	qSet.keyFields = []accesstypes.Field{"ID"}
	qSet.SetSortFields([]SortField{{Field: "Hazard", Direction: SortDescending}})
	qSet.page = &pageRequest{size: 2, count: true}
	qSet.cursor = &cursor{Direction: pageNext, Keys: []*string{strPtr("3"), strPtr("x")}}
	qSet.SetWhereClause(NewIdent[int64]("Hazard", NewPartialQueryClause(), true).GreaterThan(1))

	stmt, err := qSet.countStmt(SpannerDBType)
	if err != nil {
		t.Fatalf("countStmt() error = %v", err)
	}
	got := strings.TrimSpace(collapseWhitespace.ReplaceAllString(stmt.SQL, " "))
	want := "SELECT COUNT(*) FROM cursorTestResources WHERE `Hazard` > @_p1"
	if got != want {
		t.Errorf("countStmt() SQL = %q, want %q", got, want)
	}
}
