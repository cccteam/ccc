package generation

import (
	"fmt"

	"github.com/go-playground/errors/v5"
)

// A bounded picker source serves a read. A picker over a resource that declares a
// maximum page size (@page(max)) pages that resource one server page at a time and
// reads the chosen row by key, since the row may sit on a page the picker has not
// loaded; a source with no maximum is read whole in one request and may suppress
// its read, the chosen row resolving from the loaded list. So a resource a picker
// lists — a foreign key into a routed resource, or a field-scope @enumerate naming
// a resource or view — may not both declare a maximum and serve no read, and
// generation refuses the pair naming the two ways out.

// pickerSource is a resource as a picker sees it: whether it declares a maximum
// page size, and whether it serves a keyed read.
type pickerSource struct {
	pageMax uint64
	reads   bool
	routed  bool
}

// pickerSourceOf finds the resource a picker for the named plural resource lists; ok
// is false for a name the generator did not parse (a foreign key into a table no
// struct exposes) and for an enumeration table, whose rows ride inline with no
// request.
func (c *client) pickerSourceOf(name string) (pickerSource, bool) {
	if _, inline := c.enumerationOf(name); inline {
		return pickerSource{}, false
	}
	for _, res := range c.resources {
		if c.pluralize(res.Name()) == name {
			return pickerSource{pageMax: res.PageMax, reads: !res.ReadHandlerDisabled(), routed: !res.RoutingDisabled()}, true
		}
	}
	for _, res := range c.computedResources {
		if c.pluralize(res.Name()) == name {
			return pickerSource{pageMax: res.PageMax, reads: !res.ReadHandlerDisabled(), routed: !res.RoutingDisabled()}, true
		}
	}

	return pickerSource{}, false
}

// boundedPickerSourceRefusal is the refusal for a picker field whose source declares
// a maximum and no read, or "" when the source is fine. declared reports a
// field-scope @enumerate; an undeclared foreign key lists its target only where the
// target is routed, so an unrouted target is no picker source.
func (c *client) boundedPickerSourceRefusal(source string, declared bool) string {
	src, ok := c.pickerSourceOf(source)
	if !ok || src.pageMax == 0 || src.reads || (!declared && !src.routed) {
		return ""
	}

	return fmt.Sprintf("the picker lists %s, which serves at most %d rows per page and no read; a paged picker reads the chosen row by key, so keep the read handler of %s or drop its @page maximum", source, src.pageMax, source)
}

// validatePickerSources refuses every table, view, and computed field whose picker
// lists a bounded source with no read. Run once every kind is extracted and the
// field-scope declarations are resolved.
func (c *client) validatePickerSources(resources []*resourceInfo, computed []*computedResource) error {
	var errs []error
	for _, res := range resources {
		for _, field := range res.Fields {
			var source string
			var declared bool
			switch {
			case field.HasDeclaredEnumeration() && field.Enumeration == "":
				source, declared = field.EnumeratedResource(), true
			case field.IsForeignKey:
				source = field.ReferencedResource
			default:
				continue
			}
			if problem := c.boundedPickerSourceRefusal(source, declared); problem != "" {
				errs = append(errs, errors.Newf("struct %s field %s: %s", res.Name(), field.Name(), problem))
			}
		}
	}
	for _, res := range computed {
		for _, field := range res.Fields {
			if !field.IsEnumerated || field.Enumeration != "" {
				continue
			}
			if problem := c.boundedPickerSourceRefusal(field.EnumeratedResource(), true); problem != "" {
				errs = append(errs, errors.Newf("struct %s field %s: %s", res.Name(), field.Name(), problem))
			}
		}
	}
	if len(errs) > 0 {
		return errors.Wrap(errors.Join(errs...), "bounded picker source")
	}

	return nil
}

// validateRPCPickerSources is validatePickerSources for request fields, which
// always declare their picker source; run once the methods are parsed.
func (c *client) validateRPCPickerSources(methods []*rpcMethodInfo) error {
	var errs []error
	for _, method := range methods {
		for _, field := range method.Fields {
			if !field.IsEnumerated() || field.Enumeration != "" {
				continue
			}
			if problem := c.boundedPickerSourceRefusal(field.EnumeratedResource(), true); problem != "" {
				errs = append(errs, errors.Newf("RPC method %s field %s: %s", method.Name(), field.Name(), problem))
			}
		}
	}
	if len(errs) > 0 {
		return errors.Wrap(errors.Join(errs...), "bounded picker source")
	}

	return nil
}
