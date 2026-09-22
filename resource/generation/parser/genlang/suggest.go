package genlang

// Suggest returns the candidate closest to word, when one is close enough to have been
// meant: the did-you-mean the scanner offers for a misspelled keyword, offered to
// callers validating other vocabularies (struct-tag values) so every refusal reads the
// same. Candidates are compared in order, so a tie goes to the earlier one.
func Suggest(word string, candidates []string) (string, bool) {
	var (
		best      string
		bestScore float64
	)
	for _, candidate := range candidates {
		// Calculating a similarity score is expensive, so only candidates of nearly
		// the same length are scored.
		if v := len(word) - len(candidate); v < -2 || v > 2 {
			continue
		}
		if score := similarity(word, candidate); score > bestScore && score > 0.65 {
			best = candidate
			bestScore = score
		}
	}

	return best, best != ""
}
