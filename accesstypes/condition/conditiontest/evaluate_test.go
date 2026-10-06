package conditiontest_test

// These tests pin the reference evaluator to the language's stated meaning
// (accesstypes/condition doc, "Evaluation"): only TRUE permits; a missing
// value is UNKNOWN in every form; a number literal takes the attribute's
// storage type; and the evaluator agrees with the engine's fold on every
// expression the generator can draw.

import (
	"math/big"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/accesstypes/condition"
	"github.com/cccteam/ccc/accesstypes/condition/conditiontest"
)

func parse(t *testing.T, source string) condition.Expr {
	t.Helper()

	expr, err := condition.Parse(source)
	if err != nil {
		t.Fatalf("Parse(%q) error = %v", source, err)
	}

	return expr
}

func rat(t *testing.T, text string) *big.Rat {
	t.Helper()

	r, ok := new(big.Rat).SetString(text)
	if !ok {
		t.Fatalf("not a rational: %q", text)
	}

	return r
}

var evalNow = time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC) // a Friday

// TestEvaluate_truthTable pins the language's three-valued logic and the
// reading of a missing value in every form.
func TestEvaluate_truthTable(t *testing.T) {
	t.Parallel()

	present := map[string]any{"s": "open", "n": int64(42), "b": true, "ts": evalNow, "d": conditiontest.Date{Year: 2026, Month: 1, Day: 2}}

	tests := []struct {
		name   string
		source string
		image  conditiontest.Image
		want   conditiontest.Truth3
	}{
		{name: "comparison against a missing value is UNKNOWN", source: "s = 'open'", image: conditiontest.Image{}, want: conditiontest.Unknown},
		{name: "inequality against a missing value is UNKNOWN", source: "s != 'open'", image: conditiontest.Image{}, want: conditiontest.Unknown},
		{name: "ordering against a missing value is UNKNOWN", source: "n < 10", image: conditiontest.Image{}, want: conditiontest.Unknown},
		{name: "IN over literals against a missing value is UNKNOWN", source: "s IN ('open', 'closed')", image: conditiontest.Image{}, want: conditiontest.Unknown},
		{name: "NOT IN over literals against a missing value is UNKNOWN", source: "s NOT IN ('open')", image: conditiontest.Image{}, want: conditiontest.Unknown},
		{name: "IN over a subject set against a missing value is UNKNOWN", source: "s IN subject.teams", image: conditiontest.Image{SubjectSets: map[string][]any{"teams": {"open"}}}, want: conditiontest.Unknown},
		{name: "NOT IN over a subject set against a missing value is UNKNOWN", source: "s NOT IN subject.teams", image: conditiontest.Image{SubjectSets: map[string][]any{"teams": {"open"}}}, want: conditiontest.Unknown},
		{name: "NOT IN over an empty subject set against a missing value is UNKNOWN", source: "s NOT IN subject.teams", image: conditiontest.Image{}, want: conditiontest.Unknown},
		{name: "IS NULL on a missing value is TRUE", source: "s IS NULL", image: conditiontest.Image{}, want: conditiontest.True},
		{name: "IS NOT NULL on a missing value is FALSE", source: "s IS NOT NULL", image: conditiontest.Image{}, want: conditiontest.False},
		{name: "IS NULL on a present value is FALSE", source: "s IS NULL", image: conditiontest.Image{Pre: present}, want: conditiontest.False},
		{name: "a nil entry is a missing value", source: "s IS NULL", image: conditiontest.Image{Pre: map[string]any{"s": nil}}, want: conditiontest.True},
		{name: "a missing subject value is UNKNOWN", source: "n <= subject.quota", image: conditiontest.Image{Pre: present}, want: conditiontest.Unknown},
		{name: "now against a missing subject value is UNKNOWN", source: "now < subject.clearedUntil", image: conditiontest.Image{Pre: present, Now: evalNow}, want: conditiontest.Unknown},
		{name: "NOT of UNKNOWN is UNKNOWN", source: "NOT (s = 'open')", image: conditiontest.Image{}, want: conditiontest.Unknown},
		{name: "TRUE OR UNKNOWN is TRUE", source: "s = 'x' OR n = 42", image: conditiontest.Image{Pre: map[string]any{"n": int64(42)}}, want: conditiontest.True},
		{name: "FALSE OR UNKNOWN is UNKNOWN", source: "s = 'x' OR n = 1", image: conditiontest.Image{Pre: map[string]any{"n": int64(42)}}, want: conditiontest.Unknown},
		{name: "FALSE AND UNKNOWN is FALSE", source: "s = 'x' AND n = 1", image: conditiontest.Image{Pre: map[string]any{"n": int64(42)}}, want: conditiontest.False},
		{name: "TRUE AND UNKNOWN is UNKNOWN", source: "s = 'x' AND n = 42", image: conditiontest.Image{Pre: map[string]any{"n": int64(42)}}, want: conditiontest.Unknown},
		{name: "a folded truth leaf stands", source: "n = 42 AND s = 'open'", image: conditiontest.Image{Pre: present}, want: conditiontest.True},
		{name: "new. reads the overlay where the mutation touches the column", source: "new.s = 'closed'", image: conditiontest.Image{Pre: present, Post: map[string]any{"s": "closed"}}, want: conditiontest.True},
		{name: "new. reads the pre-image where the mutation does not touch the column", source: "new.s = 'open'", image: conditiontest.Image{Pre: present, Post: map[string]any{"n": int64(1)}}, want: conditiontest.True},
		{name: "a proposed NULL is a missing value", source: "new.s = 'open'", image: conditiontest.Image{Pre: present, Post: map[string]any{"s": nil}}, want: conditiontest.Unknown},
		{name: "old-vs-new reads the proposed left against the pre-image right", source: "new.n > n", image: conditiontest.Image{Pre: present, Post: map[string]any{"n": int64(50)}}, want: conditiontest.True},
		{name: "old-vs-new against a missing pre-image is UNKNOWN", source: "new.n > n", image: conditiontest.Image{Pre: map[string]any{}, Post: map[string]any{"n": int64(50)}}, want: conditiontest.Unknown},
		{name: "subject compares bytewise", source: "s = subject", image: conditiontest.Image{Pre: present, Subject: "open"}, want: conditiontest.True},
		{name: "now against itself", source: "now <= now", image: conditiontest.Image{Now: evalNow}, want: conditiontest.True},
		{name: "a timestamp attribute against now", source: "ts < now", image: conditiontest.Image{Pre: present, Now: evalNow.Add(time.Second)}, want: conditiontest.True},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := conditiontest.Evaluate(parse(t, tt.source), &tt.image)
			if err != nil {
				t.Fatalf("Evaluate(%q) error = %v", tt.source, err)
			}
			if got != tt.want {
				t.Fatalf("Evaluate(%q) = %s, want %s", tt.source, got, tt.want)
			}
		})
	}
}

