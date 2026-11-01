package resource

import (
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"cloud.google.com/go/civil"
	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/go-playground/errors/v5"
	"golang.org/x/text/unicode/norm"
)

// Search over resources (README sections 2, 3, 4 and 9): three matches on string fields,
// startswith, endswith and contains, and one box condition, *:search:"…", whose terms
// must each be found in at least one of the fields the caller may list. The schema
// states the facts: a lowercased stored column leading an index gives a field a cheap
// case-folded starts with (the prefix fact), and a hidden TOKENLIST column carried by the
// table's one search index gives it contains, the anchored matches, and a place in the
// box (the search fact). The generator derives both facts and writes them onto the list
// and read request structs as the search tag (SearchSpec), which the decoder reads back;
// nothing is written on a table-backed struct.

// The match operators and the box.
const (
	startswithStr = "startswith"
	endswithStr   = "endswith"
	containsStr   = "contains"
	searchStr     = "search"

	// searchBoxField is the pseudo-field the box condition names: no JSON field is
	// spelled *, so it is free.
	searchBoxField = "*"
)

// The box's limits: terms split at whitespace, each non-empty.
const (
	searchMaxTerms      = 8
	searchMaxTermLength = 64
	searchMaxLength     = 200
)

// searchOptimizerVersion is the query optimizer version every Spanner statement carrying
// a search predicate names in its request options: the service's current default. Two
// key sets in one statement that each reach a search index through a join are rejected
// unhinted with a misleading version error whatever the database's own option says,
// while any explicit version plans and runs; the emulator refuses the SQL hint form and
// accepts the request option, so the option is the form the package uses. A release
// moves the constant when Spanner moves its default.
const searchOptimizerVersion = "8"

// SearchRole is how a field's token column reads its column, which decides how a typed
// term is normalized before it is bound.
type SearchRole string

const (
	// SearchRoleText tokenizes the column as it is: TOKENIZE_SUBSTRING(col). The term
	// binds as typed; Spanner folds case and punctuation.
	SearchRoleText SearchRole = "text"
	// SearchRoleDigits tokenizes the column's digits: TOKENIZE_SUBSTRING(REGEXP_REPLACE
	// (col, r'\D', '')). Everything but digits is stripped from the term, so "123-45"
	// meets "123454321".
	SearchRoleDigits SearchRole = "digits"
	// SearchRoleDate tokenizes a date or timestamp as YYYYMMDD: TOKENIZE_SUBSTRING
	// (FORMAT_DATE('%Y%m%d', col)), or FORMAT_TIMESTAMP with the zone the day is read
	// in. The term is read as a date in the forms the server accepts and binds YYYYMMDD
	// or its prefix.
	SearchRoleDate SearchRole = "date"
)

// SearchAnchor is a relative search type a token column was tokenized with.
type SearchAnchor string

const (
	// SearchAnchorPrefix is relative_search_types value_prefix: the term matches at the
	// start of the value.
	SearchAnchorPrefix SearchAnchor = "prefix"
	// SearchAnchorSuffix is value_suffix: the term matches at the end of the value.
	SearchAnchorSuffix SearchAnchor = "suffix"
)

// SearchSpec is the search tag's content: what a field answers and through which
// columns. The generator writes it onto a list or read request struct field (Tag), the
// decoder reads it back (parseSearchSpec), and the renderer binds the field's matches
// through it. A field with a Token joins its resource's box; a field with a Lower column
// alone answers startswith and nothing else.
type SearchSpec struct {
	// Token is the TOKENLIST column tokenizing the field's column, "" without the
	// search fact.
	Token string
	// Index is the search index carrying Token: the key-set statement groups a view's
	// fields by it.
	Index string
	// Role is how Token reads the column.
	Role SearchRole
	// Anchors are the relative search types Token was tokenized with.
	Anchors []SearchAnchor
	// Contains says Token answers an unanchored match: false under
	// short_tokens_only_for_anchors, where the field answers its anchors alone.
	Contains bool
	// Lower is the stored LOWER(col) column that leads an index or sits after the tenant
	// column in one: the prefix fact. "" without it.
	Lower string
	// Diacritics says Token was tokenized with remove_diacritics, so the in-memory
	// matcher folds them too.
	Diacritics bool
	// Zone is the time zone a date role over a timestamp formats its day in, as the
	// schema names it; "" on a DATE column, which has none.
	Zone string
	// InMemory marks a computed resource's allow_filter string field: every match and the
	// box are evaluated by the handler over the rows the body yields, with no column
	// behind them.
	InMemory bool
}

