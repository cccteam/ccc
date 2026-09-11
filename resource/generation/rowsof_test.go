package generation

import (
	"strings"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/cccteam/ccc/resource/generation/parser/genlang"
)

// The struct-scope @rowsOf: a view declares the table whose rows it carries, the
// declaration is captured at extraction and resolved once every kind is extracted,
// and every rule refuses with the fix named — the target is a table-backed resource
// and never a view or an enumeration table, the view's key is the table's, the two
// share a scope, and the table follows the view onto every outlet.

// scanFixtureStruct scans a fixture struct's annotations the way extraction does.
func scanFixtureStruct(t *testing.T, structs map[string]*parser.Struct, name string) (*parser.Struct, genlang.StructAnnotations) {
	t.Helper()

	pStruct := structs[name]
	if pStruct == nil {
		t.Fatalf("struct %q not found in fixture package", name)
	}
	annotations, err := genlang.NewScanner(resourceKeywords()).ScanStruct(pStruct)
	if err != nil {
		t.Fatalf("ScanStruct(%s) error = %v", name, err)
	}

	return pStruct, annotations
}

// fixtureTable builds a table-backed resource keyed by the named fields, in that order.
func fixtureTable(t *testing.T, structs map[string]*parser.Struct, name string, keys ...string) *resourceInfo {
	t.Helper()

	return fixtureResource(t, structs, name, func(res *resourceInfo) {
		res.PkCount = len(keys)
		for _, f := range res.Fields {
			f.IsPrimaryKey = false
			for i, key := range keys {
				if f.Name() == key {
					f.IsPrimaryKey = true
					f.KeyOrdinalPosition = int64(i)
				}
			}
		}
	})
}

// fixtureVirtualView builds a virtual resource from its fixture struct as extraction
// would: the @primarykey fields keyed in declaration order and the @rowsOf declared.
func fixtureVirtualView(t *testing.T, structs map[string]*parser.Struct, name string) *resourceInfo {
	t.Helper()

	pStruct, annotations := scanFixtureStruct(t, structs, name)
	res := &resourceInfo{TypeInfo: pStruct.TypeInfo, IsVirtual: true}
	var keyCount int64
	for i, f := range pStruct.Fields() {
		field := &resourceField{Field: f, Parent: res}
		if annotations.Fields[i].Has(primarykeyKeyword) {
			field.IsPrimaryKey = true
			field.KeyOrdinalPosition = keyCount
			keyCount++
		}
		res.Fields = append(res.Fields, field)
	}
	declareRowsOf(annotations, &res.rowsOfDecl)

	return res
}

// fixtureComputedView builds a computed resource from its fixture struct as
// extraction would.
func fixtureComputedView(t *testing.T, structs map[string]*parser.Struct, name string) *computedResource {
	t.Helper()

	pStruct, annotations := scanFixtureStruct(t, structs, name)
	res := &computedResource{Struct: pStruct}
	var keyCount int
	for i, f := range pStruct.Fields() {
		field := &computedField{Field: f}
		if annotations.Fields[i].Has(primarykeyKeyword) {
			field.IsPrimaryKey = true
			field.KeyOrdinalPosition = keyCount
			keyCount++
		}
		res.Fields = append(res.Fields, field)
	}
	declareRowsOf(annotations, &res.rowsOfDecl)

	return res
}

// Test_declareRowsOf pins that extraction records the annotation as written on the
// struct that carries it and nothing on one that does not.
func Test_declareRowsOf(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "rowsoffixture"))

	tests := []struct {
		name    string
		fixture string
		want    string
	}{
		{name: "a declaring view carries the argument", fixture: "Mooring", want: "Berths"},
		{name: "a declaring computed view carries the argument", fixture: "Occupancy", want: "Berths"},
		{name: "an undeclared struct carries none", fixture: "Berth"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, annotations := scanFixtureStruct(t, structs, tt.fixture)
			var decl rowsOfDecl
			declareRowsOf(annotations, &decl)
			if (decl.rowsOfArg != nil) != (tt.want != "") {
				t.Fatalf("rowsOfArg = %v, want declared %v", decl.rowsOfArg, tt.want != "")
			}
			if tt.want != "" && string(*decl.rowsOfArg) != tt.want {
				t.Errorf("rowsOfArg = %q, want %q", *decl.rowsOfArg, tt.want)
			}
			if decl.HasRowsOf() {
				t.Error("HasRowsOf() = true before resolution")
			}
		})
	}
}

