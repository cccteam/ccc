package generation

// Struct-tag keys the generator reads from source structs (resource, virtual, computed,
// and RPC structs). Every key listed here must be documented in README.md —
// TestAnnotationsDocCoversGeneratorVocabulary enforces that, so register new keys in
// sourceStructTagKeys below.
const (
	spannerTagKey = "spanner"
	// permTagKey is no longer author vocabulary: field permissions are enforced
	// structurally from the endpoint permission, and a perm tag on a source struct is a
	// generation error (validateNoPermTags). The generator still writes this key into
	// list/read request structs as the perm:"-" primary-key exemption marker.
	permTagKey               = "perm"
	conditionsTagKey         = "conditions"
	defaultCreateFnTagKey    = "default_create_fn"
	outputOnlyUpdateFnTagKey = "output_only_update_fn"
	allowFilterTagKey        = "allow_filter"
	indexTagKey              = "index"
	uniqueIndexTagKey        = "uniqueindex"
	maskingTagKey            = "masking"
)

// sourceStructTagKeys registers every author-written struct-tag key for the
// README.md completeness test. Add every new tag-key constant here.
var sourceStructTagKeys = []string{
	spannerTagKey,
	conditionsTagKey,
	defaultCreateFnTagKey,
	outputOnlyUpdateFnTagKey,
	allowFilterTagKey,
	indexTagKey,
	uniqueIndexTagKey,
	maskingTagKey,
}

// Values recognized in a masking tag: how a field's masked cells meet a sort or a
// filter. concealing is the default and says so; positional opts the field into
// sorting and filtering on the real column while the cell stays hidden — register new
// values in maskingValues below.
const (
	maskingPositional = "positional"
	maskingConcealing = "concealing"
)

// maskingValues registers every recognized masking value, for the refusal's
// suggestion and the README completeness test.
var maskingValues = []string{maskingPositional, maskingConcealing}

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
	maskingOutTagKey   = "masking"
	// sqltypeOutTagKey carries a column's declared Spanner type onto a patch request
	// struct field the decoder sizes (resourceField.SqltypeTag).
	sqltypeOutTagKey = "sqltype"
)
