// Package conditiontest generates random condition expressions for property
// tests. One seeded generator, owned by the condition module, feeds every
// property over the pipeline — parse, fold, lowering, rendering — so a
// grammar addition reaches each of them through the same source and cannot
// slip past one of them unexercised.
//
// The generator is parameterized by a Vocabulary: the attribute names with
// their comparison types, the subject-set and subject-value names with
// theirs, and whether the post-image (new.) is legal. It emits condition.Expr
// values the parser accepts and that deploy validation (access.MigrateRoles)
// admits for that vocabulary — literals typed to their attribute, subject
// against string attributes, now against timestamps, a subject value or set
// only beside an attribute of its own type, now only against a
// timestamp-typed subject value, new. and the old-vs-new right side over
// column attributes only — and it covers every node type, operand type,
// comparison operator, temporal function, and IN form the grammar has.
//
// Callers own the random source, so a failing case reproduces from its seed.
package conditiontest

import (
	"fmt"
	"math/rand/v2"
	"regexp"
	"strings"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/accesstypes/condition"
)

// Vocabulary is the set of names a generated condition may reference.
type Vocabulary struct {
	// Attributes are the checked resource's binding names with their
	// comparison types. At least one is required.
	Attributes []Attribute

	// SubjectSets are the @subjectSet entries legal after IN subject., each
	// with the comparison type of the column the set yields. A set is drawn
	// only beside an attribute of its type; none pairable leaves the form
	// out.
	SubjectSets []SubjectBinding

	// SubjectValues are the scalar @subjectValue entries legal as operands
	// (subject.name), each with the comparison type of the column the value
	// yields. A value is drawn only beside an attribute of its type, and
	// after now only when timestamp-typed; none pairable leaves the form out.
	SubjectValues []SubjectBinding

	// PostImage reports whether new. is legal: a create or update context
	// proposes values; a read context does not.
	PostImage bool
}

// SubjectBinding is one subject-side name (a set or a value) with the
// comparison type of the column it yields.
type SubjectBinding struct {
	Name string
	Type accesstypes.AttributeType
}

// Attribute is one binding name with its comparison type.
type Attribute struct {
	Name string
	Type accesstypes.AttributeType

	// JoinPath marks an attribute resolved through a join path rather than a
	// column of the checked row: it is never read from the post-image and
	// never stands on the right side of an old-vs-new comparison, the rules
	// deploy validation enforces.
	JoinPath bool
}

// Generator draws random expressions over a vocabulary.
type Generator struct {
	rng     *rand.Rand
	vocab   Vocabulary
	columns []Attribute
	byType  map[accesstypes.AttributeType][]Attribute
	leaves  []func() condition.Expr

	// The subject entries the vocabulary can pair: sets and values with at
	// least one attribute of their type, and the timestamp-typed values now
	// may compare against.
	pairableSets    []SubjectBinding
	pairableValues  []SubjectBinding
	timestampValues []SubjectBinding
}

// New returns a generator over a copy of the vocabulary, drawing from rng.
// The vocabulary is validated once: it must declare at least one attribute,
// every name must be an identifier the language admits, and every type must
// be in the comparison-type vocabulary. A misuse is a programming error and
// panics, the way a generated-code mismatch does at construction.
func New(rng *rand.Rand, vocab *Vocabulary) *Generator {
	if rng == nil {
		panic("conditiontest.New: nil random source")
	}
	if vocab == nil || len(vocab.Attributes) == 0 {
		panic("conditiontest.New: the vocabulary declares no attributes")
	}

	g := &Generator{
		rng:    rng,
		vocab:  *vocab,
		byType: make(map[accesstypes.AttributeType][]Attribute),
	}
	for _, attr := range vocab.Attributes {
		checkName(attr.Name, "attribute")
		if !accesstypes.ValidAttributeType(attr.Type) {
			panic(fmt.Sprintf("conditiontest.New: attribute %q has type %q, which is not a comparison type", attr.Name, attr.Type))
		}
		g.byType[attr.Type] = append(g.byType[attr.Type], attr)
		if !attr.JoinPath {
			g.columns = append(g.columns, attr)
		}
	}
	for _, set := range vocab.SubjectSets {
		checkSubject(set, "subject set")
		if len(g.byType[set.Type]) > 0 {
			g.pairableSets = append(g.pairableSets, set)
		}
	}
	for _, value := range vocab.SubjectValues {
		checkSubject(value, "subject value")
		if len(g.byType[value.Type]) > 0 {
			g.pairableValues = append(g.pairableValues, value)
		}
		if value.Type == accesstypes.AttributeTypeTimestamp {
			g.timestampValues = append(g.timestampValues, value)
		}
	}

	g.leaves = g.leafProductions()

	return g
}

