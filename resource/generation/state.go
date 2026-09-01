package generation

import (
	"slices"

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
		if !annotations.Fields[i].Has(stateKeyword) {
			continue
		}

		field, ok := fieldByName[pField.Name()]
		if !ok {
			return errors.Newf("struct %s field %s: @%s requires a schema-backed field", pStruct.Name(), pField.Name(), stateKeyword)
		}
		if err := c.resolveState(field, annotations.Fields[i].Get(stateKeyword)); err != nil {
			return errors.Wrapf(err, "struct %s field %s", pStruct.Name(), pField.Name())
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
