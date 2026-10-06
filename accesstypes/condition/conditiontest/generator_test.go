package conditiontest_test

// These tests pin the generator's own contract: it reaches every form of the
// grammar, it never leaves its vocabulary or its typing rules, every emission
// is a condition the parser accepts and the fold evaluates, and it is
// deterministic from its seed.

import (
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/accesstypes/condition"
	"github.com/cccteam/ccc/accesstypes/condition/conditiontest"
)

// fullVocabulary declares every type and every subject form, with a join-path
// attribute among the columns and subject entries of several types (a
// timestamp-typed value among them, so now has a subject value to compare
// against), in a write context.
func fullVocabulary() conditiontest.Vocabulary {
	return conditiontest.Vocabulary{
		Attributes: []conditiontest.Attribute{
			{Name: "owner", Type: accesstypes.AttributeTypeString},
			{Name: "state", Type: accesstypes.AttributeTypeString},
			{Name: "priority", Type: accesstypes.AttributeTypeNumber},
			{Name: "estimated_cost", Type: accesstypes.AttributeTypeNumber},
			{Name: "archived", Type: accesstypes.AttributeTypeBool},
			{Name: "dueAt", Type: accesstypes.AttributeTypeTimestamp},
			{Name: "startsOn", Type: accesstypes.AttributeTypeDate},
			{Name: "shipClass", Type: accesstypes.AttributeTypeString, JoinPath: true},
		},
		SubjectSets: []conditiontest.SubjectBinding{
			{Name: "crews", Type: accesstypes.AttributeTypeString},
			{Name: "wings", Type: accesstypes.AttributeTypeString},
			{Name: "hazardBands", Type: accesstypes.AttributeTypeNumber},
		},
		SubjectValues: []conditiontest.SubjectBinding{
			{Name: "approvalLimit", Type: accesstypes.AttributeTypeNumber},
			{Name: "homeSector", Type: accesstypes.AttributeTypeString},
			{Name: "clearedUntil", Type: accesstypes.AttributeTypeTimestamp},
		},
		PostImage: true,
	}
}

// generate draws n expressions at depth from a fresh generator over vocab.
func generate(vocab *conditiontest.Vocabulary, n, depth int) []condition.Expr {
	gen := conditiontest.New(rand.New(rand.NewPCG(20260911, 1)), vocab)
	exprs := make([]condition.Expr, 0, n)
	for range n {
		exprs = append(exprs, gen.Expr(depth))
	}

	return exprs
}

// walk visits every node of the tree, parents before children.
func walk(e condition.Expr, visit func(condition.Expr)) {
	visit(e)
	switch n := e.(type) {
	case condition.And:
		for _, op := range n.Operands {
			walk(op, visit)
		}
	case condition.Or:
		for _, op := range n.Operands {
			walk(op, visit)
		}
	case condition.Not:
		walk(n.Operand, visit)
	}
}

// contains reports whether some node of the tree satisfies the predicate.
func contains(e condition.Expr, pred func(condition.Expr) bool) bool {
	found := false
	walk(e, func(node condition.Expr) {
		if pred(node) {
			found = true
		}
	})

	return found
}

// comparisonWith matches a comparison node satisfying the predicate.
func comparisonWith(pred func(condition.Comparison) bool) func(condition.Expr) bool {
	return func(e condition.Expr) bool {
		c, ok := e.(condition.Comparison)

		return ok && pred(c)
	}
}

// inWith matches an IN node satisfying the predicate.
func inWith(pred func(condition.In) bool) func(condition.Expr) bool {
	return func(e condition.Expr) bool {
		in, ok := e.(condition.In)

		return ok && pred(in)
	}
}

// operandOf matches a comparison whose right side has the operand type.
func operandOf[T condition.Operand]() func(condition.Expr) bool {
	return comparisonWith(func(c condition.Comparison) bool {
		_, ok := c.Right.(T)

		return ok
	})
}

