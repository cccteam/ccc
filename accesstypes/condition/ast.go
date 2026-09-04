package condition

import (
	"strings"
)

// Expr is a node of the vocabulary AST: the compiled form of condition text.
// Leaves are attribute (binding) names, the reserved facts subject and now,
// subject-attribute names, and literals — never schema facts: resolving a
// binding name onto a column or join path is the resource layer's lowering
// pass, and this package never sees a schema.
//
// Trees come from Parse (or from Fold, which may add Truth leaves); the
// parser is the gate that keeps every node well-formed.
type Expr interface {
	// String renders the node as canonical condition text: keywords
	// upper-case, single spaces, minimal parentheses. Parsing a rendering
	// yields an equivalent tree.
	String() string

	isExpr()
}

// And is the conjunction of two or more operands.
type And struct {
	Operands []Expr
}

func (And) isExpr() {}

func (a And) String() string {
	return joinLogic(a.Operands, " AND ", func(e Expr) bool {
		_, isOr := e.(Or)

		return isOr
	})
}

// Or is the disjunction of two or more operands.
type Or struct {
	Operands []Expr
}

func (Or) isExpr() {}

func (o Or) String() string {
	return joinLogic(o.Operands, " OR ", func(Expr) bool { return false })
}

// Not negates its operand.
type Not struct {
	Operand Expr
}

func (Not) isExpr() {}

func (n Not) String() string {
	switch n.Operand.(type) {
	case Comparison, In, NullTest, Truth:
		return "NOT " + n.Operand.String()
	default:
		return "NOT (" + n.Operand.String() + ")"
	}
}

// CompareOp is a relational comparison operator.
type CompareOp string

// The comparison operators. NotEq is the language's only not-equal spelling;
// renderers emit each dialect's form.
const (
	Eq        CompareOp = "="
	NotEq     CompareOp = "!="
	Less      CompareOp = "<"
	LessEq    CompareOp = "<="
	Greater   CompareOp = ">"
	GreaterEq CompareOp = ">="
)

// Comparison relates an attribute (or the now fact) to a single operand.
type Comparison struct {
	Left  Ref
	Op    CompareOp
	Right Operand
}

func (Comparison) isExpr() {}

func (c Comparison) String() string {
	return c.Left.String() + " " + string(c.Op) + " " + c.Right.String()
}

// In tests an attribute for membership: in a literal list, or in a
// @subjectSet attribute. Exactly one of Literals and SubjectSet is set —
// the parser is the gate.
type In struct {
	Left    Ref
	Negated bool

	// Literals is the literal list of `attr [NOT] IN ('a', 'b')`.
	Literals []Literal

	// SubjectSet is the @subjectSet name of `attr [NOT] IN subject.name`.
	SubjectSet string
}

func (In) isExpr() {}

func (i In) String() string {
	var b strings.Builder
	b.WriteString(i.Left.String())
	if i.Negated {
		b.WriteString(" NOT")
	}
	b.WriteString(" IN ")

	if i.SubjectSet != "" {
		b.WriteString("subject." + i.SubjectSet)

		return b.String()
	}

	b.WriteString("(")
	for n, lit := range i.Literals {
		if n > 0 {
			b.WriteString(", ")
		}
		b.WriteString(lit.String())
	}
	b.WriteString(")")

	return b.String()
}

// NullTest is `attr IS [NOT] NULL`.
type NullTest struct {
	Left    Ref
	Negated bool
}

func (NullTest) isExpr() {}

func (n NullTest) String() string {
	if n.Negated {
		return n.Left.String() + " IS NOT NULL"
	}

	return n.Left.String() + " IS NULL"
}

// Truth is a boolean leaf. The parser never produces one — the grammar has no
// bare-boolean primary; Truth enters a tree only through Fold, standing where
// a fact-only term was evaluated.
type Truth struct {
	Value bool
}

func (Truth) isExpr() {}

func (t Truth) String() string {
	if t.Value {
		return kwTrue
	}

	return kwFalse
}

// nowName is the reserved environment fact usable on a comparison's left side.
const nowName = "now"

// The temporal function names (decided 2026-09-03): both anchor on the
// environment fact now, take a zone, and always fold in the engine — they
// never lower to SQL, so no database dialect renders timezone arithmetic.
const (
	// FuncTimeOfDay is timeOfDay(now, zone): the instant's wall-clock reading
	// in the zone, compared against 'HH:MM' literals (24-hour).
	FuncTimeOfDay = "timeOfDay"

	// FuncDayOfWeek is dayOfWeek(now, zone): the instant's day name in the
	// zone — one of 'mon' … 'sun' — valid with =, != and [NOT] IN.
	FuncDayOfWeek = "dayOfWeek"
)