// TestEvaluate_numbers pins rule 3: a number literal takes the attribute's
// storage type — exact against INT64 and NUMERIC, a double against FLOAT64 —
// and two stored numbers compare exactly unless either is a double.
func TestEvaluate_numbers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		pre    map[string]any
		post   map[string]any
		want   conditiontest.Truth3
	}{
		{name: "an integer against a decimal literal is exact", source: "n = 10.5", pre: map[string]any{"n": int64(10)}, want: conditiontest.False},
		{name: "an integer orders exactly against a decimal literal", source: "n > 10.5", pre: map[string]any{"n": int64(11)}, want: conditiontest.True},
		{name: "an integer equals its decimal spelling", source: "n = 10.0", pre: map[string]any{"n": int64(10)}, want: conditiontest.True},
		{name: "a NUMERIC past seventeen digits is not its double", source: "n = 10.5", pre: map[string]any{"n": rat(t, "10.500000000000000001")}, want: conditiontest.False},
		{name: "a NUMERIC equals the exact literal", source: "n = 10.5", pre: map[string]any{"n": rat(t, "10.5")}, want: conditiontest.True},
		{name: "a NUMERIC orders exactly", source: "n > 10.5", pre: map[string]any{"n": rat(t, "10.500000000000000001")}, want: conditiontest.True},
		{name: "a FLOAT64 compares as a double", source: "n = 0.1", pre: map[string]any{"n": 0.1}, want: conditiontest.True},
		{name: "a FLOAT64 past its precision equals the rounded literal", source: "n = 100000000000000000", pre: map[string]any{"n": float64(100000000000000001)}, want: conditiontest.True},
		{name: "a NUMERIC past double precision does not equal the rounded literal", source: "n = 100000000000000000", pre: map[string]any{"n": rat(t, "100000000000000001")}, want: conditiontest.False},
		{name: "an integer literal against a NUMERIC is exact", source: "n = 42", pre: map[string]any{"n": rat(t, "42")}, want: conditiontest.True},
		{name: "a negative decimal literal", source: "n < -0.25", pre: map[string]any{"n": rat(t, "-0.3")}, want: conditiontest.True},
		{name: "two stored numbers compare exactly without a double", source: "new.n >= m", pre: map[string]any{"m": rat(t, "3.000000000000000001")}, post: map[string]any{"n": int64(3)}, want: conditiontest.False},
		{name: "two stored numbers compare as doubles beside a FLOAT64", source: "new.n = m", pre: map[string]any{"m": rat(t, "3.000000000000000001")}, post: map[string]any{"n": 3.0}, want: conditiontest.True},
		{name: "an integer against a FLOAT64 pre-image", source: "new.n = m", pre: map[string]any{"m": 3.0}, post: map[string]any{"n": int64(3)}, want: conditiontest.True},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			image := conditiontest.Image{Pre: tt.pre, Post: tt.post}
			got, err := conditiontest.Evaluate(parse(t, tt.source), &image)
			if err != nil {
				t.Fatalf("Evaluate(%q) error = %v", tt.source, err)
			}
			if got != tt.want {
				t.Fatalf("Evaluate(%q) = %s, want %s", tt.source, got, tt.want)
			}
		})
	}
}