// leafProductions lists the leaf forms the vocabulary supports. Forms whose
// operands the vocabulary cannot supply are left out rather than emitted
// ill-typed.
func (g *Generator) leafProductions() []func() condition.Expr {
	leaves := []func() condition.Expr{
		g.literalComparison,
		g.literalIn,
		g.nullTest,
		g.nowComparison,
		g.timeOfDayComparison,
		g.dayOfWeekComparison,
		g.dayOfWeekIn,
	}
	if len(g.byType[accesstypes.AttributeTypeString]) > 0 {
		leaves = append(leaves, g.subjectComparison)
	}
	if len(g.byType[accesstypes.AttributeTypeTimestamp]) > 0 {
		leaves = append(leaves, g.nowOperandComparison)
	}
	if len(g.pairableValues) > 0 {
		leaves = append(leaves, g.subjectValueComparison)
	}
	if len(g.pairableSets) > 0 {
		leaves = append(leaves, g.subjectSetIn)
	}
	if g.vocab.PostImage && len(g.columns) > 0 {
		leaves = append(leaves, g.oldVsNewComparison)
	}

	return leaves
}

// Expr draws one expression whose logic nests at most depth levels; at depth
// zero it draws a leaf. Logic nodes take two or three operands, so a
// rendering exercises the parser's n-ary AND and OR.
func (g *Generator) Expr(depth int) condition.Expr {
	if depth <= 0 {
		return g.leaf()
	}
	switch g.rng.IntN(6) {
	case 0:
		return condition.And{Operands: g.operands(depth)}
	case 1:
		return condition.Or{Operands: g.operands(depth)}
	case 2:
		return condition.Not{Operand: g.Expr(depth - 1)}
	default:
		return g.leaf()
	}
}

func (g *Generator) operands(depth int) []condition.Expr {
	n := 2 + g.rng.IntN(2)
	operands := make([]condition.Expr, 0, n)
	for range n {
		operands = append(operands, g.Expr(depth-1))
	}

	return operands
}

func (g *Generator) leaf() condition.Expr {
	return g.leaves[g.rng.IntN(len(g.leaves))]()
}

// literalComparison is attr <op> literal, the literal typed to the attribute.
func (g *Generator) literalComparison() condition.Expr {
	attr := g.attribute()

	return condition.Comparison{Left: g.ref(attr), Op: g.op(), Right: g.literal(attr.Type)}
}

// subjectComparison is attr <op> subject over a string attribute — a user id.
func (g *Generator) subjectComparison() condition.Expr {
	attr := pick(g.rng, g.byType[accesstypes.AttributeTypeString])

	return condition.Comparison{Left: g.ref(attr), Op: g.op(), Right: condition.Subject{}}
}

// nowOperandComparison is attr <op> now over a timestamp attribute.
func (g *Generator) nowOperandComparison() condition.Expr {
	attr := pick(g.rng, g.byType[accesstypes.AttributeTypeTimestamp])

	return condition.Comparison{Left: g.ref(attr), Op: g.op(), Right: condition.Now{}}
}

// subjectValueComparison is attr <op> subject.name over an attribute of the
// value's comparison type: a subject value, like an attribute, carries the
// type of the column it yields, and deploy validation pairs like with like.
func (g *Generator) subjectValueComparison() condition.Expr {
	value := pick(g.rng, g.pairableValues)
	attr := pick(g.rng, g.byType[value.Type])

	return condition.Comparison{Left: g.ref(attr), Op: g.op(), Right: condition.SubjectValue{Name: value.Name}}
}

// nowComparison is now <op> operand, the operand an RFC 3339 instant, now
// itself, or a timestamp-typed subject value — the forms deploy validation,
// folding, and rendering accept.
func (g *Generator) nowComparison() condition.Expr {
	var right condition.Operand = g.literal(accesstypes.AttributeTypeTimestamp)
	switch form := g.rng.IntN(3); {
	case form == 0:
		right = condition.Now{}
	case form == 1 && len(g.timestampValues) > 0:
		right = condition.SubjectValue{Name: pick(g.rng, g.timestampValues).Name}
	}

	return condition.Comparison{Left: condition.Ref{Name: nowName}, Op: g.op(), Right: right}
}

