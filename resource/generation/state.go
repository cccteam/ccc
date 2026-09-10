package generation

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/cccteam/ccc/resource/generation/parser/genlang"
	"github.com/ettle/strcase"
	"github.com/go-playground/errors/v5"
)

// The structural state marker (ABAC design plan §09): a stateful resource's
// state column carries `@state(default: <value>)`. State values are ordinary
// enum tables — the existing Id/Description convention, identified from the
// marked field's FK target, nothing declared on the table — and the marker
// derives the field's runtime behavior instead of the author stating it
// twice: the state field decodes output-only (create and update closed, so
// the wire cannot express a state write; transitions happen inside RPC
// bodies), its Create/Update are ungrantable (the field never registers as a
// mutable tag), reads stay grantable, and the initial state is the declared
// default, applied on the insert path. State values change only by migration:
// no mutation permission may exist against the state enum table itself.

// resolveStateAnnotations extracts and validates a resource's @state markers,
// marking the annotated fields.
func (c *client) resolveStateAnnotations(res *resourceInfo, pStruct *parser.Struct, annotations genlang.StructAnnotations) error {
	fieldByName := make(map[string]*resourceField, len(res.Fields))
	for _, f := range res.Fields {
		fieldByName[f.Name()] = f
	}

	for i, pField := range pStruct.Fields() {
		hasState := annotations.Fields[i].Has(stateKeyword)
		hasStateRoot := annotations.Fields[i].Has(stateRootKeyword)
		if !hasState && !hasStateRoot {
			continue
		}

		field, ok := fieldByName[pField.Name()]
		if !ok {
			return errors.Newf("struct %s field %s: state annotations require a schema-backed field", pStruct.Name(), pField.Name())
		}
		if hasState {
			if err := c.resolveState(field, annotations.Fields[i].Get(stateKeyword)); err != nil {
				return errors.Wrapf(err, "struct %s field %s", pStruct.Name(), pField.Name())
			}
		}
		if hasStateRoot {
			if err := resolveWorkflowMembership(field, annotations.Fields[i].Get(stateRootKeyword)); err != nil {
				return errors.Wrapf(err, "struct %s field %s", pStruct.Name(), pField.Name())
			}
		}
	}

	return nil
}

// stateDefaultArgKey is the @state annotation's one named argument.
const stateDefaultArgKey = "default"

func (c *client) resolveState(field *resourceField, arg genlang.Arg) error {
	invocations, err := arg.ParseInvocations(&genlang.ArgSpec{Keys: []string{stateDefaultArgKey}, Required: []string{stateDefaultArgKey}})
	if err != nil {
		return errors.Wrapf(err, "@%s", stateKeyword)
	}
	defaultValue, _ := invocations[0].Named(stateDefaultArgKey)

	// The FK identifies the state enum table; the closed value set is that
	// table's rows, loaded from the schema.
	if !field.IsForeignKey || field.ReferencedResource == "" {
		return errors.Newf("@%s requires a foreign key to the state enum table; %s is not a foreign key", stateKeyword, field.Name())
	}
	values, ok := c.enumValues[field.ReferencedResource]
	if !ok {
		return errors.Newf("@%s: %s references table %q, which is not an enum table (no Description column)", stateKeyword, field.Name(), field.ReferencedResource)
	}
	if !slices.ContainsFunc(values, func(v *enumData) bool { return v.ID == defaultValue }) {
		return errors.Newf("@%s(default: %s): %q is not a value of the state enum table %q", stateKeyword, defaultValue, defaultValue, field.ReferencedResource)
	}

	// The marker derives the field's decode and default behavior — stating it
	// again through tags would say one thing in two places.
	if field.HasTag(conditionsTagKey) {
		return errors.Newf("@%s derives the field's decode behavior; remove the conditions tag — behavior is never stated twice", stateKeyword)
	}
	if field.HasDefaultCreateFunc() {
		return errors.Newf("@%s declares the initial state; remove the %s tag — behavior is never stated twice", stateKeyword, defaultCreateFnTagKey)
	}

	field.IsState = true
	field.StateDefault = defaultValue

	return nil
}

