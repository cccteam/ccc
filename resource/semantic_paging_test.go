package resource

// The semantic differential's paging walk: the cursor predicate, the sealed boundary
// keys and the database's NULL placement must walk a list page by page to exactly the
// order the reference sort gives it, forward and back, over every sort type the
// fixture's columns have and in both directions.

import (
	"context"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
)

// semanticPageSize is the page size of the walk: small enough that every sort crosses
// several pages and every NULL boundary falls at a page's edge somewhere.
const semanticPageSize = 7

// pagingSorts are the fixture columns the walk sorts by: one per comparison type, a
// NULLable one among them for each, and the key.
var pagingSorts = []accesstypes.Field{"Label", "Note", "Weight", "Pieces", "Ratio", "Density", "Price", "Fee", "Insured", "ShippedAt", "DeliveredAt", "ShipDate", "DueDate"}

// linkNext and linkPrev pick a relation's target out of a Link header.
var (
	linkNext = regexp.MustCompile(`<([^>]*)>; rel="next"`)
	linkPrev = regexp.MustCompile(`<([^>]*)>; rel="prev"`)
)

// TestSemanticPaging_postgres walks the lists over PostgreSQL.
func TestSemanticPaging_postgres(t *testing.T) {
	t.Parallel()

	runSemanticPaging(t, newPostgresSemanticDatabase(t))
}

// TestSemanticPaging walks the lists over the Spanner emulator.
func TestSemanticPaging(t *testing.T) {
	t.Parallel()

	runSemanticPaging(t, newSpannerSemanticDatabase(t))
}

// runSemanticPaging writes one world and walks it by every sort.
func runSemanticPaging(t *testing.T, db semanticDatabase) {
	t.Helper()

	ctx := t.Context()
	h := newSemanticHarness(t, db)
	rng := rand.New(rand.NewPCG(semanticSeed, 9001))
	world := genWorld(rng, h.pools, 9001)
	if err := db.insertWorld(ctx, world); err != nil {
		t.Fatalf("semanticDatabase.insertWorld() error = %v", err)
	}
	t.Cleanup(func() {
		if err := db.removeWorld(context.Background(), world); err != nil {
			t.Error(err)
		}
	})

	decoder, err := NewQueryDecoder[semanticParcel, semanticListRequest](h.listSet)
	if err != nil {
		t.Fatalf("NewQueryDecoder() error = %v", err)
	}
	decoder.collection = h.collection
	decoder.WithPaging(Paging{DefaultLimit: semanticPageSize}).WithCursorKey(differentialCursorKey(t))

	for _, field := range pagingSorts {
		for _, desc := range []bool{false, true} {
			sortValue := jsonName(field)
			if desc {
				sortValue += ":desc"
			}
			want := expectedPagingOrder(world, field, desc, db.dbType())

			pages, last := walkPages(t, db, decoder, world, "/?"+url.Values{sortParam: {sortValue}}.Encode())
			var got []string
			for _, page := range pages {
				got = append(got, page...)
			}
			if !slices.Equal(got, want) {
				t.Errorf("sort %s: the walk forward diverged from the reference order:\n got %v\nwant %v", sortValue, got, want)

				continue
			}
			if len(pages) < 3 {
				t.Errorf("sort %s: the walk crossed %d pages, want at least 3", sortValue, len(pages))
			}

			// Walking back from the last page's prev link visits the earlier pages in
			// the reverse order, each in the list's own order.
			if last.prev == "" {
				t.Errorf("sort %s: the last page has no prev link", sortValue)

				continue
			}
			back := walkBack(t, db, decoder, world, last.prev)
			wantBack := make([][]string, 0, len(pages)-1)
			for i := len(pages) - 2; i >= 0; i-- {
				wantBack = append(wantBack, pages[i])
			}
			if len(back) != len(wantBack) {
				t.Errorf("sort %s: the walk back crossed %d pages, want %d", sortValue, len(back), len(wantBack))

				continue
			}
			for i := range back {
				if !slices.Equal(back[i], wantBack[i]) {
					t.Errorf("sort %s: back page %d = %v, want %v", sortValue, i, back[i], wantBack[i])
				}
			}
		}
	}
}

