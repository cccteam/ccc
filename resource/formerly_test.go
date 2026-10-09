package resource

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/google/go-cmp/cmp"
)

// formerlyResource is a row whose Title column moved to the Headline wire name.
type formerlyResource struct {
	ID       string `spanner:"Id"`
	Headline string `spanner:"Headline"`
	Body     string `spanner:"Body"`
}

func (formerlyResource) Resource() accesstypes.Resource {
	return "articles"
}

func (formerlyResource) DefaultConfig() Config {
	return Config{}
}

// formerlyRequest is the generated request struct of formerlyResource: Headline carries
// its former wire name.
type formerlyRequest struct {
	ID       string `json:"id"       index:"true"     perm:"-"`
	Headline string `json:"headline" formerly:"title" index:"true"`
	Body     string `json:"body"`
}

// Test_rewriteFormerKeys pins the body rewrite: a former key becomes the current one
// before either decoder reads the body, a body naming both is refused naming the two,
// and a body that is not an object passes through for the decoder's own refusal.
func Test_rewriteFormerKeys(t *testing.T) {
	t.Parallel()

	former := map[string]string{"title": "headline"}

	tests := []struct {
		name    string
		body    string
		want    string
		wantErr string
	}{
		{name: "the former key is rewritten", body: `{"id":"1","title":"Dawn"}`, want: `{"headline":"Dawn","id":"1"}`},
		{name: "the current key passes through", body: `{"id":"1","headline":"Dawn"}`, want: `{"id":"1","headline":"Dawn"}`},
		{name: "a body naming neither passes through", body: `{"body":"text"}`, want: `{"body":"text"}`},
		{name: "a body naming both is refused", body: `{"title":"Dawn","headline":"Dusk"}`, wantErr: "json field title is the former name of headline: the body names both, send one"},
		{name: "a null former value still counts as named", body: `{"title":null,"headline":"Dusk"}`, wantErr: "the body names both, send one"},
		{name: "an array passes through untouched", body: `[{"title":"Dawn"}]`, want: `[{"title":"Dawn"}]`},
		{name: "malformed JSON passes through untouched", body: `{"title":`, want: `{"title":`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := rewriteFormerKeys([]byte(tt.body), former)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("rewriteFormerKeys() error = %v, want it to contain %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("rewriteFormerKeys() error = %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("rewriteFormerKeys() = %s, want %s", got, tt.want)
			}
		})
	}
}

// TestStructDecoder_formerNames pins the decoders' side: a body naming the former wire
// name reaches the field as the current one would, and a body naming both is refused.
func TestStructDecoder_formerNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		body         string
		wantHeadline string
		wantErr      string
	}{
		{name: "the former name writes the field", body: `{"title":"Dawn"}`, wantHeadline: "Dawn"},
		{name: "the current name writes the field", body: `{"headline":"Dawn"}`, wantHeadline: "Dawn"},
		{name: "both names are refused", body: `{"title":"Dawn","headline":"Dusk"}`, wantErr: "json field title is the former name of headline: the body names both, send one"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			decoder, err := NewStructDecoder[formerlyRequest]()
			if err != nil {
				t.Fatalf("NewStructDecoder() error = %v", err)
			}
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/articles", strings.NewReader(tt.body))
			got, err := decoder.Decode(req)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Decode() error = %v, want it to contain %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			if got.Headline != tt.wantHeadline {
				t.Errorf("Headline = %q, want %q", got.Headline, tt.wantHeadline)
			}
		})
	}
}

// TestQueryDecoder_formerNames pins the query side: columns, sort and filter take the
// former wire name and resolve to the live field, so the permission checks and the
// SQL run against it.
func TestQueryDecoder_formerNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		query        url.Values
		wantColumns  []accesstypes.Field
		wantSort     []SortField
		wantFilter   []accesstypes.Field
		wantFilterOK bool
	}{
		{
			name:        "columns under the former name select the field",
			query:       url.Values{"columns": []string{"id,title"}},
			wantColumns: []accesstypes.Field{"ID", "Headline"},
		},
		{
			name:     "sort under the former name orders by the field",
			query:    url.Values{"sort": []string{"title:desc"}},
			wantSort: []SortField{{Field: "Headline", Direction: SortDescending}},
		},
		{
			name:         "filter under the former name narrows by the field",
			query:        url.Values{"filter": []string{"title:eq:Dawn"}},
			wantFilter:   []accesstypes.Field{"Headline"},
			wantFilterOK: true,
		},
		{
			name:         "the current name still works beside it",
			query:        url.Values{"columns": []string{"headline"}, "sort": []string{"headline:asc"}, "filter": []string{"headline:eq:Dawn"}},
			wantColumns:  []accesstypes.Field{"Headline"},
			wantSort:     []SortField{{Field: "Headline", Direction: SortAscending}},
			wantFilter:   []accesstypes.Field{"Headline"},
			wantFilterOK: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			resSet, err := NewSet[formerlyResource, formerlyRequest](accesstypes.List)
			if err != nil {
				t.Fatalf("NewSet() error = %v", err)
			}
			decoder, err := NewQueryDecoder[formerlyResource, formerlyRequest](resSet)
			if err != nil {
				t.Fatalf("NewQueryDecoder() error = %v", err)
			}
			parsed, err := decoder.parseQuery(tt.query, nil)
			if err != nil {
				t.Fatalf("parseQuery() error = %v", err)
			}
			if diff := cmp.Diff(tt.wantColumns, parsed.ColumnFields); diff != "" {
				t.Errorf("ColumnFields mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantSort, parsed.SortFields); diff != "" {
				t.Errorf("SortFields mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantFilter, parsed.FilterFields); diff != "" {
				t.Errorf("FilterFields mismatch (-want +got):\n%s", diff)
			}
			if tt.wantFilterOK {
				if parsed.FilterParser == nil {
					t.Fatal("FilterParser = nil, want the filter")
				}
				if _, err := parsed.FilterParser(SpannerDBType); err != nil {
					t.Errorf("FilterParser() error = %v", err)
				}
			}
		})
	}
}

