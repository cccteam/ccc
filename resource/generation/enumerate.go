package generation

import (
	"fmt"
	"strings"

	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/cccteam/ccc/resource/generation/parser/genlang"
	"github.com/go-playground/errors/v5"
)

// The field-scope @enumerate: a field that holds another resource's identifier
// declares, in the generator alone, which resource a picker for it lists. A request
// field has no schema, so it always declares. A table-backed, virtual, or computed
// field declares when the schema says nothing — a plain column holding an identifier
// from outside the schema — or less than the picker needs — a foreign key whose
// target lacks the display columns a view over it carries. The generated metadata is
// the one statement of which resource a picker lists; the client narrows and presents
// that resource's rows and never chooses another.

// enumerationSource is what a field-scope @enumerate(X) resolves to: a resource whose
// rows the picker lists, or an @enumerate table whose rows are the program's
// constants and ride in the metadata inline (Enumeration names the type). Name is X
// as written either way.
type enumerationSource struct {
	Name        string
	Enumeration string
	Values      []*enumData
}

// IsInline reports whether the source is an enumeration table, rendered from the
// generated values with no request and no List grant.
func (s enumerationSource) IsInline() bool {
	return s.Enumeration != ""
}

// resolveEnumerate resolves a field-scope @enumerate(X). X must name exactly one
// thing the generator knows: an enumeration table (declared by @enumerate on a named
// type; whether or not a struct also exposes it, the table wins, as it does for an
// inferred key), or a table-backed, virtual, or computed resource keyed by a single
// column, since a picker stores one key.
func (c *client) resolveEnumerate(arg genlang.Arg) (enumerationSource, error) {
	src, problem := c.enumerationSourceOf(arg)
	if problem != "" {
		return enumerationSource{}, errors.New(problem)
	}

	return src, nil
}

// enumerationSourceOf is resolveEnumerate with the refusal as text, so a caller can
// prefix the struct and field it concerns in one message.
func (c *client) enumerationSourceOf(arg genlang.Arg) (src enumerationSource, problem string) {
	if arg.Count() != 1 {
		return enumerationSource{}, fmt.Sprintf("@%s on a field takes one argument, the enumerated resource; got %d", enumerateKeyword, arg.Count())
	}
	name := string(arg)
	if typeName, ok := c.enumerationOf(name); ok {
		return enumerationSource{Name: name, Enumeration: typeName, Values: c.enumValues[name]}, ""
	}
	keys, ok := c.resourceKeyCount(name)
	switch {
	case !ok:
		return enumerationSource{}, fmt.Sprintf("@%s(%s): resource %q does not exist", enumerateKeyword, name, name)
	case keys == 0:
		return enumerationSource{}, fmt.Sprintf("@%s(%s): %s declares no primary key, and a picker stores the one key of the resource it lists; declare the key with @%s", enumerateKeyword, name, name, primarykeyKeyword)
	case keys > 1:
		return enumerationSource{}, fmt.Sprintf("@%s(%s): the primary key of %s spans %d columns, and a picker stores one", enumerateKeyword, name, name, keys)
	}

	return enumerationSource{Name: name}, ""
}

// resourceKeyCount reports how many columns key the named resource, and whether the
// generator parsed a resource of that name: a table-backed resource's key comes from
// the schema, a virtual or computed resource's from its @primarykey fields. Names are
// the plural resource names the collection registers.
func (c *client) resourceKeyCount(name string) (int, bool) {
	for _, res := range c.resources {
		if c.pluralize(res.Name()) != name {
			continue
		}
		if !res.IsVirtual {
			return res.PkCount, true
		}
		var keys int
		for range res.PrimaryKeys() {
			keys++
		}

		return keys, true
	}
	for _, res := range c.computedResources {
		if c.pluralize(res.Name()) == name {
			return len(res.PrimaryKeys()), true
		}
	}

	return 0, false
}

func (c *client) doesResourceExist(resourceName string) bool {
	_, ok := c.resourceKeyCount(resourceName)

	return ok
}