// Tag renders the spec as the search tag's value, the form parseSearchSpec reads.
func (s *SearchSpec) Tag() string {
	var parts []string
	if s.InMemory {
		parts = append(parts, "memory")
	}
	if s.Token != "" {
		parts = append(parts, "token="+s.Token)
	}
	if s.Index != "" {
		parts = append(parts, "index="+s.Index)
	}
	if s.Role != "" {
		parts = append(parts, "role="+string(s.Role))
	}
	if len(s.Anchors) > 0 {
		anchors := make([]string, 0, len(s.Anchors))
		for _, a := range s.Anchors {
			anchors = append(anchors, string(a))
		}
		parts = append(parts, "anchors="+strings.Join(anchors, ","))
	}
	if s.Contains {
		parts = append(parts, "contains")
	}
	if s.Lower != "" {
		parts = append(parts, "lower="+s.Lower)
	}
	if s.Diacritics {
		parts = append(parts, "diacritics")
	}
	if s.Zone != "" {
		parts = append(parts, "zone="+s.Zone)
	}

	return strings.Join(parts, ";")
}

// parseSearchSpec reads a search tag. A tag the runtime cannot read is the stale-struct
// guard: regenerate.
func parseSearchSpec(tag string) (*SearchSpec, error) {
	spec := &SearchSpec{}
	for part := range strings.SplitSeq(tag, ";") {
		if err := spec.readPart(part); err != nil {
			return nil, err
		}
	}
	if spec.Role == "" {
		spec.Role = SearchRoleText
	}
	if spec.Token == "" && spec.Lower == "" && !spec.InMemory {
		return nil, errors.New("search tag names neither a token column, a lowercased column nor memory")
	}

	return spec, nil
}

// readPart reads one semicolon-separated part of the tag: a flag, or a key with its
// value.
func (s *SearchSpec) readPart(part string) error {
	key, value, hasValue := strings.Cut(part, "=")
	if !hasValue {
		return s.readFlag(part)
	}
	switch key {
	case "role":
		return s.readRole(value)
	case "anchors":
		return s.readAnchors(value)
	case "token", "index", "lower", "zone":
		if value != "" {
			s.setColumn(key, value)

			return nil
		}
	}

	return errors.Newf("search tag carries %q, which is not part of its grammar", part)
}

// readFlag reads a part without a value.
func (s *SearchSpec) readFlag(key string) error {
	switch key {
	case "memory":
		s.InMemory = true
	case "contains":
		s.Contains = true
	case "diacritics":
		s.Diacritics = true
	default:
		return errors.Newf("search tag carries %q, which is not part of its grammar", key)
	}

	return nil
}

// setColumn stores a named column or zone.
func (s *SearchSpec) setColumn(key, value string) {
	switch key {
	case "token":
		s.Token = value
	case "index":
		s.Index = value
	case "lower":
		s.Lower = value
	case "zone":
		s.Zone = value
	}
}

// readRole reads the role, one of the three.
func (s *SearchSpec) readRole(value string) error {
	role := SearchRole(value)
	if role != SearchRoleText && role != SearchRoleDigits && role != SearchRoleDate {
		return errors.Newf("search tag names the role %q, which is not text, digits or date", value)
	}
	s.Role = role

	return nil
}

// readAnchors reads the comma-separated anchors.
func (s *SearchSpec) readAnchors(value string) error {
	for anchor := range strings.SplitSeq(value, ",") {
		a := SearchAnchor(anchor)
		if a != SearchAnchorPrefix && a != SearchAnchorSuffix {
			return errors.Newf("search tag names the anchor %q, which is not prefix or suffix", anchor)
		}
		s.Anchors = append(s.Anchors, a)
	}

	return nil
}

// JoinsBox reports whether the field takes part in its resource's search box: a field
// with the search fact, or a computed resource's evaluated field. A field with the prefix
// fact alone does not: the box over a table with a search index never mixes in the
// prefix fact, so a table serves its box from one index.
func (s *SearchSpec) JoinsBox() bool {
	return s != nil && (s.Token != "" || s.InMemory)
}

// HasAnchor reports whether the token column was tokenized with the anchor.
func (s *SearchSpec) HasAnchor(anchor SearchAnchor) bool {
	return s != nil && slices.Contains(s.Anchors, anchor)
}