// validateStateEnumTables enforces the one extra rule state enum tables
// carry: values change only by migration, so no mutation permission may be
// defined against the table — generated, consolidated, or manual. Read stays
// grantable (the values are ordinary reference data).
func (r *resourceGenerator) validateStateEnumTables(data resource.CollectionData) error {
	stateTables := make(map[string]string) // enum table -> the resource whose state field references it
	for _, res := range r.resources {
		for _, field := range res.Fields {
			if field.IsState {
				stateTables[field.ReferencedResource] = res.Name()
			}
		}
	}
	if len(stateTables) == 0 {
		return nil
	}

	mutations := []accesstypes.Permission{accesstypes.Create, accesstypes.Update, accesstypes.Delete}
	for i := range data.Resources {
		res := &data.Resources[i]
		declaredBy, isStateTable := stateTables[string(res.Name)]
		if !isStateTable {
			continue
		}
		for _, perm := range res.Permissions {
			if slices.Contains(mutations, perm) {
				return errors.Newf("resource %q is the state enum table of %s's @%s field: state values change only by migration, so %s cannot be granted against it — suppress the mutation handlers", res.Name, declaredBy, stateKeyword, perm)
			}
		}
	}

	return nil
}

// Workflow membership (@stateRoot): declared per member struct on the FK
// field anchoring it — the field IS the hop, so only the workflow root is
// spelled. Each member declares its immediate hop; chains compose, every hop
// is many-to-one through a real foreign key, and the generator synthesizes
// the uniform `state` binding on the root and every member, so one condition
// text (state = 'open') reads identically across the workflow. The assembled
// graph is emitted as a committed, drift-tested DOT file per workflow: the
// whole tree — members hop by hop, context references, the state set, and
// the declared transitions (@transition). Undeclared state changes stay
// RPC-body business rules the framework cannot see.

// stateBindingName is the synthesized attribute every stateful resource and
// workflow member carries.
const (
	// stateBindingName mirrors resource.StateAttribute: the runtime's
	// capability envelope lowers its transition-membership boolean against
	// the same name this generator synthesizes.
	stateBindingName = resource.StateAttribute

	// stateBindingType is the synthesized binding's comparison type: state
	// values are the enum table's string descriptions.
	stateBindingType = string(resource.AttributeTypeString)
)

// resolveWorkflowMembership captures a field's @stateRoot declaration; chain
// resolution and synthesis happen in resolveWorkflows once every resource is
// extracted.
func resolveWorkflowMembership(field *resourceField, arg genlang.Arg) error {
	invocations, err := arg.ParseInvocations(&genlang.ArgSpec{Positional: 1})
	if err != nil {
		return errors.Wrapf(err, "@%s", stateRootKeyword)
	}
	if field.IsState {
		return errors.Newf("@%s cannot sit on the state column itself; it anchors a member's hop toward the root", stateRootKeyword)
	}
	if !field.IsForeignKey || field.ReferencedResource == "" {
		return errors.Newf("@%s anchors on the foreign key the hop leaves through; %s is not a foreign key", stateRootKeyword, field.Name())
	}
	field.WorkflowRoot = invocations[0].Positional[0]

	return nil
}

// resolveWorkflows validates every workflow chain and synthesizes the uniform
// state bindings: the root's column binding on its @state field, and each
// member's join-path binding composed through its immediate hop.
func (c *client) resolveWorkflows(resources []*resourceInfo) error {
	byStruct := make(map[string]*resourceInfo, len(resources))
	byTable := make(map[string]*resourceInfo, len(resources))
	for _, res := range resources {
		byStruct[res.Name()] = res
		byTable[c.pluralize(res.Name())] = res
	}

	// Every stateful resource carries the state binding, members or not: its
	// own conditions reference it.
	for _, res := range resources {
		if state := stateField(res); state != nil {
			if err := synthesizeStateBinding(res, &attributeBinding{Name: stateBindingName, Anchor: state, Type: stateBindingType}); err != nil {
				return err
			}
		}
	}

	// Members resolve iteratively: a member's chain composes onto its
	// parent's, so parents resolve first; no progress with members left means
	// a chain never reaches its root.
	var pending []workflowMembership
	for _, res := range resources {
		var member *resourceField
		for _, field := range res.Fields {
			if field.WorkflowRoot == "" {
				continue
			}
			if member != nil {
				return errors.Newf("struct %s declares @%s twice (fields %s and %s): the synthesized %q binding would be ambiguous — a resource belongs to one workflow", res.Name(), stateRootKeyword, member.Name(), field.Name(), stateBindingName)
			}
			member = field
		}
		if member == nil {
			continue
		}

		root, ok := byStruct[member.WorkflowRoot]
		if !ok {
			return errors.Newf("struct %s field %s: @%s(%s) names an unknown resource struct", res.Name(), member.Name(), stateRootKeyword, member.WorkflowRoot)
		}
		if stateField(root) == nil {
			return errors.Newf("struct %s field %s: @%s(%s): the root carries no @%s field", res.Name(), member.Name(), stateRootKeyword, member.WorkflowRoot, stateKeyword)
		}
		if scopeOrGlobal(res.PermissionScope) != scopeOrGlobal(root.PermissionScope) {
			return errors.Newf("struct %s field %s: @%s(%s): member and root permission scopes differ — a workflow never crosses the tenancy structure", res.Name(), member.Name(), stateRootKeyword, member.WorkflowRoot)
		}
		pending = append(pending, workflowMembership{res: res, field: member})
	}

	resolved := make(map[string]*attributeBinding) // struct name -> its state binding
	for _, res := range resources {
		if state := stateField(res); state != nil {
			resolved[res.Name()] = &attributeBinding{Name: stateBindingName, Anchor: state, Type: stateBindingType}
		}
	}

	return resolveWorkflowChains(pending, resolved, byTable)
}

