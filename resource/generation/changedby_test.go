package generation

import (
	"go/format"
	"strings"
	"testing"

	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/google/go-cmp/cmp"
	"golang.org/x/tools/go/packages"
)

// The derivation and the declaration together: what a virtual resource's query names,
// what a declaration adds or must say, and what is refused, each refusal naming the fix.

// changedByClient is a client over the fixture package and a three-table schema.
func changedByClient(loaded *packages.Package) *client {
	return &client{
		tableMap:       map[string]*tableMetadata{"Berths": {}, "Docks": {}, "Visits": {}},
		loadedPackages: map[string]*packages.Package{"changedbyfixture": loaded},
	}
}

// fixtureVirtualPublished builds a virtual resource as the derivation sees it: the
// @rowsOf resolved by name and the @changedBy declared.
func fixtureVirtualPublished(t *testing.T, structs map[string]*parser.Struct, name string) *resourceInfo {
	t.Helper()

	res := fixtureVirtualView(t, structs, name)
	_, annotations := scanFixtureStruct(t, structs, name)
	declareChangedBy(annotations, &res.publishDecl)
	if res.rowsOfArg != nil {
		res.RowsOf = strings.TrimSpace(string(*res.rowsOfArg))
	}

	return res
}

// fixtureComputedPublished builds a computed resource the same way.
func fixtureComputedPublished(t *testing.T, structs map[string]*parser.Struct, name string) *computedResource {
	t.Helper()

	res := fixtureComputedView(t, structs, name)
	_, annotations := scanFixtureStruct(t, structs, name)
	declareChangedBy(annotations, &res.publishDecl)
	if res.rowsOfArg != nil {
		res.RowsOf = strings.TrimSpace(string(*res.rowsOfArg))
	}

	return res
}