// Answers reports whether the field answers the match as an explicit operator on a
// string field: contains through an unanchored token column, startswith through the
// lowercased column or a prefix anchor, endswith through a suffix anchor; an evaluated
// field answers all three.
func (s *SearchSpec) Answers(operator string) bool {
	if s == nil {
		return false
	}
	if s.InMemory {
		return operator == containsStr || operator == startswithStr || operator == endswithStr
	}
	if s.Role == SearchRoleDate {
		// A date joins the box, where a typed date meets it as YYYYMMDD; it answers no
		// text match of its own.
		return false
	}
	switch operator {
	case containsStr:
		return s.Contains
	case startswithStr:
		return s.Lower != "" || s.HasAnchor(SearchAnchorPrefix)
	case endswithStr:
		return s.HasAnchor(SearchAnchorSuffix)
	default:
		return false
	}
}

// Matches lists the explicit matches the field answers, in the order the metadata
// spells them.
func (s *SearchSpec) Matches() []string {
	var matches []string
	for _, operator := range []string{containsStr, startswithStr, endswithStr} {
		if s.Answers(operator) {
			matches = append(matches, operator)
		}
	}

	return matches
}

// SearchBranch is one @searchAlso declaration as the generated SearchBranches method
// carries it: a child table's text a parent's box also searches, mapped to the parent's
// key through the child's foreign key. The renderer adds one branch per declaration to
// each term's key set; the child field's own permission governs the branch.
type SearchBranch struct {
	// Name is the field's dotted name in the parent's search field list and the box's
	// placeholder: clientContact.displayName.
	Name string
	// Resource is the child resource and Field its Go field; Tag is the field's wire
	// name, under which its permission is checked (Resource.Tag).
	Resource accesstypes.Resource
	Field    accesstypes.Field
	Tag      accesstypes.Tag
	// Table is the child table, Column the text column, JoinColumn the foreign key to
	// the parent's key, and TenantColumn the child's bare tenant column, "" when the
	// pair is global.
	Table        string
	Column       string
	JoinColumn   string
	TenantColumn string
	// The child field's search fact.
	Token    string
	Index    string
	Role     SearchRole
	Anchors  []SearchAnchor
	Contains bool
	Zone     string
	// PII says the child field is PII, so the box posts.
	PII bool
}

// spec is the branch's search fact as a spec.
func (b *SearchBranch) spec() *SearchSpec {
	return &SearchSpec{Token: b.Token, Index: b.Index, Role: b.Role, Anchors: b.Anchors, Contains: b.Contains, Zone: b.Zone}
}

// searchBrancher is the generated declaration of a resource's @searchAlso branches, read
// off the resource type at metadata construction as the file keys are.
type searchBrancher interface {
	SearchBranches() []SearchBranch
}

// searchField is one field of the resource's own search vocabulary as a condition
// carries it: the name the parse target spells it by, its column, and its spec.
type searchField struct {
	// name is the condition's identifier for the parse target: the column for a
	// database, the Go field for the evaluator.
	name string
	// column is the field's database column, for the expressions the PostgreSQL forms
	// build and for the grants keyed by column.
	column string
	// goField and jsonName name the field for the evaluator and the messages.
	goField  string
	jsonName string
	spec     *SearchSpec
	pii      bool
	// positional marks a masking:"positional" field, whose match renders bare under a
	// conditional grant.
	positional bool
}

// searchCondition is the box as a parsed condition: the text, its terms, the resource's
// own fields that take part, and the child branches declared on the resource.
type searchCondition struct {
	text     string
	terms    []string
	fields   []searchField
	branches []SearchBranch
	// key is the resource's key column for the parse target, which the key-set
	// statement and the PostgreSQL branch form join on; "" when the resource declares
	// no single-column key, which the generator refuses where a branch needs one.
	key string
}

// IsTextMatch reports whether the condition is a startswith, endswith or contains
// match on a text field.
func (c *Condition) IsTextMatch() bool {
	return c.match != nil && c.match.spec != nil
}

// MatchesText evaluates a text match against a cell's text the way the handler
// evaluates it over a row: the text folded as the search index folds it, the term read
// for the field's role and folded the same way. A body that took the field answers its
// matches with it, since Take is all-or-nothing per field. False on any other condition.
func (c *Condition) MatchesText(text string) bool {
	if !c.IsTextMatch() {
		return false
	}
	term, _ := c.Value.(string)
	normalized, ok := matchTermForRole(term, c.match.spec)
	if !ok {
		return false
	}
	cell := text
	if c.match.spec.Role == SearchRoleText || c.match.spec.Role == "" {
		cell = foldSearchText(text, c.match.spec.Diacritics)
	}

	return matchSearchText(cell, normalized, c.Operator)
}

