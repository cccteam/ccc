package genlang

import (
	"iter"
	"strings"
)

type keywordFlag int

// If a flag's requirement is violated the scanner will return an error.
const (
	ArgsRequired keywordFlag = 1 << iota // ArgsRequired requires a keyword to have an accompanying argument.
	NoArgs                               // NoArgs requires a keyword to be called without arguments.
	Exclusive                            // Exclusive limits the keyword to a single use per instance of field or struct.
)

// KeywordOpts is used to configure the flags for keywords based on the scan mode (field or struct).
type KeywordOpts map[scanMode]keywordFlag

// Arg is the raw string passed to a keyword that accepts an argument.
type Arg string

// Count returns the number of arguments separated by `\x00`
func (a Arg) Count() int {
	if a == "" {
		return 0
	}

	return strings.Count(string(a), "\x00") + 1
}

// Seq returns an iterator over the Arg separated by `\x00`
func (a Arg) Seq() iter.Seq[string] {
	return strings.SplitSeq(string(a), "\x00")
}

// ArgMap maps a singular key (string) to multiple values ([]Args),
// with convenience methods for accessing the values.
type ArgMap struct {
	m map[string]Arg
}

// Keys returns an iterator over all of the keys in the MultiMap.
func (m ArgMap) Keys() iter.Seq[string] {
	iterator := func(yield func(string) bool) {
		for keyword := range m.m {
			if !yield(keyword) {
				return
			}
		}
	}

	return iterator
}

// Get returns the argument for a given key.
func (m ArgMap) Get(s string) Arg {
	return m.m[s]
}

// Has returns true if the MultiMap contains one or more Args instances for a given key.
func (m ArgMap) Has(s string) bool {
	_, ok := m.m[s]

	return ok
}