// Test_rejectRowsOf pins the kinds that may not declare a table: a table-backed
// resource and an RPC method, each refused naming the struct and the kind.
func Test_rejectRowsOf(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "rowsoffixture"))

	tests := []struct {
		name    string
		fixture string
		kind    string
		wantErr string
	}{
		{name: "a table-backed resource is refused", fixture: "TableWithRowsOf", kind: "table-backed resource", wantErr: "struct TableWithRowsOf: @rowsOf is only valid on @virtual and @computed structs; this table-backed resource is not a view over a table"},
		{name: "an RPC method is refused", fixture: "MethodWithRowsOf", kind: "RPC method", wantErr: "struct MethodWithRowsOf: @rowsOf is only valid on @virtual and @computed structs; this RPC method is not a view over a table"},
		{name: "a struct declaring nothing passes", fixture: "Berth", kind: "table-backed resource"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pStruct, annotations := scanFixtureStruct(t, structs, tt.fixture)
			err := rejectRowsOf(pStruct, annotations, tt.kind)
			if (err != nil) != (tt.wantErr != "") {
				t.Fatalf("rejectRowsOf() error = %v, wantErr %v", err, tt.wantErr != "")
			}
			if err != nil && !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("rejectRowsOf() error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

// Test_resolveRowsOf pins every rule, on a virtual and on a computed view: the good
// declarations resolve to the table, and each refusal names the struct, the
// declaration, the mismatch, and the fix.
func Test_resolveRowsOf(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "rowsoffixture"))
	arg := func(s string) *genlang.Arg {
		a := genlang.Arg(s)

		return &a
	}

	tests := []struct {
		name     string
		view     string // the virtual fixture struct; empty when computed is set
		computed string // the computed fixture struct
		mutate   func(res *resourceInfo)
		table    func(res *resourceInfo) // mutates the Berths table
		client   func(c *client)
		wantErr  string
	}{
		{name: "a virtual view declaring the table's key resolves", view: "Mooring"},
		{name: "a computed view declaring the table's key resolves", computed: "Occupancy"},
		{
			name: "two arguments are refused", view: "Mooring",
			mutate:  func(res *resourceInfo) { res.rowsOfArg = arg("Berths, Docks") },
			wantErr: "struct Mooring: @rowsOf takes one argument, the table resource whose rows the view carries; got \"Berths, Docks\"",
		},
		{
			name: "the view naming itself is refused", view: "Mooring",
			mutate:  func(res *resourceInfo) { res.rowsOfArg = arg("Moorings") },
			wantErr: "struct Mooring: @rowsOf(Moorings): the view names itself; name the table resource whose rows it carries",
		},
		{
			name: "an enumeration table is refused as a contradiction", view: "Mooring",
			mutate: func(res *resourceInfo) { res.rowsOfArg = arg("Kinds") },
			client: func(c *client) {
				c.enumerateTables = map[string]string{"Kinds": "Kind"}
			},
			wantErr: "struct Mooring: @rowsOf(Kinds): Kinds is an enumeration table (@enumerate on type Kind), whose rows are the program's constants; a view over them is a read-only list, so remove the annotation",
		},
		{
			name: "a struct backing an enumeration table is refused the same way", view: "Mooring",
			client: func(c *client) {
				c.enumerateTables = map[string]string{"Berths": "Berth"}
			},
			wantErr: "struct Mooring: @rowsOf(Berths): Berths is an enumeration table (@enumerate on type Berth)",
		},
		{
			name: "a virtual target is refused: a view cannot back a view", view: "Mooring",
			mutate:  func(res *resourceInfo) { res.rowsOfArg = arg("Renameds") },
			wantErr: "struct Mooring: @rowsOf(Renameds): Renameds is a virtual resource, and a view cannot back a view; name the table resource its rows come from",
		},
		{
			name: "a computed target is refused: a view cannot back a view", view: "Mooring",
			mutate:  func(res *resourceInfo) { res.rowsOfArg = arg("Occupancies") },
			wantErr: "struct Mooring: @rowsOf(Occupancies): Occupancies is a computed resource, and a view cannot back a view; name the table resource its rows come from",
		},
		{
			name: "an unknown resource is refused", view: "Mooring",
			mutate:  func(res *resourceInfo) { res.rowsOfArg = arg("Piers") },
			wantErr: `struct Mooring: @rowsOf(Piers): resource "Piers" does not exist; name a table-backed @resource by its collection name`,
		},
		{
			name: "a view declaring no key is told the table's", view: "Mooring",
			mutate: func(res *resourceInfo) {
				for _, f := range res.Fields {
					f.IsPrimaryKey = false
				}
			},
			wantErr: "struct Mooring: @rowsOf(Berths): the view declares no @primarykey; declare Berths's key on the view, in this order: DockID ccc.UUID (DockId), Slot int64",
		},
		{
			name: "a key of another count is refused", view: "Mooring",
			mutate:  func(res *resourceInfo) { res.Fields[1].IsPrimaryKey = false },
			wantErr: "struct Mooring: @rowsOf(Berths): the view's key spans 1 columns and Berths's spans 2; declare Berths's key on the view, in this order: DockID ccc.UUID (DockId), Slot int64",
		},
		{
			name: "a key in another order is refused at the first column", view: "Reordered",
			wantErr: "struct Reordered: @rowsOf(Berths): key column 1 is field Slot on the view and DockID on Berths; the view's key fields carry Berths's Go field names in its key order: DockID ccc.UUID (DockId), Slot int64",
		},
		{
			name: "a key under other Go field names is refused", view: "Misnamed",
			wantErr: "struct Misnamed: @rowsOf(Berths): key column 1 is field Dock on the view and DockID on Berths",
		},
		{
			name: "a key of another Go type is refused", view: "Retyped",
			wantErr: "struct Retyped: @rowsOf(Berths): key field DockID is string on the view and ccc.UUID on Berths; the view's key fields carry Berths's Go types, nullability included: DockID ccc.UUID (DockId), Slot int64",
		},
		{
			name: "a virtual key read from other columns is refused", view: "Renamed",
			wantErr: `struct Renamed: @rowsOf(Berths): key field DockID reads column "Dock" on the view and "DockId" on Berths; the view's projection names Berths's key columns: DockID ccc.UUID (DockId), Slot int64`,
		},
		{
			name: "a scope the table does not share is refused", view: "Mooring",
			mutate:  func(res *resourceInfo) { res.PermissionScope = accesstypes.DomainPermissionScope },
			wantErr: "struct Mooring: @rowsOf(Berths): the view is domain-scoped and Berths is global-scoped; a view carries its table's permission scope, so declare @permissionScope(global) on both",
		},
		{
			name: "a shared domain scope resolves", view: "Mooring",
			mutate: func(res *resourceInfo) { res.PermissionScope = accesstypes.DomainPermissionScope },
			table:  func(res *resourceInfo) { res.PermissionScope = accesstypes.DomainPermissionScope },
		},
		{
			name: "an outlet the table is not on is refused", view: "Mooring",
			mutate:  func(res *resourceInfo) { res.OutletNames = []string{"default", "portal"} },
			wantErr: `struct Mooring: @rowsOf(Berths): the view is served on outlet "portal" and Berths is not; attach Berths to the outlet via @outlet(portal), or take the view off it`,
		},
		{
			name: "a table on the view's outlets resolves", view: "Mooring",
			mutate: func(res *resourceInfo) { res.OutletNames = []string{"portal"} },
			table:  func(res *resourceInfo) { res.OutletNames = []string{"default", "portal"} },
		},
		{
			name: "a table off the default outlet refuses an undeclared view", view: "Mooring",
			table:   func(res *resourceInfo) { res.OutletNames = []string{"portal"} },
			wantErr: `struct Mooring: @rowsOf(Berths): the view is served on outlet "default" and Berths is not`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			berths := fixtureTable(t, structs, "Berth", "DockID", "Slot")
			if tt.table != nil {
				tt.table(berths)
			}
			renamed := fixtureVirtualView(t, structs, "Renamed")
			renamed.rowsOfArg = nil
			occupancy := fixtureComputedView(t, structs, "Occupancy")
			c := &client{
				resources:         []*resourceInfo{berths, fixtureTable(t, structs, "Dock", "ID"), renamed},
				computedResources: []*computedResource{occupancy},
			}
			if tt.client != nil {
				tt.client(c)
			}

			var resources []*resourceInfo
			var computed []*computedResource
			var decl *rowsOfDecl
			if tt.computed != "" {
				computed = []*computedResource{occupancy}
				decl = &occupancy.rowsOfDecl
			} else {
				view := fixtureVirtualView(t, structs, tt.view)
				if tt.mutate != nil {
					tt.mutate(view)
				}
				c.resources = append(c.resources, view)
				resources = []*resourceInfo{view}
				decl = &view.rowsOfDecl
			}

			err := c.resolveRowsOf(resources, computed)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("resolveRowsOf() error = %v, want it to contain %q", err, tt.wantErr)
				}
				if decl.HasRowsOf() {
					t.Error("a refused declaration must not record the table")
				}

				return
			}
			if err != nil {
				t.Fatalf("resolveRowsOf() error = %v", err)
			}
			if !decl.HasRowsOf() || decl.RowsOf != "Berths" {
				t.Errorf("RowsOf = %q, want Berths", decl.RowsOf)
			}
		})
	}
}

