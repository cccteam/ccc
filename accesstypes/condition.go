package accesstypes

// Condition is the opaque payload a Conditional decision carries for one
// ConditionGroup: the any-of combination (OR) of the covering grants'
// conditions, which the data layer must find true on the row.
//
// Placeholder — pending the expression-language design (ABAC design plan
// §11). Until that decision is recorded, Condition deliberately carries no
// fields and no behavior: no grammar, no AST, no SQL text. Nothing outside
// this package may assume anything about its contents; plumbing is written
// against the type, not a shape.
type Condition struct{}