// nodeOf matches a node of the type.
func nodeOf[T condition.Expr]() func(condition.Expr) bool {
	return func(e condition.Expr) bool {
		_, ok := e.(T)

		return ok
	}
}

// operatorOf matches a comparison using the operator.
func operatorOf(op condition.CompareOp) func(condition.Expr) bool {
	return comparisonWith(func(c condition.Comparison) bool {
		return c.Op == op
	})
}

// leftRef matches a comparison, IN, or IS NULL whose left reference satisfies
// the predicate.
func leftRef(pred func(condition.Ref) bool) func(condition.Expr) bool {
	return func(e condition.Expr) bool {
		switch n := e.(type) {
		case condition.Comparison:
			return pred(n.Left)
		case condition.In:
			return pred(n.Left)
		case condition.NullTest:
			return pred(n.Left)
		default:
			return false
		}
	}
}

// TestGenerator_coversGrammar pins that a seeded sample over the full
// vocabulary reaches every node type, operand type, comparison operator,
// temporal function, zone form, and IN form the parser accepts: the guarantee
// the downstream properties rely on.
func TestGenerator_coversGrammar(t *testing.T) {
	t.Parallel()

	full := fullVocabulary()
	exprs := generate(&full, 3000, 4)

	tests := []struct {
		name string
		seen func(condition.Expr) bool
	}{
		{name: "AND", seen: nodeOf[condition.And]()},
		{name: "OR", seen: nodeOf[condition.Or]()},
		{name: "NOT", seen: nodeOf[condition.Not]()},
		{name: "comparison", seen: nodeOf[condition.Comparison]()},
		{name: "IS NULL", seen: nodeOf[condition.NullTest]()},
		{name: "IS NOT NULL", seen: func(e condition.Expr) bool {
			n, ok := e.(condition.NullTest)

			return ok && n.Negated
		}},
		{name: "IN over literals", seen: inWith(func(in condition.In) bool { return len(in.Literals) > 0 && !in.Left.IsTemporal() })},
		{name: "IN over a subject set", seen: inWith(func(in condition.In) bool { return in.SubjectSet != "" })},
		{name: "NOT IN", seen: inWith(func(in condition.In) bool { return in.Negated })},
		{name: "string literal operand", seen: operandOf[condition.StringLiteral]()},
		{name: "number literal operand", seen: operandOf[condition.NumberLiteral]()},
		{name: "boolean literal operand", seen: operandOf[condition.BoolLiteral]()},
		{name: "subject operand", seen: operandOf[condition.Subject]()},
		{name: "now operand", seen: operandOf[condition.Now]()},
		{name: "subject value operand", seen: operandOf[condition.SubjectValue]()},
		{name: "now against a subject value", seen: comparisonWith(func(c condition.Comparison) bool {
			_, ok := c.Right.(condition.SubjectValue)

			return ok && c.Left.IsNow()
		})},
		{name: "subject set over a number attribute", seen: inWith(func(in condition.In) bool { return in.SubjectSet == "hazardBands" })},
		{name: "old-vs-new attribute operand", seen: operandOf[condition.Ref]()},
		{name: "operator =", seen: operatorOf(condition.Eq)},
		{name: "operator !=", seen: operatorOf(condition.NotEq)},
		{name: "operator <", seen: operatorOf(condition.Less)},
		{name: "operator <=", seen: operatorOf(condition.LessEq)},
		{name: "operator >", seen: operatorOf(condition.Greater)},
		{name: "operator >=", seen: operatorOf(condition.GreaterEq)},
		{name: "now on the left", seen: leftRef(func(r condition.Ref) bool { return r.IsNow() })},
		{name: "timeOfDay", seen: leftRef(func(r condition.Ref) bool { return r.Func == condition.FuncTimeOfDay })},
		{name: "dayOfWeek comparison", seen: comparisonWith(func(c condition.Comparison) bool { return c.Left.Func == condition.FuncDayOfWeek })},
		{name: "dayOfWeek IN", seen: inWith(func(in condition.In) bool { return in.Left.Func == condition.FuncDayOfWeek })},
		{name: "zone local", seen: leftRef(func(r condition.Ref) bool { return r.ZoneLocal })},
		{name: "named zone", seen: leftRef(func(r condition.Ref) bool { return r.Zone != "" })},
		{name: "post-image reference", seen: leftRef(func(r condition.Ref) bool { return r.PostImage })},
		{name: "join-path attribute", seen: leftRef(func(r condition.Ref) bool { return r.Name == "shipClass" })},
		{name: "date literal", seen: comparisonWith(func(c condition.Comparison) bool {
			s, ok := c.Right.(condition.StringLiteral)

			return ok && c.Left.Name == "startsOn" && len(s.Value) == len(time.DateOnly)
		})},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if !slices.ContainsFunc(exprs, func(e condition.Expr) bool { return contains(e, tt.seen) }) {
				t.Errorf("no generated expression contains %s in %d samples", tt.name, len(exprs))
			}
		})
	}
}

