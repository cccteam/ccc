package condition

// FailOpen returns the expression's fail-open upper bound with every atom for
// which evaluable reports false assumed to pass: such an atom becomes TRUE
// under an even number of negations and FALSE under an odd number, and the
// logic simplifies with the usual TRUE/FALSE absorption. An unknown atom is
// unknown in both polarities, so the bound of a whole expression never
// simplifies to FALSE — the caller reads a Truth result as "no evaluable
// residue". The predicate sees only atoms (Comparison, In, NullTest); logic
// nodes recurse.
func FailOpen(e Expr, evaluable func(atom Expr) bool) Expr {
	return bound(e, evaluable, true)
}

// WithoutPostImage returns the expression's fail-open upper bound with every
// post-image-reading term assumed to pass — FailOpen with "touches no `new.`"
// as the predicate.
//
// The capability envelope renders the result (§13): a term depending on a
// value the user has not yet typed counts potentially-true, while the
// evaluable residue still narrows the hint — so a grant like
// `state IN ('draft') AND new.priority <= priority` keeps its per-row state
// gating instead of collapsing to always-on. The bound is advisory only;
// enforcement always evaluates the full expression against the proposed
// image.
func WithoutPostImage(e Expr) Expr {
	return FailOpen(e, func(atom Expr) bool {
		switch n := atom.(type) {
		case Comparison:
			return !n.Left.PostImage
		case In:
			return !n.Left.PostImage
		case NullTest:
			return !n.Left.PostImage
		default:
			return true
		}
	})
}

// bound rewrites unknown atoms to the bound the polarity wants: upper (TRUE)
// where the term's truth can only widen the result, lower (FALSE) where
// negation flips it.
func bound(e Expr, evaluable func(atom Expr) bool, upper bool) Expr {
	switch n := e.(type) {
	case And:
		operands := make([]Expr, 0, len(n.Operands))
		for _, op := range n.Operands {
			bounded := bound(op, evaluable, upper)
			if t, ok := bounded.(Truth); ok {
				if !t.Value {
					return Truth{Value: false}
				}

				continue
			}
			operands = append(operands, bounded)
		}

		return rebuildLogic(operands, true)
	case Or:
		operands := make([]Expr, 0, len(n.Operands))
		for _, op := range n.Operands {
			bounded := bound(op, evaluable, upper)
			if t, ok := bounded.(Truth); ok {
				if t.Value {
					return Truth{Value: true}
				}

				continue
			}
			operands = append(operands, bounded)
		}

		return rebuildLogic(operands, false)
	case Not:
		bounded := bound(n.Operand, evaluable, !upper)
		if t, ok := bounded.(Truth); ok {
			return Truth{Value: !t.Value}
		}

		return Not{Operand: bounded}
	case Comparison, In, NullTest:
		if !evaluable(n) {
			return Truth{Value: upper}
		}

		return n
	default:
		return e
	}
}

// rebuildLogic reassembles a simplified logic node: no surviving operands
// means every one absorbed into the identity (TRUE for AND, FALSE for OR),
// one survivor stands alone, and more keep the node.
func rebuildLogic(operands []Expr, conjunction bool) Expr {
	switch len(operands) {
	case 0:
		return Truth{Value: conjunction}
	case 1:
		return operands[0]
	default:
		if conjunction {
			return And{Operands: operands}
		}

		return Or{Operands: operands}
	}
}
