package generation

// Struct-tag keys the generator reads from source structs (resource, virtual, computed,
// and RPC structs). Every key listed here must be documented in README.md —
// TestAnnotationsDocCoversGeneratorVocabulary enforces that, so register new keys in
// sourceStructTagKeys below.
const (
	spannerTagKey            = "spanner"
	permTagKey               = "perm"
	conditionsTagKey         = "conditions"
	defaultCreateFnTagKey    = "default_create_fn"
	outputOnlyUpdateFnTagKey = "output_only_update_fn"
	allowFilterTagKey        = "allow_filter"
	indexTagKey              = "index"
	uniqueIndexTagKey        = "uniqueindex"
	enumeratedTagKey         = "enumerated"
)

// sourceStructTagKeys registers every author-written struct-tag key for the
// README.md completeness test. Add every new tag-key constant here.
var sourceStructTagKeys = []string{
	spannerTagKey,
	permTagKey,
	conditionsTagKey,
	defaultCreateFnTagKey,
	outputOnlyUpdateFnTagKey,
	allowFilterTagKey,
	indexTagKey,
	uniqueIndexTagKey,
	enumeratedTagKey,
}

// Values recognized inside a conditions tag's comma-separated list — register new values
// in conditionValues below.
const (
	immutableCondition  = "immutable"
	piiCondition        = "pii"
	inputOnlyCondition  = "input_only"
	outputOnlyCondition = "output_only"
)

// conditionValues registers every recognized conditions value for the README.md
// completeness test. Add every new condition constant here.
var conditionValues = []string{
	immutableCondition,
	piiCondition,
	inputOnlyCondition,
	outputOnlyCondition,
}

// Struct-tag keys the generator writes into generated request structs, read back at
// runtime by the resource package (see resource/tags.go for the runtime side).
const (
	jsonTagKey         = "json"
	immutableOutTagKey = "immutable"
	piiOutTagKey       = "pii"
)