// TestGenerator_staysInVocabulary pins, per vocabulary, that every emission
// parses back from its rendering, names only the vocabulary, respects the
// post-image and old-vs-new rules, types its literals to the attribute, and
// folds without error against full facts — the deploy-validation rules, so a
// downstream property can assert that lowering never fails.
func TestGenerator_staysInVocabulary(t *testing.T) {
	t.Parallel()

	full := fullVocabulary()
	readOnly := fullVocabulary()
	readOnly.PostImage = false

	tests := []struct {
		name  string
		vocab conditiontest.Vocabulary
	}{
		{name: "full write vocabulary", vocab: full},
		{name: "read context", vocab: readOnly},
		{name: "no subject vocabulary", vocab: conditiontest.Vocabulary{
			Attributes: full.Attributes,
			PostImage:  true,
		}},
		{name: "join paths only", vocab: conditiontest.Vocabulary{
			Attributes: []conditiontest.Attribute{{Name: "sector", Type: accesstypes.AttributeTypeString, JoinPath: true}},
			PostImage:  true,
		}},
		{name: "one boolean attribute", vocab: conditiontest.Vocabulary{
			Attributes: []conditiontest.Attribute{{Name: "archived", Type: accesstypes.AttributeTypeBool}},
		}},
		{name: "subject vocabulary no attribute can pair with", vocab: conditiontest.Vocabulary{
			Attributes:    []conditiontest.Attribute{{Name: "archived", Type: accesstypes.AttributeTypeBool}},
			SubjectSets:   []conditiontest.SubjectBinding{{Name: "crews", Type: accesstypes.AttributeTypeString}},
			SubjectValues: []conditiontest.SubjectBinding{{Name: "approvalLimit", Type: accesstypes.AttributeTypeNumber}},
		}},
	}

	facts := condition.NewFacts().
		WithSubject("u1").
		WithNow(time.Date(2026, 9, 11, 15, 0, 0, 0, time.UTC)).
		WithZone(time.UTC)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			for i, expr := range generate(&tt.vocab, 600, 3) {
				if msg := checkEmission(expr, &tt.vocab, facts); msg != "" {
					t.Fatalf("case %d (%s): %s", i, expr.String(), msg)
				}
			}
		})
	}
}

