package condition

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

// Covers reports whether a field's condition set covers a row predicate: every
// disjunct of the predicate appears, by canonical text, among the field's
// disjuncts. The resource layer's read rendering prunes a field's CASE on this
// test (the WHERE has already proven the field's condition on every surviving
// row), and the deploy-time role validation asks the same question of a role's
// grants, so both answer alike: a CASE the renderer keeps is exactly a CASE the
// validation warns about.
//
// The test is syntactic. A disjunct that implies another without spelling it
// the same way (state = 'a' against state IN ('a', 'b')) does not cover it.
func Covers(fieldDisjuncts, rowDisjuncts []Expr) bool {
	keys := make(map[string]struct{}, len(fieldDisjuncts))
	for _, d := range fieldDisjuncts {
		keys[d.String()] = struct{}{}
	}
	for _, d := range rowDisjuncts {
		if _, ok := keys[d.String()]; !ok {
			return false
		}
	}

	return true
}