// oldVsNewComparison is new.col <op> col: the post-image against the same
// row's pre-image, both column attributes of one comparison type.
func (g *Generator) oldVsNewComparison() condition.Expr {
	left := pick(g.rng, g.columns)
	candidates := make([]Attribute, 0, len(g.columns))
	for _, attr := range g.columns {
		if attr.Type == left.Type {
			candidates = append(candidates, attr)
		}
	}
	right := pick(g.rng, candidates)

	return condition.Comparison{Left: condition.Ref{Name: left.Name, PostImage: true}, Op: g.op(), Right: condition.Ref{Name: right.Name}}
}

// literalIn is attr [NOT] IN (literal, …), the literals typed to the
// attribute.
func (g *Generator) literalIn() condition.Expr {
	attr := g.attribute()
	n := 1 + g.rng.IntN(3)
	literals := make([]condition.Literal, 0, n)
	for range n {
		literals = append(literals, g.literal(attr.Type))
	}

	return condition.In{Left: g.ref(attr), Negated: g.coin(), Literals: literals}
}

// subjectSetIn is attr [NOT] IN subject.name over an attribute of the set's
// comparison type.
func (g *Generator) subjectSetIn() condition.Expr {
	set := pick(g.rng, g.pairableSets)
	attr := pick(g.rng, g.byType[set.Type])

	return condition.In{Left: g.ref(attr), Negated: g.coin(), SubjectSet: set.Name}
}

// nullTest is attr IS [NOT] NULL.
func (g *Generator) nullTest() condition.Expr {
	return condition.NullTest{Left: g.ref(g.attribute()), Negated: g.coin()}
}

// timeOfDayComparison is timeOfDay(now, zone) <op> 'HH:MM'.
func (g *Generator) timeOfDayComparison() condition.Expr {
	return condition.Comparison{Left: g.temporal(condition.FuncTimeOfDay), Op: g.op(), Right: condition.StringLiteral{Value: pick(g.rng, timesOfDay)}}
}

// dayOfWeekComparison is dayOfWeek(now, zone) = or != a day name.
func (g *Generator) dayOfWeekComparison() condition.Expr {
	op := condition.Eq
	if g.coin() {
		op = condition.NotEq
	}

	return condition.Comparison{Left: g.temporal(condition.FuncDayOfWeek), Op: op, Right: condition.StringLiteral{Value: pick(g.rng, dayNames)}}
}

// dayOfWeekIn is dayOfWeek(now, zone) [NOT] IN (day, …).
func (g *Generator) dayOfWeekIn() condition.Expr {
	n := 1 + g.rng.IntN(3)
	literals := make([]condition.Literal, 0, n)
	for range n {
		literals = append(literals, condition.StringLiteral{Value: pick(g.rng, dayNames)})
	}

	return condition.In{Left: g.temporal(condition.FuncDayOfWeek), Negated: g.coin(), Literals: literals}
}

// temporal draws a temporal function reference: the bare word local or a
// named zone.
func (g *Generator) temporal(fn string) condition.Ref {
	if g.coin() {
		return condition.Ref{Func: fn, ZoneLocal: true}
	}

	return condition.Ref{Func: fn, Zone: pick(g.rng, zoneNames)}
}

// ref draws an attribute reference, reading the post-image a quarter of the
// time where the vocabulary and the attribute admit it.
func (g *Generator) ref(attr Attribute) condition.Ref {
	postImage := g.vocab.PostImage && !attr.JoinPath && g.rng.IntN(4) == 0

	return condition.Ref{Name: attr.Name, PostImage: postImage}
}

func (g *Generator) attribute() Attribute {
	return pick(g.rng, g.vocab.Attributes)
}

func (g *Generator) op() condition.CompareOp {
	return pick(g.rng, compareOps)
}

func (g *Generator) coin() bool {
	return g.rng.IntN(2) == 0
}

