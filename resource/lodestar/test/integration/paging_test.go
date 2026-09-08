package integration

// This suite covers the paging contract over the demo world: the default page and its
// Link header, a walk forward and back through cursors with the declared order and
// with a requested sort, Total-Count on the first page, limit=all, and the refusals
// — a cursor carried to another sector or another filter, and a sort on a field the
// caller cannot read unconditionally. Consignments (three seeded in Anvil) walk one
// row at a time; Missions declare @order(Deadline asc).

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
)

// linkRelations parses a Link header into rel → URL.
func linkRelations(t *testing.T, header string) map[string]string {
	t.Helper()

	rels := make(map[string]string)
	if header == "" {
		return rels
	}
	for part := range strings.SplitSeq(header, ", ") {
		end := strings.Index(part, ">")
		relStart := strings.Index(part, `rel="`)
		if !strings.HasPrefix(part, "<") || end < 0 || relStart < 0 {
			t.Fatalf("malformed Link relation %q in %q", part, header)
		}
		rel := strings.TrimSuffix(part[relStart+len(`rel="`):], `"`)
		rels[rel] = part[1:end]
	}

	return rels
}

func TestPaging_walk(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}

	listGrants := grants{accesstypes.List: withFields("Consignments", "bondCode", "mass", "expiresOn")}
	testApp := newTestApp(db, listGrants)

	// Page one: the earliest expiry, with a next relation and no prev.
	first := doRequestRecorded(t, testApp, http.MethodGet, sectorPath(anvil, "consignments?sort=expiresOn&limit=1&count=true"))
	assertStatus(t, first.Code, http.StatusOK, first.Body.Bytes())
	if got := first.Header().Get(resource.TotalCountHeader); got != "3" {
		t.Errorf("Total-Count = %q, want 3", got)
	}
	rows := decodeRows(t, first.Body.Bytes())
	if len(rows) != 1 || rows[0]["bondCode"] != "BND-ANV-0002" {
		t.Fatalf("page 1 = %v, want BND-ANV-0002 alone", rows)
	}
	rels := linkRelations(t, first.Header().Get(resource.LinkHeader))
	if _, ok := rels["prev"]; ok {
		t.Errorf("page 1 carries a prev relation: %v", rels)
	}
	next, ok := rels["next"]
	if !ok {
		t.Fatalf("page 1 carries no next relation: %q", first.Header().Get(resource.LinkHeader))
	}
	if strings.Contains(next, "count=") {
		t.Errorf("next URL %q carries count; later pages carry no count", next)
	}

	// Page two, followed exactly as given: the middle row, prev and next.
	second := doRequestRecorded(t, testApp, http.MethodGet, next)
	assertStatus(t, second.Code, http.StatusOK, second.Body.Bytes())
	if got := second.Header().Get(resource.TotalCountHeader); got != "" {
		t.Errorf("page 2 Total-Count = %q, want none", got)
	}
	rows = decodeRows(t, second.Body.Bytes())
	if len(rows) != 1 || rows[0]["bondCode"] != "BND-ANV-0001" {
		t.Fatalf("page 2 = %v, want BND-ANV-0001 alone", rows)
	}
	rels = linkRelations(t, second.Header().Get(resource.LinkHeader))
	prev, hasPrev := rels["prev"]
	next, hasNext := rels["next"]
	if !hasPrev || !hasNext {
		t.Fatalf("page 2 relations = %v, want prev and next", rels)
	}

	// Page three: the last row, prev only.
	third := doRequestRecorded(t, testApp, http.MethodGet, next)
	assertStatus(t, third.Code, http.StatusOK, third.Body.Bytes())
	rows = decodeRows(t, third.Body.Bytes())
	if len(rows) != 1 || rows[0]["bondCode"] != "BND-ANV-0003" {
		t.Fatalf("page 3 = %v, want BND-ANV-0003 alone", rows)
	}
	rels = linkRelations(t, third.Header().Get(resource.LinkHeader))
	if _, ok := rels["next"]; ok {
		t.Errorf("page 3 carries a next relation: %v", rels)
	}
	if _, ok := rels["prev"]; !ok {
		t.Errorf("page 3 carries no prev relation: %v", rels)
	}

	// Walking back from page two lands on page one, in the list's order, with a
	// next relation and no prev.
	back := doRequestRecorded(t, testApp, http.MethodGet, prev)
	assertStatus(t, back.Code, http.StatusOK, back.Body.Bytes())
	rows = decodeRows(t, back.Body.Bytes())
	if len(rows) != 1 || rows[0]["bondCode"] != "BND-ANV-0002" {
		t.Fatalf("walking back = %v, want BND-ANV-0002 alone", rows)
	}
	rels = linkRelations(t, back.Header().Get(resource.LinkHeader))
	if _, ok := rels["prev"]; ok {
		t.Errorf("the first page reached backwards carries a prev relation: %v", rels)
	}
	if _, ok := rels["next"]; !ok {
		t.Errorf("the first page reached backwards carries no next relation: %v", rels)
	}

	// Nothing readable rides in the URL: the cursor is a sealed token.
	u, err := url.Parse(next)
	if err != nil {
		t.Fatal(err)
	}
	if token := u.Query().Get("cursor"); !strings.HasPrefix(token, "v4.local.") || strings.Contains(token, "BND-") {
		t.Errorf("cursor = %q, want a sealed token carrying nothing readable", token)
	}
}

