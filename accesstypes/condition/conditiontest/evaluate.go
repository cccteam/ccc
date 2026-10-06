package conditiontest

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/cccteam/ccc/accesstypes/condition"
)

// The reference evaluator implements the condition language's stated meaning
// (accesstypes/condition doc, "Evaluation") over one row image and one
// requester's facts. It is written from the documentation, never from the
// resource layer's lowering or its SQL: where the two disagree, the
// differential that compares them has found a rendering fault, not an
// evaluator fault. It is test code; production evaluation is the database's.
//
// The evaluator knows no schema. A join-path attribute arrives already
// resolved (the related row's column, or nothing), a subject set arrives as
// the values the requester's anchor rows yield, and a subject value as one
// value or nothing — the harness that owns the schema assembles the Image.

// Truth3 is a three-valued truth value: FALSE, UNKNOWN, or TRUE, ordered so
// that AND is the minimum and OR the maximum (Kleene logic, as in SQL).
type Truth3 int8

// The three truth values. UNKNOWN is what a comparison against a missing value
// yields; only TRUE permits.
const (
	False Truth3 = iota
	Unknown
	True
)

// And is three-valued conjunction: FALSE absorbs, UNKNOWN otherwise wins over
// TRUE.
func (t Truth3) And(o Truth3) Truth3 {
	return min(t, o)
}

// Or is three-valued disjunction: TRUE absorbs, UNKNOWN otherwise wins over
// FALSE.
func (t Truth3) Or(o Truth3) Truth3 {
	return max(t, o)
}

// Not swaps TRUE and FALSE and leaves UNKNOWN as it is.
func (t Truth3) Not() Truth3 {
	switch t {
	case True:
		return False
	case False:
		return True
	default:
		return Unknown
	}
}

// Permits reports whether the value permits: only TRUE does. A row survives,
// a cell shows, a check group passes, and a capability holds on TRUE alone.
func (t Truth3) Permits() bool {
	return t == True
}

// String renders the value as SQL spells it.
func (t Truth3) String() string {
	switch t {
	case True:
		return "TRUE"
	case False:
		return "FALSE"
	default:
		return "UNKNOWN"
	}
}

// Date is a civil date — a date attribute's value, compared as a calendar day
// with no instant and no zone.
type Date struct {
	Year  int
	Month time.Month
	Day   int
}

// ParseDate parses the language's date literal form, YYYY-MM-DD.
func ParseDate(text string) (Date, error) {
	parsed, err := time.Parse(time.DateOnly, text)
	if err != nil {
		return Date{}, fmt.Errorf("conditiontest: %q is not a YYYY-MM-DD date", text)
	}

	return Date{Year: parsed.Year(), Month: parsed.Month(), Day: parsed.Day()}, nil
}

// String renders the date in its literal form.
func (d Date) String() string {
	return fmt.Sprintf("%04d-%02d-%02d", d.Year, int(d.Month), d.Day)
}

func (d Date) compare(o Date) int {
	switch {
	case d.Year != o.Year:
		return cmpInt(d.Year, o.Year)
	case d.Month != o.Month:
		return cmpInt(int(d.Month), int(o.Month))
	default:
		return cmpInt(d.Day, o.Day)
	}
}

// Image is everything one evaluation reads: the row's values and the
// requester's facts.
//
// Values are Go values of the language's storage types: string, int64,
// float64, *big.Rat (an exact decimal, the NUMERIC storage type), bool,
// time.Time (a timestamp, an instant), and Date. A map entry that is absent
// or nil is "no value" — a NULL column, a NULL foreign key, a join path that
// reaches no row, a subject value whose requester has no anchor row.
type Image struct {
	// Pre holds the pre-image attribute values by binding name: the row as it
	// is. A join-path attribute is already resolved to the related row's
	// column or to nothing.
	Pre map[string]any

	// Post holds the post-image overlay for a mutation: the proposed value of
	// every column the mutation touches, by binding name. new.attr reads the
	// overlay where the mutation touches the column (a nil entry is a
	// proposed NULL) and the pre-image where it does not. Nil for a read.
	Post map[string]any

	// Subject is the requester's identity, the reserved fact subject.
	Subject string

	// Now is the request's decision instant, the reserved fact now.
	Now time.Time

	// Zone is the zone the bare word local resolves to inside a temporal
	// function; nil when the environment carries none, which is an error the
	// moment a condition asks for it.
	Zone *time.Location

	// SubjectSets holds each @subjectSet's values: the non-null values the
	// requester's anchor rows yield, restricted to the request's partition
	// where the anchor is domain-bound. An absent set is empty.
	SubjectSets map[string][]any

	// SubjectValues holds each @subjectValue's value: the requester's anchor
	// row's column, or nothing.
	SubjectValues map[string]any
}