// pageLinks are the relations a page's Link header carries.
type pageLinks struct {
	next, prev string
}

// walkPages reads the list from its first page forward to its last, returning each page's
// row ids and the last page's links.
func walkPages(t *testing.T, db semanticDatabase, decoder *QueryDecoder[semanticParcel, semanticListRequest], w *semanticWorld, target string) (pages [][]string, last pageLinks) {
	t.Helper()

	for range 200 {
		ids, links := readPage(t, db, decoder, w, target)
		pages = append(pages, ids)
		last = links
		if links.next == "" {
			return pages, last
		}
		target = links.next
	}
	t.Fatalf("the walk did not end after 200 pages")

	return nil, last
}

// walkBack reads the list from a prev link back to its first page, returning each page's
// row ids in the list's order.
func walkBack(t *testing.T, db semanticDatabase, decoder *QueryDecoder[semanticParcel, semanticListRequest], w *semanticWorld, target string) [][]string {
	t.Helper()

	var pages [][]string
	for range 200 {
		ids, links := readPage(t, db, decoder, w, target)
		pages = append(pages, ids)
		if links.prev == "" {
			return pages
		}
		target = links.prev
	}
	t.Fatalf("the walk back did not end after 200 pages")

	return nil
}

// readPage decodes the request, lists one page the way the generated handler does, and
// returns the page's row ids in the list's order with the links its headers carry.
func readPage(t *testing.T, db semanticDatabase, decoder *QueryDecoder[semanticParcel, semanticListRequest], w *semanticWorld, target string) ([]string, pageLinks) {
	t.Helper()

	ctx := t.Context()
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, target, http.NoBody)
	qSet, err := decoder.Decode(req, renderStubPermissions{}, w.scope())
	if err != nil {
		t.Fatalf("read %s: Decode() error = %v", target, err)
	}
	qSet.env = w.env()

	page := qSet.Page()
	var ids []string
	for row, err := range qSet.List(ctx, db.client()) {
		if err != nil {
			t.Fatalf("read %s: List() error = %v", target, err)
		}
		if !page.Add(row) {
			break
		}
		ids = append(ids, row.Data.ID.String())
	}
	if page.Reversed() {
		slices.Reverse(ids)
	}

	rec := httptest.NewRecorder()
	if err := page.WriteHeaders(rec, req); err != nil {
		t.Fatalf("read %s: WriteHeaders() error = %v", target, err)
	}
	link := rec.Header().Get(LinkHeader)
	var links pageLinks
	if m := linkNext.FindStringSubmatch(link); m != nil {
		links.next = m[1]
	}
	if m := linkPrev.FindStringSubmatch(link); m != nil {
		links.prev = m[1]
	}

	return ids, links
}

// expectedPagingOrder is the reference order of the world's parcels by the field: the
// database's NULL placement, then the key.
func expectedPagingOrder(w *semanticWorld, field accesstypes.Field, desc bool, dbType DBType) []string {
	parcels := slices.Clone(w.parcels)
	sort.SliceStable(parcels, func(i, j int) bool {
		a, b := semanticValue(semanticFieldValue(parcels[i], field)), semanticValue(semanticFieldValue(parcels[j], field))
		order := compareSemantic(a, b)
		if (a == nil) != (b == nil) && dbType == PostgresDBType {
			order = -order
		}
		if desc {
			order = -order
		}
		if order != 0 {
			return order < 0
		}

		return parcels[i].ID.String() < parcels[j].ID.String()
	})

	ids := make([]string, len(parcels))
	for i, p := range parcels {
		ids[i] = p.ID.String()
	}

	return ids
}

// jsonName is the wire name of a fixture field.
func jsonName(field accesstypes.Field) string {
	for _, f := range semanticFields {
		if f.field == field {
			return f.json
		}
	}

	panic("jsonName: unknown field " + string(field))
}
