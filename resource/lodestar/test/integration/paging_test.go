package integration

// This suite covers the paging contract over the demo world: the default page and its
// Link header, a walk forward and back through cursors with the declared order and
// with a requested sort, Total-Count on the first page, limit=all, and the refusals
// — a cursor carried to another sector or another filter, and a sort on a field the
// caller is denied. A sort or filter over a masked field runs over the visible
// projection. Consignments (three seeded in Anvil) walk one row at a time; Missions
// declare @order(Deadline asc).

import (
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"cloud.google.com/go/civil"
	"cloud.google.com/go/spanner"
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

// TestPaging_declaredMaximum pins the Missions declaration, @page(default: 25, max:
// 200): a page over the maximum is refused naming it, never clamped, and so is
// limit=all; a page within it is served.
func TestPaging_declaredMaximum(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}
	testApp := newTestApp(db, grants{accesstypes.List: withFields("Missions", "title", "deadline")})

	tests := []struct {
		name       string
		target     string
		wantStatus int
		wantRows   int
	}{
		{name: "a page within the maximum", target: sectorPath(anvil, "missions?limit=200"), wantStatus: http.StatusOK, wantRows: 8},
		{name: "a page over the maximum is refused", target: sectorPath(anvil, "missions?limit=201"), wantStatus: http.StatusBadRequest},
		{name: "limit=all is refused where a maximum is declared", target: sectorPath(anvil, "missions?limit=all"), wantStatus: http.StatusBadRequest},
		{name: "the declared default page", target: sectorPath(anvil, "missions"), wantStatus: http.StatusOK, wantRows: 8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rr := doRequestRecorded(t, testApp, http.MethodGet, tt.target)
			assertStatus(t, rr.Code, tt.wantStatus, rr.Body.Bytes())
			if tt.wantStatus == http.StatusOK {
				if rows := decodeRows(t, rr.Body.Bytes()); len(rows) != tt.wantRows {
					t.Errorf("row count = %d, want %d", len(rows), tt.wantRows)
				}
			}
		})
	}
}

// TestPaging_walkSurvivesWrites pins the property offset paging lacks: a row inserted
// before the walk's position, and one deleted before it, move nothing after the
// position. The walk over Anvil's consignments by expiry still sees each remaining
// row exactly once.
func TestPaging_walkSurvivesWrites(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}
	testApp := newTestApp(db, grants{accesstypes.List: withFields("Consignments", "bondCode", "expiresOn")})

	// Page one: BND-ANV-0002 (expires 2026-08-01).
	first := doRequestRecorded(t, testApp, http.MethodGet, sectorPath(anvil, "consignments?sort=expiresOn&limit=1"))
	assertStatus(t, first.Code, http.StatusOK, first.Body.Bytes())
	if rows := decodeRows(t, first.Body.Bytes()); len(rows) != 1 || rows[0]["bondCode"] != "BND-ANV-0002" {
		t.Fatalf("page 1 = %v", rows)
	}
	next := linkRelations(t, first.Header().Get(resource.LinkHeader))["next"]

	// Between pages: a consignment expiring before the position arrives, and the
	// row the position names is deleted.
	_, err = db.Client.Apply(ctx, []*spanner.Mutation{
		spanner.InsertMap("Consignments", map[string]any{
			"Id":          "b0000000-0000-4000-8000-00000000000a",
			"SectorId":    anvil,
			"ClientId":    clientHalvardID,
			"BondCode":    "BND-ANV-0009",
			"Description": "Arrived between pages",
			"Mass":        1.0,
			"ExpiresOn":   civil.Date{Year: 2026, Month: 1, Day: 1},
		}),
		spanner.Delete("Consignments", spanner.Key{consignmentDronesID}),
	})
	if err != nil {
		t.Fatalf("spanner.Client.Apply() error = %v", err)
	}

	// Page two is still the row after the position, not a repeat and not a skip.
	second := doRequestRecorded(t, testApp, http.MethodGet, next)
	assertStatus(t, second.Code, http.StatusOK, second.Body.Bytes())
	rows := decodeRows(t, second.Body.Bytes())
	if len(rows) != 1 || rows[0]["bondCode"] != "BND-ANV-0001" {
		t.Fatalf("page 2 after writes = %v, want BND-ANV-0001", rows)
	}
	third := doRequestRecorded(t, testApp, http.MethodGet, linkRelations(t, second.Header().Get(resource.LinkHeader))["next"])
	assertStatus(t, third.Code, http.StatusOK, third.Body.Bytes())
	if rows := decodeRows(t, third.Body.Bytes()); len(rows) != 1 || rows[0]["bondCode"] != "BND-ANV-0003" {
		t.Fatalf("page 3 after writes = %v, want BND-ANV-0003", rows)
	}
	// Walking back from page two reaches the inserted row: it sorts before the
	// deleted one did, and the walk sees the list as it is now.
	back := doRequestRecorded(t, testApp, http.MethodGet, linkRelations(t, second.Header().Get(resource.LinkHeader))["prev"])
	assertStatus(t, back.Code, http.StatusOK, back.Body.Bytes())
	if rows := decodeRows(t, back.Body.Bytes()); len(rows) != 1 || rows[0]["bondCode"] != "BND-ANV-0009" {
		t.Errorf("walking back after writes = %v, want the inserted BND-ANV-0009", rows)
	}
}