// Evaluate returns the expression's three-valued truth over the image. It
// errors on a malformed literal, a comparison between values of different
// types, a temporal zone it cannot resolve, and a value of a type outside the
// storage vocabulary — each a misuse deploy validation refuses, so an error
// here means the harness or the generator is wrong, never the condition.
func Evaluate(expr condition.Expr, image *Image) (Truth3, error) {
	switch n := expr.(type) {
	case condition.And:
		result := True
		for _, operand := range n.Operands {
			t, err := Evaluate(operand, image)
			if err != nil {
				return Unknown, err
			}
			result = result.And(t)
		}

		return result, nil

	case condition.Or:
		result := False
		for _, operand := range n.Operands {
			t, err := Evaluate(operand, image)
			if err != nil {
				return Unknown, err
			}
			result = result.Or(t)
		}

		return result, nil

	case condition.Not:
		t, err := Evaluate(n.Operand, image)
		if err != nil {
			return Unknown, err
		}

		return t.Not(), nil

	case condition.Truth:
		if n.Value {
			return True, nil
		}

		return False, nil

	case condition.Comparison:
		return evaluateComparison(&n, image)

	case condition.In:
		return evaluateIn(&n, image)

	case condition.NullTest:
		// IS NULL is TRUE on no value and FALSE on a value; IS NOT NULL the
		// reverse — the one form a missing value settles.
		_, present := image.ref(n.Left)

		return truth(present == n.Negated), nil

	default:
		return Unknown, fmt.Errorf("conditiontest: unsupported expression node %T", expr)
	}
}

// evaluateComparison relates the left side to the operand. A temporal
// function reads the instant through its zone; now is the instant; an
// attribute reads the image, the post-image overlay under new. A missing
// value on either side is UNKNOWN before any comparison.
func evaluateComparison(c *condition.Comparison, image *Image) (Truth3, error) {
	if c.Left.IsTemporal() {
		return evaluateTemporalComparison(c, image)
	}

	var left any
	if c.Left.IsNow() {
		left = image.Now
	} else {
		var present bool
		left, present = image.ref(c.Left)
		if !present {
			return Unknown, nil
		}
	}

	right, present, err := image.operand(c.Right, left)
	if err != nil {
		return Unknown, fmt.Errorf("%w in %q", err, c.String())
	}
	if !present {
		return Unknown, nil
	}

	order, err := compareValues(left, right)
	if err != nil {
		return Unknown, fmt.Errorf("%w in %q", err, c.String())
	}

	return truth(applyOp(c.Op, order)), nil
}

// evaluateIn tests membership. Against no value the test is UNKNOWN in every
// form; a present value is a member of a literal list when it equals one of
// the literals, and of a subject set when it equals one of the set's values —
// so an empty set makes IN FALSE and NOT IN TRUE.
func evaluateIn(in *condition.In, image *Image) (Truth3, error) {
	if in.Left.IsTemporal() {
		return evaluateTemporalIn(in, image)
	}

	left, present := image.ref(in.Left)
	if !present {
		return Unknown, nil
	}

	member := false
	if in.SubjectSet != "" {
		for _, value := range image.SubjectSets[in.SubjectSet] {
			if value == nil {
				continue
			}
			order, err := compareValues(left, value)
			if err != nil {
				return Unknown, fmt.Errorf("%w in %q", err, in.String())
			}
			if order == 0 {
				member = true
			}
		}
	} else {
		for _, lit := range in.Literals {
			value, err := literalValue(lit, left)
			if err != nil {
				return Unknown, fmt.Errorf("%w in %q", err, in.String())
			}
			order, err := compareValues(left, value)
			if err != nil {
				return Unknown, fmt.Errorf("%w in %q", err, in.String())
			}
			if order == 0 {
				member = true
			}
		}
	}

	result := truth(member)
	if in.Negated {
		return result.Not(), nil
	}

	return result, nil
}

// ref reads an attribute reference: the post-image overlay for new.attr where
// the mutation touches the column, the pre-image otherwise. present is false
// for no value.
func (image *Image) ref(ref condition.Ref) (any, bool) {
	if ref.PostImage {
		if value, touched := image.Post[ref.Name]; touched {
			return value, value != nil
		}
	}
	value, ok := image.Pre[ref.Name]

	return value, ok && value != nil
}