// TestGenerator_pairsSubjectTypes pins the subject-side typing over the full
// vocabulary: every subject value comparison and every subject set membership
// pairs an attribute of the entry's comparison type, and every now-against-value
// comparison names a timestamp-typed value — and each of the three forms is
// actually drawn, so the property is not vacuous.
func TestGenerator_pairsSubjectTypes(t *testing.T) {
	t.Parallel()

	full := fullVocabulary()
	v := indexVocabulary(&full)

	tests := []struct {
		name  string
		match func(condition.Expr) (subject, attribute string, ok bool)
		want  func(subject, attribute string) bool
	}{
		{
			name: "attribute against a subject value",
			match: func(e condition.Expr) (string, string, bool) {
				c, ok := e.(condition.Comparison)
				if !ok || c.Left.IsNow() {
					return "", "", false
				}
				value, ok := c.Right.(condition.SubjectValue)

				return value.Name, c.Left.Name, ok
			},
			want: func(subject, attribute string) bool {
				return v.values[subject] == v.types[attribute]
			},
		},
		{
			name: "attribute in a subject set",
			match: func(e condition.Expr) (string, string, bool) {
				in, ok := e.(condition.In)

				return in.SubjectSet, in.Left.Name, ok && in.SubjectSet != ""
			},
			want: func(subject, attribute string) bool {
				return v.sets[subject] == v.types[attribute]
			},
		},
		{
			name: "now against a subject value",
			match: func(e condition.Expr) (string, string, bool) {
				c, ok := e.(condition.Comparison)
				if !ok || !c.Left.IsNow() {
					return "", "", false
				}
				value, ok := c.Right.(condition.SubjectValue)

				return value.Name, "now", ok
			},
			want: func(subject, _ string) bool {
				return v.values[subject] == accesstypes.AttributeTypeTimestamp
			},
		},
	}

	exprs := generate(&full, 3000, 4)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			drawn := 0
			for _, expr := range exprs {
				walk(expr, func(node condition.Expr) {
					subject, attribute, ok := tt.match(node)
					if !ok {
						return
					}
					drawn++
					if !tt.want(subject, attribute) {
						t.Errorf("%s pairs subject.%s with %s across comparison types", node.String(), subject, attribute)
					}
				})
			}
			if drawn == 0 {
				t.Errorf("no generated expression draws the form in %d samples", len(exprs))
			}
		})
	}
}

// vocabularyTypes is a vocabulary indexed for the typing checks: attribute
// types, which attributes are columns, and the subject set and value types.
type vocabularyTypes struct {
	types   map[string]accesstypes.AttributeType
	columns map[string]bool
	sets    map[string]accesstypes.AttributeType
	values  map[string]accesstypes.AttributeType
}

func indexVocabulary(vocab *conditiontest.Vocabulary) *vocabularyTypes {
	v := &vocabularyTypes{
		types:   make(map[string]accesstypes.AttributeType, len(vocab.Attributes)),
		columns: make(map[string]bool, len(vocab.Attributes)),
		sets:    make(map[string]accesstypes.AttributeType, len(vocab.SubjectSets)),
		values:  make(map[string]accesstypes.AttributeType, len(vocab.SubjectValues)),
	}
	for _, attr := range vocab.Attributes {
		v.types[attr.Name] = attr.Type
		v.columns[attr.Name] = !attr.JoinPath
	}
	for _, set := range vocab.SubjectSets {
		v.sets[set.Name] = set.Type
	}
	for _, value := range vocab.SubjectValues {
		v.values[value.Name] = value.Type
	}

	return v
}

// checkEmission applies the generator's contract to one expression and
// returns the first violation, or "" when it holds.
func checkEmission(expr condition.Expr, vocab *conditiontest.Vocabulary, facts condition.Facts) string {
	v := indexVocabulary(vocab)

	source := expr.String()
	reparsed, err := condition.Parse(source)
	if err != nil {
		return "Parse() error = " + err.Error()
	}
	if got := reparsed.String(); got != source {
		return "reparse diverged: " + got
	}

	for _, name := range condition.Bindings(expr) {
		if _, ok := v.types[name]; !ok {
			return "references " + name + " outside the vocabulary"
		}
	}
	for _, name := range condition.SubjectSets(expr) {
		if _, ok := v.sets[name]; !ok {
			return "references subject set " + name + " outside the vocabulary"
		}
	}
	for _, name := range condition.SubjectValues(expr) {
		if _, ok := v.values[name]; !ok {
			return "references subject value " + name + " outside the vocabulary"
		}
	}
	if !vocab.PostImage && condition.UsesPostImage(expr) {
		return "reads the post-image in a read context"
	}

	violation := ""
	walk(expr, func(node condition.Expr) {
		if violation == "" {
			violation = checkTyping(node, v)
		}
	})
	if violation != "" {
		return violation
	}

	if _, err := condition.Fold(expr, facts); err != nil {
		return "Fold() error = " + err.Error()
	}

	return ""
}