// TestNewSetData_formerTags pins the set data a renamed field registers: the former
// wire name by the current tag, and none where no field was renamed.
func TestNewSetData_formerTags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		fields []FieldTags
		want   map[accesstypes.Tag]accesstypes.Tag
	}{
		{
			name: "a renamed field",
			fields: []FieldTags{
				{Field: "ID", JSON: "id", Perm: "-"},
				{Field: "Headline", JSON: "headline", Formerly: "title"},
				{Field: "Body", JSON: "body"},
			},
			want: map[accesstypes.Tag]accesstypes.Tag{"headline": "title"},
		},
		{
			name:   "no renamed field",
			fields: []FieldTags{{Field: "ID", JSON: "id", Perm: "-"}, {Field: "Body", JSON: "body"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			set, err := NewSetData(tt.fields, accesstypes.List)
			if err != nil {
				t.Fatalf("NewSetData() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, set.FormerTags); diff != "" {
				t.Errorf("FormerTags mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestGeneratedCollection_formerNames pins the collection's carriage of former names:
// a field's and a method's reach the accessors through the builder and through the
// data a generated file declares, the data round-trips, and a former name that is
// another tag's name, one two tags share, or a method naming itself is refused.
func TestGeneratedCollection_formerNames(t *testing.T) {
	t.Parallel()

	t.Run("through the builder", func(t *testing.T) {
		t.Parallel()

		set, err := NewSetData([]FieldTags{
			{Field: "ID", JSON: "id", Perm: "-"},
			{Field: "Headline", JSON: "headline", Formerly: "title"},
		}, accesstypes.List)
		if err != nil {
			t.Fatalf("NewSetData() error = %v", err)
		}
		b := NewCollectionBuilder()
		if err := b.AddResourceSet(accesstypes.GlobalPermissionScope, "Articles", set); err != nil {
			t.Fatalf("AddResourceSet() error = %v", err)
		}
		if err := b.AddMethodResource(accesstypes.GlobalPermissionScope, accesstypes.Execute, "Publish"); err != nil {
			t.Fatalf("AddMethodResource() error = %v", err)
		}
		b.SetMethodFormerName(accesstypes.GlobalPermissionScope, "Publish", "Release")

		g := b.GeneratedCollection()
		assertFormerNames(t, g)

		roundTripped, err := NewGeneratedCollection(g.Data())
		if err != nil {
			t.Fatalf("NewGeneratedCollection(round trip) error = %v", err)
		}
		assertFormerNames(t, roundTripped)
		if diff := cmp.Diff(g.Data(), roundTripped.Data()); diff != "" {
			t.Errorf("Data() round trip mismatch (-first +second):\n%s", diff)
		}
	})

	tests := []struct {
		name    string
		data    CollectionData
		wantErr string
	}{
		{
			name: "a former name that is another tag's name",
			data: CollectionData{Resources: []CollectionResource{{
				Name: "Articles", Scope: accesstypes.GlobalPermissionScope, Permissions: []accesstypes.Permission{accesstypes.List},
				Tags: []TagData{{Name: "headline", Formerly: "body"}, {Name: "body"}},
			}}},
			wantErr: `tag "headline" under resource "Articles" names "body" as its former name, which is another tag's name`,
		},
		{
			name: "a former name two tags share",
			data: CollectionData{Resources: []CollectionResource{{
				Name: "Articles", Scope: accesstypes.GlobalPermissionScope, Permissions: []accesstypes.Permission{accesstypes.List},
				Tags: []TagData{{Name: "headline", Formerly: "title"}, {Name: "body", Formerly: "title"}},
			}}},
			wantErr: `tags "headline" and "body" under resource "Articles" name "title" as their former name; a former name belongs to one field`,
		},
		{
			name: "a method naming itself",
			data: CollectionData{Resources: []CollectionResource{{
				Name: "Publish", Scope: accesstypes.GlobalPermissionScope, Permissions: []accesstypes.Permission{accesstypes.Execute}, Formerly: "Publish",
			}}},
			wantErr: `resource "Publish" names itself as its former name`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := NewGeneratedCollection(tt.data)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("NewGeneratedCollection() error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

// assertFormerNames checks the renamed field and method the former-name tests declare.
func assertFormerNames(t *testing.T, g *GeneratedCollection) {
	t.Helper()

	former, ok := g.FormerTagName(accesstypes.GlobalPermissionScope, "Articles", "headline")
	if !ok || former != "title" {
		t.Errorf("FormerTagName(Articles, headline) = %q, %v; want title, true", former, ok)
	}
	if _, ok := g.FormerTagName(accesstypes.GlobalPermissionScope, "Articles", "id"); ok {
		t.Error("FormerTagName(Articles, id) ok = true for a field never renamed")
	}
	method, ok := g.FormerName(accesstypes.GlobalPermissionScope, "Publish")
	if !ok || method != "Release" {
		t.Errorf("FormerName(Publish) = %q, %v; want Release, true", method, ok)
	}
	if _, ok := g.FormerName(accesstypes.GlobalPermissionScope, "Articles"); ok {
		t.Error("FormerName(Articles) ok = true for a resource never renamed")
	}
}