func TestPaging_contract(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}

	listGrants := grants{accesstypes.List: append(
		withFields("Consignments", "bondCode", "mass", "expiresOn"),
		withFields("Missions", "title", "deadline", "hazard")...,
	)}
	testApp := newTestApp(db, listGrants)

	// A genuine cursor from Anvil's walk, to present elsewhere.
	first := doRequestRecorded(t, testApp, http.MethodGet, sectorPath(anvil, "consignments?sort=expiresOn&limit=1"))
	assertStatus(t, first.Code, http.StatusOK, first.Body.Bytes())
	anvilNext := linkRelations(t, first.Header().Get(resource.LinkHeader))["next"]
	anvilCursor := func(t *testing.T) string {
		t.Helper()

		u, err := url.Parse(anvilNext)
		if err != nil {
			t.Fatal(err)
		}

		return u.Query().Get("cursor")
	}

	tests := []struct {
		name       string
		target     func(t *testing.T) string
		wantStatus int
		wantRows   int
		wantLink   bool
		wantMore   string
		wantTotal  string
	}{
		{
			name:       "the declared order pages a sort-less request",
			target:     func(*testing.T) string { return sectorPath(anvil, "missions?limit=3") },
			wantStatus: http.StatusOK,
			wantRows:   3,
			wantLink:   true,
		},
		{
			name:       "a default page on a primary-key-only order signals more rows without a cursor",
			target:     func(*testing.T) string { return sectorPath(anvil, "consignments?limit=2") },
			wantStatus: http.StatusOK,
			wantRows:   2,
			wantMore:   "true",
		},
		{
			name:       "a primary-key-only order that fits the page signals nothing",
			target:     func(*testing.T) string { return sectorPath(anvil, "consignments?limit=3") },
			wantStatus: http.StatusOK,
			wantRows:   3,
		},
		{
			name:       "limit=all returns every row with no Link header",
			target:     func(*testing.T) string { return sectorPath(anvil, "consignments?sort=expiresOn&limit=all") },
			wantStatus: http.StatusOK,
			wantRows:   3,
		},
		{
			name: "count=true answers the total under the same filter",
			target: func(*testing.T) string {
				return sectorPath(anvil, "consignments?sort=expiresOn&filter=bondCode:in:(BND-ANV-0001,BND-ANV-0003)&limit=1&count=true")
			},
			wantStatus: http.StatusOK,
			wantRows:   1,
			wantLink:   true,
			wantTotal:  "2",
		},
		{
			name: "a cursor carried to another sector is refused",
			target: func(t *testing.T) string {
				return sectorPath(bastion, "consignments?sort=expiresOn&limit=1&cursor="+url.QueryEscape(anvilCursor(t)))
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "a cursor presented with a different filter is refused",
			target: func(t *testing.T) string {
				return sectorPath(anvil, "consignments?sort=expiresOn&limit=1&filter=bondCode:eq:BND-ANV-0001&cursor="+url.QueryEscape(anvilCursor(t)))
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "a cursor presented with a different page size is refused",
			target: func(t *testing.T) string {
				return sectorPath(anvil, "consignments?sort=expiresOn&limit=2&cursor="+url.QueryEscape(anvilCursor(t)))
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "an altered cursor is refused",
			target: func(t *testing.T) string {
				return sectorPath(anvil, "consignments?sort=expiresOn&limit=1&cursor="+url.QueryEscape(anvilCursor(t)+"x"))
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "a sort on a field the caller cannot read is refused",
			target:     func(*testing.T) string { return sectorPath(anvil, "consignments?sort=description") },
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "offset is refused",
			target:     func(*testing.T) string { return sectorPath(anvil, "consignments?sort=expiresOn&offset=1") },
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rr := doRequestRecorded(t, testApp, http.MethodGet, tt.target(t))
			assertStatus(t, rr.Code, tt.wantStatus, rr.Body.Bytes())
			if tt.wantStatus != http.StatusOK {
				return
			}
			if rows := decodeRows(t, rr.Body.Bytes()); len(rows) != tt.wantRows {
				t.Errorf("row count = %d, want %d: %s", len(rows), tt.wantRows, rr.Body.Bytes())
			}
			if hasLink := rr.Header().Get(resource.LinkHeader) != ""; hasLink != tt.wantLink {
				t.Errorf("Link header present = %v, want %v: %q", hasLink, tt.wantLink, rr.Header().Get(resource.LinkHeader))
			}
			if got := rr.Header().Get(resource.PageMoreHeader); got != tt.wantMore {
				t.Errorf("Page-More = %q, want %q", got, tt.wantMore)
			}
			if got := rr.Header().Get(resource.TotalCountHeader); got != tt.wantTotal {
				t.Errorf("Total-Count = %q, want %q", got, tt.wantTotal)
			}
		})
	}
}