// workflowMembership pairs a member resource with its anchoring FK field.
type workflowMembership struct {
	res   *resourceInfo
	field *resourceField
}

// resolveWorkflowChains iterates the pending memberships until every chain
// composes onto its parent's resolved binding; no progress with members left
// means a chain never reaches its root.
func resolveWorkflowChains(pending []workflowMembership, resolved map[string]*attributeBinding, byTable map[string]*resourceInfo) error {
	for len(pending) > 0 {
		progressed := false
		remaining := pending[:0]
		for _, m := range pending {
			parent, ok := byTable[m.field.ReferencedResource]
			if !ok {
				return errors.Newf("struct %s field %s: @%s references table %q, which no resource struct backs", m.res.Name(), m.field.Name(), stateRootKeyword, m.field.ReferencedResource)
			}
			parentBinding, parentResolved := resolved[parent.Name()]
			if !parentResolved {
				remaining = append(remaining, m)

				continue
			}
			// The hop must land on the root itself or on a member of the
			// same workflow, or the chains disagree about the root.
			if parent.Name() != m.field.WorkflowRoot && parentWorkflowRoot(parent) != m.field.WorkflowRoot {
				return errors.Newf("struct %s field %s: @%s(%s) hops onto %s, which belongs to a different workflow", m.res.Name(), m.field.Name(), stateRootKeyword, m.field.WorkflowRoot, parent.Name())
			}

			binding := &attributeBinding{
				Name:   stateBindingName,
				Anchor: m.field,
				Type:   stateBindingType,
				Path: append([]bindingHop{{
					Table:      m.field.ReferencedResource,
					JoinColumn: m.field.ReferencedField,
					Column:     fieldColumn(parentBinding.Anchor),
				}}, parentBinding.Path...),
			}
			if err := synthesizeStateBinding(m.res, binding); err != nil {
				return err
			}
			resolved[m.res.Name()] = binding
			progressed = true
		}
		pending = remaining
		if !progressed && len(pending) > 0 {
			names := make([]string, 0, len(pending))
			for _, m := range pending {
				names = append(names, m.res.Name())
			}
			slices.Sort(names)

			return errors.Newf("workflow chains never reach their roots (a cycle, or a hop onto a non-member): %v", names)
		}
	}

	return nil
}

// synthesizeStateBinding appends the state binding, rejecting a user-declared
// name collision: the marker owns the name.
func synthesizeStateBinding(res *resourceInfo, binding *attributeBinding) error {
	for _, attr := range res.Attributes {
		if attr.Name == stateBindingName {
			return errors.Newf("struct %s: %q is synthesized by the @%s/@%s markers and cannot be declared with @%s", res.Name(), stateBindingName, stateKeyword, stateRootKeyword, attributeKeyword)
		}
	}
	for _, subject := range slices.Concat(res.SubjectSets, res.SubjectValues) {
		if subject.Name == stateBindingName {
			return errors.Newf("struct %s: %q is synthesized by the @%s/@%s markers and cannot be declared as subject vocabulary", res.Name(), stateBindingName, stateKeyword, stateRootKeyword)
		}
	}
	res.Attributes = append(res.Attributes, binding)

	return nil
}

// stateField returns the resource's @state field, or nil.
func stateField(res *resourceInfo) *resourceField {
	for _, field := range res.Fields {
		if field.IsState {
			return field
		}
	}

	return nil
}

// parentWorkflowRoot returns the workflow root a resource's membership names,
// or empty for a non-member.
func parentWorkflowRoot(res *resourceInfo) string {
	for _, field := range res.Fields {
		if field.WorkflowRoot != "" {
			return field.WorkflowRoot
		}
	}

	return ""
}