// SearchTerms returns the box's terms, the words the typed text split into, for a body
// that took the box and pushes it down itself.
func (c *Condition) SearchTerms() []string {
	if c.search == nil {
		return nil
	}

	return slices.Clone(c.search.terms)
}

// SearchFields names the Go fields the box searches on the resource, in declaration
// order, for a body that took the box.
func (c *Condition) SearchFields() []string {
	if c.search == nil {
		return nil
	}
	fields := make([]string, 0, len(c.search.fields))
	for _, f := range c.search.fields {
		fields = append(fields, f.goField)
	}

	return fields
}

// splitSearchTerms splits the box's text into its terms and applies the limits.
func splitSearchTerms(text string) ([]string, error) {
	if len(text) > searchMaxLength {
		return nil, fmt.Errorf("the search text is %d characters long; at most %d are read", len(text), searchMaxLength)
	}
	terms := strings.Fields(text)
	if len(terms) == 0 {
		return nil, errors.New("the search text is empty")
	}
	if len(terms) > searchMaxTerms {
		return nil, fmt.Errorf("the search text has %d terms; at most %d are read", len(terms), searchMaxTerms)
	}
	for _, term := range terms {
		if len(term) > searchMaxTermLength {
			return nil, fmt.Errorf("the search term %q is %d characters long; at most %d are read", term, len(term), searchMaxTermLength)
		}
	}

	return terms, nil
}

// normalizeSearchTerm reads a typed term for a field's role: as typed for text, its
// digits for digits, and as YYYYMMDD or a prefix of it for a date. The second result is
// false when the term normalizes to nothing for the role, in which case it contributes
// nothing for the field.
func normalizeSearchTerm(term string, role SearchRole) (string, bool) {
	switch role {
	case SearchRoleDigits:
		digits := digitsOf(term)

		return digits, digits != ""
	case SearchRoleDate:
		return dateDigits(term)
	default:
		term = strings.TrimSpace(term)

		return term, term != ""
	}
}

// digitsOf keeps the decimal digits of s.
func digitsOf(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}

	return b.String()
}

// The typed date forms the server reads: ISO (a year, a year and month, a full date), US
// month/day/year and month/year, and eight digits as YYYYMMDD.
var (
	isoYearPattern   = regexp.MustCompile(`^(\d{4})$`)
	isoMonthPattern  = regexp.MustCompile(`^(\d{4})-(\d{1,2})$`)
	isoDatePattern   = regexp.MustCompile(`^(\d{4})-(\d{1,2})-(\d{1,2})$`)
	usDatePattern    = regexp.MustCompile(`^(\d{1,2})/(\d{1,2})/(\d{4})$`)
	usMonthPattern   = regexp.MustCompile(`^(\d{1,2})/(\d{4})$`)
	digitDatePattern = regexp.MustCompile(`^(\d{4})(\d{2})(\d{2})$`)
)

// dateDigits reads a typed term as a date and answers YYYYMMDD, YYYYMM or YYYY.
func dateDigits(term string) (string, bool) {
	term = strings.TrimSpace(term)
	if m := isoYearPattern.FindStringSubmatch(term); m != nil {
		return m[1], true
	}
	if m := isoMonthPattern.FindStringSubmatch(term); m != nil {
		return yearMonth(m[1], m[2])
	}
	if m := usMonthPattern.FindStringSubmatch(term); m != nil {
		return yearMonth(m[2], m[1])
	}
	if m := isoDatePattern.FindStringSubmatch(term); m != nil {
		return fullDate(m[1], m[2], m[3])
	}
	if m := usDatePattern.FindStringSubmatch(term); m != nil {
		return fullDate(m[3], m[1], m[2])
	}
	if m := digitDatePattern.FindStringSubmatch(term); m != nil {
		return fullDate(m[1], m[2], m[3])
	}

	return "", false
}

func yearMonth(year, month string) (string, bool) {
	m, err := strconv.Atoi(month)
	if err != nil || m < 1 || m > 12 {
		return "", false
	}

	return fmt.Sprintf("%s%02d", year, m), true
}

func fullDate(year, month, day string) (string, bool) {
	y, errY := strconv.Atoi(year)
	m, errM := strconv.Atoi(month)
	d, errD := strconv.Atoi(day)
	if errY != nil || errM != nil || errD != nil {
		return "", false
	}
	date := civil.Date{Year: y, Month: time.Month(m), Day: d}
	if !date.IsValid() {
		return "", false
	}

	return fmt.Sprintf("%04d%02d%02d", y, m, d), true
}

