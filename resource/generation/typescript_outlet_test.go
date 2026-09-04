package generation

import (
	"strings"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/ettle/strcase"
	"github.com/google/go-cmp/cmp"
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
			method: &rpcMethodInfo{Struct: structs["DoSomething"], Transition: &rpcTransition{rpcTarget: rpcTarget{RootResource: "Widgets"}}},
		},
		{
			name:         "a transition root on another outlet fails",
			method:       &rpcMethodInfo{Struct: structs["DoSomething"], Transition: &rpcTransition{rpcTarget: rpcTarget{RootResource: "Gadgets"}}},
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

// Test_typescriptGenerator_applyOutletFilter pins the filter's order: the parsed
// sets arrive whole (an RPC method resolves its root against every resource), then
// every member off the target outlet falls away — a method on another outlet may
// name a resource on another outlet freely — and only the surviving methods'
// cross-outlet references fail generation.
func Test_typescriptGenerator_applyOutletFilter(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadCollectionFixture(t))
	newResources := func(t *testing.T) []*resourceInfo {
		t.Helper()

		widget := fixtureResource(t, structs, "Widget", func(res *resourceInfo) { res.OutletNames = []string{"portal"} })
		gadget := fixtureResource(t, structs, "Gadget", nil)

		return []*resourceInfo{widget, gadget}
	}
	memberOn := func(name, root string, outlets ...string) *rpcMethodInfo {
		return &rpcMethodInfo{
			Struct:           structs[name],
			Transition:       &rpcTransition{rpcTarget: rpcTarget{RootResource: root}},
			outletMembership: outletMembership{OutletNames: outlets},
		}
	}

	tests := []struct {
		name           string
		methods        []*rpcMethodInfo
		wantMethods    []string
		wantExcluded   []accesstypes.Resource
		wantErrContain string
	}{
		{
			name:         "an off-outlet method may root on an off-outlet resource",
			methods:      []*rpcMethodInfo{memberOn("DoSomething", "Widgets", "portal"), memberOn("HiddenMethod", "Gadgets")},
			wantMethods:  []string{"DoSomething"},
			wantExcluded: []accesstypes.Resource{"Gadgets", "HiddenMethod"},
		},
		{
			name:           "a member method rooted off the outlet still fails",
			methods:        []*rpcMethodInfo{memberOn("DoSomething", "Gadgets", "portal")},
			wantErrContain: "declares a transition on Gadgets, which is not on the outlet",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			g := &typescriptGenerator{
				outletName:      "portal",
				routerResources: []accesstypes.Resource{"Widgets", "Gadgets"},
				client:          &client{rpcMethods: tt.methods},
			}

			resources, _, err := g.applyOutletFilter(newResources(t), nil)
			if tt.wantErrContain != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErrContain) {
					t.Fatalf("applyOutletFilter() error = %v, want containing %q", err, tt.wantErrContain)
				}

				return
			}
			if err != nil {
				t.Fatalf("applyOutletFilter() error = %v", err)
			}

			if len(resources) != 1 || resources[0].Name() != "Widget" {
				t.Errorf("resources = %v, want exactly Widget", resources)
			}
			gotMethods := make([]string, 0, len(g.rpcMethods))
			for _, m := range g.rpcMethods {
				gotMethods = append(gotMethods, m.Name())
			}
			if diff := cmp.Diff(tt.wantMethods, gotMethods); diff != "" {
				t.Errorf("rpcMethods mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantExcluded, g.outletExcluded); diff != "" {
				t.Errorf("outletExcluded mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff([]accesstypes.Resource{"Widgets"}, g.routerResources); diff != "" {
				t.Errorf("routerResources mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// Test_typescriptGenerator_manualMethods pins that the Methods constants gain the
// Execute registrations without a parsed RPC struct — @manualAddResource(Execute) —
// and never repeat a generated method.
func Test_typescriptGenerator_manualMethods(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadCollectionFixture(t))

	tests := []struct {
		name    string
		methods []accesstypes.Resource
		parsed  []*rpcMethodInfo
		want    []accesstypes.Resource
	}{
		{
			name:    "a manual registration joins, a generated one is skipped",
			methods: []accesstypes.Resource{"DoSomething", "ViewAsUser"},
			parsed:  []*rpcMethodInfo{{Struct: structs["DoSomething"]}},
			want:    []accesstypes.Resource{"ViewAsUser"},
		},
		{
			name:    "no manual registrations yields none",
			methods: []accesstypes.Resource{"DoSomething"},
			parsed:  []*rpcMethodInfo{{Struct: structs["DoSomething"]}},
			want:    []accesstypes.Resource{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			g := &typescriptGenerator{client: &client{rpcMethods: tt.parsed}}
			if diff := cmp.Diff(tt.want, g.manualMethods(tt.methods)); diff != "" {
				t.Errorf("manualMethods() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// Test_typescriptConstantsTemplate_manualMethods pins the emitted constant: a manual
// Execute registration lands in Methods after the generated methods, typed Method.
func Test_typescriptConstantsTemplate_manualMethods(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadCollectionFixture(t))
	c := &client{}
	out, err := c.generateTemplateOutput("typescriptConstantsTemplate", typescriptConstantsTemplate, tsConstantsData{
		File:          &typescriptGenerator{client: c},
		Data:          &resource.TypescriptData{Permissions: []accesstypes.Permission{accesstypes.Execute}},
		RPCMethods:    []*rpcMethodInfo{{Struct: structs["DoSomething"]}},
		ManualMethods: []accesstypes.Resource{"ViewAsUser"},
	})
	if err != nil {
		t.Fatalf("generateTemplateOutput() error = %v", err)
	}

	want := "export const Methods = {\n  DoSomething: 'DoSomething' as Method,\n  ViewAsUser: 'ViewAsUser' as Method,\n};"
	if !strings.Contains(string(out), want) {
		t.Errorf("typescriptConstantsTemplate output missing %q:\n%s", want, out)
	}
}
