package accesstypes

// AttributeType is the closed comparison-type vocabulary of attribute
// bindings: what a condition literal must look like to compare against the
// attribute. Generation derives each attribute's type from its bound column
// and carries it in the application's Collection; MigrateRoles validates a
// grant condition's literals against it at deploy time. The renderer never
// compares values itself — the database is the one comparison engine — so the
// vocabulary stays coarse.
type AttributeType string

const (
	// AttributeTypeString compares against string literals (UUIDs included —
	// binary, case-sensitive string equality per the expression language).
	AttributeTypeString AttributeType = "string"
	// AttributeTypeNumber compares against numeric literals.
	AttributeTypeNumber AttributeType = "number"
	// AttributeTypeBool compares against TRUE and FALSE.
	AttributeTypeBool AttributeType = "bool"
	// AttributeTypeTimestamp compares against RFC 3339 timestamp strings and now.
	AttributeTypeTimestamp AttributeType = "timestamp"
	// AttributeTypeDate compares against date strings (YYYY-MM-DD).
	AttributeTypeDate AttributeType = "date"
)

// ValidAttributeType reports whether t is in the vocabulary.
func ValidAttributeType(t AttributeType) bool {
	switch t {
	case AttributeTypeString, AttributeTypeNumber, AttributeTypeBool, AttributeTypeTimestamp, AttributeTypeDate:
		return true
	default:
		return false
	}
}