// foldSearchText normalizes text the way the substring tokenizer reads it on every case
// the probe exercised: lowercased, apostrophes removed, other punctuation and symbols
// mapped to spaces, whitespace collapsed; with diacritics, combining marks are dropped
// after decomposition. The in-memory evaluator and the whole-list picker compare folded
// text with folded terms.
func foldSearchText(s string, diacritics bool) string {
	if diacritics {
		s = norm.NFD.String(s)
	}
	var b strings.Builder
	b.Grow(len(s))
	space := false
	for _, r := range s {
		switch {
		case r == '\'' || r == '’':
			continue
		case diacritics && unicode.Is(unicode.Mn, r):
			continue
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			space = false
			b.WriteRune(unicode.ToLower(r))
		default:
			space = true
		}
	}

	return b.String()
}

// searchValueText renders a row's cell for the in-memory matcher by the field's role:
// text folded, digits kept, a date or timestamp as YYYYMMDD in the spec's zone. The
// second result is false for a NULL cell, which matches nothing.
func searchValueText(field reflect.Value, spec *SearchSpec) (string, bool) {
	value, isNull := derefNullable(field)
	if isNull {
		return "", false
	}
	switch spec.Role {
	case SearchRoleDigits:
		return digitsOf(fmt.Sprint(value.Interface())), true
	case SearchRoleDate:
		return dateCellDigits(value, spec.Zone)
	default:
		return foldSearchText(fmt.Sprint(value.Interface()), spec.Diacritics), true
	}
}

// dateCellDigits renders a date or timestamp cell as YYYYMMDD.
func dateCellDigits(value reflect.Value, zone string) (string, bool) {
	switch v := value.Interface().(type) {
	case civil.Date:
		return fmt.Sprintf("%04d%02d%02d", v.Year, int(v.Month), v.Day), true
	case time.Time:
		loc := time.UTC
		if zone != "" {
			if l, err := time.LoadLocation(zone); err == nil {
				loc = l
			}
		}
		t := v.In(loc)

		return fmt.Sprintf("%04d%02d%02d", t.Year(), int(t.Month()), t.Day()), true
	case spanner.NullDate:
		return dateCellDigits(reflect.ValueOf(v.Date), zone)
	case spanner.NullTime:
		return dateCellDigits(reflect.ValueOf(v.Time), zone)
	default:
		return dateDigits(fmt.Sprint(value.Interface()))
	}
}

// matchSearchText evaluates one match of a normalized term against a cell's text, the
// in-memory reading of the renderer's forms.
func matchSearchText(cell, term, operator string) bool {
	switch operator {
	case startswithStr:
		return strings.HasPrefix(cell, term)
	case endswithStr:
		return strings.HasSuffix(cell, term)
	default:
		return strings.Contains(cell, term)
	}
}

// boxMatches evaluates a spec's box contribution for one normalized term against a
// cell: each declared anchor, or contains where none is declared; an evaluated field
// contributes contains.
func boxMatches(cell, term string, spec *SearchSpec) bool {
	if spec.InMemory || len(spec.Anchors) == 0 {
		return strings.Contains(cell, term)
	}
	for _, anchor := range spec.Anchors {
		switch anchor {
		case SearchAnchorPrefix:
			if strings.HasPrefix(cell, term) {
				return true
			}
		case SearchAnchorSuffix:
			if strings.HasSuffix(cell, term) {
				return true
			}
		}
	}

	return false
}

// matchTermForRole reads an explicit match's term for the field's role and folds it the
// way the cell is folded.
func matchTermForRole(term string, spec *SearchSpec) (string, bool) {
	normalized, ok := normalizeSearchTerm(term, spec.Role)
	if !ok {
		return "", false
	}
	if spec.Role == SearchRoleText || spec.Role == "" {
		normalized = foldSearchText(normalized, spec.Diacritics)
	}

	return normalized, normalized != ""
}

// spannerSearchTerm is the parameter a Spanner search function binds for a term: the
// role's normalization, as typed for text.
func spannerSearchTerm(term string, spec *SearchSpec) (string, bool) {
	return normalizeSearchTerm(term, spec.Role)
}

// relativeSearchType spells an anchor as the SEARCH_SUBSTRING argument.
func relativeSearchType(anchor SearchAnchor) string {
	if anchor == SearchAnchorSuffix {
		return "value_suffix"
	}

	return "value_prefix"
}

// escapeLikePattern escapes the LIKE metacharacters of a term for a PostgreSQL pattern:
// the backslash, the percent sign and the underscore.
func escapeLikePattern(term string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

	return replacer.Replace(term)
}
