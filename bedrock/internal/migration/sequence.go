// sequence.go is the rule a migrations directory keeps, the one bedrock check and the
// pipeline's migration guard both apply: six-digit indexes, one up file per index, at
// most one down, contiguous from the lowest index present to the highest, so the migrate
// command applies every migration once and in order. It reads names only, so the guard
// can read the working tree together with the default branch's listing.

package migration

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"path"
	"sort"
	"strconv"
)

// Entry is one file of a migrations directory as the rule reads it: its name, and a note
// saying where it was found when that is not the tree being checked ("on master, not in
// this pull request").
type Entry struct {
	Name string
	Note string
}

// Problem is one way the entries break the rule: the file it is about (its name and
// note; empty for the directory as a whole) and what is wrong, the way a person reads it.
type Problem struct {
	File string
	Note string
	Text string
}

// Span is what a sound sequence holds: how many migrations (up files), and the lowest
// and highest index. Count is 0 for none.
type Span struct {
	Count, Low, High int
}

// Sequence reads the entries by the rule and answers every problem, in index order: a
// .sql file that is not a migration, an index with two up files or two down files or a
// down file without an up, and each gap. Files that are not .sql are not the rule's.
func Sequence(entries []Entry) ([]Problem, Span) {
	var problems []Problem
	ups := map[int][]Entry{}
	downs := map[int][]Entry{}
	for _, e := range entries {
		if path.Ext(e.Name) != ".sql" {
			continue
		}
		m := NameRE.FindStringSubmatch(e.Name)
		if m == nil {
			problems = append(problems, Problem{File: e.Name, Note: e.Note, Text: "not a migration file name (NNNNNN_name.up.sql or NNNNNN_name.down.sql)"})

			continue
		}
		idx, _ := strconv.Atoi(m[1])
		if m[3] == upKind {
			ups[idx] = append(ups[idx], e)
		} else {
			downs[idx] = append(downs[idx], e)
		}
	}
	indexes := make([]int, 0, len(ups)+len(downs))
	for idx := range ups {
		indexes = append(indexes, idx)
	}
	for idx := range downs {
		if _, ok := ups[idx]; !ok {
			indexes = append(indexes, idx)
		}
	}
	sort.Ints(indexes)
	for _, idx := range indexes {
		problems = append(problems, indexProblems(idx, ups[idx], downs[idx])...)
	}
	if len(indexes) == 0 {
		return problems, Span{}
	}
	low, high := indexes[0], indexes[len(indexes)-1]
	for idx := low; idx <= high; idx++ {
		_, up := ups[idx]
		_, down := downs[idx]
		if !up && !down {
			problems = append(problems, Problem{Text: fmt.Sprintf("gap: no migration %06d between %06d and %06d; the sequence is contiguous so nothing is skipped", idx, low, high)})
		}
	}

	return problems, Span{Count: len(ups), Low: low, High: high}
}

// indexProblems are one index's problems: two up files, two down files, a down file
// without an up.
func indexProblems(idx int, ups, downs []Entry) []Problem {
	var problems []Problem
	if len(ups) > 1 {
		for _, e := range ups {
			problems = append(problems, Problem{File: e.Name, Note: e.Note, Text: fmt.Sprintf("index %06d has %d up files; one migration per index", idx, len(ups))})
		}
	}
	if len(downs) > 1 {
		for _, e := range downs {
			problems = append(problems, Problem{File: e.Name, Note: e.Note, Text: fmt.Sprintf("index %06d has %d down files; at most one", idx, len(downs))})
		}
	}
	if len(ups) == 0 {
		for _, e := range downs {
			problems = append(problems, Problem{File: e.Name, Note: e.Note, Text: fmt.Sprintf("index %06d has a down file and no up file", idx)})
		}
	}

	return problems
}

// BlobSHA is the name git gives a file with the content (git hash-object): what the
// GitHub API lists for a file at a commit, so a file in the tree is compared with the
// one committed without reading the committed content.
func BlobSHA(content []byte) string {
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(content))
	h.Write(content)

	return hex.EncodeToString(h.Sum(nil))
}
