package generation

import (
	"strings"
	"testing"

	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/google/go-cmp/cmp"
)

// searchFixtureTables is the synthetic schema behind the searchfixture structs: the
// token columns as INFORMATION_SCHEMA reports them (a TOKENLIST column with its
// generation expression), each table's search index with its token columns then its
// partition column, the stored lowercased column and the index it sits second in, and
// one table per refusal. The per-column index flags derive from the index list the way
// the schema read derives them.
func searchFixtureTables() map[string]*tableMetadata {
	pk := columnMeta{IsPrimaryKey: true, SpannerType: "STRING(36)"}
	tenant := columnMeta{IsForeignKey: true, ReferencedTable: "Tenants", ReferencedColumn: "Id", SpannerType: "STRING(MAX)"}
	text := columnMeta{SpannerType: "STRING(MAX)"}
	token := func(expression string) columnMeta {
		return columnMeta{SpannerType: "TOKENLIST", GenerationExpression: expression}
	}
	foreign := func(table string) columnMeta {
		return columnMeta{IsForeignKey: true, ReferencedTable: table, ReferencedColumn: "Id", SpannerType: "STRING(36)"}
	}
	primaryKey := indexMeta{Name: "PRIMARY_KEY", PrimaryKey: true, Unique: true, Key: []indexColumn{{Column: "Id"}}}
	backing := func(table, column string) indexMeta {
		return indexMeta{Name: "IDX_" + table + "_" + column, Managed: true, Key: []indexColumn{{Column: column}}}
	}
	search := func(name string, storing []string, columns ...string) indexMeta {
		key := make([]indexColumn, 0, len(columns))
		for _, column := range columns {
			key = append(key, indexColumn{Column: column})
		}

		return indexMeta{Name: name, Search: true, Key: key, Storing: storing}
	}
	simple := func(columns map[string]columnMeta, indexes ...indexMeta) *tableMetadata {
		columns["Id"] = pk

		return &tableMetadata{PkCount: 1, Columns: columns, Indexes: append([]indexMeta{primaryKey}, indexes...)}
	}

	tables := map[string]*tableMetadata{
		"Tenants": simple(map[string]columnMeta{}),
		"Accounts": simple(map[string]columnMeta{
			"TenantId": tenant, "Name": text, "Phone": text, "BornOn": {SpannerType: "DATE"}, "OpenedAt": {SpannerType: "TIMESTAMP"},
			"Email": text, "Notes": text, "Code": text,
			"Initials":        {SpannerType: "STRING(MAX)", GenerationExpression: "SUBSTR(Name, 1, 1)"},
			"Name_Search":     token("TOKENIZE_SUBSTRING(Name, relative_search_types=>['value_prefix', 'value_suffix'])"),
			"Phone_Search":    token(`TOKENIZE_SUBSTRING(REGEXP_REPLACE(Phone, r'\D', ''))`),
			"BornOn_Search":   token("TOKENIZE_SUBSTRING(FORMAT_DATE('%Y%m%d', BornOn))"),
			"OpenedAt_Search": token("TOKENIZE_SUBSTRING(FORMAT_TIMESTAMP('%Y%m%d', OpenedAt, 'America/New_York'))"),
			"Email_Search":    token("TOKENIZE_SUBSTRING(Email, relative_search_types=>['value_prefix'], short_tokens_only_for_anchors=>true, remove_diacritics=>true, ngram_size_max=>6)"),
			"Notes_Search":    token("TOKENIZE_FULLTEXT(Notes)"),
			"Code_Lower":      {SpannerType: "STRING(MAX)", GenerationExpression: "LOWER(Code)"},
		}, backing("Accounts", "TenantId"),
			indexMeta{Name: "AccountsByTenantIdCodeLower", Key: []indexColumn{{Column: "TenantId"}, {Column: "Code_Lower"}}},
			search("AccountsSearch", nil, "Name_Search", "Phone_Search", "BornOn_Search", "OpenedAt_Search", "Email_Search", "Notes_Search", "TenantId")),
		"AccountContacts": simple(map[string]columnMeta{
			"TenantId": tenant, "AccountId": foreign("Accounts"), "DisplayName": text, "UserId": text,
			"DisplayName_Search": token("TOKENIZE_SUBSTRING(DisplayName)"),
		}, backing("AccountContacts", "TenantId"), backing("AccountContacts", "AccountId"),
			indexMeta{Name: "AccountContactsByTenantIdAccountId", Key: []indexColumn{{Column: "TenantId"}, {Column: "AccountId"}}},
			search("AccountContactsSearch", []string{"AccountId"}, "DisplayName_Search", "TenantId")),
		"AccountNotes": simple(map[string]columnMeta{
			"TenantId": tenant, "AccountId": foreign("Accounts"), "Body": text,
			"Body_Search": token("TOKENIZE_SUBSTRING(Body)"),
		}, backing("AccountNotes", "TenantId"), backing("AccountNotes", "AccountId"),
			search("AccountNotesSearch", nil, "Body_Search", "TenantId")),
		"Pairings": simple(map[string]columnMeta{
			"TenantId": tenant, "AccountId": foreign("Accounts"), "OtherAccountId": foreign("Accounts"), "Label": text,
			"Label_Search": token("TOKENIZE_SUBSTRING(Label)"),
		}, backing("Pairings", "TenantId"), backing("Pairings", "AccountId"), backing("Pairings", "OtherAccountId"),
			search("PairingsSearch", []string{"AccountId", "OtherAccountId"}, "Label_Search", "TenantId")),
		"Unpartitioneds": simple(map[string]columnMeta{
			"TenantId": tenant, "Name": text, "Name_Search": token("TOKENIZE_SUBSTRING(Name)"),
		}, backing("Unpartitioneds", "TenantId"), search("UnpartitionedsSearch", nil, "Name_Search")),
		"Globals": simple(map[string]columnMeta{
			"Name": text, "Name_Search": token("TOKENIZE_SUBSTRING(Name)"),
		}, search("GlobalsSearch", nil, "Name_Search")),
		"GlobalContacts": simple(map[string]columnMeta{
			"TenantId": tenant, "GlobalId": foreign("Globals"), "DisplayName": text,
			"DisplayName_Search": token("TOKENIZE_SUBSTRING(DisplayName)"),
		}, backing("GlobalContacts", "TenantId"), backing("GlobalContacts", "GlobalId"),
			search("GlobalContactsSearch", []string{"GlobalId"}, "DisplayName_Search", "TenantId")),
		"Twins":   simple(map[string]columnMeta{"TenantId": tenant}, backing("Twins", "TenantId")),
		"Misvias": simple(map[string]columnMeta{"TenantId": tenant}, backing("Misvias", "TenantId")),
		"TwinLinks": simple(map[string]columnMeta{
			"TenantId": tenant, "TwinId": foreign("Twins"), "OtherTwinId": foreign("Twins"), "Label": text,
			"Label_Search": token("TOKENIZE_SUBSTRING(Label)"),
		}, backing("TwinLinks", "TenantId"), backing("TwinLinks", "TwinId"), backing("TwinLinks", "OtherTwinId"),
			search("TwinLinksSearch", []string{"TwinId", "OtherTwinId"}, "Label_Search", "TenantId")),
		"Unfacteds":  simple(map[string]columnMeta{"TenantId": tenant}, backing("Unfacteds", "TenantId")),
		"Malformeds": simple(map[string]columnMeta{}),
		"Keyeds": {PkCount: 2, Columns: map[string]columnMeta{
			"TenantId": {IsPrimaryKey: true, IsForeignKey: true, ReferencedTable: "Tenants", ReferencedColumn: "Id", SpannerType: "STRING(MAX)"},
			"Sequence": {IsPrimaryKey: true, KeyOrdinalPosition: 1, SpannerType: "INT64"},
		}, Indexes: []indexMeta{
			{Name: "PRIMARY_KEY", PrimaryKey: true, Unique: true, Key: []indexColumn{{Column: "TenantId"}, {Column: "Sequence"}}},
			backing("Keyeds", "TenantId"),
		}},
		"Doubleds": simple(map[string]columnMeta{
			"Name": text, "Name_Search": token("TOKENIZE_SUBSTRING(Name)"),
		}, search("DoubledsSearch", nil, "Name_Search"), search("DoubledsOtherSearch", nil, "Name_Search")),
		"Orphaneds": simple(map[string]columnMeta{
			"Name": text, "Name_Search": token("TOKENIZE_SUBSTRING(Name)"),
		}),
		"Twinneds": simple(map[string]columnMeta{
			"Name": text, "Name_Search": token("TOKENIZE_SUBSTRING(Name)"), "Name_Again": token("TOKENIZE_SUBSTRING(Name)"),
		}, search("TwinnedsSearch", nil, "Name_Again", "Name_Search")),
		"Misparteds": simple(map[string]columnMeta{
			"TenantId": tenant, "Region": text, "Name": text, "Name_Search": token("TOKENIZE_SUBSTRING(Name)"),
		}, backing("Misparteds", "TenantId"), search("MispartedsSearch", nil, "Name_Search", "Region")),
		"Mistypeds": simple(map[string]columnMeta{
			"BornOn": text, "BornOn_Search": token("TOKENIZE_SUBSTRING(FORMAT_DATE('%Y%m%d', BornOn))"),
		}, search("MistypedsSearch", nil, "BornOn_Search")),
		"Numbereds": simple(map[string]columnMeta{
			"Count": {SpannerType: "INT64"}, "Count_Search": token("TOKENIZE_SUBSTRING(CAST(Count AS STRING))"),
		}, search("NumberedsSearch", nil, "Count_Search")),
		"Unzoneds": simple(map[string]columnMeta{
			"OpenedAt": {SpannerType: "TIMESTAMP"}, "OpenedAt_Search": token("TOKENIZE_SUBSTRING(FORMAT_TIMESTAMP('%Y%m%d', OpenedAt))"),
		}, search("UnzonedsSearch", nil, "OpenedAt_Search")),
		"Tokeneds": simple(map[string]columnMeta{
			"Name": text, "Name_Search": token("TOKENIZE_SUBSTRING(Name)"),
		}, search("TokenedsSearch", nil, "Name_Search")),
		"Restateds": simple(map[string]columnMeta{
			"Name": text, "Initials": {SpannerType: "STRING(MAX)", GenerationExpression: "SUBSTR(Name, 1, 1)"},
		}),
		"Taggeds": simple(map[string]columnMeta{"Name": text}),
	}
	for _, table := range tables {
		table.deriveIndexFlags()
	}

	return tables
}

