package resource

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/civil"
	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

// The search vocabulary under test (README section 4): a resource with every token
// shape, a field in a second index, the prefix fact, and a @searchAlso branch.
var (
	searchNameSpec   = &SearchSpec{Token: "Name_Search", Index: "RowsSearch", Role: SearchRoleText, Anchors: []SearchAnchor{SearchAnchorPrefix, SearchAnchorSuffix}, Contains: true}
	searchPhoneSpec  = &SearchSpec{Token: "Phone_Search", Index: "RowsSearch", Role: SearchRoleDigits, Contains: true}
	searchBornOnSpec = &SearchSpec{Token: "BornOn_Search", Index: "RowsSearch", Role: SearchRoleDate, Contains: true}
	searchEmailSpec  = &SearchSpec{Token: "Email_Search", Index: "RowsSearch", Role: SearchRoleText, Contains: true, Diacritics: true}
	searchTitleSpec  = &SearchSpec{Token: "Title_Search", Index: "OtherSearch", Role: SearchRoleText, Contains: true}
	searchCodeSpec   = &SearchSpec{Lower: "Code_Lower", Role: SearchRoleText}
	searchBranch     = SearchBranch{Name: "contact.displayName", Resource: "Contacts", Field: "DisplayName", Tag: "displayName", Table: "Contacts", Column: "DisplayName", JoinColumn: "RowId", Token: "DisplayName_Search", Index: "ContactsSearch", Role: SearchRoleText, Contains: true}
	searchKeys       = map[DBType]string{SpannerDBType: "Id", PostgresDBType: "Id", goFieldNames: "ID"}
)

func searchTestFields() map[jsonFieldName]FilterFieldInfo {
	cols := func(name string) map[DBType]string {
		return map[DBType]string{SpannerDBType: name, PostgresDBType: name}
	}

	return map[jsonFieldName]FilterFieldInfo{
		"id":     {JSONFieldName: "id", GOFieldName: "ID", dbColumnNames: cols("Id"), Kind: reflect.String, Indexed: true, order: 0},
		"name":   {JSONFieldName: "name", GOFieldName: "Name", dbColumnNames: cols("Name"), Kind: reflect.String, Indexed: true, order: 1, Search: searchNameSpec},
		"phone":  {JSONFieldName: "phone", GOFieldName: "Phone", dbColumnNames: cols("Phone"), Kind: reflect.String, order: 2, Search: searchPhoneSpec},
		"bornOn": {JSONFieldName: "bornOn", GOFieldName: "BornOn", dbColumnNames: cols("BornOn"), Kind: reflect.Struct, FieldType: reflect.TypeFor[civil.Date](), order: 3, Search: searchBornOnSpec},
		"email":  {JSONFieldName: "email", GOFieldName: "Email", dbColumnNames: cols("Email"), Kind: reflect.String, PII: true, order: 4, Search: searchEmailSpec},
		"title":  {JSONFieldName: "title", GOFieldName: "Title", dbColumnNames: cols("Title"), Kind: reflect.String, order: 5, Search: searchTitleSpec},
		"code":   {JSONFieldName: "code", GOFieldName: "Code", dbColumnNames: cols("Code"), Kind: reflect.String, order: 6, Search: searchCodeSpec},
		"plain":  {JSONFieldName: "plain", GOFieldName: "Plain", dbColumnNames: cols("Plain"), Kind: reflect.String, Indexed: true, order: 7},
	}
}

// parseSearchFilter parses a filter over the search fields for a target, with the
// branches and keys when asked.
func parseSearchFilter(t *testing.T, filter string, target DBType, withBranches bool) (ExpressionNode, error) {
	t.Helper()

	parser, err := NewFilterParser(NewFilterLexer(filter), searchTestFields())
	if err != nil {
		return nil, err
	}
	if withBranches {
		parser.WithSearch([]SearchBranch{searchBranch}, searchKeys)
	} else {
		parser.WithSearch(nil, searchKeys)
	}

	return parser.Parse(target)
}

func TestSearchSpec_TagRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		spec    *SearchSpec
		wantTag string
		matches []string
		box     bool
	}{
		{name: "anchored text", spec: searchNameSpec, wantTag: "token=Name_Search;index=RowsSearch;role=text;anchors=prefix,suffix;contains", matches: []string{"contains", "startswith", "endswith"}, box: true},
		{name: "digits", spec: searchPhoneSpec, wantTag: "token=Phone_Search;index=RowsSearch;role=digits;contains", matches: []string{"contains"}, box: true},
		{name: "a date answers no match and joins the box", spec: searchBornOnSpec, wantTag: "token=BornOn_Search;index=RowsSearch;role=date;contains", matches: nil, box: true},
		{name: "diacritics folded", spec: searchEmailSpec, wantTag: "token=Email_Search;index=RowsSearch;role=text;contains;diacritics", matches: []string{"contains"}, box: true},
		{name: "the prefix fact alone", spec: searchCodeSpec, wantTag: "role=text;lower=Code_Lower", matches: []string{"startswith"}, box: false},
		{name: "anchors alone under short_tokens_only_for_anchors", spec: &SearchSpec{Token: "T", Index: "I", Role: SearchRoleText, Anchors: []SearchAnchor{SearchAnchorPrefix}}, wantTag: "token=T;index=I;role=text;anchors=prefix", matches: []string{"startswith"}, box: true},
		{name: "a zoned timestamp", spec: &SearchSpec{Token: "T", Index: "I", Role: SearchRoleDate, Contains: true, Zone: "America/New_York"}, wantTag: "token=T;index=I;role=date;contains;zone=America/New_York", matches: nil, box: true},
		{name: "in memory", spec: &SearchSpec{InMemory: true, Role: SearchRoleText}, wantTag: "memory;role=text", matches: []string{"contains", "startswith", "endswith"}, box: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tag := tt.spec.Tag()
			if tag != tt.wantTag {
				t.Errorf("Tag() = %q, want %q", tag, tt.wantTag)
			}
			parsed, err := parseSearchSpec(tag)
			if err != nil {
				t.Fatalf("parseSearchSpec(%q) error = %v", tag, err)
			}
			if diff := cmp.Diff(tt.spec, parsed); diff != "" {
				t.Errorf("parseSearchSpec(Tag()) round trip mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.matches, parsed.Matches()); diff != "" {
				t.Errorf("Matches() mismatch (-want +got):\n%s", diff)
			}
			if got := parsed.JoinsBox(); got != tt.box {
				t.Errorf("JoinsBox() = %v, want %v", got, tt.box)
			}
		})
	}
}

