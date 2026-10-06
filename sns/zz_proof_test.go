package sns

import "testing"

// TestProofOfTheSeriesLoop passes: with the failing one in cache it proves on the pull request that the
// packages after a failed one still run. It is removed before the pull request merges.
func TestProofOfTheSeriesLoop(t *testing.T) {
	t.Parallel()
}
