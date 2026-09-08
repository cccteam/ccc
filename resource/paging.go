package resource

import "strconv"

// Paging is a resource's declared paging contract: the order its list takes when
// the request states none, and the page sizes the decoder applies and admits. The
// generator reads it off the @order and @page annotations and hands it to the
// resource's query decoder (WithPaging); a resource that declares nothing takes the
// zero value, which the decoder reads as primary-key order, a page of
// DefaultPageSize, and no maximum.
type Paging struct {
	// Order is the order a list takes when the request carries no sort. The
	// primary key is appended so the order is total; a request's sort replaces
	// it for that request.
	Order []SortField
	// DefaultLimit is the page size a request without limit receives; 0 means
	// DefaultPageSize.
	DefaultLimit uint64
	// MaxLimit is the largest page a request may ask for; 0 means no maximum,
	// which also permits limit=all.
	MaxLimit uint64
}

// DefaultPageSize is the generator-wide page size a resource takes when it
// declares none.
const DefaultPageSize uint64 = 50

// allLimit is the limit value that asks for every row.
const allLimit = "all"

// pageRequest is the page a decoded request asked for. The decoder builds it
// from limit, cursor, and count against the resource's Paging; a hand-built
// QuerySet has none and reads exactly the limit its builder set.
type pageRequest struct {
	// size is the rows per page; the statement fetches one more to learn whether
	// a next page exists. Meaningless when all is set.
	size uint64
	// all asks for every row: no LIMIT, no cursor, no Link header.
	all bool
	// count asks for the total under the same WHERE, answered on a first page.
	count bool
	// token is the cursor exactly as the request carried it; "" on a first page.
	token string
}

// limitString is the page size as the cursor fingerprint spells it.
func (p *pageRequest) limitString() string {
	if p.all {
		return allLimit
	}

	return strconv.FormatUint(p.size, 10)
}
