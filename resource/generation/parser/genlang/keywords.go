package genlang

import "iter"

type keywordFlag int

const (
	ArgsRequired keywordFlag = 1 << iota
	DualArgsRequired
	NoArgs
	Exclusive // limit usage of the keyword to 1 per instance
)

type KeywordOpts map[scanMode]keywordFlag

type Args struct {
	Arg1 string
	Arg2 *string
}

type MultiMap struct {
	m map[string][]Args
}

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

func (m MultiMap) Get(s string) []Args {
	return m.m[s]
}

func (m MultiMap) GetOne(s string) Args {
	return m.m[s][0]
}

func (m MultiMap) Has(s string) bool {
	_, ok := m.m[s]

	return ok
}

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

func (m MultiMap) GetIter(s string) iter.Seq[Args] {
	iterator := func(yield func(Args) bool) {
		for _, arg := range m.m[s] {
			if !yield(arg) {
				return
			}
		}
	}

	return iterator
}