// operand reads a comparison's right side. A literal is typed by the left
// value it compares against; subject and now are the facts; a subject value
// is the anchor row's value or nothing; a bare attribute (the old-vs-new
// right side) reads the pre-image.
func (image *Image) operand(operand condition.Operand, left any) (value any, present bool, err error) {
	switch o := operand.(type) {
	case condition.Literal:
		value, err := literalValue(o, left)
		if err != nil {
			return nil, false, err
		}

		return value, true, nil
	case condition.Subject:
		return image.Subject, true, nil
	case condition.Now:
		return image.Now, true, nil
	case condition.SubjectValue:
		value, ok := image.SubjectValues[o.Name]

		return value, ok && value != nil, nil
	case condition.Ref:
		value, ok := image.Pre[o.Name]

		return value, ok && value != nil, nil
	default:
		return nil, false, fmt.Errorf("conditiontest: unsupported operand %T", operand)
	}
}

// literalValue types a literal by the stored value it compares against: a
// string literal is a string, an RFC 3339 instant, or a date by the
// attribute's type; a number literal takes the attribute's storage type — an
// exact decimal against an integer or a NUMERIC, a double against a FLOAT64,
// whose value is already the nearest double.
func literalValue(lit condition.Literal, like any) (any, error) {
	switch l := lit.(type) {
	case condition.StringLiteral:
		switch like.(type) {
		case string:
			return l.Value, nil
		case time.Time:
			instant, err := time.Parse(time.RFC3339, l.Value)
			if err != nil {
				return nil, fmt.Errorf("conditiontest: %q is not an RFC 3339 instant", l.Value)
			}

			return instant.UTC(), nil
		case Date:
			return ParseDate(l.Value)
		default:
			return nil, fmt.Errorf("conditiontest: string literal %s against a value of type %s", l.String(), kindName(like))
		}
	case condition.NumberLiteral:
		switch like.(type) {
		case int64, *big.Rat:
			rat, ok := new(big.Rat).SetString(l.Text)
			if !ok {
				return nil, fmt.Errorf("conditiontest: %q is not a number", l.Text)
			}

			return rat, nil
		case float64:
			f, err := strconv.ParseFloat(l.Text, 64)
			if err != nil {
				return nil, fmt.Errorf("conditiontest: %q is not a number", l.Text)
			}

			return f, nil
		default:
			return nil, fmt.Errorf("conditiontest: number literal %s against a value of type %s", l.Text, kindName(like))
		}
	case condition.BoolLiteral:
		if _, ok := like.(bool); !ok {
			return nil, fmt.Errorf("conditiontest: boolean literal against a value of type %s", kindName(like))
		}

		return l.Value, nil
	default:
		return nil, fmt.Errorf("conditiontest: unsupported literal %T", lit)
	}
}

// compareValues orders two present values of one storage type: strings
// bytewise (code-point order), booleans FALSE before TRUE, timestamps as
// instants, dates as calendar days, and numbers exactly — unless either is a
// double, when both compare as doubles, the way the database widens an
// integer or a NUMERIC beside a FLOAT64.
func compareValues(a, b any) (int, error) {
	switch left := a.(type) {
	case string:
		right, ok := b.(string)
		if !ok {
			return 0, mismatch(a, b)
		}

		return strings.Compare(left, right), nil
	case bool:
		right, ok := b.(bool)
		if !ok {
			return 0, mismatch(a, b)
		}

		return cmpInt(boolInt(left), boolInt(right)), nil
	case time.Time:
		right, ok := b.(time.Time)
		if !ok {
			return 0, mismatch(a, b)
		}

		return left.Compare(right), nil
	case Date:
		right, ok := b.(Date)
		if !ok {
			return 0, mismatch(a, b)
		}

		return left.compare(right), nil
	case int64, float64, *big.Rat:
		if !isNumber(b) {
			return 0, mismatch(a, b)
		}

		return compareNumbers(a, b), nil
	default:
		return 0, fmt.Errorf("conditiontest: %T is not a storage type the language compares", a)
	}
}

func compareNumbers(a, b any) int {
	_, aDouble := a.(float64)
	_, bDouble := b.(float64)
	if aDouble || bDouble {
		return cmpFloat(toFloat(a), toFloat(b))
	}

	return toRat(a).Cmp(toRat(b))
}

func isNumber(v any) bool {
	switch v.(type) {
	case int64, float64, *big.Rat:
		return true
	default:
		return false
	}
}

func toRat(v any) *big.Rat {
	switch n := v.(type) {
	case int64:
		return new(big.Rat).SetInt64(n)
	case *big.Rat:
		return n
	default:
		return new(big.Rat)
	}
}

func toFloat(v any) float64 {
	switch n := v.(type) {
	case int64:
		return float64(n)
	case float64:
		return n
	case *big.Rat:
		f, _ := n.Float64()

		return f
	default:
		return 0
	}
}

