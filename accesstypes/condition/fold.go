package condition

import (
	"fmt"
	"time"
)

// Facts carries the fact values folding may substitute: the checked user's
// identity (subject), the request's UTC instant (now), and the zone the bare
// word local resolves to inside a temporal function. Facts are the engine's
// whole evaluation surface — everything else in a condition is data, which
// only the database compares.
//
// Presence is tracked per fact, mirroring accesstypes.Environment: folding a
// comparison that needs an absent fact is a fail-loud error, never a silent
// allow or deny. The zero Facts carries nothing.
type Facts struct {
	subject    string
	hasSubject bool
	now        time.Time
	hasNow     bool
	zone       *time.Location
}

// NewFacts returns a Facts carrying nothing.
func NewFacts() Facts {
	return Facts{}
}

// WithSubject returns a copy carrying the checked user's identity. Subject
// comparison is binary and case-sensitive — the one string semantic the
// language spec pins.
func (f Facts) WithSubject(subject string) Facts {
	f.subject = subject
	f.hasSubject = true

	return f
}

// WithNow returns a copy carrying the request's instant, normalized to UTC —
// the same value the caller binds into SQL, so the two consumers can never
// disagree about a window boundary.
func (f Facts) WithNow(now time.Time) Facts {
	f.now = now.UTC()
	f.hasNow = true

	return f
}

// WithZone returns a copy carrying the zone the bare word local resolves to
// inside a temporal function — the Environment's zone fact. How the zone is
// chosen (application config, a session claim, the tenant's record) is the
// application's business; folding sees only the resolved location.
func (f Facts) WithZone(zone *time.Location) Facts {
	f.zone = zone

	return f
}

// Zone returns the local zone and true, or nil and false when the Facts carry
// none.
func (f Facts) Zone() (*time.Location, bool) {
	if f.zone == nil {
		return nil, false
	}

	return f.zone, true
}

// Subject returns the checked user's identity and true, or the empty string
// and false when the Facts carry none.
func (f Facts) Subject() (string, bool) {
	if !f.hasSubject {
		return "", false
	}

	return f.subject, true
}

// Now returns the request's UTC instant and true, or the zero time and false
// when the Facts carry none.
func (f Facts) Now() (time.Time, bool) {
	if !f.hasNow {
		return time.Time{}, false
	}

	return f.now, true
}

// Fold returns the expression with every fact-only term evaluated and the
// logic simplified: TRUE and FALSE absorb through AND, OR, and NOT, so a
// fully factual expression folds to a single Truth leaf and a mixed one is
// left with only the terms the database must evaluate.
//
// Folding is confined to facts: a term touching row data or a subject
// attribute passes through untouched — there is no Go-side comparison of
// app-typed values, by design. Facts are never NULL, so folding needs no
// three-valued logic. A fact-only term referencing a fact the Facts value
// does not carry, or comparing a fact against a literal of the wrong type,
// is an error.
func Fold(e Expr, f Facts) (Expr, error) {
	switch n := e.(type) {
	case And:
		operands := make([]Expr, 0, len(n.Operands))
		for _, op := range n.Operands {
			folded, err := Fold(op, f)
			if err != nil {
				return nil, err
			}
			if t, ok := folded.(Truth); ok {
				if !t.Value {
					return Truth{Value: false}, nil
				}

				continue // TRUE AND x = x
			}
			operands = append(operands, folded)
		}
		switch len(operands) {
		case 0:
			return Truth{Value: true}, nil
		case 1:
			return operands[0], nil
		default:
			return And{Operands: operands}, nil
		}

	case Or:
		operands := make([]Expr, 0, len(n.Operands))
		for _, op := range n.Operands {
			folded, err := Fold(op, f)
			if err != nil {
				return nil, err
			}
			if t, ok := folded.(Truth); ok {
				if t.Value {
					return Truth{Value: true}, nil
				}

				continue // FALSE OR x = x
			}
			operands = append(operands, folded)
		}
		switch len(operands) {
		case 0:
			return Truth{Value: false}, nil
		case 1:
			return operands[0], nil
		default:
			return Or{Operands: operands}, nil
		}

	case Not:
		folded, err := Fold(n.Operand, f)
		if err != nil {
			return nil, err
		}
		if t, ok := folded.(Truth); ok {
			return Truth{Value: !t.Value}, nil
		}

		return Not{Operand: folded}, nil

	case Comparison:
		return foldComparison(&n, f)

	case In:
		return foldIn(&n, f)

	default:
		// NullTest always touches row data; Truth is already folded.
		return e, nil
	}
}

// foldIn evaluates a temporal membership test (dayOfWeek IN days); an In over
// a binding always touches row data and passes through for the database.
func foldIn(in *In, f Facts) (Expr, error) {
	if in.Left.IsTemporal() {
		return foldTemporalIn(in, f)
	}

	return *in, nil
}

// foldComparison evaluates a comparison when both sides are facts; anything
// touching row data or a subject attribute passes through for the database.
func foldComparison(c *Comparison, f Facts) (Expr, error) {
	if c.Left.IsTemporal() {
		return foldTemporalComparison(c, f)
	}
	if !c.Left.IsNow() {
		return *c, nil
	}

	if !f.hasNow {
		return nil, fmt.Errorf("condition: %q references now, which the environment does not carry", c.String())
	}

	var right time.Time
	switch operand := c.Right.(type) {
	case Now:
		right = f.now
	case StringLiteral:
		instant, err := time.Parse(time.RFC3339, operand.Value)
		if err != nil {
			return nil, fmt.Errorf("condition: %q is not an RFC 3339 instant in %q", operand.Value, c.String())
		}
		right = instant.UTC()
	case SubjectValue:
		// A subject attribute is data, not a fact — the database compares it.
		return *c, nil
	default:
		return nil, fmt.Errorf("condition: now cannot be compared with %q in %q", c.Right.String(), c.String())
	}

	result, err := compareInstants(f.now, c.Op, right)
	if err != nil {
		return nil, fmt.Errorf("%w in %q", err, c.String())
	}

	return Truth{Value: result}, nil
}

// compareInstants applies a relational operator to two UTC instants.
func compareInstants(left time.Time, op CompareOp, right time.Time) (bool, error) {
	switch op {
	case Eq:
		return left.Equal(right), nil
	case NotEq:
		return !left.Equal(right), nil
	case Less:
		return left.Before(right), nil
	case LessEq:
		return !left.After(right), nil
	case Greater:
		return left.After(right), nil
	case GreaterEq:
		return !left.Before(right), nil
	default:
		return false, fmt.Errorf("condition: unsupported operator %q", op)
	}
}
