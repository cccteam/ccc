package resource

import (
	"context"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"

	"github.com/go-playground/errors/v5"
)

// Response headers a paged list carries. Link (RFC 8288) positions the walk with a
// complete URL per relation that exists; Total-Count answers count=true on a
// first page; Page-More marks a list served in primary-key order whose rows did
// not fit the page, where no cursor is issued and paging further requires a sort.
const (
	LinkHeader       = "Link"
	TotalCountHeader = "Total-Count"
	PageMoreHeader   = "Page-More"
)

// The Link relations a page carries.
const (
	relNext = "next"
	relPrev = "prev"
)

// Page collects one page of a decoded list on the generated handler's behalf. The
// statement fetches one row past the page size; Add keeps the page's rows and
// reports the extra one, which tells the page a next page exists and is never
// encoded. WriteHeaders then writes the Link, Total-Count, and Page-More headers
// from the rows it saw, before the handler encodes the body.
type Page[Resource Resourcer] struct {
	qSet  *QuerySet[Resource]
	first *Resource
	last  *Resource
	kept  uint64
	more  bool
	total *int64
	// rows holds the page's rows when the QuerySet collected them itself
	// (Collect, for a computed resource); a table handler encodes as it adds.
	rows []*Resource
}

// Rows returns the page's rows in read order when the QuerySet collected them
// (Collect); the handler reverses them when Reversed reports a backward walk.
func (p *Page[Resource]) Rows() []*Resource {
	return p.rows
}

// Page returns the collector for the page this QuerySet was decoded to read.
func (q *QuerySet[Resource]) Page() *Page[Resource] {
	return &Page[Resource]{qSet: q}
}

// Count runs the total query when the request asked for one (count=true) and
// nothing otherwise. The handler calls it before listing, so the total and the
// page are read under the same request.
func (p *Page[Resource]) Count(ctx context.Context, txn ReadOnlyTransaction) error {
	if p.qSet.page == nil || !p.qSet.page.count {
		return nil
	}

	total, err := p.qSet.Count(ctx, txn)
	if err != nil {
		return err
	}
	p.total = &total

	return nil
}

// Add records a row the statement yielded and reports whether it belongs to the
// page. The row past the page size is the more-exists signal: Add answers false
// and the handler stops without encoding it.
func (p *Page[Resource]) Add(row *Resource) bool {
	if pg := p.qSet.page; pg != nil && !pg.all && p.kept == pg.size {
		p.more = true

		return false
	}
	if p.first == nil {
		p.first = row
	}
	p.last = row
	p.kept++

	return true
}

// Reversed reports whether the page was read backwards, walking to the previous
// page, so the handler reverses the encoded rows into the list's order.
func (p *Page[Resource]) Reversed() bool {
	return p.qSet.cursor != nil && p.qSet.cursor.Direction == pagePrev
}

// WriteHeaders writes the page's headers onto the response before the body:
// Total-Count when a count was asked for, and Link with a complete URL per
// relation that exists — the first page has no prev, the last page no next.
// The URLs carry the request's own query with the cursor set and count removed,
// so a client follows them as given and never assembles one. A list in
// primary-key order with no sort issues no cursor; Page-More marks its
// truncation instead. A hand-built QuerySet and limit=all write nothing.
func (p *Page[Resource]) WriteHeaders(w http.ResponseWriter, r *http.Request) error {
	pg := p.qSet.page
	if pg == nil || pg.all {
		return nil
	}
	if p.total != nil {
		w.Header().Set(TotalCountHeader, strconv.FormatInt(*p.total, 10))
	}
	if !p.qSet.issuesCursors() {
		if p.more {
			w.Header().Set(PageMoreHeader, trueStr)
		}

		return nil
	}
	if p.kept == 0 {
		return nil
	}

	// In read order the first row added is the boundary toward prev and the last
	// toward next; a page read backwards swaps them.
	first, last := p.first, p.last
	if p.Reversed() {
		first, last = last, first
	}

	var links []string
	if p.hasNext() {
		link, err := p.link(r, pageNext, last)
		if err != nil {
			return err
		}
		links = append(links, link)
	}
	if p.hasPrev() {
		link, err := p.link(r, pagePrev, first)
		if err != nil {
			return err
		}
		links = append(links, link)
	}
	if len(links) > 0 {
		w.Header().Set(LinkHeader, strings.Join(links, ", "))
	}

	return nil
}

// hasNext: a first page or a forward page has a next page when the extra row
// came; a page reached by walking back always has one, the page it came from.
func (p *Page[Resource]) hasNext() bool {
	if c := p.qSet.cursor; c != nil && c.Direction == pagePrev {
		return true
	}

	return p.more
}

// hasPrev: a page reached by walking forward always has a previous page, the
// page it came from; a page reached by walking back has one when the extra row
// came; a first page has none.
func (p *Page[Resource]) hasPrev() bool {
	c := p.qSet.cursor
	if c == nil {
		return false
	}
	if c.Direction == pageNext {
		return true
	}

	return p.more
}

// link renders one Link relation: the request URL with the cursor for the
// boundary row set and count removed.
func (p *Page[Resource]) link(r *http.Request, direction pageDirection, boundary *Resource) (string, error) {
	if p.qSet.cursorKey == nil {
		return "", errors.New("resource.Page: the list pages but the decoder has no cursor key; pass resource.NewCursorKey(cookieKey) to WithCursorKey")
	}

	keys, err := p.qSet.boundaryKeys(boundary)
	if err != nil {
		return "", err
	}
	token, err := p.qSet.cursorKey.seal(cursor{Query: p.qSet.queryHash(p.qSet.scope), Direction: direction, Keys: keys})
	if err != nil {
		return "", errors.Wrap(err, "CursorKey.seal()")
	}

	query := r.URL.Query()
	query.Set(cursorParam, token)
	query.Del(countParam)
	target := url.URL{Path: r.URL.Path, RawQuery: query.Encode()}

	rel := relNext
	if direction == pagePrev {
		rel = relPrev
	}

	return "<" + target.String() + `>; rel="` + rel + `"`, nil
}

// boundaryKeys encodes a boundary row's values in the list's total order, the
// primary key last, at full precision.
func (q *QuerySet[Resource]) boundaryKeys(row *Resource) ([]*string, error) {
	order := q.Order()
	value := reflect.ValueOf(row).Elem()
	keys := make([]*string, 0, len(order))
	for _, sf := range order {
		field := value.FieldByName(sf.Field)
		if !field.IsValid() {
			return nil, errors.Newf("resource.Page: sort field %s is not a field of %T", sf.Field, *row)
		}
		text, err := cursorText(field)
		if err != nil {
			return nil, errors.Wrapf(err, "sort field %s", sf.Field)
		}
		keys = append(keys, text)
	}

	return keys, nil
}
