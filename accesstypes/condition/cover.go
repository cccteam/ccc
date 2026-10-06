package condition

import "slices"

// Disjuncts flattens an any-of tree into its disjuncts: the operands of a
// top-level OR, recursively, or the expression itself when it is not an OR.
// OR is associative, so the finer granularity only sharpens a covering test,
// never changes what the expression means.
func Disjuncts(e Expr) []Expr {
	or, ok := e.(Or)
	if !ok {
		return []Expr{e}
	}

	var out []Expr
	for _, operand := range or.Operands {
		out = append(out, Disjuncts(operand)...)
	}

	return out
}

// Implies reports whether premise implies conclusion under a small closed set
// of proven rules on one attribute. Everything outside the rules is decided by
// identity of canonical text and never guessed:
//
//  1. Identical canonical text.
//  2. Positive literal-set inclusion on the same reference: a = v is read as
//     a IN (v), and a IN S implies a IN T when every literal of S is in T.
//  3. The negated dual: a != v is read as a NOT IN (v), and a NOT IN S implies
//     a NOT IN T when every literal of T is in S.
//  4. A conjunction implies the conclusion when any one of its conjuncts
//     does, through nested ANDs. An OR nested inside an AND is one conjunct
//     and is tested by identity alone.
//
// References compare by canonical text (new.state is not state), and so do
// literals (1 is not 1.0, 'A' is not 'a'): the package never sees the
// attribute's type. Subject sets and values, null tests, ranges, and
// comparisons across attributes are identity only.
//
// Soundness. The read path prunes a field's CASE when every disjunct of the
// row predicate implies the field's condition: a row survives the WHERE
// because some disjunct evaluated TRUE under three-valued logic, each rule
// preserves TRUE, so the field's condition is TRUE on that row and the CASE
// would yield the column anyway. The WHERE itself is never simplified.
func Implies(premise, conclusion Expr) bool {
	if premise.String() == conclusion.String() {
		return true
	}

	if p := literalSetOf(premise); p != nil {
		if c := literalSetOf(conclusion); c != nil && p.implies(c) {
			return true
		}
	}

	if and, ok := premise.(And); ok {
		for _, conjunct := range and.Operands {
			if Implies(conjunct, conclusion) {
				return true
			}
		}
	}

	return false
}

// Uncovered lists the row disjuncts no field disjunct implies, in row order.
// A row disjunct is covered when it implies one of the field's disjuncts, or
// the field's positive literal sets on its reference merged into one:
// state = 'failed' OR state = 'stood_down' covers state IN ('failed',
// 'stood_down'), because a row disjunct implies the field's condition when it
// implies the OR of the field's disjuncts. Negated field disjuncts are tested
// one at a time under Implies.
func Uncovered(fieldDisjuncts, rowDisjuncts []Expr) []Expr {
	targets := append(slices.Clone(fieldDisjuncts), mergedLiteralSets(fieldDisjuncts)...)

	var out []Expr
	for _, d := range rowDisjuncts {
		covered := slices.ContainsFunc(targets, func(target Expr) bool {
			return Implies(d, target)
		})
		if !covered {
			out = append(out, d)
		}
	}

	return out
}

// Covers reports whether a field's condition set covers a row predicate:
// Uncovered is empty, so every disjunct of the predicate implies the field's
// condition. The resource layer's read rendering prunes a field's CASE on this
// test (the WHERE has already proven the field's condition on every surviving
// row), and the deploy-time role validation asks the same question of a
// role's grants, so both answer alike: a CASE the renderer keeps is exactly a
// CASE the validation warns about.
func Covers(fieldDisjuncts, rowDisjuncts []Expr) bool {
	return len(Uncovered(fieldDisjuncts, rowDisjuncts)) == 0
}

// literalSet is the normal form rules 2 and 3 compare: one reference tested
// for membership in a set of literals, positively or negated. An equality or
// inequality is a one-literal set. A subject-set IN, a range, a null test, and
// a comparison against anything but a literal have no literal set.
type literalSet struct {
	ref      Ref
	negated  bool
	literals []Literal
}

// literalSetOf reads e as a literal set, or returns nil when it is not one.
func literalSetOf(e Expr) *literalSet {
	switch e := e.(type) {
	case Comparison:
		lit, isLiteral := e.Right.(Literal)
		if !isLiteral || (e.Op != Eq && e.Op != NotEq) {
			return nil
		}

		return &literalSet{ref: e.Left, negated: e.Op == NotEq, literals: []Literal{lit}}
	case In:
		if e.SubjectSet != "" {
			return nil
		}

		return &literalSet{ref: e.Left, negated: e.Negated, literals: e.Literals}
	default:
		return nil
	}
}

// implies applies rules 2 and 3: the same reference and polarity, and the
// positive premise's literals all in the conclusion's, or the negated
// conclusion's literals all in the premise's.
func (s *literalSet) implies(c *literalSet) bool {
	if s.ref.String() != c.ref.String() || s.negated != c.negated {
		return false
	}
	if s.negated {
		return subset(c.literals, s.literals)
	}

	return subset(s.literals, c.literals)
}

// subset reports whether every literal of inner appears, by canonical text,
// in outer.
func subset(inner, outer []Literal) bool {
	texts := make(map[string]struct{}, len(outer))
	for _, lit := range outer {
		texts[lit.String()] = struct{}{}
	}
	for _, lit := range inner {
		if _, ok := texts[lit.String()]; !ok {
			return false
		}
	}

	return true
}

// mergedLiteralSets merges the positive literal sets among disjuncts by
// reference into one IN per reference, in first-appearance order, and returns
// the ones that merged more than one disjunct; a lone set is already its own
// disjunct. The OR of a reference's positive sets is membership in their
// union, so the merged IN is implied by exactly what the OR is.
func mergedLiteralSets(disjuncts []Expr) []Expr {
	var refs []string
	byRef := make(map[string]*In)
	counts := make(map[string]int)
	for _, d := range disjuncts {
		s := literalSetOf(d)
		if s == nil || s.negated {
			continue
		}
		key := s.ref.String()
		merged, seen := byRef[key]
		if !seen {
			merged = &In{Left: s.ref}
			byRef[key] = merged
			refs = append(refs, key)
		}
		counts[key]++
		for _, lit := range s.literals {
			if !slices.ContainsFunc(merged.Literals, func(have Literal) bool {
				return have.String() == lit.String()
			}) {
				merged.Literals = append(merged.Literals, lit)
			}
		}
	}

	var out []Expr
	for _, key := range refs {
		if counts[key] > 1 {
			out = append(out, *byRef[key])
		}
	}

	return out
}
