package accesstypes

import (
	"fmt"

	"github.com/cccteam/ccc/accesstypes/condition"
)

// Condition is the payload a Conditional decision carries for one
// ConditionGroup: the any-of combination (OR) of the covering grants'
// conditions, which the data layer must find true on the row. It wraps the
// compiled vocabulary AST together with the verbatim source text it was
// compiled from — the engine validates and folds the tree; the resource
// layer lowers it onto the schema and renders SQL.
//
// The zero Condition carries nothing (IsZero reports true); a Conditional
// decision's groups always carry a compiled condition once the engine folds
// real policy — the zero value survives only as plumbing written before the
// expression language landed.
type Condition struct {
	source string
	expr   condition.Expr
}

// NewCondition compiles source into a Condition, enforcing the language's
// grammar and fixed limits (4 KB, nesting depth 32). Schema validation —
// binding names against the Collection, literal types against attribute
// types, image validity per permission — is MigrateRoles' job, downstream.
func NewCondition(source string) (Condition, error) {
	expr, err := condition.Parse(source)
	if err != nil {
		return Condition{}, fmt.Errorf("accesstypes.NewCondition: %w", err)
	}

	return Condition{source: source, expr: expr}, nil
}

// ConditionFromExpr wraps an already-compiled tree — the engine constructs
// folded conditions this way — rendering its canonical text as the source.
func ConditionFromExpr(expr condition.Expr) Condition {
	if expr == nil {
		return Condition{}
	}

	return Condition{source: expr.String(), expr: expr}
}

// IsZero reports whether the Condition carries no expression.
func (c Condition) IsZero() bool {
	return c.expr == nil
}

// Source returns the verbatim condition text the Condition was compiled
// from, or the empty string for the zero Condition.
func (c Condition) Source() string {
	return c.source
}

// Expr returns the compiled vocabulary AST, or nil for the zero Condition.
func (c Condition) Expr() condition.Expr {
	return c.expr
}