// workflowGraph is one assembled workflow: the stateful root, its members in
// name order with their anchoring hops, the context resources workflow rows
// reference, the closed state set with its default, and the declared
// transitions. Both emitters render from it — the DOT writer draws the whole
// tree, and the TypeScript resource metadata carries the same facts (minus
// context) so a frontend can render the graph itself.
type workflowGraph struct {
	Root       *resourceInfo
	StateField *resourceField
	States     []*enumData
	Default    string
	Members    []*workflowGraphMember
	// Contexts are the FK references leaving the tree — one edge per field of
	// the root or a member whose target is neither the root, a member, nor the
	// state enum table (the tenant record's FK included).
	Contexts    []*workflowContextEdge
	Transitions []*rpcMethodInfo
}

// workflowGraphMember is one member's hop: the member resource, the struct
// name the hop lands on (the root, or another member of the chain), and the
// anchoring FK field.
type workflowGraphMember struct {
	Res    *resourceInfo
	Parent string
	Field  *resourceField
}

// workflowContextEdge is one FK reference leaving the workflow tree.
type workflowContextEdge struct {
	Source string
	Target string
	Field  *resourceField
}

// assembleWorkflows builds one graph per stateful resource from the parsed
// sets the caller holds — the resource generator's full sets, or a TypeScript
// target's outlet-filtered ones — sorted throughout so rendered output is
// byte-stable across runs.
func (c *client) assembleWorkflows() []*workflowGraph {
	byTable := make(map[string]*resourceInfo, len(c.resources))
	for _, res := range c.resources {
		byTable[c.pluralize(res.Name())] = res
	}
	// structName resolves a referenced table to the struct that backs it,
	// falling back to the table name for tables outside the parsed set.
	structName := func(table string) string {
		if res, ok := byTable[table]; ok {
			return res.Name()
		}

		return table
	}

	var graphs []*workflowGraph
	for _, root := range c.resources {
		state := stateField(root)
		if state == nil {
			continue
		}

		wf := &workflowGraph{
			Root:       root,
			StateField: state,
			States:     c.enumValues[state.ReferencedResource],
			Default:    state.StateDefault,
		}

		treeNames := map[string]bool{root.Name(): true}
		for _, res := range c.resources {
			if parentWorkflowRoot(res) != root.Name() {
				continue
			}
			for _, field := range res.Fields {
				if field.WorkflowRoot != "" {
					wf.Members = append(wf.Members, &workflowGraphMember{Res: res, Parent: structName(field.ReferencedResource), Field: field})
					treeNames[res.Name()] = true
				}
			}
		}
		slices.SortFunc(wf.Members, func(a, b *workflowGraphMember) int { return strings.Compare(a.Res.Name(), b.Res.Name()) })

		sources := append([]*resourceInfo{root}, membersOf(wf)...)
		for _, src := range sources {
			for _, field := range src.Fields {
				if !field.IsForeignKey || field.ReferencedResource == "" || field.IsState || field.WorkflowRoot != "" {
					continue
				}
				target := structName(field.ReferencedResource)
				if treeNames[target] {
					continue
				}
				wf.Contexts = append(wf.Contexts, &workflowContextEdge{Source: src.Name(), Target: target, Field: field})
			}
		}
		slices.SortFunc(wf.Contexts, func(a, b *workflowContextEdge) int {
			if cmp := strings.Compare(a.Source, b.Source); cmp != 0 {
				return cmp
			}

			return strings.Compare(a.Field.Name(), b.Field.Name())
		})

		for _, method := range c.rpcMethods {
			if method.Transition != nil && method.Transition.RootStruct == root.Name() {
				wf.Transitions = append(wf.Transitions, method)
			}
		}
		slices.SortFunc(wf.Transitions, func(a, b *rpcMethodInfo) int { return strings.Compare(a.Name(), b.Name()) })

		graphs = append(graphs, wf)
	}
	slices.SortFunc(graphs, func(a, b *workflowGraph) int { return strings.Compare(a.Root.Name(), b.Root.Name()) })

	return graphs
}

// membersOf returns the member resources in the graph's sorted order.
func membersOf(wf *workflowGraph) []*resourceInfo {
	members := make([]*resourceInfo, 0, len(wf.Members))
	for _, m := range wf.Members {
		members = append(members, m.Res)
	}

	return members
}