// zoneLocalName is the bare word naming the Environment's zone fact inside a
// temporal function's zone argument. It is reserved only in that position.
const zoneLocalName = "local"

// Ref is an attribute reference: a binding (row attribute) name, optionally
// read from the post-write image (`new.`), the reserved environment fact
// now — distinguished with IsNow, never by matching Name yourself — or a
// temporal function of now (IsTemporal). Every comparison's left side is a
// Ref; a Ref may also stand as the right operand in the old-vs-new form —
// `new.attr <op> attr` — where the left side is post-image and the right
// reads the same row's pre-image (the parser is the gate: an attribute
// operand is only legal against a `new.`-qualified left side, and never
// itself `new.`-qualified).
type Ref struct {
	Name string

	// PostImage marks the `new.` prefix: the proposed value where the
	// mutation touches the column, the existing value where it doesn't.
	PostImage bool

	// Func names the temporal function wrapping now — FuncTimeOfDay or
	// FuncDayOfWeek — with Zone/ZoneLocal its zone argument. Empty for a
	// plain reference; a function Ref carries no Name (the anchor is always
	// now). Temporal terms are environment facts: they fold at check time
	// and are row-free.
	Func string

	// Zone is the function's zone argument: an IANA name ('America/Denver'),
	// empty when ZoneLocal.
	Zone string

	// ZoneLocal marks the bare word local: the zone resolves from the
	// Environment's zone fact at fold time.
	ZoneLocal bool
}

func (Ref) isOperand() {}

// IsNow reports whether the Ref is the reserved environment fact now rather
// than a binding name.
func (r Ref) IsNow() bool {
	return !r.PostImage && r.Func == "" && r.Name == nowName
}

// IsTemporal reports whether the Ref is a temporal function of now rather
// than a binding name — distinguished with this, never by matching Func
// yourself.
func (r Ref) IsTemporal() bool {
	return r.Func != ""
}

func (r Ref) String() string {
	if r.Func != "" {
		zone := "'" + strings.ReplaceAll(r.Zone, "'", "''") + "'"
		if r.ZoneLocal {
			zone = zoneLocalName
		}

		return r.Func + "(" + nowName + ", " + zone + ")"
	}
	if r.PostImage {
		return "new." + r.Name
	}

	return r.Name
}

// Operand is a comparison's right side: a literal, the reserved facts subject
// and now, a @subjectValue attribute, or — against a `new.`-qualified left
// side only — a pre-image attribute Ref (the old-vs-new form).
type Operand interface {
	String() string

	isOperand()
}

// Literal is a literal operand: a string, a number, or a boolean. Strings and
// numbers are typed by context — MigrateRoles knows each binding's type from
// the Collection, so a timestamp is a quoted RFC 3339 string against a
// timestamp attribute, not a dedicated syntax.
type Literal interface {
	Operand

	isLiteral()
}

// StringLiteral is a quoted string. Value is the unescaped content; String
// re-escapes by doubling quotes.
type StringLiteral struct {
	Value string
}

func (StringLiteral) isOperand() {}
func (StringLiteral) isLiteral() {}

func (s StringLiteral) String() string {
	return "'" + strings.ReplaceAll(s.Value, "'", "''") + "'"
}

// NumberLiteral is a signed integer or decimal, kept verbatim: numeric
// exactness is the database's concern, so nothing here forces a float.
type NumberLiteral struct {
	Text string
}

func (NumberLiteral) isOperand() {}
func (NumberLiteral) isLiteral() {}

func (n NumberLiteral) String() string { return n.Text }

// BoolLiteral is true or false.
type BoolLiteral struct {
	Value bool
}

func (BoolLiteral) isOperand() {}
func (BoolLiteral) isLiteral() {}

func (b BoolLiteral) String() string {
	if b.Value {
		return "true"
	}

	return "false"
}

// Subject is the reserved fact subject: the checked user's identity.
type Subject struct{}

func (Subject) isOperand() {}

func (Subject) String() string { return "subject" }

// Now is the reserved fact now used as an operand: the request's UTC instant.
type Now struct{}

func (Now) isOperand() {}

func (Now) String() string { return nowName }

// SubjectValue is a scalar @subjectValue attribute operand (`subject.name`).
type SubjectValue struct {
	Name string
}

func (SubjectValue) isOperand() {}

func (s SubjectValue) String() string { return "subject." + s.Name }

// joinLogic renders logic operands separated by sep, parenthesizing the
// operands needsParens selects (plus any that are themselves the opposite
// logic node, which the caller's predicate decides).
func joinLogic(operands []Expr, sep string, needsParens func(Expr) bool) string {
	parts := make([]string, 0, len(operands))
	for _, op := range operands {
		s := op.String()
		if needsParens(op) {
			s = "(" + s + ")"
		}
		parts = append(parts, s)
	}

	return strings.Join(parts, sep)
}
