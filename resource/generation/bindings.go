package generation

import (
	"regexp"
	"slices"
	"strings"

	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/cccteam/ccc/resource/generation/parser/genlang"
	"github.com/go-playground/errors/v5"
)

// This file resolves the attribute-binding annotations (ABAC design plan §04)
// into compiled form: @attribute (row attributes conditions reference),
// @domain (the structural tenancy binding), and @subjectSet / @subjectValue
// (subject-side vocabulary anchored at user-id columns). Placement is
// field-level and anchored — the annotation sits on the column or FK field
// that anchors it, so the local anchor is never a spelled name; only remote
// path segments are spelled, and every hop is validated many-to-one against
// the FK metadata loaded from the emulator.

// bindingName pins the attribute-name charset, agreed jointly with the
// condition expression language: identifiers are case-sensitive,
// [A-Za-z_][A-Za-z0-9_]*.
var bindingNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// reservedBindingNames are the expression language's reserved words; a
// binding carrying one could never be referenced.
var reservedBindingNames = []string{"subject", "now", "new"}

// bindingHop is one resolved step of a join path: read Table, joined on
// JoinColumn equal to the previous step's value, continuing through (or
// terminating at) Column. Every hop follows a real foreign key, so the walk
// is many-to-one by construction.
type bindingHop struct {
	// Table is the table this hop reads.
	Table string

	// JoinColumn is the column on Table equated with the previous step's
	// value (the referenced column of the foreign key that led here).
	JoinColumn string

	// Column is the column on Table the binding continues through — a
	// foreign key, for a non-terminal hop — or terminates at.
	Column string
}

// attributeBinding is one entry of a resource's attribute vocabulary: the
// name conditions reference, resolved to data — the anchor column itself
// (empty Path), or a join path to a remote column.
type attributeBinding struct {
	Name   string
	Anchor *resourceField

	// Path is the resolved remote chain for a join-path binding; empty for a
	// column binding.
	Path []bindingHop
}

// domainBinding resolves every row of a domain-scoped resource to its tenant:
// the anchor column itself, or a join path to the tenant key. @domain is
// deliberately not an @attribute — it is consumed by tenancy injection and
// never referencable from conditions.
type domainBinding struct {
	Anchor *resourceField
	Path   []bindingHop
}

// subjectBinding is one entry of the subject-side vocabulary
// (subject.<name>), anchored at the user-id column that correlates rows to
// the requester. ValueField is the local field the set or value yields; a
// dotted value: continues through Path.
type subjectBinding struct {
	Name       string
	Anchor     *resourceField
	ValueField *resourceField
	Path       []bindingHop

	// Scalar marks a @subjectValue (threshold comparisons, unique-anchored);
	// false is a @subjectSet (IN membership, no cardinality claim).
	Scalar bool
}

// resolveBindingAnnotations extracts and validates every binding annotation
// on a table-backed resource's fields, attaching the compiled bindings to the
// resource.
func (c *client) resolveBindingAnnotations(res *resourceInfo, pStruct *parser.Struct, annotations genlang.StructAnnotations, structsByTable map[string]*parser.Struct) error {
	fieldByName := make(map[string]*resourceField, len(res.Fields))
	for _, f := range res.Fields {
		fieldByName[f.Name()] = f
	}

	names := make(map[string]string) // binding name -> the keyword that claimed it
	claim := func(name, keyword string) error {
		if err := validateBindingName(name, keyword); err != nil {
			return err
		}
		if prev, taken := names[name]; taken {
			return errors.Newf("@%s(%s): name already declared by @%s on this resource", keyword, name, prev)
		}
		names[name] = keyword

		return nil
	}

	for i, pField := range pStruct.Fields() {
		fieldAnnotations := annotations.Fields[i]
		if !fieldAnnotations.Has(attributeKeyword) && !fieldAnnotations.Has(domainKeyword) &&
			!fieldAnnotations.Has(subjectSetKeyword) && !fieldAnnotations.Has(subjectValueKeyword) {
			continue
		}

		anchor, ok := fieldByName[pField.Name()]
		if !ok {
			return errors.Newf("struct %s field %s: binding annotations require a schema-backed field", pStruct.Name(), pField.Name())
		}

		if fieldAnnotations.Has(attributeKeyword) {
			binding, err := c.resolveAttribute(anchor, fieldAnnotations.Get(attributeKeyword), structsByTable)
			if err != nil {
				return errors.Wrapf(err, "struct %s field %s", pStruct.Name(), pField.Name())
			}
			if err := claim(binding.Name, attributeKeyword); err != nil {
				return errors.Wrapf(err, "struct %s field %s", pStruct.Name(), pField.Name())
			}
			res.Attributes = append(res.Attributes, binding)
		}

		if fieldAnnotations.Has(domainKeyword) {
			binding, err := c.resolveDomain(anchor, fieldAnnotations.Get(domainKeyword), structsByTable)
			if err != nil {
				return errors.Wrapf(err, "struct %s field %s", pStruct.Name(), pField.Name())
			}
			if res.DomainBinding != nil {
				return errors.Newf("struct %s field %s: @%s already declared on field %s — a resource resolves to exactly one tenant", pStruct.Name(), pField.Name(), domainKeyword, res.DomainBinding.Anchor.Name())
			}
			res.DomainBinding = binding
		}

		for _, keyword := range []string{subjectSetKeyword, subjectValueKeyword} {
			if !fieldAnnotations.Has(keyword) {
				continue
			}
			bindings, err := c.resolveSubjectBindings(res, anchor, keyword, fieldAnnotations.Get(keyword), structsByTable)
			if err != nil {
				return errors.Wrapf(err, "struct %s field %s", pStruct.Name(), pField.Name())
			}
			for _, binding := range bindings {
				if err := claim(binding.Name, keyword); err != nil {
					return errors.Wrapf(err, "struct %s field %s", pStruct.Name(), pField.Name())
				}
				if binding.Scalar {
					res.SubjectValues = append(res.SubjectValues, binding)
				} else {
					res.SubjectSets = append(res.SubjectSets, binding)
				}
			}
		}
	}

	return nil
}

