package condition

// WithoutPostImage returns the expression's fail-open upper bound with every
// post-image-reading term assumed to pass: an atom touching `new.` becomes
// TRUE under an even number of negations and FALSE under an odd number, and
// the logic simplifies with the usual TRUE/FALSE absorption.
//
// The capability envelope renders the result (§13): a term depending on a
// value the user has not yet typed counts potentially-true, while the
// evaluable residue still narrows the hint — so a grant like
// `state IN ('draft') AND new.priority <= priority` keeps its per-row state
// gating instead of collapsing to always-on. The bound is advisory only;
// enforcement always evaluates the full expression against the proposed
// image.
func WithoutPostImage(e Expr) Expr {
	return postImageBound(e, true)
}

// postImageBound rewrites post-image atoms to the bound the polarity wants:
// upper (TRUE) where the term's truth can only widen the result, lower
// (FALSE) where negation flips it.
func postImageBound(e Expr, upper bool) Expr {
	switch n := e.(type) {
	case And:
		operands := make([]Expr, 0, len(n.Operands))
		for _, op := range n.Operands {
			bounded := postImageBound(op, upper)
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
			bounded := postImageBound(op, upper)
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
		bounded := postImageBound(n.Operand, !upper)
		if t, ok := bounded.(Truth); ok {
			return Truth{Value: !t.Value}
		}

		return Not{Operand: bounded}
	case Comparison:
		if n.Left.PostImage {
			return Truth{Value: upper}
		}

		return n
	case In:
		if n.Left.PostImage {
			return Truth{Value: upper}
		}

		return n
	case NullTest:
		if n.Left.PostImage {
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
