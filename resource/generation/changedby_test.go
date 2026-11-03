package generation

import (
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
		// wantWarnings are the warnings resolving raises, rendered.
		wantWarnings []string
		wantErr      string
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
			name:         "a declaration replaces what the query names, and the difference is a warning",
			structName:   "Extra",
			want:         &publishedSet{Root: "Berths", Tables: []string{"Visits"}},
			wantWarnings: []string{"Extra publishes its changes from @changedBy(Berths, Visits), which differs from what the query behind Subquery names: the query names Docks, which the declaration leaves out; the declaration names Visits, which the query does not"},
		},
		{
			name:       "a query naming neither a table nor a CTE in a table position is refused, the declaration the way out",
			structName: "Unread",
			wantErr:    "struct Unread: the query behind Subquery names Quays at line 1, column 18 in a table position (after FROM, JOIN or a comma of a FROM list), and that is neither a table in the schema nor a CTE in scope; declare the tables it reads with @changedBy(Table, ...), which replaces the reading",
		},
		{
			name:       "a query the lexer refuses is refused with the lexer's place",
			structName: "Broken",
			wantErr:    "struct Broken: the query behind Subquery cannot be read: the lexer stopped at line 1, column 42 (unclosed string literal); declare the tables it reads with @changedBy(Table, ...), which replaces the reading",
		},
		{
			name:       "a declaration beside a query the lexer refuses is taken without a word",
			structName: "Rescued",
			want:       &publishedSet{Tables: []string{"Berths"}},
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
			var decl *publishDecl
			var err error
			if tt.computed {
				res := fixtureComputedPublished(t, structs, tt.structName)
				err = c.resolvePublished(nil, []*computedResource{res})
				decl = &res.publishDecl
			} else {
				res := fixtureVirtualPublished(t, structs, tt.structName)
				err = c.resolvePublished([]*resourceInfo{res}, nil)
				decl = &res.publishDecl
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
			if diff := cmp.Diff(tt.want, decl.published, cmp.AllowUnexported(publishedSet{})); diff != "" {
				t.Errorf("published set mismatch (-want +got):\n%s", diff)
			}
			var warnings []string
			for _, warning := range decl.publishWarnings {
				warnings = append(warnings, warning.String())
			}
			if diff := cmp.Diff(tt.wantWarnings, warnings); diff != "" {
				t.Errorf("warnings mismatch (-want +got):\n%s", diff)
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