// TestPaging_sensitiveSortColumn pins that a PII sort column puts nothing readable
// in the Link header: the boundary value rides inside the sealed cursor.
func TestPaging_sensitiveSortColumn(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}
	testApp := newTestApp(db, grants{accesstypes.List: withFields("Clients", "name", "contactEmail")})

	rr := doRequestRecorded(t, testApp, http.MethodGet, "/api/clients?sort=contactEmail&limit=1")
	assertStatus(t, rr.Code, http.StatusOK, rr.Body.Bytes())
	rows := decodeRows(t, rr.Body.Bytes())
	if len(rows) != 1 {
		t.Fatalf("rows = %v, want one", rows)
	}
	email, _ := rows[0]["contactEmail"].(string)
	link := rr.Header().Get(resource.LinkHeader)
	if link == "" {
		t.Fatal("no Link header on a page with more rows")
	}
	if email == "" || strings.Contains(link, email) || strings.Contains(link, "@") || strings.Contains(link, "%40") {
		t.Errorf("Link = %q carries the boundary e-mail %q in the clear", link, email)
	}
}

// TestPaging_visibleProjection pins the visible-projection rule against the demo
// world. The archivist lists closed missions (completed, failed, stood down) and sees
// the fee only on a completed one, so the fee is masked on the failed and the
// stood-down rows: a sort by fee puts them in the NULL region, last ascending and
// first descending, and a fee filter matches them only through isnull. The
// archivist's title rides the row condition itself, so it sorts on the raw column;
// the cadet's every field rides one row condition and sorts freely; the
// quartermaster, granted no fee at all, is refused.
func TestPaging_visibleProjection(t *testing.T) {
	t.Parallel()

	_, h, _ := sharedWorld(t)

	const (
		completedPod = "80000000-0000-4000-8000-000000000005"
		failedTow    = "80000000-0000-4000-8000-000000000006"
		stoodDown    = "80000000-0000-4000-8000-000000000007"
	)

	tests := []struct {
		name       string
		user       accesstypes.User
		target     string
		wantStatus int
		wantOrder  []string
		wantIDs    []string
	}{
		{
			name: "the archivist's masked fees sort last ascending", user: "archivist",
			target: sectorPath(anvil, "missions?sort=fee"), wantStatus: http.StatusOK,
			wantOrder: []string{completedPod, failedTow, stoodDown},
		},
		{
			name: "and first descending", user: "archivist",
			target: sectorPath(anvil, "missions?sort=fee:desc"), wantStatus: http.StatusOK,
			wantOrder: []string{failedTow, stoodDown, completedPod},
		},
		{
			name: "isnull on the fee returns the masked rows", user: "archivist",
			target: sectorPath(anvil, "missions?filter=sectorId:eq:anvil,fee:isnull"), wantStatus: http.StatusOK,
			wantIDs: []string{failedTow, stoodDown},
		},
		{
			name: "isnotnull on the fee returns the visible row", user: "archivist",
			target: sectorPath(anvil, "missions?filter=sectorId:eq:anvil,fee:isnotnull"), wantStatus: http.StatusOK,
			wantIDs: []string{completedPod},
		},
		{
			name: "a field on the row condition itself sorts on the raw column", user: "archivist",
			target: sectorPath(anvil, "missions?sort=title"), wantStatus: http.StatusOK,
			wantOrder: []string{stoodDown, completedPod, failedTow},
		},
		{
			name: "the cadet pages the board sorted by any field", user: "cadet",
			target: sectorPath(anvil, "missions?sort=hazard:desc,title&limit=2"), wantStatus: http.StatusOK,
			wantOrder: []string{"80000000-0000-4000-8000-000000000008", "80000000-0000-4000-8000-000000000001"},
		},
		{
			name: "the quartermaster, granted no fee, is refused as a sort", user: "quartermaster",
			target: sectorPath(anvil, "missions?sort=fee"), wantStatus: http.StatusForbidden,
		},
		{
			name: "and as a filter", user: "quartermaster",
			target: sectorPath(anvil, "missions?filter=statusId:eq:open,fee:isnull"), wantStatus: http.StatusForbidden,
		},
		{name: "the marshal's unconditional fee sorts", user: "marshal", target: sectorPath(anvil, "missions?sort=fee"), wantStatus: http.StatusOK},
		{
			name: "a fee compares as a number: the fractional literal matches the seeded convoy fee", user: "marshal",
			target: sectorPath(anvil, "missions?filter=sectorId:eq:anvil,fee:eq:15000.00"), wantStatus: http.StatusOK,
			wantIDs: []string{"80000000-0000-4000-8000-000000000003"},
		},
		{
			name: "and a threshold only the pod clears", user: "marshal",
			target: sectorPath(anvil, "missions?filter=sectorId:eq:anvil,fee:gt:30000"), wantStatus: http.StatusOK,
			wantIDs: []string{completedPod},
		},
		{
			name: "a value that is not a number is refused at decode", user: "marshal",
			target: sectorPath(anvil, "missions?filter=sectorId:eq:anvil,fee:gt:abc"), wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status, body := doRequestAs(t, h, tt.user, http.MethodGet, tt.target, "")
			assertStatus(t, status, tt.wantStatus, body)
			if status != http.StatusOK {
				want := "cannot sort or filter on fee"
				if status == http.StatusBadRequest {
					want = "value 'abc' in condition 'fee:gt:abc' is not a valid decimal number"
				}
				if !strings.Contains(string(body), want) {
					t.Errorf("body = %s, want it to say %q", body, want)
				}

				return
			}
			rows := decodeRows(t, body)
			if tt.wantOrder != nil {
				got := make([]string, 0, len(rows))
				for _, row := range rows {
					id, _ := row["id"].(string)
					got = append(got, id)
				}
				if !slices.Equal(got, tt.wantOrder) {
					t.Errorf("order = %v, want %v", got, tt.wantOrder)
				}
			}
			if tt.wantIDs != nil {
				if got := idsOf(t, rows); !slices.Equal(got, tt.wantIDs) {
					t.Errorf("ids = %v, want %v", got, tt.wantIDs)
				}
			}
		})
	}
}