// literal draws a literal of the attribute's comparison type.
func (g *Generator) literal(t accesstypes.AttributeType) condition.Literal {
	switch t {
	case accesstypes.AttributeTypeNumber:
		return condition.NumberLiteral{Text: pick(g.rng, numberTexts)}
	case accesstypes.AttributeTypeBool:
		return condition.BoolLiteral{Value: g.coin()}
	case accesstypes.AttributeTypeTimestamp:
		return condition.StringLiteral{Value: pick(g.rng, timestamps)}
	case accesstypes.AttributeTypeDate:
		return condition.StringLiteral{Value: pick(g.rng, dates)}
	default:
		return condition.StringLiteral{Value: pick(g.rng, stringValues)}
	}
}

// LiteralPool returns the literals the generator draws for a comparison type,
// so a row generator can draw stored values from the same pool and make equal,
// less, greater, and absent all occur against the conditions it emits.
func LiteralPool(t accesstypes.AttributeType) []condition.Literal {
	var out []condition.Literal
	switch t {
	case accesstypes.AttributeTypeNumber:
		for _, text := range numberTexts {
			out = append(out, condition.NumberLiteral{Text: text})
		}
	case accesstypes.AttributeTypeBool:
		out = append(out, condition.BoolLiteral{Value: false}, condition.BoolLiteral{Value: true})
	case accesstypes.AttributeTypeTimestamp:
		for _, text := range timestamps {
			out = append(out, condition.StringLiteral{Value: text})
		}
	case accesstypes.AttributeTypeDate:
		for _, text := range dates {
			out = append(out, condition.StringLiteral{Value: text})
		}
	default:
		for _, text := range stringValues {
			out = append(out, condition.StringLiteral{Value: text})
		}
	}

	return out
}

func pick[T any](rng *rand.Rand, values []T) T {
	return values[rng.IntN(len(values))]
}

// nowName is the reserved environment fact on a comparison's left side.
const nowName = "now"

var compareOps = []condition.CompareOp{condition.Eq, condition.NotEq, condition.Less, condition.LessEq, condition.Greater, condition.GreaterEq}

// The literal pools. Strings include the quote-doubling edge, the empty
// string, spaces, and keyword- and reserved-word-shaped content, which must
// survive rendering as data.
var (
	stringValues = []string{"open", "it's", "''", "", "AND", "subject", "new", "now", "local", "2026-01-02T15:04:05Z", "a b c"}
	// The last number spells more digits than a double carries, so a NUMERIC
	// or INT64 neighbor compared through FLOAT64 would wrongly equal it.
	numberTexts = []string{"0", "42", "10.5", "-3", "-0.25", "1000000", "1234567890123456789.5"}
	timestamps  = []string{"2026-01-02T15:04:05Z", "1999-12-31T23:59:59Z", "2026-06-30T08:00:00+02:00"}
	dates       = []string{"2026-01-02", "1999-12-31"}
	timesOfDay  = []string{"00:00", "06:30", "17:00", "23:59"}
	dayNames    = []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}
	zoneNames   = []string{"America/Denver", "Europe/London", "Asia/Tokyo", "UTC"}
)

var identifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// The words an identifier position refuses: the case-insensitive keywords and
// the case-sensitive reserved words.
var (
	keywords      = []string{"AND", "OR", "NOT", "IN", "IS", "NULL", "TRUE", "FALSE"}
	reservedWords = []string{"subject", nowName, "new"}
)

// checkSubject panics unless the subject entry's name is an identifier the
// language admits in the role and its type is a comparison type.
func checkSubject(entry SubjectBinding, role string) {
	checkName(entry.Name, role)
	if !accesstypes.ValidAttributeType(entry.Type) {
		panic(fmt.Sprintf("conditiontest.New: %s %q has type %q, which is not a comparison type", role, entry.Name, entry.Type))
	}
}

// checkName panics unless name is an identifier the language admits in the
// role.
func checkName(name, role string) {
	if !identifier.MatchString(name) {
		panic(fmt.Sprintf("conditiontest.New: %s %q is not an identifier", role, name))
	}
	for _, kw := range keywords {
		if strings.EqualFold(name, kw) {
			panic(fmt.Sprintf("conditiontest.New: %s %q is a keyword", role, name))
		}
	}
	for _, word := range reservedWords {
		if name == word {
			panic(fmt.Sprintf("conditiontest.New: %s %q is a reserved word", role, name))
		}
	}
}