func Test_resolvePublished(t *testing.T) {
	t.Parallel()

	loaded, parsed := loadFixturePackage(t, "changedbyfixture")
	structs := fixtureStructs(parsed)

	tests := []struct {
		name       string
		structName string
		computed   bool
		want       *publishedSet
		wantErr    string
	}{
		{
			name:       "an embedded file: the tables it names, the root apart, comments and literals ignored",
			structName: "Roster",
			want:       &publishedSet{Root: "Berths", Tables: []string{"Docks"}},
		},
		{
			name:       "a string literal",
			structName: "Literal",
			want:       &publishedSet{Tables: []string{"Docks"}},
		},
		{
			name:       "a concatenation of constants, in parentheses",
			structName: "Joined",
			want:       &publishedSet{Tables: []string{"Berths", "Visits"}},
		},
		{
			name:       "a query built at run time with no declaration is refused",
			structName: "RunTime",
			wantErr:    "struct RunTime: the query behind Subquery is built at run time",
		},
		{
			name:       "a query built at run time with a declaration takes the declaration",
			structName: "Declared",
			want:       &publishedSet{Tables: []string{"Berths", "Visits"}},
		},
		{
			name:       "a query that does not name the @rowsOf root is refused",
			structName: "Rootless",
			wantErr:    "struct Rootless: the query behind Subquery names no table called Berths",
		},
		{
			name:       "a declared table the schema lacks is refused",
			structName: "Unknown",
			wantErr:    `struct Unknown: @changedBy(Nowhere): "Nowhere" is not a table in the schema`,
		},
		{
			name:       "a declaration adds to what the query names",
			structName: "Extra",
			want:       &publishedSet{Root: "Berths", Tables: []string{"Docks", "Visits"}},
		},
		{
			name:       "a computed resource is its declaration",
			structName: "Tally",
			computed:   true,
			want:       &publishedSet{Tables: []string{"Docks", "Visits"}},
		},
		{
			name:       "a computed resource with no declaration is not published",
			structName: "Silent",
			computed:   true,
		},
		{
			name:       "a computed declaration that leaves out the @rowsOf root is refused",
			structName: "Partial",
			computed:   true,
			wantErr:    "struct Partial: @changedBy(Visits) leaves out Berths, the table @rowsOf names",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := changedByClient(loaded)
			var got *publishedSet
			var err error
			if tt.computed {
				res := fixtureComputedPublished(t, structs, tt.structName)
				err = c.resolvePublished(nil, []*computedResource{res})
				got = res.published
			} else {
				res := fixtureVirtualPublished(t, structs, tt.structName)
				err = c.resolvePublished([]*resourceInfo{res}, nil)
				got = res.published
			}
			if (err != nil) != (tt.wantErr != "") {
				t.Fatalf("resolvePublished() error = %v, wantErr %v", err, tt.wantErr != "")
			}
			if err != nil {
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("resolvePublished() error = %v, want it to contain %q", err, tt.wantErr)
				}

				return
			}
			if diff := cmp.Diff(tt.want, got, cmp.AllowUnexported(publishedSet{})); diff != "" {
				t.Errorf("published set mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// Test_rejectChangedBy pins the kinds that may not declare what changes them: a
// table-backed resource and an RPC method publish their own rows, or none.
func Test_rejectChangedBy(t *testing.T) {
	t.Parallel()

	_, parsed := loadFixturePackage(t, "changedbyfixture")
	structs := fixtureStructs(parsed)

	tests := []struct {
		name       string
		structName string
		kind       string
		wantErr    string
	}{
		{name: "a table-backed resource is refused", structName: "Plain", kind: "table-backed resource", wantErr: "struct Plain: @changedBy is only valid on @virtual and @computed structs"},
		{name: "an RPC method is refused", structName: "Plain", kind: "RPC method", wantErr: "this RPC method publishes its own rows"},
		{name: "a struct declaring nothing passes", structName: "Berth", kind: "table-backed resource"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pStruct, annotations := scanFixtureStruct(t, structs, tt.structName)
			err := rejectChangedBy(pStruct, annotations, tt.kind)
			if (err != nil) != (tt.wantErr != "") {
				t.Fatalf("rejectChangedBy() error = %v, wantErr %v", err, tt.wantErr != "")
			}
			if err != nil && !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("rejectChangedBy() error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func Test_tablesNamed(t *testing.T) {
	t.Parallel()

	tables := map[string]string{"berths": "Berths", "docks": "Docks", "visits": "Visits", "name": "Name"}
	tests := []struct {
		name string
		text string
		want []string
	}{
		{name: "plain names, aliases ignored", text: "SELECT b.Slot FROM Berths b JOIN Docks d ON d.Id = b.DockId", want: []string{"Berths", "Docks"}},
		{name: "backticked and dotted names", text: "SELECT 1 FROM `Berths` JOIN sch.Docks ON TRUE", want: []string{"Berths", "Docks"}},
		{name: "case is not significant", text: "select * from berths", want: []string{"Berths"}},
		{name: "a line comment does not count", text: "SELECT 1 FROM Berths -- and Docks\n", want: []string{"Berths"}},
		{name: "a block comment does not count", text: "SELECT 1 /* Visits\n Docks */ FROM Berths", want: []string{"Berths"}},
		{name: "a string literal does not count", text: "SELECT 'Docks' AS Label, \"Visits\" AS Other FROM Berths", want: []string{"Berths"}},
		{name: "a CTE named like no table drops out", text: "WITH Booked AS (SELECT * FROM Visits) SELECT * FROM Booked", want: []string{"Visits"}},
		{name: "a column sharing a table's name over-includes it", text: "SELECT d.Name FROM Docks d", want: []string{"Docks", "Name"}},
		{name: "a parameter is not a table", text: "SELECT 1 FROM Berths WHERE Slot > @docks", want: []string{"Berths", "Docks"}},
		{name: "nothing named", text: "SELECT 1", want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(tt.want, tablesNamed(tt.text, tables)); diff != "" {
				t.Errorf("tablesNamed() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// Test_liveDerivationsTemplate renders the generated live derivations: the empty
// value, and entries with a root, with tables, global and not.
func Test_liveDerivationsTemplate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		data            *liveDerivationsData
		wantContains    []string
		wantNotContains []string
	}{
		{
			name:            "nothing derives: the empty value, still handed to every publish",
			data:            &liveDerivationsData{Source: "pkg/resources", Package: "app"},
			wantContains:    []string{"package app", "var liveDerivations = &live.Derivations{}"},
			wantNotContains: []string{"Resources:"},
		},
		{
			name: "a root and its tables, a rootless global resource",
			data: &liveDerivationsData{Source: "pkg/resources", Package: "app", Resources: []derivedResourceData{
				{Resource: "MissionBoards", Root: "Missions", Tables: []sourceTableData{{Name: "Clients", Global: true}, {Name: "Squadrons"}}},
				{Resource: "Rosters", Global: true, Tables: []sourceTableData{{Name: "Pilots"}}},
			}},
			wantContains: []string{
				`{Resource: "MissionBoards", Root: "Missions", Tables: []live.SourceTable{`,
				`{Name: "Clients", Global: true},`,
				`{Name: "Squadrons"},`,
				`{Resource: "Rosters", Global: true, Tables: []live.SourceTable{`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := &client{}
			rendered, err := c.generateTemplateOutput("liveDerivationsTemplate", liveDerivationsTemplate, tt.data)
			if err != nil {
				t.Fatalf("generateTemplateOutput() error = %v", err)
			}
			// The writer formats the file, so the expectations read the formatted text.
			out, err := format.Source(rendered)
			if err != nil {
				t.Fatalf("format.Source() error = %v:\n%s", err, rendered)
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(string(out), want) {
					t.Errorf("output lacks %q:\n%s", want, out)
				}
			}
			for _, notWant := range tt.wantNotContains {
				if strings.Contains(string(out), notWant) {
					t.Errorf("output carries %q:\n%s", notWant, out)
				}
			}
		})
	}
}