// searchFixture extracts the named table structs with the search schema behind them.
func searchFixture(t *testing.T, names ...string) (*client, []*resourceInfo, error) {
	t.Helper()

	c := &client{tableMap: searchFixtureTables()}
	structs := fixtureStructs(loadFixture(t, "searchfixture"))
	selected := make([]*parser.Struct, 0, len(names))
	for _, name := range names {
		s := structs[name]
		if s == nil {
			t.Fatalf("struct %q not found in the search fixture", name)
		}
		selected = append(selected, s)
	}
	resources, err := c.structsToResources(selected)

	return c, resources, err
}

func resourceNamed(t *testing.T, resources []*resourceInfo, name string) *resourceInfo {
	t.Helper()

	for _, res := range resources {
		if res.Name() == name {
			return res
		}
	}
	t.Fatalf("resource %q not extracted", name)

	return nil
}

func fieldNamed(t *testing.T, res *resourceInfo, name string) *resourceField {
	t.Helper()

	for _, f := range res.Fields {
		if f.Name() == name {
			return f
		}
	}
	t.Fatalf("%s has no field %q", res.Name(), name)

	return nil
}

// TestSearchFacts pins the vocabulary the generator derives from the fixture schema:
// each token shape's spec, the prefix fact, the generated column, the branches with
// their warnings, and what the templates read off them.
func TestSearchFacts(t *testing.T) {
	t.Parallel()

	c, resources, err := searchFixture(t, "Account", "AccountContact", "AccountNote", "Pairing", "Unpartitioned")
	if err != nil {
		t.Fatalf("structsToResources() error = %v", err)
	}
	account := resourceNamed(t, resources, "Account")

	t.Run("each field's spec follows its token column", func(t *testing.T) {
		t.Parallel()

		want := map[string]*resource.SearchSpec{
			"Name":     {Token: "Name_Search", Index: "AccountsSearch", Role: resource.SearchRoleText, Anchors: []resource.SearchAnchor{resource.SearchAnchorPrefix, resource.SearchAnchorSuffix}, Contains: true},
			"Phone":    {Token: "Phone_Search", Index: "AccountsSearch", Role: resource.SearchRoleDigits, Contains: true},
			"BornOn":   {Token: "BornOn_Search", Index: "AccountsSearch", Role: resource.SearchRoleDate, Contains: true},
			"OpenedAt": {Token: "OpenedAt_Search", Index: "AccountsSearch", Role: resource.SearchRoleDate, Contains: true, Zone: "America/New_York"},
			"Email":    {Token: "Email_Search", Index: "AccountsSearch", Role: resource.SearchRoleText, Anchors: []resource.SearchAnchor{resource.SearchAnchorPrefix}, Contains: false, Diacritics: true},
			"Code":     {Lower: "Code_Lower"},
			"Notes":    nil,
			"Initials": nil,
			"TenantID": nil,
		}
		for name, spec := range want {
			if diff := cmp.Diff(spec, fieldNamed(t, account, name).Search); diff != "" {
				t.Errorf("%s.Search mismatch (-want +got):\n%s", name, diff)
			}
		}
	})

	t.Run("a field over a generated column is output-only by derivation", func(t *testing.T) {
		t.Parallel()

		initials := fieldNamed(t, account, "Initials")
		if !initials.IsGenerated || !initials.IsOutputOnly() {
			t.Errorf("Initials: IsGenerated = %v, IsOutputOnly = %v, want both", initials.IsGenerated, initials.IsOutputOnly())
		}
		if name := fieldNamed(t, account, "Name"); name.IsGenerated || name.IsOutputOnly() {
			t.Error("Name is a plain column, not output-only")
		}
	})

	t.Run("the branches, each through its foreign key", func(t *testing.T) {
		t.Parallel()

		want := []resource.SearchBranch{
			{Name: "accountContact.displayName", Resource: "AccountContacts", Field: "DisplayName", Tag: "displayName", Table: "AccountContacts", Column: "DisplayName", JoinColumn: "AccountId", TenantColumn: "TenantId", Token: "DisplayName_Search", Index: "AccountContactsSearch", Role: resource.SearchRoleText, Contains: true},
			{Name: "accountNote.body", Resource: "AccountNotes", Field: "Body", Tag: "body", Table: "AccountNotes", Column: "Body", JoinColumn: "AccountId", TenantColumn: "TenantId", Token: "Body_Search", Index: "AccountNotesSearch", Role: resource.SearchRoleText, Contains: true},
			{Name: "pairing.label", Resource: "Pairings", Field: "Label", Tag: "label", Table: "Pairings", Column: "Label", JoinColumn: "OtherAccountId", TenantColumn: "TenantId", Token: "Label_Search", Index: "PairingsSearch", Role: resource.SearchRoleText, Contains: true},
		}
		got := make([]resource.SearchBranch, 0, len(account.SearchBranches))
		for _, branch := range account.SearchBranches {
			got = append(got, branch.SearchBranch)
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("SearchBranches mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("the warnings: an unpartitioned index on a tenant-scoped table, a branch index not storing the key", func(t *testing.T) {
		t.Parallel()

		var got []string
		for _, w := range c.schemaWarnings(resources) {
			switch w := w.(type) {
			case SearchPartitionWarning:
				if diff := cmp.Diff(SearchPartitionWarning{Resource: "Unpartitioned", Table: "Unpartitioneds", IndexName: "UnpartitionedsSearch", TenantColumn: "TenantId"}, w); diff != "" {
					t.Errorf("SearchPartitionWarning mismatch (-want +got):\n%s", diff)
				}
				got = append(got, "partition")
			case SearchBranchStoringWarning:
				if diff := cmp.Diff(SearchBranchStoringWarning{Resource: "Account", Child: "AccountNotes", IndexName: "AccountNotesSearch", JoinColumn: "AccountId"}, w); diff != "" {
					t.Errorf("SearchBranchStoringWarning mismatch (-want +got):\n%s", diff)
				}
				got = append(got, "storing")
			}
		}
		if diff := cmp.Diff([]string{"storing", "partition"}, got); diff != "" {
			t.Errorf("search warnings mismatch (-want +got):\n%s", diff)
		}
		if s := (SearchPartitionWarning{Resource: "Unpartitioned", Table: "Unpartitioneds", IndexName: "UnpartitionedsSearch", TenantColumn: "TenantId"}).String(); s != "Unpartitioned searches through UnpartitionedsSearch on Unpartitioneds, which is not partitioned, so every search reads every tenant's tokens; wanted: PARTITION BY TenantId on the index" {
			t.Errorf("SearchPartitionWarning.String() = %q", s)
		}
	})

	t.Run("what the templates read", func(t *testing.T) {
		t.Parallel()

		if diff := cmp.Diff([]string{"name", "phone", "bornOn", "openedAt", "email", "accountContact.displayName", "accountNote.body", "pairing.label"}, account.SearchFieldNames()); diff != "" {
			t.Errorf("SearchFieldNames() mismatch (-want +got):\n%s", diff)
		}
		if !account.HasSearchBox() || account.SearchKeyColumn() != "ID" {
			t.Errorf("HasSearchBox() = %v, SearchKeyColumn() = %q, want true and ID", account.HasSearchBox(), account.SearchKeyColumn())
		}
		columns := account.SearchColumns()
		if len(columns) != 5 || columns[0].Column != "Name" || columns[0].Tag != "token=Name_Search;index=AccountsSearch;role=text;anchors=prefix,suffix;contains" || columns[3].Tag != "token=OpenedAt_Search;index=AccountsSearch;role=date;contains;zone=America/New_York" {
			t.Errorf("SearchColumns() = %+v", columns)
		}
		for name, want := range map[string]struct {
			eligible   bool
			ts         string
			filterable string
			tag        string
		}{
			"Name":     {eligible: true, ts: "'contains', 'startswith', 'endswith'", filterable: "withIndexed", tag: `search:"token=Name_Search;index=AccountsSearch;role=text;anchors=prefix,suffix;contains"`},
			"Email":    {eligible: true, ts: "'startswith'", filterable: "withIndexed", tag: `search:"token=Email_Search;index=AccountsSearch;role=text;anchors=prefix;diacritics"`},
			"Code":     {eligible: true, ts: "'startswith'", filterable: "withIndexed", tag: `search:"lower=Code_Lower"`},
			"BornOn":   {eligible: false, ts: "", filterable: "withIndexed", tag: `search:"token=BornOn_Search;index=AccountsSearch;role=date;contains"`},
			"Notes":    {eligible: false, ts: "", filterable: "", tag: ""},
			"TenantID": {eligible: false, ts: "", filterable: "always", tag: ""},
		} {
			f := fieldNamed(t, account, name)
			if f.TextIdentEligible() != want.eligible || f.TypescriptSearch() != want.ts || f.TypescriptFilterable() != want.filterable || f.SearchTag() != want.tag {
				t.Errorf("%s: TextIdentEligible = %v, TypescriptSearch = %q, TypescriptFilterable = %q, SearchTag = %q; want %+v", name, f.TextIdentEligible(), f.TypescriptSearch(), f.TypescriptFilterable(), f.SearchTag(), want)
			}
		}
		email := fieldNamed(t, account, "Email")
		if email.AnswersMatch("contains") || !email.AnswersMatch("startswith") {
			t.Error("Email answers startswith alone under short_tokens_only_for_anchors")
		}
	})

	t.Run("the generated resource file: the branches, the text identifiers and the box", func(t *testing.T) {
		t.Parallel()

		out, err := c.generateTemplateOutput("resourceFileTemplate", resourceFileTemplate, &resourceFileData{Source: "pkg/resources", Package: "resources", Resource: account})
		if err != nil {
			t.Fatalf("generateTemplateOutput() error = %v", err)
		}
		source := string(out)
		for _, want := range []string{
			"func (Account) SearchBranches() []resource.SearchBranch {",
			`{Name: "accountContact.displayName", Resource: "AccountContacts", Field: "DisplayName", Tag: "displayName", Table: "AccountContacts", Column: "DisplayName", JoinColumn: "AccountId", TenantColumn: "TenantId", Token: "DisplayName_Search", Index: "AccountContactsSearch", Role: resource.SearchRoleText, Anchors: nil, Contains: true, Zone: "", PII: false},`,
			`JoinColumn: "OtherAccountId"`,
			"func (i AccountNameQueryIdent) EndsWith(term string) AccountQueryClause {",
			"func (i AccountEmailQueryIdent) StartsWith(term string) AccountQueryClause {",
			"func (i AccountCodeQueryIdent) StartsWith(term string) AccountQueryClause {",
			"func (p AccountQueryPartialClause) Search(text string) (AccountQueryClause, error) {",
			`clause, err := p.partialClause.Search(text, "ID", []resource.SearchColumn{`,
			`{Column: "BornOn", Tag: "token=BornOn_Search;index=AccountsSearch;role=date;contains"},`,
			"}, Account{}.SearchBranches())",
			`{Column: "OpenedAt", Tag: "token=OpenedAt_Search;index=AccountsSearch;role=date;contains;zone=America/New_York"},`,
		} {
			if !strings.Contains(source, want) {
				t.Errorf("resource file lacks %q:\n%s", want, source)
			}
		}
		for _, absent := range []string{"func (i AccountEmailQueryIdent) Contains", "func (i AccountCodeQueryIdent) Contains", "AccountBornOnQueryIdent", "AccountNotesQueryIdent"} {
			if strings.Contains(source, absent) {
				t.Errorf("resource file carries %q, want absent", absent)
			}
		}
	})

	t.Run("the TypeScript metadata: the matches per field and the box's fields", func(t *testing.T) {
		t.Parallel()

		for _, f := range account.Fields {
			f.typescriptType = "string"
		}
		data := tsResourcesData{Resources: []*resourceInfo{account}, GenPrefix: "zz_gen"}
		data.File = &typescriptGenerator{client: c}
		out, err := c.generateTemplateOutput("typescriptResourcesTemplate", typescriptResourcesTemplate, data)
		if err != nil {
			t.Fatalf("generateTemplateOutput() error = %v", err)
		}
		source := string(out)
		for _, want := range []string{
			"{ fieldName: 'name', displayType: 'string', required: true, isIndex: false, filterable: 'withIndexed', search: ['contains', 'startswith', 'endswith'] }",
			"{ fieldName: 'email', displayType: 'string', required: true, isIndex: false, filterable: 'withIndexed', search: ['startswith'] }",
			"{ fieldName: 'code', displayType: 'string', required: true, isIndex: false, filterable: 'withIndexed', search: ['startswith'] }",
			"{ fieldName: 'bornOn', displayType: 'string', required: true, isIndex: false, filterable: 'withIndexed' }",
			"{ fieldName: 'notes', displayType: 'string', required: true, isIndex: false }",
			"searchFields: ['name', 'phone', 'bornOn', 'openedAt', 'email', 'accountContact.displayName', 'accountNote.body', 'pairing.label'],",
		} {
			if !strings.Contains(source, want) {
				t.Errorf("metadata lacks %q:\n%s", want, source)
			}
		}
	})
}

// TestSearchFacts_View pins a view's authored search tags: the table column's fact,
// read off the schema, lands on the view's field.
func TestSearchFacts_View(t *testing.T) {
	t.Parallel()

	c := &client{tableMap: searchFixtureTables()}
	structs := fixtureStructs(loadFixture(t, "searchfixture"))
	views, err := c.structsToVirtualResources([]*parser.Struct{structs["Roster"]})
	if err != nil {
		t.Fatalf("structsToVirtualResources() error = %v", err)
	}
	roster := resourceNamed(t, views, "Roster")
	if diff := cmp.Diff(&resource.SearchSpec{Token: "Name_Search", Index: "AccountsSearch", Role: resource.SearchRoleText, Anchors: []resource.SearchAnchor{resource.SearchAnchorPrefix, resource.SearchAnchorSuffix}, Contains: true}, fieldNamed(t, roster, "Name").Search); diff != "" {
		t.Errorf("Roster.Name.Search mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(&resource.SearchSpec{Lower: "Code_Lower"}, fieldNamed(t, roster, "Code").Search); diff != "" {
		t.Errorf("Roster.Code.Search mismatch (-want +got):\n%s", diff)
	}
	if name := fieldNamed(t, roster, "Name"); name.TypescriptFilterable() != "always" || name.SearchTag() != `search:"token=Name_Search;index=AccountsSearch;role=text;anchors=prefix,suffix;contains"` {
		t.Errorf("Roster.Name: filterable %q, tag %q", name.TypescriptFilterable(), name.SearchTag())
	}
	if diff := cmp.Diff([]string{"name"}, roster.SearchFieldNames()); diff != "" {
		t.Errorf("SearchFieldNames() mismatch (-want +got):\n%s", diff)
	}

	// A view whose box spans two indexes passes when its root carries the (tenant, join
	// column) index the key set needs.
	views, err = c.structsToVirtualResources([]*parser.Struct{structs["Ledger"]})
	if err != nil {
		t.Fatalf("structsToVirtualResources(Ledger) error = %v", err)
	}
	if diff := cmp.Diff([]string{"displayName", "accountName"}, resourceNamed(t, views, "Ledger").SearchFieldNames()); diff != "" {
		t.Errorf("Ledger.SearchFieldNames() mismatch (-want +got):\n%s", diff)
	}
}

// TestSearchFacts_Refusals pins every refusal: the schema shapes the generator will
// not half-read, the declarations it will not resolve, and the tags an author may not
// write.
func TestSearchFacts_Refusals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		structs []string
		virtual bool
		wantErr string
	}{
		{name: "two search indexes on one table", structs: []string{"Doubled"}, wantErr: "declares two search indexes, DoubledsSearch and DoubledsOtherSearch"},
		{name: "a token column in no search index", structs: []string{"Orphaned"}, wantErr: "the token column Name_Search is in no search index"},
		{name: "two token columns over one column", structs: []string{"Twinned"}, wantErr: "the token columns Name_Again and Name_Search both tokenize Name"},
		{name: "a search index partitioned by a column that is not the tenant's", structs: []string{"Misparted"}, wantErr: "is partitioned by Region, and the resource's tenant column is TenantId"},
		{name: "a date token over a text field", structs: []string{"Mistyped"}, wantErr: "reads BornOn as a date, and the field is typed string"},
		{name: "a text token over a number field", structs: []string{"Numbered"}, wantErr: "which is not a column, the column's digits"},
		{name: "a timestamp formatted without its zone", structs: []string{"Unzoned"}, wantErr: "FORMAT_TIMESTAMP('%Y%m%d', OpenedAt) names no time zone"},
		{name: "a field over the token column", structs: []string{"Tokened"}, wantErr: "the column is a TOKENLIST column, which the table's search index reads and no field carries"},
		{name: "output_only restated on a generated column", structs: []string{"Restated"}, wantErr: `so the field is output-only by derivation; drop conditions:"output_only"`},
		{name: "a search tag on a table field", structs: []string{"Tagged"}, wantErr: "cannot use search tag in non-virtual resource; the schema states the search facts"},
		{name: "a branch from a two-column key", structs: []string{"Keyed", "AccountContact"}, wantErr: "Keyed declares 2 key columns; a search branch needs one"},
		{name: "a branch to a field with no search fact", structs: []string{"Unfacted", "AccountContact"}, wantErr: "AccountContact.UserID carries no search fact"},
		{name: "a branch across tenancies", structs: []string{"Global", "GlobalContact"}, wantErr: "Global and GlobalContact are not under the same tenancy"},
		{name: "a branch through an unnamed one of two foreign keys", structs: []string{"Twin", "TwinLink"}, wantErr: "TwinLink has 2 foreign keys referencing Twins (TwinId, OtherTwinId); name the one the box joins through with via:"},
		{name: "a branch through a foreign key the child lacks", structs: []string{"Misvia", "AccountContact"}, wantErr: "AccountContact has no foreign key Nope referencing Misvias.Id"},
		{name: "a branch to an unknown child", structs: []string{"Malformed"}, wantErr: "names a child field as Child.Field"},
		{name: "a view naming a table the schema lacks", structs: []string{"Lost"}, virtual: true, wantErr: "names table Nowhere, which the schema does not have"},
		{name: "a view naming a column with no fact", structs: []string{"Unfound"}, virtual: true, wantErr: "Accounts.Notes carries no search fact"},
		{name: "a view tag without the dot", structs: []string{"Dotless"}, virtual: true, wantErr: "names the table column the field carries as Table.Column"},
		{name: "a branch declared on a view", structs: []string{"Branched"}, virtual: true, wantErr: "a virtual resource declares none"},
		{name: "a view over two indexes whose root lacks the tenant-and-join index", structs: []string{"Journal"}, virtual: true, wantErr: "AccountNotes has no index on (TenantId, AccountId), so every search would scan its rows for the keys found; wanted: CREATE INDEX AccountNotesByTenantIdAccountId ON AccountNotes(TenantId, AccountId)"},
		{name: "a view over two indexes without a root", structs: []string{"Sprawl"}, virtual: true, wantErr: "reaches 2 search indexes (AccountContacts, Accounts), which the statement reads through the view's root table; declare @rowsOf(Table)"},
		{name: "a view whose root has no foreign key to the table its box reaches", structs: []string{"Detached"}, virtual: true, wantErr: "Globals has no foreign key to Accounts"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var err error
			if tt.virtual {
				c := &client{tableMap: searchFixtureTables()}
				structs := fixtureStructs(loadFixture(t, "searchfixture"))
				_, err = c.structsToVirtualResources([]*parser.Struct{structs[tt.structs[0]]})
			} else {
				_, _, err = searchFixture(t, tt.structs...)
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

// TestParseTokenExpression pins the reading of a TOKENLIST column's expression.
func TestParseTokenExpression(t *testing.T) {
	t.Parallel()

	tests := []struct {
		expression string
		want       *tokenFact
		wantErr    string
	}{
		{expression: "TOKENIZE_SUBSTRING(Name)", want: &tokenFact{Column: "T", Source: "Name", Role: resource.SearchRoleText, Contains: true}},
		{expression: "tokenize_substring(Name, relative_search_types => ['value_prefix', 'word_prefix'], ngram_size_min => 2)", want: &tokenFact{Column: "T", Source: "Name", Role: resource.SearchRoleText, Contains: true, Anchors: []resource.SearchAnchor{resource.SearchAnchorPrefix}}},
		{expression: "TOKENIZE_SUBSTRING(Name, short_tokens_only_for_anchors => true, remove_diacritics => true)", want: &tokenFact{Column: "T", Source: "Name", Role: resource.SearchRoleText, Contains: false, Diacritics: true}},
		{expression: `TOKENIZE_SUBSTRING(REGEXP_REPLACE(Phone, r'\D', ''))`, want: &tokenFact{Column: "T", Source: "Phone", Role: resource.SearchRoleDigits, Contains: true}},
		{expression: "TOKENIZE_SUBSTRING(FORMAT_DATE('%Y%m%d', BornOn))", want: &tokenFact{Column: "T", Source: "BornOn", Role: resource.SearchRoleDate, Contains: true}},
		{expression: "TOKENIZE_SUBSTRING(FORMAT_TIMESTAMP('%Y%m%d', OpenedAt, 'UTC'))", want: &tokenFact{Column: "T", Source: "OpenedAt", Role: resource.SearchRoleDate, Contains: true, Zone: "UTC"}},
		{expression: "TOKENIZE_FULLTEXT(Notes)", want: nil},
		{expression: "TOKENIZE_NGRAMS(Notes)", want: nil},
		{expression: "TOKENIZE_SUBSTRING(FORMAT_TIMESTAMP('%Y%m%d', OpenedAt))", wantErr: "names no time zone"},
		{expression: "TOKENIZE_SUBSTRING(FORMAT_DATE('%Y-%m-%d', BornOn))", wantErr: "reads a date as FORMAT_DATE('%Y%m%d', col)"},
		{expression: `TOKENIZE_SUBSTRING(REGEXP_REPLACE(Phone, r'[^0-9]', ''))`, wantErr: `reads a column's digits as REGEXP_REPLACE(col, r'\D', '')`},
		{expression: "TOKENIZE_SUBSTRING(LOWER(Name))", wantErr: "tokenizes LOWER(…), which is not a column"},
		{expression: "TOKENIZE_SUBSTRING(Name, language_tag => 'en', content_type => 'text/plain', flavor => 'x')", wantErr: "names the argument flavor, which the generator does not know"},
		{expression: "TOKENIZE_SUBSTRING(Name, First)", wantErr: "takes the tokenized expression as its one positional argument"},
		{expression: "Name", wantErr: "which is not a tokenizer call"},
		{expression: "TOKENIZE_SOMETHING(Name)", wantErr: "generated by TOKENIZE_SOMETHING, which the generator does not read"},
		{expression: "TOKENIZE_SUBSTRING(", wantErr: "does not parse"},
	}
	for _, tt := range tests {
		t.Run(tt.expression, func(t *testing.T) {
			t.Parallel()

			got, err := parseTokenExpression("T", tt.expression)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("parseTokenExpression() error = %v, want %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("parseTokenExpression() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got, cmp.AllowUnexported(tokenFact{})); diff != "" {
				t.Errorf("fact mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestParseLowerExpression(t *testing.T) {
	t.Parallel()

	for expression, want := range map[string]string{
		"LOWER(Name)":        "Name",
		"lower(Name)":        "Name",
		"UPPER(Name)":        "",
		"LOWER(Name, 'x')":   "",
		"LOWER(TRIM(Name))":  "",
		"SUBSTR(Name, 1, 1)": "",
		"LOWER(":             "",
	} {
		if got := parseLowerExpression(expression); got != want {
			t.Errorf("parseLowerExpression(%q) = %q, want %q", expression, got, want)
		}
	}
}

func TestLeadsIndex(t *testing.T) {
	t.Parallel()

	meta := searchFixtureTables()["Accounts"]
	tests := []struct {
		column    string
		tenant    string
		anySecond bool
		want      bool
	}{
		{column: "Id", want: true},
		{column: "TenantId", want: true},
		{column: "Code_Lower", tenant: "TenantId", want: true},
		{column: "Code_Lower", want: false},
		{column: "Code_Lower", anySecond: true, want: true},
		{column: "Name_Search", want: false},
	}
	for _, tt := range tests {
		if got := leadsIndex(meta, tt.column, tt.tenant, tt.anySecond); got != tt.want {
			t.Errorf("leadsIndex(%s, tenant %q, anySecond %v) = %v, want %v", tt.column, tt.tenant, tt.anySecond, got, tt.want)
		}
	}
}