// resolveAttribute compiles one @attribute(name[, via: Remote.Segments]):
// bare is a column binding on the anchor itself; via: resolves a join path
// leaving through the anchor.
func (c *client) resolveAttribute(anchor *resourceField, arg genlang.Arg, structsByTable map[string]*parser.Struct) (*attributeBinding, error) {
	invocations, err := arg.ParseInvocations(genlang.ArgSpec{Positional: 1, Keys: []string{"via"}})
	if err != nil {
		return nil, errors.Wrapf(err, "@%s", attributeKeyword)
	}
	invocation := invocations[0]

	binding := &attributeBinding{Name: invocation.Positional[0], Anchor: anchor}
	if via, ok := invocation.Named("via"); ok {
		binding.Path, err = c.resolveRemotePath(anchor, splitPathSegments(via), structsByTable)
		if err != nil {
			return nil, errors.Wrapf(err, "@%s(%s)", attributeKeyword, binding.Name)
		}
	}

	return binding, nil
}

// resolveDomain compiles a @domain annotation: bare marks the anchor as the
// tenant key; @domain(via: ...) resolves a join path to it.
func (c *client) resolveDomain(anchor *resourceField, arg genlang.Arg, structsByTable map[string]*parser.Struct) (*domainBinding, error) {
	binding := &domainBinding{Anchor: anchor}
	if arg.Count() == 0 {
		return binding, nil
	}

	invocations, err := arg.ParseInvocations(genlang.ArgSpec{Keys: []string{"via"}})
	if err != nil {
		return nil, errors.Wrapf(err, "@%s", domainKeyword)
	}
	via, ok := invocations[0].Named("via")
	if !ok {
		return nil, errors.Newf("@%s takes no arguments, or via: alone", domainKeyword)
	}
	binding.Path, err = c.resolveRemotePath(anchor, splitPathSegments(via), structsByTable)
	if err != nil {
		return nil, errors.Wrapf(err, "@%s", domainKeyword)
	}

	return binding, nil
}

// resolveSubjectBindings compiles the (repeatable) @subjectSet / @subjectValue
// annotations on one user-id anchor. value: names the sibling Go field the
// set or value yields, dotted to continue through remote hops.
func (c *client) resolveSubjectBindings(res *resourceInfo, anchor *resourceField, keyword string, arg genlang.Arg, structsByTable map[string]*parser.Struct) ([]*subjectBinding, error) {
	scalar := keyword == subjectValueKeyword
	uniqueAnchor := anchor.IsUniqueIndex || (anchor.IsPrimaryKey && res.PkCount == 1)
	if scalar && !uniqueAnchor {
		return nil, errors.Newf("@%s requires its anchor column to be the primary key or unique-indexed, so the database enforces exactly one row per user", subjectValueKeyword)
	}

	invocations, err := arg.ParseInvocations(genlang.ArgSpec{Positional: 1, Keys: []string{"value"}, Required: []string{"value"}})
	if err != nil {
		return nil, errors.Wrapf(err, "@%s", keyword)
	}

	fieldByName := make(map[string]*resourceField, len(res.Fields))
	for _, f := range res.Fields {
		fieldByName[f.Name()] = f
	}

	bindings := make([]*subjectBinding, 0, len(invocations))
	for _, invocation := range invocations {
		value, _ := invocation.Named("value")
		segments := splitPathSegments(value)

		local, ok := fieldByName[segments[0]]
		if !ok {
			return nil, errors.Newf("@%s(%s): value field %q not found on the resource", keyword, invocation.Positional[0], segments[0])
		}

		binding := &subjectBinding{
			Name:       invocation.Positional[0],
			Anchor:     anchor,
			ValueField: local,
			Scalar:     scalar,
		}
		if len(segments) > 1 {
			binding.Path, err = c.resolveRemotePath(local, segments[1:], structsByTable)
			if err != nil {
				return nil, errors.Wrapf(err, "@%s(%s)", keyword, binding.Name)
			}
		}
		bindings = append(bindings, binding)
	}

	return bindings, nil
}

