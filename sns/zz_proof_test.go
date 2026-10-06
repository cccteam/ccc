package sns

import "testing"

// TestProofOfTheSeriesLoop fails on purpose: it proves on the pull request that a failed package
// does not stop the packages after it. It is removed before the pull request merges.
func TestProofOfTheSeriesLoop(t *testing.T) {
	t.Parallel()
	t.Fatal("deliberate failure proving that a failed package does not stop the others")
}