// TestEvaluate_types pins the other storage types' orderings and the subject
// set reading.
func TestEvaluate_types(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		image  conditiontest.Image
		want   conditiontest.Truth3
	}{
		{name: "strings order bytewise", source: "s < 'a'", image: conditiontest.Image{Pre: map[string]any{"s": "B"}}, want: conditiontest.True},
		{name: "the empty string is a value", source: "s = ''", image: conditiontest.Image{Pre: map[string]any{"s": ""}}, want: conditiontest.True},
		{name: "a doubled quote is data", source: "s = 'it''s'", image: conditiontest.Image{Pre: map[string]any{"s": "it's"}}, want: conditiontest.True},
		{name: "FALSE orders before TRUE", source: "b < true", image: conditiontest.Image{Pre: map[string]any{"b": false}}, want: conditiontest.True},
		{name: "a boolean equality", source: "b = true", image: conditiontest.Image{Pre: map[string]any{"b": true}}, want: conditiontest.True},
		{name: "timestamps compare as instants across offsets", source: "ts = '2026-06-30T08:00:00+02:00'", image: conditiontest.Image{Pre: map[string]any{"ts": time.Date(2026, 6, 30, 6, 0, 0, 0, time.UTC)}}, want: conditiontest.True},
		{name: "timestamps order as instants", source: "ts < '2026-01-02T15:04:05Z'", image: conditiontest.Image{Pre: map[string]any{"ts": time.Date(1999, 12, 31, 23, 59, 59, 0, time.UTC)}}, want: conditiontest.True},
		{name: "dates compare as calendar days", source: "d < '2026-01-02'", image: conditiontest.Image{Pre: map[string]any{"d": conditiontest.Date{Year: 1999, Month: 12, Day: 31}}}, want: conditiontest.True},
		{name: "a date equality", source: "d IN ('2026-01-02', '1999-12-31')", image: conditiontest.Image{Pre: map[string]any{"d": conditiontest.Date{Year: 2026, Month: 1, Day: 2}}}, want: conditiontest.True},
		{name: "a member of a subject set", source: "s IN subject.teams", image: conditiontest.Image{Pre: map[string]any{"s": "open"}, SubjectSets: map[string][]any{"teams": {"closed", "open"}}}, want: conditiontest.True},
		{name: "a non-member of a subject set", source: "s IN subject.teams", image: conditiontest.Image{Pre: map[string]any{"s": "open"}, SubjectSets: map[string][]any{"teams": {"closed"}}}, want: conditiontest.False},
		{name: "a present value is IN an empty set FALSE", source: "s IN subject.teams", image: conditiontest.Image{Pre: map[string]any{"s": "open"}}, want: conditiontest.False},
		{name: "a present value is NOT IN an empty set TRUE", source: "s NOT IN subject.teams", image: conditiontest.Image{Pre: map[string]any{"s": "open"}}, want: conditiontest.True},
		{name: "a numeric subject set compares exactly", source: "n IN subject.tiers", image: conditiontest.Image{Pre: map[string]any{"n": int64(2)}, SubjectSets: map[string][]any{"tiers": {int64(1), rat(t, "2")}}}, want: conditiontest.True},
		{name: "a null set member is skipped", source: "s NOT IN subject.teams", image: conditiontest.Image{Pre: map[string]any{"s": "open"}, SubjectSets: map[string][]any{"teams": {nil}}}, want: conditiontest.True},
		{name: "a present subject value", source: "n <= subject.quota", image: conditiontest.Image{Pre: map[string]any{"n": int64(5)}, SubjectValues: map[string]any{"quota": rat(t, "5.5")}}, want: conditiontest.True},
		{name: "now against a present subject value", source: "now < subject.clearedUntil", image: conditiontest.Image{Now: evalNow, SubjectValues: map[string]any{"clearedUntil": evalNow.Add(time.Hour)}}, want: conditiontest.True},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := conditiontest.Evaluate(parse(t, tt.source), &tt.image)
			if err != nil {
				t.Fatalf("Evaluate(%q) error = %v", tt.source, err)
			}
			if got != tt.want {
				t.Fatalf("Evaluate(%q) = %s, want %s", tt.source, got, tt.want)
			}
		})
	}
}