// evaluateTemporalComparison reads the instant through the function's zone:
// timeOfDay compares minutes since midnight against an 'HH:MM' literal,
// dayOfWeek compares the day name with = and !=.
func evaluateTemporalComparison(c *condition.Comparison, image *Image) (Truth3, error) {
	local, err := image.wallClock(c.Left)
	if err != nil {
		return Unknown, fmt.Errorf("%w in %q", err, c.String())
	}
	literal, ok := c.Right.(condition.StringLiteral)
	if !ok {
		return Unknown, fmt.Errorf("conditiontest: %s compares against a quoted literal, not %s, in %q", c.Left.Func, c.Right.String(), c.String())
	}

	switch c.Left.Func {
	case condition.FuncTimeOfDay:
		right, err := condition.ParseTimeOfDay(literal.Value)
		if err != nil {
			return Unknown, fmt.Errorf("%w in %q", err, c.String())
		}
		left := local.Hour()*60 + local.Minute()

		return truth(applyOp(c.Op, cmpInt(left, right))), nil

	case condition.FuncDayOfWeek:
		if !condition.ValidDayName(literal.Value) {
			return Unknown, fmt.Errorf("conditiontest: %q is not a day name in %q", literal.Value, c.String())
		}
		equal := weekdayName(local.Weekday()) == literal.Value
		switch c.Op {
		case condition.Eq:
			return truth(equal), nil
		case condition.NotEq:
			return truth(!equal), nil
		default:
			return Unknown, fmt.Errorf("conditiontest: %s admits =, != and [NOT] IN, not %q, in %q", condition.FuncDayOfWeek, c.Op, c.String())
		}

	default:
		return Unknown, fmt.Errorf("conditiontest: unknown temporal function %q in %q", c.Left.Func, c.String())
	}
}

// evaluateTemporalIn tests dayOfWeek's membership in a literal day list.
func evaluateTemporalIn(in *condition.In, image *Image) (Truth3, error) {
	if in.Left.Func != condition.FuncDayOfWeek {
		return Unknown, fmt.Errorf("conditiontest: %s has no IN form in %q", in.Left.Func, in.String())
	}
	local, err := image.wallClock(in.Left)
	if err != nil {
		return Unknown, fmt.Errorf("%w in %q", err, in.String())
	}
	day := weekdayName(local.Weekday())

	member := false
	for _, lit := range in.Literals {
		literal, ok := lit.(condition.StringLiteral)
		if !ok || !condition.ValidDayName(literal.Value) {
			return Unknown, fmt.Errorf("conditiontest: %s is not a day name in %q", lit.String(), in.String())
		}
		if literal.Value == day {
			member = true
		}
	}

	result := truth(member)
	if in.Negated {
		return result.Not(), nil
	}

	return result, nil
}

// wallClock reads the decision instant in a temporal function's zone: the
// image's zone for the bare word local, the named zone otherwise.
func (image *Image) wallClock(ref condition.Ref) (time.Time, error) {
	if ref.ZoneLocal {
		if image.Zone == nil {
			return time.Time{}, fmt.Errorf("conditiontest: %s references local, which the environment does not carry", ref.String())
		}

		return image.Now.In(image.Zone), nil
	}
	zone, err := condition.LoadZone(ref.Zone)
	if err != nil {
		return time.Time{}, fmt.Errorf("conditiontest: %w", err)
	}

	return image.Now.In(zone), nil
}

// weekdayNames spells dayOfWeek's vocabulary in time.Weekday order (Sunday
// first), as the language documents it: 'mon' … 'sun'.
var weekdayNames = [7]string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}

func weekdayName(day time.Weekday) string {
	return weekdayNames[day]
}

// applyOp reads a three-way ordering through a relational operator.
func applyOp(op condition.CompareOp, order int) bool {
	switch op {
	case condition.Eq:
		return order == 0
	case condition.NotEq:
		return order != 0
	case condition.Less:
		return order < 0
	case condition.LessEq:
		return order <= 0
	case condition.Greater:
		return order > 0
	case condition.GreaterEq:
		return order >= 0
	default:
		return false
	}
}

func truth(b bool) Truth3 {
	if b {
		return True
	}

	return False
}

func mismatch(a, b any) error {
	return fmt.Errorf("conditiontest: cannot compare a value of type %s with a value of type %s", kindName(a), kindName(b))
}

// kindName names a value's storage type for error messages.
func kindName(v any) string {
	switch v.(type) {
	case string:
		return "string"
	case int64:
		return "INT64"
	case float64:
		return "FLOAT64"
	case *big.Rat:
		return "NUMERIC"
	case bool:
		return "bool"
	case time.Time:
		return "timestamp"
	case Date:
		return "date"
	default:
		return fmt.Sprintf("%T", v)
	}
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

func cmpFloat(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

func boolInt(b bool) int {
	if b {
		return 1
	}

	return 0
}