// renderWorkflowDOT draws one workflow's full tree: solid boxes for the root
// (doubleoctagon) and its members with one labeled edge per hop, dashed boxes
// and edges for context references, the state cluster with the default marked
// and one labeled edge per declared transition, and a legend. Facts and labels
// only — layout stays Graphviz's, and every list is pre-sorted, so the
// committed file is byte-stable across runs.
func renderWorkflowDOT(wf *workflowGraph) string {
	root := wf.Root.Name()

	var b strings.Builder
	fmt.Fprintf(&b, "// Code generated by resourcegeneration. DO NOT EDIT.\n")
	fmt.Fprintf(&b, "// Workflow %s: the full tree. Solid boxes are the root and its members, one\n", root)
	fmt.Fprintf(&b, "// edge per hop labeled with the anchoring foreign-key field; dashed boxes are\n")
	fmt.Fprintf(&b, "// the context resources workflow rows reference, the tenant record included;\n")
	fmt.Fprintf(&b, "// the state cluster holds every state value (default marked) and one labeled\n")
	fmt.Fprintf(&b, "// edge per declared @transition. Facts only: who may run a method or hold a\n")
	fmt.Fprintf(&b, "// grant is policy (RoleConfig), never drawn here.\n")
	fmt.Fprintf(&b, "digraph %sWorkflow {\n", root)
	fmt.Fprintf(&b, "\trankdir=RL;\n")
	fmt.Fprintf(&b, "\tcompound=true;\n")
	fmt.Fprintf(&b, "\tnode [shape=box];\n")
	fmt.Fprintf(&b, "\t%q [shape=doubleoctagon];\n", root)
	for _, m := range wf.Members {
		fmt.Fprintf(&b, "\t%q;\n", m.Res.Name())
	}
	contextTargets := make([]string, 0, len(wf.Contexts))
	for _, ctx := range wf.Contexts {
		contextTargets = append(contextTargets, ctx.Target)
	}
	slices.Sort(contextTargets)
	for _, target := range slices.Compact(contextTargets) {
		fmt.Fprintf(&b, "\t%q [style=dashed];\n", target)
	}
	for _, m := range wf.Members {
		fmt.Fprintf(&b, "\t%q -> %q [label=%q];\n", m.Res.Name(), m.Parent, strcase.ToCamel(m.Field.Name()))
	}
	for _, ctx := range wf.Contexts {
		fmt.Fprintf(&b, "\t%q -> %q [style=dashed, label=%q];\n", ctx.Source, ctx.Target, strcase.ToCamel(ctx.Field.Name()))
	}
	fmt.Fprintf(&b, "\tsubgraph cluster_states {\n")
	fmt.Fprintf(&b, "\t\tlabel=\"states (default: %s)\";\n", wf.Default)
	fmt.Fprintf(&b, "\t\tnode [shape=ellipse];\n")
	for _, v := range wf.States {
		if v.ID == wf.Default {
			fmt.Fprintf(&b, "\t\t%q [label=%q, peripheries=2];\n", "state:"+v.ID, v.ID)
		} else {
			fmt.Fprintf(&b, "\t\t%q [label=%q];\n", "state:"+v.ID, v.ID)
		}
	}
	for _, m := range wf.Transitions {
		for _, from := range m.Transition.From {
			fmt.Fprintf(&b, "\t\t%q -> %q [label=%q];\n", "state:"+from, "state:"+m.Transition.To, m.Name())
		}
	}
	fmt.Fprintf(&b, "\t}\n")
	fmt.Fprintf(&b, "\t%q -> %q [lhead=\"cluster_states\", style=dotted, label=%q];\n", root, "state:"+wf.Default, strcase.ToCamel(wf.StateField.Name()))
	fmt.Fprintf(&b, "\tsubgraph cluster_legend {\n")
	fmt.Fprintf(&b, "\t\tlabel=\"legend\";\n")
	fmt.Fprintf(&b, "\t\t\"root\" [shape=doubleoctagon];\n")
	fmt.Fprintf(&b, "\t\t\"member\" [shape=box];\n")
	fmt.Fprintf(&b, "\t\t\"context\" [shape=box, style=dashed];\n")
	fmt.Fprintf(&b, "\t\t\"state\" [shape=ellipse];\n")
	fmt.Fprintf(&b, "\t}\n")
	fmt.Fprintf(&b, "}\n")

	return b.String()
}

// generateWorkflowGraphs emits one DOT file per stateful resource into the
// resources package: the committed, drift-tested review surface for what each
// workflow governs — the whole tree, its states, and its declared transitions.
func (r *resourceGenerator) generateWorkflowGraphs() error {
	for _, wf := range r.assembleWorkflows() {
		fileName := fmt.Sprintf("%s_workflow_%s.dot", genPrefix, caser.ToSnake(wf.Root.Name()))
		destination := filepath.Join(r.resource.Dir(), fileName)
		if err := os.WriteFile(destination, []byte(renderWorkflowDOT(wf)), 0o644); err != nil {
			return errors.Wrapf(err, "os.WriteFile(): file: %s", destination)
		}
		log.Printf("Generated workflow graph: %v\n", destination)
	}

	return nil
}