// Test_typescriptResourcesTemplate_rowsOf pins the emitted metadata: a declared view
// carries rowsOf naming the table's constant, on both kinds, and an undeclared one
// carries nothing.
func Test_typescriptResourcesTemplate_rowsOf(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "rowsoffixture"))
	stringTyped := func(res *resourceInfo) {
		for _, f := range res.Fields {
			f.typescriptType = "string"
		}
	}
	declared := fixtureVirtualView(t, structs, "Mooring")
	stringTyped(declared)
	declared.RowsOf = "Berths"
	undeclared := fixtureVirtualView(t, structs, "Renamed")
	stringTyped(undeclared)
	computed := fixtureComputedView(t, structs, "Occupancy")
	for _, f := range computed.Fields {
		f.typescriptType = "string"
	}
	computed.RowsOf = "Berths"

	tests := []struct {
		name            string
		data            tsResourcesData
		wantContains    []string
		wantNotContains []string
	}{
		{
			name:         "a declared virtual view names its table",
			data:         tsResourcesData{Resources: []*resourceInfo{declared}, GenPrefix: "zz_gen"},
			wantContains: []string{"[Resources.Moorings]: {\n    route: 'moorings',\n    rowsOf: Resources.Berths,\n"},
		},
		{
			name:            "an undeclared view carries no rowsOf",
			data:            tsResourcesData{Resources: []*resourceInfo{undeclared}, GenPrefix: "zz_gen"},
			wantNotContains: []string{"rowsOf"},
		},
		{
			name:         "a declared computed view names its table",
			data:         tsResourcesData{ComputedResources: []*computedResource{computed}, GenPrefix: "zz_gen"},
			wantContains: []string{"[Resources.Occupancies]: {\n    route: 'occupancies',\n    rowsOf: Resources.Berths,\n"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := &client{}
			tt.data.File = &typescriptGenerator{client: c}
			out, err := c.generateTemplateOutput(typescriptResourcesTemplate, typescriptResourcesTemplate, tt.data)
			if err != nil {
				t.Fatalf("generateTemplateOutput() error = %v", err)
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(string(out), want) {
					t.Errorf("output lacks %q\n%s", want, out)
				}
			}
			for _, unwanted := range tt.wantNotContains {
				if strings.Contains(string(out), unwanted) {
					t.Errorf("output must not contain %q\n%s", unwanted, out)
				}
			}
		})
	}
}
