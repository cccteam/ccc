package generation

import (
	"strings"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/cccteam/ccc/resource/generation/parser/genlang"
)

// stateFixtureClient extends the binding fixture schema with the state enum
// table's values, loaded from the schema at generation time.
func stateFixtureClient() *client {
	return &client{
		tableMap: bindingFixtureTables(),
		enumValues: map[string][]*enumData{
			"TaskStates": {
				{ID: "open", Description: "Open"},
				{ID: "approved", Description: "Approved"},
				{ID: "closed", Description: "Closed"},
			},
		},
	}
}

// resolveFixtureState runs the @state resolution on one fixture struct the
// way structsToResources does.
func resolveFixtureState(t *testing.T, c *client, structs map[string]*parser.Struct, name string) (*resourceInfo, error) {
	t.Helper()

	s := structs[name]
	if s == nil {
		t.Fatalf("struct %q not found in fixture package", name)
	}
	annotations, err := genlang.NewScanner(resourceKeywords()).ScanStruct(s)
	if err != nil {
		t.Fatalf("ScanStruct(%s) error = %v", name, err)
	}
	table, err := c.tableMetadataFor(name)
	if err != nil {
		t.Fatalf("tableMetadataFor(%s) error = %v", name, err)
	}
	res := &resourceInfo{TypeInfo: s.TypeInfo, PkCount: table.PkCount}
	res.Fields, err = newResourceFields(res, s, table)
	if err != nil {
		t.Fatalf("newResourceFields(%s) error = %v", name, err)
	}

	return res, c.resolveStateAnnotations(res, s, annotations)
}

// TestResolveStateAnnotations pins the marker's derivations: the FK
// identifies the state enum table, the declared default must be one of its
// values, the marked field decodes output-only (create and update closed —
// the patch request struct hides it from the wire, which also keeps
// Create/Update ungrantable), and stating the derived behavior again through
// tags is an error.
func TestResolveStateAnnotations(t *testing.T) {
	t.Parallel()

	c := stateFixtureClient()
	structs := fixtureStructs(loadFixture(t, "bindingfixture"))

	t.Run("marker derives output-only decode and the default", func(t *testing.T) {
		t.Parallel()

		res, err := resolveFixtureState(t, c, structs, "StatefulTask")
		if err != nil {
			t.Fatalf("resolveStateAnnotations() error = %v", err)
		}

		var state *resourceField
		for _, f := range res.Fields {
			if f.Name() == "State" {
				state = f
			}
		}
		if state == nil {
			t.Fatal("State field not found")
		}
		if !state.IsState || state.StateDefault != "open" {
			t.Errorf("State field = {IsState:%v StateDefault:%q}, want marked with default open", state.IsState, state.StateDefault)
		}
		if !state.IsOutputOnly() {
			t.Error("IsOutputOnly() = false for a state field; the wire must not express a state write")
		}
		if got, want := state.JSONTagForPatch(), `json:"-"`; got != want {
			t.Errorf("JSONTagForPatch() = %q, want %q — create and update must be closed", got, want)
		}
		if got, want := state.JSONTag(), `json:"state"`; got != want {
			t.Errorf("JSONTag() = %q, want %q — reads stay open", got, want)
		}
	})

	rejections := []struct {
		name        string
		structName  string
		wantContain string
	}{
		{
			name:        "marker requires a foreign key to the enum table",
			structName:  "StateOnNonFK",
			wantContain: "not a foreign key",
		},
		{
			name:        "default outside the enum values",
			structName:  "StateBadDefault",
			wantContain: "not a value of the state enum table",
		},
		{
			name:        "derived behavior stated twice",
			structName:  "StateStatedTwice",
			wantContain: "never stated twice",
		},
	}
	for _, tt := range rejections {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := resolveFixtureState(t, c, structs, tt.structName)
			if err == nil {
				t.Fatalf("resolveStateAnnotations(%s) expected an error containing %q, got nil", tt.structName, tt.wantContain)
			}
			if !strings.Contains(err.Error(), tt.wantContain) {
				t.Errorf("resolveStateAnnotations(%s) error = %q, want containing %q", tt.structName, err, tt.wantContain)
			}
		})
	}
}

// TestResourceFileTemplate_stateDefault pins the generated initial state: the
// create patch registers a default returning the declared value in the state
// column's own type, so the insert path applies it — never a database DEFAULT.
func TestResourceFileTemplate_stateDefault(t *testing.T) {
	t.Parallel()

	c := stateFixtureClient()
	structs := fixtureStructs(loadFixture(t, "bindingfixture"))
	res, err := resolveFixtureState(t, c, structs, "StatefulTask")
	if err != nil {
		t.Fatalf("resolveStateAnnotations() error = %v", err)
	}

	r := &resourceGenerator{client: c}
	output, err := r.generateTemplateOutput("resourceFileTemplate", resourceFileTemplate, &resourceFileData{
		Source:   "fixture",
		Package:  "resources",
		Resource: res,
	})
	if err != nil {
		t.Fatalf("generateTemplateOutput() error = %v", err)
	}

	want := `p.patchSet.RegisterDefaultCreateFunc("State", func(context.Context, resource.ReadWriteTransaction) (any, error) {`
	if !strings.Contains(string(output), want) {
		t.Errorf("rendered resource file missing the state default registration %q:\n%s", want, output)
	}
	if !strings.Contains(string(output), `return TaskState("open"), nil`) {
		t.Errorf("rendered resource file missing the typed default value:\n%s", output)
	}
}

// TestValidateStateEnumTables pins the enum-table rule: state values change
// only by migration, so a mutation permission against the state enum table
// fails generation while Read stays grantable.
func TestValidateStateEnumTables(t *testing.T) {
	t.Parallel()

	c := stateFixtureClient()
	structs := fixtureStructs(loadFixture(t, "bindingfixture"))
	res, err := resolveFixtureState(t, c, structs, "StatefulTask")
	if err != nil {
		t.Fatalf("resolveStateAnnotations() error = %v", err)
	}
	r := &resourceGenerator{client: c}
	r.resources = []*resourceInfo{res}

	tests := []struct {
		name        string
		data        resource.CollectionData
		wantContain string
	}{
		{
			name: "read on the state enum table is grantable",
			data: resource.CollectionData{Resources: []resource.CollectionResource{
				{Name: "TaskStates", Scope: accesstypes.GlobalPermissionScope, Permissions: []accesstypes.Permission{accesstypes.List, accesstypes.Read}},
			}},
		},
		{
			name: "mutation on the state enum table fails generation",
			data: resource.CollectionData{Resources: []resource.CollectionResource{
				{Name: "TaskStates", Scope: accesstypes.GlobalPermissionScope, Permissions: []accesstypes.Permission{accesstypes.Read, accesstypes.Update}},
			}},
			wantContain: "state values change only by migration",
		},
		{
			name: "other tables are untouched",
			data: resource.CollectionData{Resources: []resource.CollectionResource{
				{Name: "StatefulTasks", Scope: accesstypes.GlobalPermissionScope, Permissions: []accesstypes.Permission{accesstypes.Create, accesstypes.Update}},
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := r.validateStateEnumTables(tt.data)
			if tt.wantContain == "" {
				if err != nil {
					t.Fatalf("validateStateEnumTables() error = %v, want nil", err)
				}

				return
			}
			if err == nil {
				t.Fatalf("validateStateEnumTables() expected an error containing %q, got nil", tt.wantContain)
			}
			if !strings.Contains(err.Error(), tt.wantContain) {
				t.Errorf("validateStateEnumTables() error = %q, want containing %q", err, tt.wantContain)
			}
		})
	}
}
