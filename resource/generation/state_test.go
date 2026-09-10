package generation

import (
	"os"
	"path/filepath"
	"slices"
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

// buildWorkflowFixture assembles one struct's resourceInfo the way
// structsToResources does, through state-annotation resolution.
func buildWorkflowFixture(t *testing.T, c *client, structs map[string]*parser.Struct, name string) *resourceInfo {
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
	if err := resolveResourceAnnotations(res, annotations); err != nil {
		t.Fatalf("resolveResourceAnnotations(%s) error = %v", name, err)
	}
	if err := c.resolveStateAnnotations(res, s, annotations); err != nil {
		t.Fatalf("resolveStateAnnotations(%s) error = %v", name, err)
	}

	return res
}

// TestResolveWorkflows pins the §09 membership machinery: the root and every
// member gain the uniform synthesized state binding — a column binding on the
// root, a join path composed through each member's immediate hop — and the
// chain, root, and scope validations reject what the design rejects.
func TestResolveWorkflows(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "bindingfixture"))

	t.Run("chains compose and the uniform binding synthesizes", func(t *testing.T) {
		t.Parallel()

		c := stateFixtureClient()
		root := buildWorkflowFixture(t, c, structs, "StatefulTask")
		part := buildWorkflowFixture(t, c, structs, "TaskPart")
		order := buildWorkflowFixture(t, c, structs, "PartOrder")

		// Members listed before parents: resolution iterates until chains reach the root.
		if err := c.resolveWorkflows([]*resourceInfo{order, part, root}); err != nil {
			t.Fatalf("resolveWorkflows() error = %v", err)
		}

		stateBinding := func(res *resourceInfo) *attributeBinding {
			for _, attr := range res.Attributes {
				if attr.Name == "state" {
					return attr
				}
			}
			t.Fatalf("%s carries no synthesized state binding", res.Name())

			return nil
		}

		rootState := stateBinding(root)
		if rootState.Anchor.Name() != "State" || len(rootState.Path) != 0 {
			t.Errorf("root state binding = {Anchor:%s Path:%v}, want the column binding on State", rootState.Anchor.Name(), rootState.Path)
		}

		partState := stateBinding(part)
		wantPartPath := []bindingHop{{Table: "StatefulTasks", JoinColumn: "Id", Column: "State"}}
		if partState.Anchor.Name() != "TaskID" || !slices.Equal(partState.Path, wantPartPath) {
			t.Errorf("member state binding = {Anchor:%s Path:%v}, want anchored on TaskID with path %v", partState.Anchor.Name(), partState.Path, wantPartPath)
		}

		orderState := stateBinding(order)
		wantOrderPath := []bindingHop{
			{Table: "TaskParts", JoinColumn: "Id", Column: "TaskId"},
			{Table: "StatefulTasks", JoinColumn: "Id", Column: "State"},
		}
		if orderState.Anchor.Name() != "PartID" || !slices.Equal(orderState.Path, wantOrderPath) {
			t.Errorf("two-hop state binding = {Anchor:%s Path:%v}, want anchored on PartID with path %v", orderState.Anchor.Name(), orderState.Path, wantOrderPath)
		}
	})

	rejections := []struct {
		name        string
		members     []string
		wantContain string
	}{
		{
			name:        "unknown root",
			members:     []string{"StatefulTask", "UnknownRootMember"},
			wantContain: "unknown resource struct",
		},
		{
			name:        "hop onto a non-member never reaches the root",
			members:     []string{"StatefulTask", "Ship", "ChainBreakMember"},
			wantContain: "never reach their roots",
		},
		{
			name:        "hop onto a table no resource backs",
			members:     []string{"StatefulTask", "ChainBreakMember"},
			wantContain: "no resource struct backs",
		},
		{
			name:        "scope mismatch",
			members:     []string{"StatefulTask", "ScopedMember"},
			wantContain: "permission scopes differ",
		},
	}
	for _, tt := range rejections {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := stateFixtureClient()
			resources := make([]*resourceInfo, 0, len(tt.members))
			for _, name := range tt.members {
				resources = append(resources, buildWorkflowFixture(t, c, structs, name))
			}
			err := c.resolveWorkflows(resources)
			if err == nil {
				t.Fatalf("resolveWorkflows() expected an error containing %q, got nil", tt.wantContain)
			}
			if !strings.Contains(err.Error(), tt.wantContain) {
				t.Errorf("resolveWorkflows() error = %q, want containing %q", err, tt.wantContain)
			}
		})
	}
}

// workflowGraphFixture assembles the fixture workflow the way Generate does:
// the two-hop chain (StatefulTask ← TaskPart ← PartOrder), the Ship resource
// backing the context FKs, and optionally the two declared transitions.
func workflowGraphFixture(t *testing.T, structs map[string]*parser.Struct, withTransitions bool) (r *resourceGenerator, dir string) {
	t.Helper()

	c := stateFixtureClient()
	root := buildWorkflowFixture(t, c, structs, "StatefulTask")
	part := buildWorkflowFixture(t, c, structs, "TaskPart")
	order := buildWorkflowFixture(t, c, structs, "PartOrder")
	ship := buildWorkflowFixture(t, c, structs, "Ship")
	if err := c.resolveWorkflows([]*resourceInfo{root, part, order}); err != nil {
		t.Fatalf("resolveWorkflows() error = %v", err)
	}

	dir = t.TempDir()
	r = &resourceGenerator{client: c}
	r.resources = []*resourceInfo{root, part, order, ship}
	r.resource = packageDir(dir)

	if withTransitions {
		for _, name := range []string{"CloseTask", "ApproveTask"} {
			rpcMethod, err := resolveFixtureTransition(t, c, structs, name)
			if err != nil {
				t.Fatalf("resolveFixtureTransition(%s) error = %v", name, err)
			}
			r.rpcMethods = append(r.rpcMethods, rpcMethod)
		}
	}

	return r, dir
}