// checkTyping applies the deploy-time typing rules to one node: post-image
// and old-vs-new right sides over columns only, literals of the attribute's
// type, subject against strings, now against timestamps, and a subject value
// or set only beside an attribute of its own type (now only against a
// timestamp-typed value).
func checkTyping(node condition.Expr, v *vocabularyTypes) string {
	switch n := node.(type) {
	case condition.Comparison:
		if n.Left.IsTemporal() {
			return ""
		}
		if n.Left.IsNow() {
			return nowFits(&n, v)
		}
		if n.Left.PostImage && !v.columns[n.Left.Name] {
			return "new." + n.Left.Name + " reads a join-path attribute"
		}
		attrType := v.types[n.Left.Name]
		switch right := n.Right.(type) {
		case condition.Literal:
			return literalFits(attrType, n.Left.Name, right)
		case condition.Subject:
			if attrType != accesstypes.AttributeTypeString {
				return n.Left.Name + " compares against subject but is not a string"
			}
		case condition.Now:
			if attrType != accesstypes.AttributeTypeTimestamp {
				return n.Left.Name + " compares against now but is not a timestamp"
			}
		case condition.SubjectValue:
			if v.values[right.Name] != attrType {
				return "subject value across types: " + n.String()
			}
		case condition.Ref:
			if !n.Left.PostImage || right.PostImage {
				return "old-vs-new form with the wrong images: " + n.String()
			}
			if !v.columns[right.Name] {
				return right.Name + " is a join-path attribute on the right of old-vs-new"
			}
			if v.types[right.Name] != attrType {
				return "old-vs-new across types: " + n.String()
			}
		}
	case condition.In:
		return inFits(&n, v)
	case condition.NullTest:
		if n.Left.PostImage && !v.columns[n.Left.Name] {
			return "new." + n.Left.Name + " reads a join-path attribute"
		}
	}

	return ""
}

// nowFits checks a comparison with now on the left: the operand is a
// timestamp literal, now, or a timestamp-typed subject value.
func nowFits(n *condition.Comparison, v *vocabularyTypes) string {
	if value, ok := n.Right.(condition.SubjectValue); ok && v.values[value.Name] != accesstypes.AttributeTypeTimestamp {
		return "now against a subject value that is not a timestamp: " + n.String()
	}

	return ""
}

// inFits checks an IN node: literals of the attribute's type, or a subject set
// of the attribute's type.
func inFits(n *condition.In, v *vocabularyTypes) string {
	if n.Left.IsTemporal() {
		return ""
	}
	if n.Left.PostImage && !v.columns[n.Left.Name] {
		return "new." + n.Left.Name + " reads a join-path attribute"
	}
	attrType := v.types[n.Left.Name]
	if n.SubjectSet != "" && v.sets[n.SubjectSet] != attrType {
		return "subject set across types: " + n.String()
	}
	for _, literal := range n.Literals {
		if msg := literalFits(attrType, n.Left.Name, literal); msg != "" {
			return msg
		}
	}

	return ""
}

