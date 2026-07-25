package resource

// Struct-tag keys the resource package reads at runtime from generated request structs.
// The Resource Generator writes these tags (see resource/generation); application code
// never writes them by hand. Every key listed here must be documented in README.md —
// TestAnnotationsDocCoversRuntimeVocabulary enforces that, so register new keys in
// runtimeTagKeys below.
const (
	jsonTagKey        = "json"
	permTagKey        = "perm"
	immutableTagKey   = "immutable"
	indexTagKey       = "index"
	allowFilterTagKey = "allow_filter"
	piiTagKey         = "pii"
)

// runtimeTagKeys registers every runtime-read struct-tag key for the README.md
// completeness test. Add every new tag-key constant here.
var runtimeTagKeys = []string{
	jsonTagKey,
	permTagKey,
	immutableTagKey,
	indexTagKey,
	allowFilterTagKey,
	piiTagKey,
}

// Reserved query-string parameter names consumed by QueryDecoder; they can never be used
// as filterable field names. Documented in README.md alongside the struct tags —
// register new parameters in reservedQueryParams below.
const (
	columnsParam = "columns"
	filterParam  = "filter"
	sortParam    = "sort"
	limitParam   = "limit"
	offsetParam  = "offset"
)

// reservedQueryParams registers every reserved query parameter for the README.md
// completeness test. Add every new parameter constant here.
var reservedQueryParams = []string{
	columnsParam,
	filterParam,
	sortParam,
	limitParam,
	offsetParam,
}