// TestEvaluate_temporal pins the temporal functions' wall-clock reading.
func TestEvaluate_temporal(t *testing.T) {
	t.Parallel()

	denver, err := time.LoadLocation("America/Denver")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		source  string
		zone    *time.Location
		want    conditiontest.Truth3
		wantErr string
	}{
		{name: "timeOfDay reads the named zone's clock", source: "timeOfDay(now, 'Asia/Tokyo') < '06:30'", want: conditiontest.True},
		{name: "timeOfDay in UTC", source: "timeOfDay(now, 'UTC') >= '15:04'", want: conditiontest.True},
		{name: "dayOfWeek equality", source: "dayOfWeek(now, 'UTC') = 'fri'", want: conditiontest.True},
		{name: "dayOfWeek crosses midnight in the zone", source: "dayOfWeek(now, 'Asia/Tokyo') = 'sat'", want: conditiontest.True},
		{name: "dayOfWeek membership", source: "dayOfWeek(now, 'UTC') IN ('sat', 'sun')", want: conditiontest.False},
		{name: "dayOfWeek negated membership", source: "dayOfWeek(now, 'UTC') NOT IN ('sat', 'sun')", want: conditiontest.True},
		{name: "local resolves through the image's zone", source: "timeOfDay(now, local) = '08:04'", zone: denver, want: conditiontest.True},
		{name: "local without a zone is an error", source: "timeOfDay(now, local) = '08:04'", wantErr: "does not carry"},
		{name: "an unknown zone is an error", source: "timeOfDay(now, 'Mars/Olympus') = '08:04'", wantErr: "timezone"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			image := conditiontest.Image{Now: evalNow, Zone: tt.zone}
			got, err := conditiontest.Evaluate(parse(t, tt.source), &image)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Evaluate(%q) error = %v, want containing %q", tt.source, err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("Evaluate(%q) error = %v", tt.source, err)
			}
			if got != tt.want {
				t.Fatalf("Evaluate(%q) = %s, want %s", tt.source, got, tt.want)
			}
		})
	}
}

