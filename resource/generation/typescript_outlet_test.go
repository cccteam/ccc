package generation

import (
	"strings"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/ettle/strcase"
)

// Test_typescriptGenerator_excludeFromOutlet pins the outlet filter's bookkeeping: a
// member off the target outlet is excluded and recorded — its collection resource
// name always, its table name only for table-backed kinds — while a member on the
// outlet passes untouched.
func Test_typescriptGenerator_excludeFromOutlet(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		targetOutlet  string
		memberOutlets []string
		isTable       bool
		wantExcluded  bool
		wantTable     bool
	}{
		{name: "unannotated member stays on a default target", targetOutlet: "", memberOutlets: nil, isTable: true},
		{name: "unannotated member falls off an extra-outlet target", targetOutlet: "portal", memberOutlets: nil, isTable: true, wantExcluded: true, wantTable: true},
		{name: "outlet member stays on its own target", targetOutlet: "portal", memberOutlets: []string{"portal"}, isTable: true},
		{name: "outlet member falls off the default target", targetOutlet: "", memberOutlets: []string{"portal"}, isTable: true, wantExcluded: true, wantTable: true},
		{name: "a method is never recorded as a table", targetOutlet: "portal", memberOutlets: nil, isTable: false, wantExcluded: true, wantTable: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			g := &typescriptGenerator{outletName: tt.targetOutlet, outletExcludedTables: make(map[string]struct{})}
			m := &outletMembership{OutletNames: tt.memberOutlets}

			if got := g.excludeFromOutlet(m, "Widgets", tt.isTable); got != tt.wantExcluded {
				t.Fatalf("excludeFromOutlet() = %v, want %v", got, tt.wantExcluded)
			}
			if got := len(g.outletExcluded) == 1 && g.outletExcluded[0] == "Widgets"; got != tt.wantExcluded {
				t.Errorf("outletExcluded = %v, want recorded = %v", g.outletExcluded, tt.wantExcluded)
			}
			if _, got := g.outletExcludedTables["Widgets"]; got != tt.wantTable {
				t.Errorf("outletExcludedTables[Widgets] present = %v, want %v", got, tt.wantTable)
			}
		})
	}
}

// Test_typescriptGenerator_validateOutletMemberReferences pins the fail-loud rule for
// cross-outlet references: a member method whose transition root or enumerated field
// names an excluded resource fails generation, because the emitted metadata would
// reference a Resources constant the filtered constants file no longer declares.
func Test_typescriptGenerator_validateOutletMemberReferences(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadCollectionFixture(t))
	enumerated := "Gadgets"

	tests := []struct {
		name         string
		method       *rpcMethodInfo
		excluded     []accesstypes.Resource
		wantContains string
	}{
		{
			name:   "a member method with no excluded references passes",
			method: &rpcMethodInfo{Struct: structs["DoSomething"], Transition: &rpcTransition{RootResource: "Widgets"}},
		},
		{
			name:         "a transition root on another outlet fails",
			method:       &rpcMethodInfo{Struct: structs["DoSomething"], Transition: &rpcTransition{RootResource: "Gadgets"}},
			excluded:     []accesstypes.Resource{"Gadgets"},
			wantContains: "declares a transition on Gadgets, which is not on the outlet",
		},
		{
			name: "an enumerated field referencing another outlet fails",
			method: &rpcMethodInfo{
				Struct: structs["DoSomething"],
				Fields: []*rpcField{{Field: structs["DoSomething"].Fields()[0], enumeratedResource: &enumerated}},
			},
			excluded:     []accesstypes.Resource{"Gadgets"},
			wantContains: "enumerates Gadgets, which is not on the outlet",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			g := &typescriptGenerator{
				outletName:     "portal",
				outletExcluded: tt.excluded,
				client:         &client{rpcMethods: []*rpcMethodInfo{tt.method}},
			}

			err := g.validateOutletMemberReferences()
			if tt.wantContains == "" {
				if err != nil {
					t.Fatalf("validateOutletMemberReferences() error = %v, want nil", err)
				}

				return
			}
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tt.wantContains) {
				t.Fatalf("error %q does not contain %q", err, tt.wantContains)
			}
		})
	}
}

// Test_apiClientData_targetOutlet pins that the client descriptor follows the
// target's outlet: an extra-outlet target emits exactly its members, and the default
// target emits none of them.
func Test_apiClientData_targetOutlet(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadCollectionFixture(t))
	newGenerator := func(outletName string) *typescriptGenerator {
		widget := fixtureResource(t, structs, "Widget", func(res *resourceInfo) {
			for _, f := range res.Fields {
				f.typescriptType = "string"
			}
			res.OutletNames = []string{"portal"}
		})

		return &typescriptGenerator{
			outletName: outletName,
			client: &client{
				resources: []*resourceInfo{widget},
				rpcMethods: []*rpcMethodInfo{
					{Struct: structs["DoSomething"], outletMembership: outletMembership{OutletNames: []string{"portal"}}},
				},
			},
		}
	}

	portal := newGenerator("portal").apiClientData()
	if len(portal.Resources) != 1 || portal.Resources[0].Name != "Widgets" {
		t.Errorf("portal target Resources = %+v, want exactly Widgets", portal.Resources)
	}
	if len(portal.Methods) != 1 || portal.Methods[0].Name != strcase.ToPascal("DoSomething") {
		t.Errorf("portal target Methods = %+v, want exactly DoSomething", portal.Methods)
	}

	standard := newGenerator("").apiClientData()
	if len(standard.Resources) != 0 || len(standard.Methods) != 0 {
		t.Errorf("default target must not carry portal members; Resources = %+v, Methods = %+v", standard.Resources, standard.Methods)
	}
}
