package resource

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
