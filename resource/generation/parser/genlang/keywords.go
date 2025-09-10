package genlang

import "iter"

type keywordFlag int

// If a flag's requirement is violated the scanner will return an error.
const (
	// ArgsRequired requires a keyword to have an accompanying argument.
	ArgsRequired keywordFlag = 1 << iota
	// DualArgsRequired requires a keyword to have two comma-separated arguments.
	DualArgsRequired
	// NoArgs requires a keyword to be called without arguments.
	NoArgs
	// StrictSingleArgs limits the number of comma-separated arguments to one
	StrictSingleArgs
	// Exclusive limits the keyword to a single use per instance of field or struct.
	Exclusive
)

// KeywordOpts is used to configure the flags for keywords based on the scan mode (field or struct).
type KeywordOpts map[scanMode]keywordFlag

// Args contains the raw string data for an argument, and possibly a second argument separated by a comma.
type Args struct {
	Arg1 string
	Arg2 *string
}

// MultiMap maps a singular key (string) to multiple values ([]Args),
// with convenience methods for accessing the values.
type MultiMap struct {
	m map[string][]Args
}

// Keys returns an iterator over all of the keys in the MultiMap.
func (m MultiMap) Keys() iter.Seq[string] {
	iterator := func(yield func(string) bool) {
		for keyword := range m.m {
			if !yield(keyword) {
				return
			}
		}
	}

	return iterator
}

// Get returns the slice of Args for a given key.
func (m MultiMap) Get(s string) []Args {
	return m.m[s]
}

// GetOne returns the first instance of Args for a given key. The caller should check that Args *do* exist
// for the given key before calling.
func (m MultiMap) GetOne(s string) Args {
	return m.m[s][0]
}

// Has returns true if the MultiMap contains one or more Args instances for a given key.
func (m MultiMap) Has(s string) bool {
	_, ok := m.m[s]

	return ok
}

// GetSingleArgs returns an iterator over the first argument in each instance of Args for a given key.
func (m MultiMap) GetSingleArgs(s string) iter.Seq[string] {
	iterator := func(yield func(string) bool) {
		for _, arg := range m.m[s] {
			if !yield(arg.Arg1) {
				return
			}
		}
	}

	return iterator
}

// GetDualArgs returns an iterator over the first and second argument in each instance of Args for a given key.
func (m MultiMap) GetDualArgs(s string) iter.Seq2[string, *string] {
	iterator := func(yield func(string, *string) bool) {
		for _, arg := range m.m[s] {
			if !yield(arg.Arg1, arg.Arg2) {
				return
			}
		}
	}

	return iterator
}

// Iter returns an iterator over the Args for a given key.
func (m MultiMap) Iter(s string) iter.Seq[Args] {
	iterator := func(yield func(Args) bool) {
		if _, ok := m.m[s]; !ok {
			return
		}

		for _, arg := range m.m[s] {
			if !yield(arg) {
				return
			}
		}
	}

	return iterator
}
