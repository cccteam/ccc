package resource

// Struct-tag keys the resource package reads at runtime from generated request structs.
// The Resource Generator writes these tags (see resource/generation); application code
// never writes them by hand. Every key listed here must be documented in README.md —
// TestAnnotationsDocCoversRuntimeVocabulary enforces that, so register new keys in
// runtimeTagKeys below.
const (
	jsonTagKey = "json"
	// permTagKey's only legal value is permTagExempt: field permissions are enforced
	// structurally from the endpoint permission, and the tag survives solely as the
	// primary-key exemption marker. Any other value is rejected at Set construction.
	permTagKey        = "perm"
	immutableTagKey   = "immutable"
	indexTagKey       = "index"
	allowFilterTagKey = "allow_filter"
	piiTagKey         = "pii"
	// maskingTagKey's only legal value at runtime is maskingPositional: the
	// generator writes it onto a field the source declared masking:"positional",
	// and leaves it off a concealing field. Any other value is rejected at Set
	// construction, like a stale perm value.
	maskingTagKey = "masking"
	// sqltypeTagKey carries the column's declared Spanner type onto a patch request
	// struct field whose value the decoder sizes before anything is buffered (see
	// value_limits.go). The generator writes it only where the field's Go type and the
	// column type together have a rule; a value the runtime cannot pair with the field
	// is rejected at Set construction, like a stale perm value.
	sqltypeTagKey = "sqltype"
	// nullableTagKey marks a slice-typed patch request struct field whose column allows
	// NULL: a Go slice has one form, so the decoder cannot read the fact off the field's
	// type as it does off a pointer or a Null wrapper, and the generator writes it where
	// the schema says so (see nullable_fields.go). true is the only value written; a tag
	// the runtime cannot pair with a slice field is rejected at Set construction, like a
	// stale perm value.
	nullableTagKey = "nullable"
	// featureTagKey names the feature flag a request struct field is gated behind
	// (feature.go). The generator writes it onto every field the source annotated
	// @feature; while the flag is off the decoders answer the field as unknown and the
	// handlers leave it out of every response.
	featureTagKey = "feature"
	// formerlyTagKey carries a request struct field's former wire name (formerly.go).
	// The generator writes it onto every field the source annotated @formerly; a body,
	// a columns, sort or filter parameter naming the former name reaches the field
	// under its current one.
	formerlyTagKey = "formerly"
)

// maskingPositional is the masking tag value the generator writes for a
// positional field.
const maskingPositional = "positional"

// permTagExempt marks a primary-key field as exempt from field-level enforcement; its
// readability follows the resource-level grant.
const permTagExempt = "-"

// runtimeTagKeys registers every runtime-read struct-tag key for the README.md
// completeness test. Add every new tag-key constant here.
var runtimeTagKeys = []string{
	jsonTagKey,
	permTagKey,
	immutableTagKey,
	indexTagKey,
	allowFilterTagKey,
	piiTagKey,
	maskingTagKey,
	sqltypeTagKey,
	nullableTagKey,
	featureTagKey,
	formerlyTagKey,
}

// Reserved query-string parameter names consumed by QueryDecoder; they can never be used
// as filterable field names. Documented in README.md alongside the struct tags —
// register new parameters in reservedQueryParams below.
const (
	columnsParam      = "columns"
	filterParam       = "filter"
	sortParam         = "sort"
	limitParam        = "limit"
	cursorParam       = "cursor"
	countParam        = "count"
	capabilitiesParam = "capabilities"
	// offsetParam is reserved so a request still carrying it is refused with a
	// message naming the cursor as its replacement, never treated as a filter.
	offsetParam = "offset"
	// VersionParam is the live pages' version parameter: a client that holds a live
	// row or list appends it so the browser's cache keys the answer by the change it
	// last saw (the change document's timestamp, or a seed minted at login before any
	// change arrived). The decoders accept and skip it, and a list or read response
	// to a request carrying it is the one response that carries a Cache-Control
	// header (resource/live). It is exported because the live package names it to
	// the client library.
	VersionParam = "_v"
)

// reservedQueryParams registers every reserved query parameter for the README.md
// completeness test. Add every new parameter constant here.
var reservedQueryParams = []string{
	columnsParam,
	filterParam,
	sortParam,
	limitParam,
	cursorParam,
	countParam,
	capabilitiesParam,
	offsetParam,
	VersionParam,
}
