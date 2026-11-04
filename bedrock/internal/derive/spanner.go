// spanner.go is Google's rule for the databases a Spanner instance holds, which bedrock
// counts against before a stack asks an instance for one more: bedrock org register
// against the shared instance the placement declares, and the stack's plan against the
// instance the environment's databases live on.

package derive

// Google's rule for the databases an instance holds (Spanner's quotas and limits,
// 2026-10): ten for every hundred processing units of an instance below a node, and a
// hundred from a node, a thousand processing units, on.
const (
	spannerNodeUnits           = 1000
	spannerHundredUnits        = 100
	spannerDatabasesPerHundred = 10
	spannerDatabasesFromNodeOn = 100
)

// SpannerDatabaseCapacity is the databases an instance of the processing units holds.
func SpannerDatabaseCapacity(processingUnits int) int {
	if processingUnits >= spannerNodeUnits {
		return spannerDatabasesFromNodeOn
	}

	return processingUnits / spannerHundredUnits * spannerDatabasesPerHundred
}

// SpannerCapacityRule says, for a sentence, why an instance of the processing units
// holds what it holds: the rule below a node, or the flat hundred from a node on.
func SpannerCapacityRule(processingUnits int) string {
	if processingUnits >= spannerNodeUnits {
		return "a hundred from a node on"
	}

	return "ten for every hundred below a node"
}

// SpannerRaise is the way out through the instance's size, for a refusal that ends
// with the other way out, destroying an application: below a node, raising the setting
// holds more databases; from a node on no raise does, and the sentence says so.
func SpannerRaise(setting string, processingUnits int) string {
	if processingUnits >= spannerNodeUnits {
		return "no raise of " + setting + " adds a database from a node on, so"
	}

	return "raise " + setting + " (every hundred units below a node hold ten more databases; a node, 1000 units, holds a hundred), or"
}