// TestEvaluate_errors pins that a type the language never admits is an error,
// never a silent FALSE.
func TestEvaluate_errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		source  string
		image   conditiontest.Image
		wantErr string
	}{
		{name: "a string literal against a number", source: "n = 'x'", image: conditiontest.Image{Pre: map[string]any{"n": int64(1)}}, wantErr: "string literal"},
		{name: "a number literal against a string", source: "s = 1", image: conditiontest.Image{Pre: map[string]any{"s": "x"}}, wantErr: "number literal"},
		{name: "a boolean literal against a string", source: "s = true", image: conditiontest.Image{Pre: map[string]any{"s": "x"}}, wantErr: "boolean literal"},
		{name: "a malformed instant", source: "ts = 'yesterday'", image: conditiontest.Image{Pre: map[string]any{"ts": evalNow}}, wantErr: "RFC 3339"},
		{name: "a malformed date", source: "d = '2026-1-2'", image: conditiontest.Image{Pre: map[string]any{"d": conditiontest.Date{}}}, wantErr: "YYYY-MM-DD"},
		{name: "a subject value of another type", source: "n = subject.nickname", image: conditiontest.Image{Pre: map[string]any{"n": int64(1)}, SubjectValues: map[string]any{"nickname": "x"}}, wantErr: "cannot compare"},
		{name: "a set of another type", source: "n IN subject.teams", image: conditiontest.Image{Pre: map[string]any{"n": int64(1)}, SubjectSets: map[string][]any{"teams": {"x"}}}, wantErr: "cannot compare"},
		{name: "a value outside the storage vocabulary", source: "n = 1", image: conditiontest.Image{Pre: map[string]any{"n": 1}}, wantErr: "type int"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := conditiontest.Evaluate(parse(t, tt.source), &tt.image)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Evaluate(%q) error = %v, want containing %q", tt.source, err, tt.wantErr)
			}
		})
	}
}

// evaluatorVocabulary spans every type, both subject forms, a join-path
// attribute, and a write context.
var evaluatorVocabulary = conditiontest.Vocabulary{
	Attributes: []conditiontest.Attribute{
		{Name: "label", Type: accesstypes.AttributeTypeString},
		{Name: "note", Type: accesstypes.AttributeTypeString},
		{Name: "weight", Type: accesstypes.AttributeTypeNumber},
		{Name: "price", Type: accesstypes.AttributeTypeNumber},
		{Name: "fragile", Type: accesstypes.AttributeTypeBool},
		{Name: "shippedAt", Type: accesstypes.AttributeTypeTimestamp},
		{Name: "dueDate", Type: accesstypes.AttributeTypeDate},
		{Name: "carrierCode", Type: accesstypes.AttributeTypeString, JoinPath: true},
	},
	SubjectSets: []conditiontest.SubjectBinding{
		{Name: "teams", Type: accesstypes.AttributeTypeString},
		{Name: "tiers", Type: accesstypes.AttributeTypeNumber},
	},
	SubjectValues: []conditiontest.SubjectBinding{
		{Name: "quota", Type: accesstypes.AttributeTypeNumber},
		{Name: "nickname", Type: accesstypes.AttributeTypeString},
		{Name: "clearedUntil", Type: accesstypes.AttributeTypeTimestamp},
	},
	PostImage: true,
}