// literalFits mirrors deploy validation's literal typing.
func literalFits(attrType accesstypes.AttributeType, name string, literal condition.Literal) string {
	switch l := literal.(type) {
	case condition.NumberLiteral:
		if attrType != accesstypes.AttributeTypeNumber {
			return name + " is not a number but compares against " + l.Text
		}
	case condition.BoolLiteral:
		if attrType != accesstypes.AttributeTypeBool {
			return name + " is not a bool but compares against " + l.String()
		}
	case condition.StringLiteral:
		switch attrType {
		case accesstypes.AttributeTypeString:
		case accesstypes.AttributeTypeTimestamp:
			if _, err := time.Parse(time.RFC3339, l.Value); err != nil {
				return name + " is a timestamp but compares against " + l.String()
			}
		case accesstypes.AttributeTypeDate:
			if _, err := time.Parse(time.DateOnly, l.Value); err != nil {
				return name + " is a date but compares against " + l.String()
			}
		default:
			return name + " is a " + string(attrType) + " but compares against the string " + l.String()
		}
	}

	return ""
}

// TestGenerator_deterministic pins that the sequence is a function of the
// seed, so a failing downstream case reproduces.
func TestGenerator_deterministic(t *testing.T) {
	t.Parallel()

	render := func() []string {
		full := fullVocabulary()
		gen := conditiontest.New(rand.New(rand.NewPCG(7, 11)), &full)
		out := make([]string, 0, 200)
		for range 200 {
			out = append(out, gen.Expr(4).String())
		}

		return out
	}

	if first, second := render(), render(); !slices.Equal(first, second) {
		t.Fatal("two generators from one seed diverged")
	}
}

// TestNew_rejectsVocabulary pins the construction gate: a vocabulary the
// language could not have admitted is a programming error and panics.
func TestNew_rejectsVocabulary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		vocab     conditiontest.Vocabulary
		wantPanic string
	}{
		{
			name:      "no attributes",
			vocab:     conditiontest.Vocabulary{SubjectSets: []conditiontest.SubjectBinding{{Name: "crews", Type: accesstypes.AttributeTypeString}}},
			wantPanic: "declares no attributes",
		},
		{
			name:      "attribute named after a reserved word",
			vocab:     conditiontest.Vocabulary{Attributes: []conditiontest.Attribute{{Name: "subject", Type: accesstypes.AttributeTypeString}}},
			wantPanic: "reserved word",
		},
		{
			name:      "attribute named after a keyword",
			vocab:     conditiontest.Vocabulary{Attributes: []conditiontest.Attribute{{Name: "null", Type: accesstypes.AttributeTypeString}}},
			wantPanic: "keyword",
		},
		{
			name:      "attribute that is not an identifier",
			vocab:     conditiontest.Vocabulary{Attributes: []conditiontest.Attribute{{Name: "ship-class", Type: accesstypes.AttributeTypeString}}},
			wantPanic: "not an identifier",
		},
		{
			name:      "attribute of an unknown type",
			vocab:     conditiontest.Vocabulary{Attributes: []conditiontest.Attribute{{Name: "owner", Type: "uuid"}}},
			wantPanic: "not a comparison type",
		},
		{
			name: "subject set named after a reserved word",
			vocab: conditiontest.Vocabulary{
				Attributes:  []conditiontest.Attribute{{Name: "owner", Type: accesstypes.AttributeTypeString}},
				SubjectSets: []conditiontest.SubjectBinding{{Name: "new", Type: accesstypes.AttributeTypeString}},
			},
			wantPanic: "reserved word",
		},
		{
			name: "subject value of an unknown type",
			vocab: conditiontest.Vocabulary{
				Attributes:    []conditiontest.Attribute{{Name: "owner", Type: accesstypes.AttributeTypeString}},
				SubjectValues: []conditiontest.SubjectBinding{{Name: "approvalLimit", Type: "uuid"}},
			},
			wantPanic: "not a comparison type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			defer func() {
				recovered := recover()
				if recovered == nil {
					t.Fatal("New() accepted the vocabulary, want a panic")
				}
				msg, ok := recovered.(string)
				if !ok || !strings.Contains(msg, tt.wantPanic) {
					t.Fatalf("New() panic = %v, want containing %q", recovered, tt.wantPanic)
				}
			}()

			conditiontest.New(rand.New(rand.NewPCG(1, 1)), &tt.vocab)
		})
	}
}
