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