// declareFieldEnumerations records each field's field-scope @enumerate argument for
// resolveFieldEnumerations, which runs once every kind is extracted: a declaration
// may name a computed resource, and those are parsed last.
func declareFieldEnumerations(pStruct *parser.Struct, fields []*resourceField, annotations genlang.StructAnnotations) {
	for i, structField := range pStruct.Fields() {
		if !annotations.Fields[i].Has(enumerateKeyword) {
			continue
		}
		arg := annotations.Fields[i].Get(enumerateKeyword)
		for _, field := range fields {
			if field.Field == structField {
				field.enumerateArg = &arg
			}
		}
	}
}

// resolveFieldEnumerations resolves every field-scope @enumerate declared on a
// table-backed, virtual, or computed field. A plain column names the resource the
// picker lists, or an enumeration table whose values ride inline. A foreign key may
// name a resource other than its target — typically a view over the target that
// carries display columns the target lacks, keyed like it — while the constraint
// stays the correctness guard at write time. Naming the target itself is refused as
// redundant, and any declaration on a key into an enumeration table is refused as a
// contradiction: those values are the program's constants, and a runtime list for
// them would be policy about nothing.
func (c *client) resolveFieldEnumerations(resources []*resourceInfo, computed []*computedResource) error {
	var errs []error
	for _, res := range resources {
		for _, field := range res.Fields {
			if field.enumerateArg == nil {
				continue
			}
			if err := c.resolveResourceFieldEnumeration(res, field); err != nil {
				errs = append(errs, err)
			}
		}
	}
	for _, res := range computed {
		for _, field := range res.Fields {
			if field.enumerateArg == nil {
				continue
			}
			src, problem := c.enumerationSourceOf(*field.enumerateArg)
			if problem != "" {
				errs = append(errs, errors.Newf("struct %s field %s: %s", res.Name(), field.Name(), problem))

				continue
			}
			field.applyEnumeration(src)
		}
	}
	if len(errs) > 0 {
		return errors.Wrapf(errors.Join(errs...), "field-scope @%s", enumerateKeyword)
	}

	return nil
}

func (c *client) resolveResourceFieldEnumeration(res *resourceInfo, field *resourceField) error {
	src, problem := c.enumerationSourceOf(*field.enumerateArg)
	if problem != "" {
		return errors.Newf("struct %s field %s: %s", res.Name(), field.Name(), problem)
	}
	if field.IsForeignKey {
		if typeName, ok := c.enumerationOf(field.ReferencedResource); ok {
			return errors.Newf("struct %s field %s: @%s(%s) contradicts the foreign key into %s, an enumeration table (@%s on type %s): its rows are the program's constants, and the field renders from the generated values; remove the annotation", res.Name(), field.Name(), enumerateKeyword, src.Name, field.ReferencedResource, enumerateKeyword, typeName)
		}
		if src.Name == field.ReferencedResource {
			return errors.Newf("struct %s field %s: @%s(%s) is redundant: the schema's foreign key already names %s, and the picker lists it undeclared; remove the annotation", res.Name(), field.Name(), enumerateKeyword, src.Name, src.Name)
		}
		if src.IsInline() {
			return errors.Newf("struct %s field %s: @%s(%s) names an enumeration table, but the foreign key constrains the column to the rows of %s; a foreign key may name only a resource keyed like its target", res.Name(), field.Name(), enumerateKeyword, src.Name, field.ReferencedResource)
		}
	}
	field.applyEnumeration(src)

	return nil
}

// enumerationLiteral renders an enumeration's rows as the TypeScript metadata carries
// them: [{ id: "gear", display: "Gear" }, …].
func enumerationLiteral(values []*enumData) string {
	parts := make([]string, 0, len(values))
	for _, v := range values {
		parts = append(parts, fmt.Sprintf("{ id: %q, display: %q }", v.ID, v.Description))
	}

	return "[" + strings.Join(parts, ", ") + "]"
}