func TestParseSearchSpec_Refusals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		tag     string
		wantErr string
	}{
		{tag: "token=T;role=sound", wantErr: "not text, digits or date"},
		{tag: "token=T;anchors=middle", wantErr: "not prefix or suffix"},
		{tag: "token=T;shape=round", wantErr: "not part of its grammar"},
		{tag: "contains", wantErr: "neither a token column, a lowercased column nor memory"},
		{tag: "", wantErr: "not part of its grammar"},
	}
	for _, tt := range tests {
		t.Run(tt.tag, func(t *testing.T) {
			t.Parallel()

			_, err := parseSearchSpec(tt.tag)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("parseSearchSpec(%q) error = %v, want %q", tt.tag, err, tt.wantErr)
			}
		})
	}
}

func TestNormalizeSearchTerm(t *testing.T) {
	t.Parallel()

	tests := []struct {
		term   string
		role   SearchRole
		want   string
		wantOK bool
	}{
		{term: " Ann ", role: SearchRoleText, want: "Ann", wantOK: true},
		{term: "   ", role: SearchRoleText, wantOK: false},
		{term: "555-12", role: SearchRoleDigits, want: "55512", wantOK: true},
		{term: "ext.", role: SearchRoleDigits, wantOK: false},
		{term: "1987", role: SearchRoleDate, want: "1987", wantOK: true},
		{term: "1987-3", role: SearchRoleDate, want: "198703", wantOK: true},
		{term: "3/1987", role: SearchRoleDate, want: "198703", wantOK: true},
		{term: "1987-03-14", role: SearchRoleDate, want: "19870314", wantOK: true},
		{term: "3/14/1987", role: SearchRoleDate, want: "19870314", wantOK: true},
		{term: "19870314", role: SearchRoleDate, want: "19870314", wantOK: true},
		{term: "1987-13", role: SearchRoleDate, wantOK: false},
		{term: "2/30/1987", role: SearchRoleDate, wantOK: false},
		{term: "19871332", role: SearchRoleDate, wantOK: false},
		{term: "ann", role: SearchRoleDate, wantOK: false},
		{term: "555", role: SearchRoleDate, wantOK: false},
	}
	for _, tt := range tests {
		t.Run(string(tt.role)+" "+tt.term, func(t *testing.T) {
			t.Parallel()

			got, ok := normalizeSearchTerm(tt.term, tt.role)
			if ok != tt.wantOK || got != tt.want {
				t.Errorf("normalizeSearchTerm(%q, %s) = %q, %v, want %q, %v", tt.term, tt.role, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestSplitSearchTerms_Limits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		text    string
		want    []string
		wantErr string
	}{
		{name: "words", text: "  nguyen 1987\t4321 ", want: []string{"nguyen", "1987", "4321"}},
		{name: "empty", text: "   ", wantErr: "empty"},
		{name: "too many terms", text: strings.Repeat("a ", searchMaxTerms+1), wantErr: "at most 8"},
		{name: "a term too long", text: strings.Repeat("a", searchMaxTermLength+1), wantErr: "at most 64"},
		{name: "text too long", text: strings.Repeat("abcdefg ", 30), wantErr: "at most 200"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := splitSearchTerms(tt.text)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("splitSearchTerms() error = %v, want %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("splitSearchTerms() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("terms mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestFilterParser_Search(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		filter       string
		branches     bool
		wantErr      string
		wantOperator string
		wantTerms    []string
		wantFields   []string
	}{
		{name: "startswith on an anchored field", filter: "name:startswith:ann", wantOperator: startswithStr},
		{name: "endswith on an anchored field", filter: "name:endswith:son", wantOperator: endswithStr},
		{name: "contains through the lowercased column is not answered", filter: "code:contains:b", wantErr: "unknown operator 'contains' for 'code' in condition 'code:contains:b': the field answers startswith"},
		{name: "startswith on a plain token column is not answered", filter: "email:startswith:a", wantErr: "the field answers contains"},
		{name: "a date answers no match", filter: "bornOn:contains:1987", wantErr: "matches text and 'bornOn' is not a text field"},
		{name: "a field without a search fact answers no match", filter: "plain:contains:x", wantErr: "the field answers no text match: eq, ne, in, notin, isnull and isnotnull"},
		{name: "a match stands alone as the indexed touch", filter: "phone:contains:555", wantOperator: containsStr},
		{name: "an eq on a search field needs an indexed companion", filter: "phone:eq:555", wantErr: "Filter must contain at least one column that is indexed"},
		{name: "an empty match term is refused", filter: `name:contains:""`, wantErr: "requires a non-empty value"},
		{name: "the box", filter: `*:search:"nguyen 1987 4321"`, wantOperator: searchStr, wantTerms: []string{"nguyen", "1987", "4321"}, wantFields: []string{"Name", "Phone", "BornOn", "Email", "Title"}},
		{name: "the box with branches", filter: `*:search:"ann"`, branches: true, wantOperator: searchStr, wantTerms: []string{"ann"}, wantFields: []string{"Name", "Phone", "BornOn", "Email", "Title"}},
		{name: "the box takes search alone", filter: `*:eq:ann`, wantErr: "'*' takes the operator 'search' alone"},
		{name: "search names no field", filter: `name:search:ann`, wantErr: "operator 'search' is the search box's and names no field"},
		{name: "one box per filter", filter: `*:search:"a",*:search:"b"`, wantErr: "one search box per filter"},
		{name: "the box's limits apply", filter: `*:search:"a b c d e f g h i"`, wantErr: "at most 8"},
		{name: "a quoted value carries the separators", filter: `name:eq:"Ann, Lee|Co (Ltd)"`, wantOperator: eqStr},
		{name: "a backslash escapes a quote", filter: `name:eq:"say \"hi\""`, wantOperator: eqStr},
		{name: "an unclosed quote is refused", filter: `name:eq:"Ann`, wantErr: "never closed"},
		{name: "a stray escape is refused", filter: `name:eq:"a\nb"`, wantErr: "escapes a quote or a backslash only"},
		{name: "quoted values in an in list", filter: `name:in:("Ann Lee","Bo, Li")`, wantOperator: inStr},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ast, err := parseSearchFilter(t, tt.filter, SpannerDBType, tt.branches)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Parse(%q) error = %v, want %q", tt.filter, err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("Parse(%q) error = %v", tt.filter, err)
			}
			node, ok := ast.(*ConditionNode)
			if !ok {
				t.Fatalf("Parse(%q) = %T, want one condition", tt.filter, ast)
			}
			if node.Condition.Operator != tt.wantOperator {
				t.Errorf("Operator = %q, want %q", node.Condition.Operator, tt.wantOperator)
			}
			switch tt.wantOperator {
			case searchStr:
				if !node.Condition.IsSearch() {
					t.Error("IsSearch() = false, want true")
				}
				if diff := cmp.Diff(tt.wantTerms, node.Condition.SearchTerms()); diff != "" {
					t.Errorf("SearchTerms() mismatch (-want +got):\n%s", diff)
				}
				if diff := cmp.Diff(tt.wantFields, node.Condition.SearchFields()); diff != "" {
					t.Errorf("SearchFields() mismatch (-want +got):\n%s", diff)
				}
				if got := len(node.Condition.search.branches); got != map[bool]int{true: 1, false: 0}[tt.branches] {
					t.Errorf("branches = %d, want %d", got, map[bool]int{true: 1, false: 0}[tt.branches])
				}
				if node.Condition.search.key != "Id" {
					t.Errorf("key = %q, want Id", node.Condition.search.key)
				}
			case startswithStr, endswithStr, containsStr:
				if !node.Condition.IsTextMatch() {
					t.Error("IsTextMatch() = false, want true")
				}
			}
		})
	}
}

func TestFilterParser_QuotedValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		filter string
		want   any
	}{
		{filter: `name:eq:"Ann, Lee|Co (Ltd)"`, want: "Ann, Lee|Co (Ltd)"},
		{filter: `name:eq:"say \"hi\" \\ bye"`, want: `say "hi" \ bye`},
		{filter: `name:eq:plain`, want: "plain"},
	}
	for _, tt := range tests {
		t.Run(tt.filter, func(t *testing.T) {
			t.Parallel()

			ast, err := parseSearchFilter(t, tt.filter, SpannerDBType, false)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			node, ok := ast.(*ConditionNode)
			if !ok {
				t.Fatalf("Parse() = %T, want one condition", ast)
			}
			if node.Condition.Value != tt.want {
				t.Errorf("Value = %#v, want %#v", node.Condition.Value, tt.want)
			}
		})
	}
	t.Run("an in list splits at the commas outside quotes", func(t *testing.T) {
		t.Parallel()

		ast, err := parseSearchFilter(t, `name:in:("Ann Lee","Bo, Li",Cy)`, SpannerDBType, false)
		if err != nil {
			t.Fatalf("Parse() error = %v", err)
		}
		node, ok := ast.(*ConditionNode)
		if !ok {
			t.Fatalf("Parse() = %T, want one condition", ast)
		}
		if diff := cmp.Diff([]any{"Ann Lee", "Bo, Li", "Cy"}, node.Condition.Values); diff != "" {
			t.Errorf("Values mismatch (-want +got):\n%s", diff)
		}
	})
}

func TestSQLGenerator_Search(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		filter     string
		dialect    SQLDialect
		branches   bool
		grants     map[string]searchGrant
		branchDrop bool
		wantSQL    string
		wantParams map[string]any
		wantErr    string
	}{
		{name: "startswith through a prefix anchor", filter: "name:startswith:Ann", dialect: Spanner, wantSQL: "SEARCH_SUBSTRING(`Name_Search`, @_p1, relative_search_type => 'value_prefix')", wantParams: map[string]any{"_p1": "Ann"}},
		{name: "endswith through a suffix anchor", filter: "name:endswith:son", dialect: Spanner, wantSQL: "SEARCH_SUBSTRING(`Name_Search`, @_p1, relative_search_type => 'value_suffix')", wantParams: map[string]any{"_p1": "son"}},
		{name: "contains", filter: "name:contains:nn", dialect: Spanner, wantSQL: "SEARCH_SUBSTRING(`Name_Search`, @_p1)", wantParams: map[string]any{"_p1": "nn"}},
		{name: "digits strip the term", filter: "phone:contains:555-12", dialect: Spanner, wantSQL: "SEARCH_SUBSTRING(`Phone_Search`, @_p1)", wantParams: map[string]any{"_p1": "55512"}},
		{name: "a term with no digits never meets a digits field", filter: "phone:contains:ext", dialect: Spanner, wantSQL: "FALSE", wantParams: map[string]any{}},
		{name: "startswith through the lowercased column", filter: "code:startswith:AbC", dialect: Spanner, wantSQL: "STARTS_WITH(`Code_Lower`, @_p1)", wantParams: map[string]any{"_p1": "abc"}},
		{name: "the box inline over one index", filter: `*:search:"ann"`, dialect: Spanner, wantSQL: "((SEARCH_SUBSTRING(`Name_Search`, @_p1, relative_search_type => 'value_prefix') OR SEARCH_SUBSTRING(`Name_Search`, @_p2, relative_search_type => 'value_suffix')) OR SEARCH_SUBSTRING(`Email_Search`, @_p3) OR SEARCH_SUBSTRING(`Title_Search`, @_p4))", wantParams: map[string]any{"_p1": "ann", "_p2": "ann", "_p3": "ann", "_p4": "ann"}},
		{name: "the box ANDs its terms, each read for every role", filter: `*:search:"555 1987-03"`, dialect: Spanner, wantSQL: "(((SEARCH_SUBSTRING(`Name_Search`, @_p1, relative_search_type => 'value_prefix') OR SEARCH_SUBSTRING(`Name_Search`, @_p2, relative_search_type => 'value_suffix')) OR SEARCH_SUBSTRING(`Phone_Search`, @_p3) OR SEARCH_SUBSTRING(`Email_Search`, @_p4) OR SEARCH_SUBSTRING(`Title_Search`, @_p5)) AND ((SEARCH_SUBSTRING(`Name_Search`, @_p6, relative_search_type => 'value_prefix') OR SEARCH_SUBSTRING(`Name_Search`, @_p7, relative_search_type => 'value_suffix')) OR SEARCH_SUBSTRING(`Phone_Search`, @_p8) OR SEARCH_SUBSTRING(`BornOn_Search`, @_p9) OR SEARCH_SUBSTRING(`Email_Search`, @_p10) OR SEARCH_SUBSTRING(`Title_Search`, @_p11)))", wantParams: map[string]any{"_p1": "555", "_p2": "555", "_p3": "555", "_p4": "555", "_p5": "555", "_p6": "1987-03", "_p7": "1987-03", "_p8": "198703", "_p9": "198703", "_p10": "1987-03", "_p11": "1987-03"}},
		{name: "a branch renders as a key set on Spanner, never inline", filter: `*:search:"ann"`, dialect: Spanner, branches: true, wantErr: "renders as a key set on Spanner, never inline"},
		{name: "a denied field drops out of the box silently", filter: `*:search:"ann"`, dialect: Spanner, grants: map[string]searchGrant{"Email": {dropped: true}, "Title": {dropped: true}}, wantSQL: "(SEARCH_SUBSTRING(`Name_Search`, @_p1, relative_search_type => 'value_prefix') OR SEARCH_SUBSTRING(`Name_Search`, @_p2, relative_search_type => 'value_suffix'))", wantParams: map[string]any{"_p1": "ann", "_p2": "ann"}},
		{name: "a conditional field's match is conjoined with its condition", filter: `*:search:"ann"`, dialect: Spanner, grants: map[string]searchGrant{"Name": {condition: "`Trusted` = TRUE"}, "Email": {dropped: true}, "Title": {dropped: true}}, wantSQL: "(`Trusted` = TRUE AND (SEARCH_SUBSTRING(`Name_Search`, @_p1, relative_search_type => 'value_prefix') OR SEARCH_SUBSTRING(`Name_Search`, @_p2, relative_search_type => 'value_suffix')))", wantParams: map[string]any{"_p1": "ann", "_p2": "ann"}},
		{name: "an explicit match on a denied field renders false", filter: "email:contains:ann", dialect: Spanner, grants: map[string]searchGrant{"Email": {dropped: true}}, wantSQL: "FALSE", wantParams: map[string]any{}},
		{name: "an explicit match on a conditional field carries the condition", filter: "name:contains:ann", dialect: Spanner, grants: map[string]searchGrant{"Name": {condition: "`Trusted` = TRUE"}}, wantSQL: "(`Trusted` = TRUE AND SEARCH_SUBSTRING(`Name_Search`, @_p1))", wantParams: map[string]any{"_p1": "ann"}},
		{name: "every field denied leaves the box false", filter: `*:search:"ann"`, dialect: Spanner, grants: map[string]searchGrant{"Name": {dropped: true}, "Email": {dropped: true}, "Title": {dropped: true}}, wantSQL: "FALSE", wantParams: map[string]any{}},
		{name: "postgres startswith", filter: "name:startswith:An_n", dialect: PostgreSQL, wantSQL: `"Name" ILIKE @_p1`, wantParams: map[string]any{"_p1": `An\_n%`}},
		{name: "postgres endswith", filter: "name:endswith:50%", dialect: PostgreSQL, wantSQL: `"Name" ILIKE @_p1`, wantParams: map[string]any{"_p1": `%50\%`}},
		{name: "postgres digits", filter: "phone:contains:555-12", dialect: PostgreSQL, wantSQL: `regexp_replace("Phone", '\D', '', 'g') ILIKE @_p1`, wantParams: map[string]any{"_p1": "%55512%"}},
		{name: "postgres prefix fact", filter: "code:startswith:AbC", dialect: PostgreSQL, wantSQL: `"Code_Lower" LIKE @_p1`, wantParams: map[string]any{"_p1": "abc%"}},
		{name: "postgres box with a date and a branch as a correlated EXISTS", filter: `*:search:"1987"`, dialect: PostgreSQL, branches: true, wantSQL: `(("Name" ILIKE @_p1 OR "Name" ILIKE @_p2) OR regexp_replace("Phone", '\D', '', 'g') ILIKE @_p3 OR to_char("BornOn", 'YYYYMMDD') ILIKE @_p4 OR "Email" ILIKE @_p5 OR "Title" ILIKE @_p6 OR EXISTS (SELECT 1 FROM "Contacts" WHERE "Contacts"."RowId" = "Id" AND "DisplayName" ILIKE @_p7))`, wantParams: map[string]any{"_p1": "1987%", "_p2": "%1987", "_p3": "%1987%", "_p4": "%1987%", "_p5": "%1987%", "_p6": "%1987%", "_p7": "%1987%"}},
		{name: "postgres branch under a denied child field drops out", filter: `*:search:"ann"`, dialect: PostgreSQL, branches: true, branchDrop: true, wantSQL: `(("Name" ILIKE @_p1 OR "Name" ILIKE @_p2) OR "Email" ILIKE @_p3 OR "Title" ILIKE @_p4)`, wantParams: map[string]any{"_p1": "ann%", "_p2": "%ann", "_p3": "%ann%", "_p4": "%ann%"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			target := SpannerDBType
			if tt.dialect == PostgreSQL {
				target = PostgresDBType
			}
			ast, err := parseSearchFilter(t, tt.filter, target, tt.branches)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			gen := newSQLGenerator(tt.dialect)
			branchGrants := map[string]searchGrant{}
			if tt.branchDrop {
				branchGrants[searchBranch.Name] = searchGrant{dropped: true}
			}
			gen.setSearchGrants("", tt.grants, branchGrants)
			sql, params, err := gen.GenerateSQL(ast)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("GenerateSQL() error = %v, want %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("GenerateSQL() error = %v", err)
			}
			if sql != tt.wantSQL {
				t.Errorf("GenerateSQL() = %q, want %q", sql, tt.wantSQL)
			}
			got := map[string]any{}
			for _, p := range params {
				got[p.Name] = p.Value
			}
			if diff := cmp.Diff(tt.wantParams, got); diff != "" {
				t.Errorf("params mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestRenderSearchKeySet(t *testing.T) {
	t.Parallel()

	boxOf := func(t *testing.T, filter string) *searchCondition {
		t.Helper()

		ast, err := parseSearchFilter(t, filter, SpannerDBType, true)
		if err != nil {
			t.Fatalf("Parse() error = %v", err)
		}
		box := findSearchBox(ast)
		if box == nil {
			t.Fatal("no box in the filter")
		}

		return box.Condition.search
	}

	t.Run("one key set per term, one branch per index and per child, joined back on the key", func(t *testing.T) {
		t.Parallel()

		gen := newSQLGenerator(Spanner)
		gen.setSearchGrants("Rows", map[string]searchGrant{"Email": {dropped: true}}, nil)
		set, err := gen.renderSearchKeySet(boxOf(t, `*:search:"ann 555"`), "`Rows`", "Rows", "`Rows`.`TenantId` = @domain")
		if err != nil {
			t.Fatalf("renderSearchKeySet() error = %v", err)
		}
		if set.empty {
			t.Fatal("empty = true, want a key set")
		}
		want := "(SELECT `Rows`.`Id` AS zzSearchKey FROM `Rows` WHERE `Rows`.`TenantId` = @domain AND (SEARCH_SUBSTRING(`Name_Search`, @_p1, relative_search_type => 'value_prefix') OR SEARCH_SUBSTRING(`Name_Search`, @_p2, relative_search_type => 'value_suffix'))\n" +
			" UNION DISTINCT\n" +
			" SELECT `Rows`.`Id` AS zzSearchKey FROM `Rows` WHERE `Rows`.`TenantId` = @domain AND SEARCH_SUBSTRING(`Title_Search`, @_p3)\n" +
			" UNION DISTINCT\n" +
			" SELECT `Contacts`.`RowId` AS zzSearchKey FROM `Contacts` WHERE SEARCH_SUBSTRING(`DisplayName_Search`, @_p4)) AS zzSearchKey1\n" +
			"JOIN (SELECT `Rows`.`Id` AS zzSearchKey FROM `Rows` WHERE `Rows`.`TenantId` = @domain AND ((SEARCH_SUBSTRING(`Name_Search`, @_p5, relative_search_type => 'value_prefix') OR SEARCH_SUBSTRING(`Name_Search`, @_p6, relative_search_type => 'value_suffix')) OR SEARCH_SUBSTRING(`Phone_Search`, @_p7))\n" +
			" UNION DISTINCT\n" +
			" SELECT `Rows`.`Id` AS zzSearchKey FROM `Rows` WHERE `Rows`.`TenantId` = @domain AND SEARCH_SUBSTRING(`Title_Search`, @_p8)\n" +
			" UNION DISTINCT\n" +
			" SELECT `Contacts`.`RowId` AS zzSearchKey FROM `Contacts` WHERE SEARCH_SUBSTRING(`DisplayName_Search`, @_p9)) AS zzSearchKey2 ON zzSearchKey2.zzSearchKey = zzSearchKey1.zzSearchKey\n" +
			"JOIN@{JOIN_METHOD=APPLY_JOIN} `Rows` ON `Rows`.`Id` = zzSearchKey1.zzSearchKey"
		if set.from != want {
			t.Errorf("from =\n%s\nwant\n%s", set.from, want)
		}
		if len(set.params) != 9 || set.params[6].Value != "555" {
			t.Errorf("params = %+v, want nine, the seventh the digits 555", set.params)
		}
	})

	t.Run("a tenant-scoped child carries the tenancy and its grant's condition", func(t *testing.T) {
		t.Parallel()

		scoped := searchBranch
		scoped.TenantColumn = "TenantId"
		parser, err := NewFilterParser(NewFilterLexer(`*:search:"ann"`), searchTestFields())
		if err != nil {
			t.Fatalf("NewFilterParser() error = %v", err)
		}
		ast, err := parser.WithSearch([]SearchBranch{scoped}, searchKeys).Parse(SpannerDBType)
		if err != nil {
			t.Fatalf("Parse() error = %v", err)
		}
		gen := newSQLGenerator(Spanner)
		gen.setSearchGrants("Rows", map[string]searchGrant{"Name": {dropped: true}, "Email": {dropped: true}, "Title": {dropped: true}}, map[string]searchGrant{scoped.Name: {condition: "`Contacts`.`Active` = TRUE"}})
		set, err := gen.renderSearchKeySet(findSearchBox(ast).Condition.search, "`Rows`", "Rows", "")
		if err != nil {
			t.Fatalf("renderSearchKeySet() error = %v", err)
		}
		want := "(SELECT `Contacts`.`RowId` AS zzSearchKey FROM `Contacts` WHERE (`Contacts`.`TenantId` = @domain) AND `Contacts`.`Active` = TRUE AND SEARCH_SUBSTRING(`DisplayName_Search`, @_p1)) AS zzSearchKey1\n" +
			"JOIN@{JOIN_METHOD=APPLY_JOIN} `Rows` ON `Rows`.`Id` = zzSearchKey1.zzSearchKey"
		if set.from != want {
			t.Errorf("from =\n%s\nwant\n%s", set.from, want)
		}
	})

	t.Run("a term no branch can search empties the set", func(t *testing.T) {
		t.Parallel()

		gen := newSQLGenerator(Spanner)
		gen.setSearchGrants("Rows", map[string]searchGrant{"Name": {dropped: true}, "Email": {dropped: true}, "Title": {dropped: true}}, map[string]searchGrant{searchBranch.Name: {dropped: true}})
		set, err := gen.renderSearchKeySet(boxOf(t, `*:search:"ann 555"`), "`Rows`", "Rows", "")
		if err != nil {
			t.Fatalf("renderSearchKeySet() error = %v", err)
		}
		if !set.empty {
			t.Errorf("empty = false, want true: %s", set.from)
		}
	})

	t.Run("a resource with no single-column key is refused", func(t *testing.T) {
		t.Parallel()

		parser, err := NewFilterParser(NewFilterLexer(`*:search:"ann"`), searchTestFields())
		if err != nil {
			t.Fatalf("NewFilterParser() error = %v", err)
		}
		ast, err := parser.WithSearch([]SearchBranch{searchBranch}, nil).Parse(SpannerDBType)
		if err != nil {
			t.Fatalf("Parse() error = %v", err)
		}
		_, err = newSQLGenerator(Spanner).renderSearchKeySet(findSearchBox(ast).Condition.search, "`Rows`", "Rows", "")
		if err == nil || !strings.Contains(err.Error(), "single-column key") {
			t.Errorf("renderSearchKeySet() error = %v, want the key refusal", err)
		}
	})
}

func TestSplitSearchConjunct(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		filter       string
		wantOK       bool
		wantResidual string // the residual rendered on Spanner, "" for none
		wantKeySet   bool
	}{
		{name: "the box alone", filter: `*:search:"ann"`, wantOK: true, wantKeySet: true},
		{name: "the box beside a condition", filter: `plain:eq:x,*:search:"ann"`, wantOK: true, wantResidual: "`Plain` = @_p1", wantKeySet: true},
		{name: "the box first", filter: `*:search:"ann",plain:eq:x,name:eq:y`, wantOK: true, wantResidual: "`Plain` = @_p1 AND `Name` = @_p2", wantKeySet: true},
		{name: "the box inside a group joined by AND", filter: `(*:search:"ann",plain:eq:x),name:eq:y`, wantOK: true, wantResidual: "(`Plain` = @_p1) AND `Name` = @_p2", wantKeySet: true},
		{name: "the box under an OR cannot be lifted", filter: `plain:eq:x|*:search:"ann"`, wantOK: false, wantKeySet: true},
		{name: "no box", filter: `plain:eq:x`, wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ast, err := parseSearchFilter(t, tt.filter, SpannerDBType, true)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if box := findSearchBox(ast); (box != nil) != tt.wantKeySet {
				t.Fatalf("findSearchBox() = %v, want a box %v", box, tt.wantKeySet)
			} else if box != nil && !box.Condition.search.needsKeySet() {
				t.Error("needsKeySet() = false, want true with a branch declared")
			}
			if got := hasSearchPredicate(ast); got != tt.wantKeySet {
				t.Errorf("hasSearchPredicate() = %v, want %v", got, tt.wantKeySet)
			}
			box, residual, ok := splitSearchConjunct(ast)
			if ok != tt.wantOK {
				t.Fatalf("splitSearchConjunct() ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if box == nil || box.Condition.search == nil {
				t.Fatal("splitSearchConjunct() returned no box")
			}
			if (residual == nil) != (tt.wantResidual == "") {
				t.Fatalf("residual = %v, want %q", residual, tt.wantResidual)
			}
			if residual == nil {
				return
			}
			sql, _, err := newSQLGenerator(Spanner).GenerateSQL(residual)
			if err != nil {
				t.Fatalf("GenerateSQL(residual) error = %v", err)
			}
			if sql != tt.wantResidual {
				t.Errorf("residual = %q, want %q", sql, tt.wantResidual)
			}
		})
	}
}

func TestNeedsKeySet(t *testing.T) {
	t.Parallel()

	oneIndex := &searchCondition{fields: []searchField{{spec: searchNameSpec}, {spec: searchEmailSpec}}}
	if oneIndex.needsKeySet() {
		t.Error("fields of one index need no key set")
	}
	twoIndexes := &searchCondition{fields: []searchField{{spec: searchNameSpec}, {spec: searchTitleSpec}}}
	if !twoIndexes.needsKeySet() {
		t.Error("fields of two indexes need a key set")
	}
	if groups := groupByIndex(twoIndexes.fields); len(groups) != 2 || groups[0][0].spec != searchNameSpec {
		t.Errorf("groupByIndex() = %v, want two groups in first-appearance order", groups)
	}
	branched := &searchCondition{fields: []searchField{{spec: searchNameSpec}}, branches: []SearchBranch{searchBranch}}
	if !branched.needsKeySet() {
		t.Error("a branch needs a key set")
	}
}

// searchRow and searchRequest are the decoder fixture: every token shape, a PII field
// in the box, the prefix fact, and a branch.
type searchRow struct {
	ID     ccc.UUID   `spanner:"Id"`
	Name   string     `spanner:"Name"`
	Phone  string     `spanner:"Phone"`
	BornOn civil.Date `spanner:"BornOn"`
	Email  string     `spanner:"Email"`
	Code   string     `spanner:"Code"`
	Plain  string     `spanner:"Plain"`
}

func (searchRow) Resource() accesstypes.Resource { return "searchRows" }

func (searchRow) DefaultConfig() Config { return Config{} }

func (searchRow) SearchBranches() []SearchBranch { return []SearchBranch{searchBranch} }

type searchRequest struct {
	ID     ccc.UUID   `json:"id"     index:"true"                                                      perm:"-"`
	Name   string     `json:"name"   index:"true"                                                      search:"token=Name_Search;index=RowsSearch;role=text;anchors=prefix,suffix;contains"`
	Phone  string     `json:"phone"  search:"token=Phone_Search;index=RowsSearch;role=digits;contains"`
	BornOn civil.Date `json:"bornOn" search:"token=BornOn_Search;index=RowsSearch;role=date;contains"`
	Email  string     `json:"email"  pii:"true"                                                        search:"token=Email_Search;index=RowsSearch;role=text;contains"`
	Code   string     `json:"code"   search:"lower=Code_Lower"`
	Plain  string     `json:"plain"`
}

type mistypedSearchRequest struct {
	ID    ccc.UUID `json:"id"    index:"true"                                                    perm:"-"`
	Phone int64    `json:"phone" search:"token=Phone_Search;index=RowsSearch;role=text;contains"`
}

type mistypedSearchRow struct {
	ID    ccc.UUID `spanner:"Id"`
	Phone int64    `spanner:"Phone"`
}

func (mistypedSearchRow) Resource() accesstypes.Resource { return "mistypedSearchRows" }

func (mistypedSearchRow) DefaultConfig() Config { return Config{} }

type staleSearchRequest struct {
	ID   ccc.UUID `json:"id"   index:"true"                          perm:"-"`
	Name string   `json:"name" search:"token=Name_Search;role=sound"`
}

func TestQueryDecoder_Search(t *testing.T) {
	t.Parallel()

	newDecoder := func(t *testing.T) *QueryDecoder[searchRow, searchRequest] {
		t.Helper()

		resSet, err := NewSet[searchRow, searchRequest](accesstypes.List)
		if err != nil {
			t.Fatalf("NewSet() error = %v", err)
		}
		decoder, err := NewQueryDecoder[searchRow, searchRequest](resSet)
		if err != nil {
			t.Fatalf("NewQueryDecoder() error = %v", err)
		}
		decoder.WithPaging(Paging{Order: []SortField{{Field: "Name", Direction: SortAscending}}})

		return decoder
	}

	t.Run("the decoder reads the search tags into the box, the PII flag and the key", func(t *testing.T) {
		t.Parallel()

		decoder := newDecoder(t)
		if diff := cmp.Diff([]accesstypes.Field{"Name", "Phone", "BornOn", "Email"}, decoder.boxFields); diff != "" {
			t.Errorf("boxFields mismatch (-want +got):\n%s", diff)
		}
		if !decoder.boxPII {
			t.Error("boxPII = false, want true: Email is PII and joins the box")
		}
		if diff := cmp.Diff(map[DBType]string{SpannerDBType: "Id", goFieldNames: "ID"}, decoder.searchKeys); diff != "" {
			t.Errorf("searchKeys mismatch (-want +got):\n%s", diff)
		}
		fields := decoder.filterParserFields
		if fields["code"].Search == nil || fields["code"].Search.Lower != "Code_Lower" {
			t.Errorf("code's search = %+v, want the prefix fact", fields["code"].Search)
		}
		if fields["bornOn"].Search == nil || fields["bornOn"].Search.Role != SearchRoleDate {
			t.Errorf("bornOn's search = %+v, want the date role", fields["bornOn"].Search)
		}
		if fields["plain"].Search != nil {
			t.Errorf("plain's search = %+v, want none", fields["plain"].Search)
		}
	})

	t.Run("a box over a PII field posts: the URL form is refused", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/rows?"+url.Values{"filter": {`*:search:"ann"`}}.Encode(), http.NoBody)
		_, err := newDecoder(t).DecodeWithoutPermissions(req)
		if err == nil || !strings.Contains(err.Error(), "send the filter in the request body") {
			t.Errorf("DecodeWithoutPermissions() error = %v, want the body placement refusal", err)
		}
	})

	t.Run("a match that names no PII field travels in the URL", func(t *testing.T) {
		t.Parallel()

		parsed, err := newDecoder(t).parseQuery(url.Values{"filter": {`name:startswith:an`}}, nil)
		if err != nil {
			t.Fatalf("parseQuery() error = %v", err)
		}
		if parsed.SearchBox || parsed.SearchFields != nil {
			t.Errorf("SearchBox = %v, SearchFields = %v, want none for a match", parsed.SearchBox, parsed.SearchFields)
		}
	})

	t.Run("a posted box stamps the query set with the fields it searches", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/rows", strings.NewReader(`{"filter":"*:search:\"ann 555\""}`))
		req.Header.Set("Content-Type", "application/json")
		qSet, err := newDecoder(t).DecodeWithoutPermissions(req)
		if err != nil {
			t.Fatalf("DecodeWithoutPermissions() error = %v", err)
		}
		if !qSet.searchBox {
			t.Error("searchBox = false, want true")
		}
		if diff := cmp.Diff([]accesstypes.Field{"Name", "Phone", "BornOn", "Email"}, qSet.searchFields); diff != "" {
			t.Errorf("searchFields mismatch (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff([]accesstypes.Field{"Name", "Phone", "BornOn", "Email"}, qSet.activeSearchFields()); diff != "" {
			t.Errorf("activeSearchFields() mismatch (-want +got):\n%s", diff)
		}
		if got := qSet.optimizerVersion(SpannerDBType, mustFilterAst(t, qSet, SpannerDBType)); got != searchOptimizerVersion {
			t.Errorf("optimizerVersion() = %q, want %q", got, searchOptimizerVersion)
		}
	})

	t.Run("a text search tag on a non-text field is the stale-struct guard", func(t *testing.T) {
		t.Parallel()

		resSet, err := NewSet[mistypedSearchRow, mistypedSearchRequest](accesstypes.List)
		if err != nil {
			t.Fatalf("NewSet() error = %v", err)
		}
		if _, err := NewQueryDecoder[mistypedSearchRow, mistypedSearchRequest](resSet); err == nil || !strings.Contains(err.Error(), "search") {
			t.Errorf("NewQueryDecoder() error = %v, want the search tag refusal", err)
		}
	})

	t.Run("a search tag the runtime cannot read is refused", func(t *testing.T) {
		t.Parallel()

		resSet, err := NewSet[searchRow, staleSearchRequest](accesstypes.List)
		if err != nil {
			t.Fatalf("NewSet() error = %v", err)
		}
		if _, err := NewQueryDecoder[searchRow, staleSearchRequest](resSet); err == nil || !strings.Contains(err.Error(), "not text, digits or date") {
			t.Errorf("NewQueryDecoder() error = %v, want the grammar refusal", err)
		}
	})
}

func mustFilterAst[Resource Resourcer](t *testing.T, qSet *QuerySet[Resource], dbType DBType) ExpressionNode {
	t.Helper()

	ast, err := qSet.FilterAst(dbType)
	if err != nil {
		t.Fatalf("FilterAst() error = %v", err)
	}

	return ast
}

func TestSearch_InMemory(t *testing.T) {
	t.Parallel()

	type row struct {
		Name     string
		Phone    string
		BornOn   civil.Date
		OpenedAt time.Time
		Email    *string
		Note     spanner.NullString
	}
	email := "ana@example.com"
	r := row{
		Name:     "Ánn O'Neil-Smith",
		Phone:    "+1 (555) 123-4567",
		BornOn:   civil.Date{Year: 1987, Month: 3, Day: 14},
		OpenedAt: time.Date(2024, 5, 1, 3, 30, 0, 0, time.UTC),
		Email:    &email,
	}
	value := reflect.ValueOf(r)
	field := value.FieldByName
	zoned := &SearchSpec{Token: "T", Index: "I", Role: SearchRoleDate, Contains: true, Zone: "America/New_York"}

	tests := []struct {
		name     string
		field    string
		spec     *SearchSpec
		term     string
		operator string
		want     bool
	}{
		{name: "contains folds case and punctuation", field: "Name", spec: searchEmailSpec, term: "oneil", operator: containsStr, want: true},
		{name: "diacritics fold under the flag", field: "Name", spec: searchEmailSpec, term: "ann", operator: startswithStr, want: true},
		{name: "diacritics stay without the flag", field: "Name", spec: searchNameSpec, term: "ann", operator: startswithStr, want: false},
		{name: "endswith", field: "Name", spec: searchNameSpec, term: "smith", operator: endswithStr, want: true},
		{name: "digits", field: "Phone", spec: searchPhoneSpec, term: "555-123", operator: containsStr, want: true},
		{name: "a term with no digits never meets a digits field", field: "Phone", spec: searchPhoneSpec, term: "ext", operator: containsStr, want: false},
		{name: "a date as its prefix", field: "BornOn", spec: searchBornOnSpec, term: "1987-03", operator: containsStr, want: true},
		{name: "a timestamp's day in the named zone: the first of May in UTC is April 30 in New York", field: "OpenedAt", spec: zoned, term: "2024-04-30", operator: containsStr, want: true},
		{name: "and not May", field: "OpenedAt", spec: zoned, term: "2024-05-01", operator: containsStr, want: false},
		{name: "a pointer cell", field: "Email", spec: searchEmailSpec, term: "example", operator: containsStr, want: true},
		{name: "a NULL cell matches nothing", field: "Note", spec: searchNameSpec, term: "", operator: containsStr, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := matchFieldText(field(tt.field), tt.spec, tt.term, tt.operator); got != tt.want {
				t.Errorf("matchFieldText(%s, %q, %s) = %v, want %v", tt.field, tt.term, tt.operator, got, tt.want)
			}
		})
	}

	t.Run("the box over a row: every term in some field, each anchor honored", func(t *testing.T) {
		t.Parallel()

		box := &searchCondition{fields: []searchField{
			{goField: "Name", spec: searchNameSpec},
			{goField: "Phone", spec: searchPhoneSpec},
			{goField: "BornOn", spec: searchBornOnSpec},
		}}
		for _, tt := range []struct {
			terms []string
			want  bool
		}{
			{terms: []string{"ánn", "4567", "1987"}, want: true},
			{terms: []string{"neil"}, want: false}, // Name is anchored: a middle term meets no anchor
			{terms: []string{"smith", "3/14/1987"}, want: true},
			{terms: []string{"smith", "1999"}, want: false},
		} {
			box.terms = tt.terms
			got, err := matchSearchBox(box, value)
			if err != nil {
				t.Fatalf("matchSearchBox(%v) error = %v", tt.terms, err)
			}
			if got != tt.want {
				t.Errorf("matchSearchBox(%v) = %v, want %v", tt.terms, got, tt.want)
			}
		}
	})

	t.Run("a field the row lacks is an error, not a miss", func(t *testing.T) {
		t.Parallel()

		box := &searchCondition{terms: []string{"x"}, fields: []searchField{{goField: "Missing", spec: searchNameSpec}}}
		if _, err := matchSearchBox(box, value); err == nil {
			t.Error("matchSearchBox() error = nil, want the missing field named")
		}
	})
}

func TestCondition_MatchesText(t *testing.T) {
	t.Parallel()

	ast, err := parseSearchFilter(t, `name:startswith:Án,plain:eq:x`, goFieldNames, false)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	conditions := leaves(ast)
	match, plain := conditions[0].Condition, conditions[1].Condition
	if !match.IsTextMatch() || plain.IsTextMatch() {
		t.Fatalf("IsTextMatch() = %v, %v, want true, false", match.IsTextMatch(), plain.IsTextMatch())
	}
	if !match.MatchesText("Ánna") || match.MatchesText("Anna") || match.MatchesText("Bánna") {
		t.Error("MatchesText() reads the term folded as the cell, diacritics kept without the flag")
	}
	if plain.MatchesText("x") {
		t.Error("MatchesText() = true on an eq, want false")
	}
}

func TestQueryBuilder_Search(t *testing.T) {
	t.Parallel()

	px := PartialQueryClause{}
	nameTag := searchNameSpec.Tag()
	t.Run("a text identifier renders the matches its tag answers", func(t *testing.T) {
		t.Parallel()

		clause := NewTextIdent("Name", px, true, nameTag).EndsWith("son")
		sql, params, err := newSQLGenerator(Spanner).GenerateSQL(clause.tree)
		if err != nil {
			t.Fatalf("GenerateSQL() error = %v", err)
		}
		if sql != "SEARCH_SUBSTRING(`Name_Search`, @_p1, relative_search_type => 'value_suffix')" || params[0].Value != "son" {
			t.Errorf("EndsWith() = %q %v", sql, params)
		}
		if !clause.hasIndexedField {
			t.Error("a match is the indexed touch")
		}
	})

	t.Run("a match the tag does not answer panics: hand-written code against the schema", func(t *testing.T) {
		t.Parallel()

		defer func() {
			message, _ := recover().(string)
			if !strings.Contains(message, "does not answer contains") {
				t.Errorf("recover() = %q, want the refusal", message)
			}
		}()
		NewTextIdent("Code", px, false, searchCodeSpec.Tag()).Contains("x")
	})

	t.Run("the box over the declared columns and branches", func(t *testing.T) {
		t.Parallel()

		clause, err := px.Search("ann 555", "Id", []SearchColumn{{Column: "Name", Tag: nameTag}, {Column: "Code", Tag: searchCodeSpec.Tag()}, {Column: "Phone", Tag: searchPhoneSpec.Tag()}}, []SearchBranch{searchBranch})
		if err != nil {
			t.Fatalf("Search() error = %v", err)
		}
		box := findSearchBox(clause.tree)
		if box == nil {
			t.Fatal("Search() built no box")
		}
		if diff := cmp.Diff([]string{"Name", "Phone"}, box.Condition.SearchFields()); diff != "" {
			t.Errorf("the prefix fact never joins the box (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff([]string{"ann", "555"}, box.Condition.SearchTerms()); diff != "" {
			t.Errorf("SearchTerms() mismatch (-want +got):\n%s", diff)
		}
		if box.Condition.search.key != "Id" || len(box.Condition.search.branches) != 1 {
			t.Errorf("key = %q, branches = %d, want Id and one", box.Condition.search.key, len(box.Condition.search.branches))
		}
	})

	t.Run("a box with nothing to search is refused", func(t *testing.T) {
		t.Parallel()

		if _, err := px.Search("ann", "Id", []SearchColumn{{Column: "Code", Tag: searchCodeSpec.Tag()}}, nil); err == nil || !strings.Contains(err.Error(), "no column joins the search box") {
			t.Errorf("Search() error = %v, want the refusal", err)
		}
		if _, err := px.Search("   ", "Id", []SearchColumn{{Column: "Name", Tag: nameTag}}, nil); err == nil || !strings.Contains(err.Error(), "empty") {
			t.Errorf("Search() error = %v, want the empty text refusal", err)
		}
	})
}

func TestFoldSearchText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in         string
		diacritics bool
		want       string
	}{
		{in: "Ánn O'Neil-Smith", want: "ánn oneil smith"},
		{in: "Ánn O'Neil-Smith", diacritics: true, want: "ann oneil smith"},
		{in: "  Two   words , here. ", want: "two words here"},
		{in: "crème brûlée", diacritics: true, want: "creme brulee"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()

			if got := foldSearchText(tt.in, tt.diacritics); got != tt.want {
				t.Errorf("foldSearchText(%q, %v) = %q, want %q", tt.in, tt.diacritics, got, tt.want)
			}
		})
	}
}

// The cmp options the condition comparisons share.
var _ = cmpopts.IgnoreUnexported