// TestGenerateWorkflowGraphs pins the DOT review surface: the full tree — root
// and members as solid nodes with hop-labeled edges, context references dashed
// and deduplicated, the state cluster with the default marked and one labeled
// edge per declared transition, the legend — and byte-stable output.
func TestGenerateWorkflowGraphs(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "bindingfixture"))

	generate := func(t *testing.T, withTransitions bool) string {
		t.Helper()

		r, dir := workflowGraphFixture(t, structs, withTransitions)
		if err := r.generateWorkflowGraphs(); err != nil {
			t.Fatalf("generateWorkflowGraphs() error = %v", err)
		}

		content, err := os.ReadFile(filepath.Join(dir, "zz_gen_workflow_stateful_task.dot"))
		if err != nil {
			t.Fatalf("reading workflow graph: %v", err)
		}

		return string(content)
	}

	t.Run("the full tree: members hop by hop, context dashed, states and legend", func(t *testing.T) {
		t.Parallel()

		content := generate(t, false)
		for _, want := range []string{
			"digraph StatefulTaskWorkflow {",
			`"StatefulTask" [shape=doubleoctagon];`,
			// Multi-hop chains draw hop by hop, labeled with the anchoring FK field.
			`"TaskPart" -> "StatefulTask" [label="taskId"];`,
			`"PartOrder" -> "TaskPart" [label="partId"];`,
			// Context references are dashed and labeled with the referencing field.
			`"Ship" [style=dashed];`,
			`"StatefulTask" -> "Ship" [style=dashed, label="shipId"];`,
			`"TaskPart" -> "Ship" [style=dashed, label="shipId"];`,
			// The state cluster holds every value with the default marked, and the
			// root attaches through its state field.
			`label="states (default: open)";`,
			`"state:open" [label="open", peripheries=2];`,
			`"state:approved" [label="approved"];`,
			`"state:closed" [label="closed"];`,
			`"StatefulTask" -> "state:open" [lhead="cluster_states", style=dotted, label="state"];`,
			`subgraph cluster_legend {`,
		} {
			if !strings.Contains(content, want) {
				t.Errorf("workflow graph missing %q:\n%s", want, content)
			}
		}
		// Two member FKs to one target share one dashed node.
		if got := strings.Count(content, `"Ship" [style=dashed];`); got != 1 {
			t.Errorf("context node declared %d times, want exactly once:\n%s", got, content)
		}
		// The state enum table FK is the cluster attachment, never a context edge.
		if strings.Contains(content, "TaskStates") {
			t.Errorf("the state enum table must not appear as a context resource:\n%s", content)
		}
	})

	t.Run("declared transitions draw labeled edges inside the state cluster", func(t *testing.T) {
		t.Parallel()

		content := generate(t, true)
		for _, want := range []string{
			`"state:open" -> "state:approved" [label="ApproveTask"];`,
			`"state:open" -> "state:closed" [label="CloseTask"];`,
			`"state:approved" -> "state:closed" [label="CloseTask"];`,
		} {
			if !strings.Contains(content, want) {
				t.Errorf("workflow graph missing %q:\n%s", want, content)
			}
		}
	})

	t.Run("output is byte-stable across runs", func(t *testing.T) {
		t.Parallel()

		if first, second := generate(t, true), generate(t, true); first != second {
			t.Errorf("two runs rendered different output:\n--- first\n%s\n--- second\n%s", first, second)
		}
	})
}

// TestTypescriptResourcesTemplate_workflows pins the TypeScript half of the
// workflow facts: the Workflows constant carries root, members with hops,
// states with the default, and declared transitions — and renders nothing at
// all for applications without workflows.
func TestTypescriptResourcesTemplate_workflows(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "bindingfixture"))
	r, _ := workflowGraphFixture(t, structs, true)

	out, err := r.generateTemplateOutput(typescriptResourcesTemplate, typescriptResourcesTemplate, tsResourcesData{
		File:      &typescriptGenerator{client: r.client},
		GenPrefix: genPrefix,
		Workflows: r.assembleWorkflows(),
	})
	if err != nil {
		t.Fatalf("generateTemplateOutput() error = %v", err)
	}

	for _, want := range []string{
		"export const Workflows: Workflow[] = [",
		"root: Resources.StatefulTasks,",
		"states: ['open', 'approved', 'closed'],",
		"defaultState: 'open',",
		"{ resource: Resources.PartOrders, parent: Resources.TaskParts, field: 'partId' },",
		"{ resource: Resources.TaskParts, parent: Resources.StatefulTasks, field: 'taskId' },",
		"{ method: 'ApproveTask', from: ['open'], to: 'approved' },",
		"{ method: 'CloseTask', from: ['open', 'approved'], to: 'closed' },",
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("typescriptResourcesTemplate output missing %q:\n%s", want, out)
		}
	}

	empty, err := r.generateTemplateOutput(typescriptResourcesTemplate, typescriptResourcesTemplate, tsResourcesData{
		File:      &typescriptGenerator{client: r.client},
		GenPrefix: genPrefix,
	})
	if err != nil {
		t.Fatalf("generateTemplateOutput() error = %v", err)
	}
	if strings.Contains(string(empty), "Workflow") {
		t.Errorf("an application without workflows must emit no workflow block:\n%s", empty)
	}
}