// resolveRemotePath resolves dotted remote segments (Go field names on each
// successive struct) leaving through anchor, which must be a foreign key.
// Every non-terminal segment must itself be a foreign key — each hop follows
// a real FK relationship, so the walk is many-to-one — and the terminal
// segment is the column the binding reads.
func (c *client) resolveRemotePath(anchor *resourceField, segments []string, structsByTable map[string]*parser.Struct) ([]bindingHop, error) {
	if !anchor.IsForeignKey || anchor.ReferencedResource == "" {
		return nil, errors.Newf("a join path must leave through a foreign key, and %s is not one", anchor.Name())
	}

	hops := make([]bindingHop, 0, len(segments))
	table := anchor.ReferencedResource
	joinColumn := anchor.ReferencedField

	for i, segment := range segments {
		if segment == "" {
			return nil, errors.Newf("path %q contains an empty segment", strings.Join(segments, "."))
		}

		remoteStruct, ok := structsByTable[table]
		if !ok {
			return nil, errors.Newf("no struct found for table %q to resolve Go field %q", table, segment)
		}
		remoteTable, ok := c.tableMap[table]
		if !ok {
			return nil, errors.Newf("table %q not found in database", table)
		}

		column, meta, err := columnForGoField(remoteStruct, remoteTable, segment)
		if err != nil {
			return nil, err
		}

		hops = append(hops, bindingHop{Table: table, JoinColumn: joinColumn, Column: column})

		if i == len(segments)-1 {
			break
		}
		if !meta.IsForeignKey || meta.ReferencedTable == "" {
			return nil, errors.Newf("path segment %q on %s must be a foreign key to continue the path", segment, table)
		}
		table = meta.ReferencedTable
		joinColumn = meta.ReferencedColumn
	}

	return hops, nil
}

// columnForGoField maps a Go field name on a remote struct to its table
// column and metadata through the struct's spanner tag.
func columnForGoField(pStruct *parser.Struct, table *tableMetadata, goField string) (string, columnMeta, error) {
	for _, field := range pStruct.Fields() {
		if field.Name() != goField {
			continue
		}
		column, ok := field.LookupTag(spannerTagKey)
		if !ok {
			return "", columnMeta{}, errors.Newf("field %q on %s has no spanner tag", goField, pStruct.Name())
		}
		meta, ok := table.Columns[column]
		if !ok {
			return "", columnMeta{}, errors.Newf("field %q on %s maps to column %q, which is not in the table", goField, pStruct.Name(), column)
		}

		return column, meta, nil
	}

	return "", columnMeta{}, errors.Newf("field %q not found on %s", goField, pStruct.Name())
}

func validateBindingName(name, keyword string) error {
	if !bindingNamePattern.MatchString(name) {
		return errors.Newf("@%s(%s): a binding name must match %s", keyword, name, bindingNamePattern)
	}
	if slices.Contains(reservedBindingNames, name) {
		return errors.Newf("@%s(%s): %q is reserved by the condition expression language", keyword, name, name)
	}

	return nil
}

// rejectBindingAnnotations fails extraction when a binding annotation appears
// on a struct kind that has no schema to resolve it against: bindings are
// table-backed resources' vocabulary.
func rejectBindingAnnotations(pStruct *parser.Struct, annotations genlang.StructAnnotations, kind string) error {
	var errs []error
	for i, field := range pStruct.Fields() {
		for _, keyword := range []string{attributeKeyword, domainKeyword, subjectSetKeyword, subjectValueKeyword} {
			if annotations.Fields[i].Has(keyword) {
				errs = append(errs, errors.Newf("struct %s field %s: @%s is only valid on @resource structs; a %s has no schema to resolve a binding against", pStruct.Name(), field.Name(), keyword, kind))
			}
		}
	}
	if len(errs) > 0 {
		return errors.Wrap(errors.Join(errs...), "binding annotation error")
	}

	return nil
}

// splitPathSegments splits a dotted path into its Go-field-name segments.
func splitPathSegments(path string) []string {
	return strings.Split(path, ".")
}
