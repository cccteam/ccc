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
	invocations, err := arg.ParseInvocations(genlang.ArgSpec{Keys: []string{stateDefaultArgKey}, Required: []string{stateDefaultArgKey}})
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
// graph is emitted as a committed, drift-tested DOT file per workflow —
// membership only, never transition edges: the framework knows the values,
// the transitions are RPC-body business rules.

// stateBindingName is the synthesized attribute every stateful resource and
// workflow member carries.
const (
	stateBindingName = "state"

	// stateBindingType is the synthesized binding's comparison type: state
	// values are the enum table's string descriptions.
	stateBindingType = string(resource.AttributeTypeString)
)

// resolveWorkflowMembership captures a field's @stateRoot declaration; chain
// resolution and synthesis happen in resolveWorkflows once every resource is
// extracted.
func resolveWorkflowMembership(field *resourceField, arg genlang.Arg) error {
	invocations, err := arg.ParseInvocations(genlang.ArgSpec{Positional: 1})
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

// generateWorkflowGraphs emits one DOT file per workflow into the resources
// package: the committed, drift-tested review surface for what the workflow
// governs. The graph draws membership only — never transition edges: the
// framework knows the state values, and allowed transitions are RPC-body
// business rules it deliberately cannot see.
func (r *resourceGenerator) generateWorkflowGraphs() error {
	type member struct {
		name   string
		parent string
	}
	workflows := make(map[string][]member) // root struct name -> members
	for _, res := range r.resources {
		for _, field := range res.Fields {
			if field.WorkflowRoot == "" {
				continue
			}
			parent := field.ReferencedResource
			for _, other := range r.resources {
				if r.pluralize(other.Name()) == field.ReferencedResource {
					parent = other.Name()
				}
			}
			workflows[field.WorkflowRoot] = append(workflows[field.WorkflowRoot], member{name: res.Name(), parent: parent})
		}
	}

	for root, members := range workflows {
		slices.SortFunc(members, func(a, b member) int { return strings.Compare(a.name, b.name) })

		var rootRes *resourceInfo
		for _, res := range r.resources {
			if res.Name() == root {
				rootRes = res
			}
		}
		state := stateField(rootRes)
		values := make([]string, 0, len(r.enumValues[state.ReferencedResource]))
		for _, v := range r.enumValues[state.ReferencedResource] {
			values = append(values, v.ID)
		}

		var b strings.Builder
		fmt.Fprintf(&b, "// Code generated by resourcegeneration. DO NOT EDIT.\n")
		fmt.Fprintf(&b, "// Workflow %s: membership only — transitions are RPC-body business rules.\n", root)
		fmt.Fprintf(&b, "digraph %sWorkflow {\n", root)
		fmt.Fprintf(&b, "\trankdir=RL;\n")
		fmt.Fprintf(&b, "\t%q [shape=doubleoctagon, label=\"%s\\nstates: %s\"];\n", root, root, strings.Join(values, " | "))
		for _, m := range members {
			fmt.Fprintf(&b, "\t%q -> %q;\n", m.name, m.parent)
		}
		fmt.Fprintf(&b, "}\n")

		fileName := fmt.Sprintf("%s_workflow_%s.dot", genPrefix, caser.ToSnake(root))
		destination := filepath.Join(r.resource.Dir(), fileName)
		if err := os.WriteFile(destination, []byte(b.String()), 0o644); err != nil {
			return errors.Wrapf(err, "os.WriteFile(): file: %s", destination)
		}
		log.Printf("Generated workflow graph: %v\n", destination)
	}

	return nil
}