// randomValue draws a stored value of the type from the generator's literal
// pool, in a random storage representation for numbers, or nothing.
func randomValue(t *testing.T, rng *rand.Rand, typ accesstypes.AttributeType) any {
	t.Helper()

	if rng.IntN(4) == 0 {
		return nil
	}
	pool := conditiontest.LiteralPool(typ)
	lit := pool[rng.IntN(len(pool))]
	switch l := lit.(type) {
	case condition.BoolLiteral:
		return l.Value
	case condition.NumberLiteral:
		r := rat(t, l.Text)
		switch rng.IntN(3) {
		case 0:
			if r.IsInt() {
				return r.Num().Int64()
			}

			return r
		case 1:
			f, _ := r.Float64()

			return f
		default:
			return r
		}
	case condition.StringLiteral:
		switch typ {
		case accesstypes.AttributeTypeTimestamp:
			instant, err := time.Parse(time.RFC3339, l.Value)
			if err != nil {
				t.Fatal(err)
			}

			return instant.UTC()
		case accesstypes.AttributeTypeDate:
			d, err := conditiontest.ParseDate(l.Value)
			if err != nil {
				t.Fatal(err)
			}

			return d
		default:
			return l.Value
		}
	default:
		t.Fatalf("unexpected literal %T", lit)

		return nil
	}
}

// randomImage assembles an image over the vocabulary with random values,
// sets, and facts.
func randomImage(t *testing.T, rng *rand.Rand, vocab *conditiontest.Vocabulary) *conditiontest.Image {
	t.Helper()

	image := &conditiontest.Image{
		Pre:           make(map[string]any),
		Post:          make(map[string]any),
		Subject:       []string{"open", "u1", ""}[rng.IntN(3)],
		Now:           []time.Time{evalNow, evalNow.Add(200 * 24 * time.Hour), time.Date(1999, 12, 31, 23, 59, 59, 0, time.UTC)}[rng.IntN(3)],
		Zone:          time.UTC,
		SubjectSets:   make(map[string][]any),
		SubjectValues: make(map[string]any),
	}
	for _, attr := range vocab.Attributes {
		image.Pre[attr.Name] = randomValue(t, rng, attr.Type)
		if !attr.JoinPath && rng.IntN(2) == 0 {
			image.Post[attr.Name] = randomValue(t, rng, attr.Type)
		}
	}
	for _, set := range vocab.SubjectSets {
		for range rng.IntN(4) {
			image.SubjectSets[set.Name] = append(image.SubjectSets[set.Name], randomValue(t, rng, set.Type))
		}
	}
	for _, value := range vocab.SubjectValues {
		image.SubjectValues[value.Name] = randomValue(t, rng, value.Type)
	}

	return image
}

// TestEvaluate_agreesWithFold: for random expressions and random images, the
// engine's fold preserves meaning — the folded residue evaluates to what the
// whole expression does — and a fully factual expression folds to exactly
// the truth the evaluator finds. The generator never draws a subject value in
// a set position or a temporal term the fold refuses, so every case folds.
func TestEvaluate_agreesWithFold(t *testing.T) {
	t.Parallel()

	rng := rand.New(rand.NewPCG(20260914, 52))
	gen := conditiontest.New(rng, &evaluatorVocabulary)
	for i := range 3000 {
		expr := gen.Expr(3)
		source := expr.String()
		image := randomImage(t, rng, &evaluatorVocabulary)

		facts := condition.NewFacts().WithSubject(image.Subject).WithNow(image.Now).WithZone(image.Zone)
		folded, err := condition.Fold(expr, facts)
		if err != nil {
			t.Fatalf("case %d (%s): Fold() error = %v", i, source, err)
		}

		whole, err := conditiontest.Evaluate(expr, image)
		if err != nil {
			t.Fatalf("case %d (%s): Evaluate() error = %v", i, source, err)
		}
		residue, err := conditiontest.Evaluate(folded, image)
		if err != nil {
			t.Fatalf("case %d (%s): Evaluate(fold) error = %v", i, source, err)
		}
		if whole != residue {
			t.Fatalf("case %d (%s): the fold changed the meaning: whole = %s, folded %q = %s", i, source, whole, folded.String(), residue)
		}
		if truth, ok := folded.(condition.Truth); ok {
			want := conditiontest.False
			if truth.Value {
				want = conditiontest.True
			}
			if whole != want {
				t.Fatalf("case %d (%s): fold settled %v, evaluator found %s", i, source, truth.Value, whole)
			}
		}
	}
}
