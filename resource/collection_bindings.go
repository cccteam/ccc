package resource

import (
	"slices"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/go-playground/errors/v5"
)

// The binding vocabulary a permission collection carries (ABAC design plan
// §04). Bindings travel in generated code — the Collection is the
// authoritative attribute listing — never as store rows: the generator
// compiles the field-level binding annotations and registers the result here,
// where deployment tooling (MigrateRoles condition validation) and the SQL
// lowering read it back.

// BindingHop is one resolved step of a join path: read Table, joined on
// JoinColumn equal to the previous step's value, continuing through (or
// terminating at) Column. Every hop follows a real foreign key, validated at
// generation, so the walk is many-to-one.
type BindingHop struct {
	Table      string
	JoinColumn string
	Column     string
}

// AttributeType is the attribute comparison-type vocabulary, canonically
// defined in accesstypes so deployment tooling (MigrateRoles) shares one
// definition with generation.
type AttributeType = accesstypes.AttributeType

// The vocabulary's values, re-exported for generated code and callers already
// importing resource.
const (
	AttributeTypeString    = accesstypes.AttributeTypeString
	AttributeTypeNumber    = accesstypes.AttributeTypeNumber
	AttributeTypeBool      = accesstypes.AttributeTypeBool
	AttributeTypeTimestamp = accesstypes.AttributeTypeTimestamp
	AttributeTypeDate      = accesstypes.AttributeTypeDate
)

// AttributeData is one attribute binding: the vocabulary name grant
// conditions reference, resolved to data. Column is the anchor column on the
// resource's own table — the attribute itself when Path is empty, or the
// foreign key the join path leaves through. Type is the attribute's
// comparison type, derived by generation from the bound column's Go type.
type AttributeData struct {
	Name   string
	Column string
	Type   AttributeType
	Path   []BindingHop
}

// DomainBindingData is the structural tenancy binding: how every row of a
// domain-scoped resource resolves to its tenant. Column is the tenant-key
// column itself when Path is empty, or the foreign key the path to it leaves
// through. It is deliberately not an attribute — tenancy injection consumes
// it, and conditions can never reference it.
type DomainBindingData struct {
	Column string
	Path   []BindingHop
}

// SubjectBindingData is one entry of the subject-side vocabulary
// (subject.<name>), anchored on the declaring resource's user-id column.
// UserColumn correlates rows to the requester; Column is the local column the
// set or value yields — the terminal when Path is empty, or the foreign key a
// dotted value: continues through.
type SubjectBindingData struct {
	Name       string
	UserColumn string
	Column     string
	Path       []BindingHop
}

// Bindings is one resource's complete binding vocabulary.
type Bindings struct {
	Attributes    []AttributeData
	Domain        *DomainBindingData
	SubjectSets   []SubjectBindingData
	SubjectValues []SubjectBindingData
}

// isZero reports whether no vocabulary is declared.
func (r *Bindings) isZero() bool {
	return len(r.Attributes) == 0 && r.Domain == nil && len(r.SubjectSets) == 0 && len(r.SubjectValues) == 0
}

// sorted returns the canonical form: attributes and subject vocabulary sorted
// by name (path order inside each entry is meaningful and preserved).
func (r *Bindings) sorted() Bindings {
	byName := func(a, b string) int {
		switch {
		case a < b:
			return -1
		case a > b:
			return 1
		default:
			return 0
		}
	}
	out := *r
	out.Attributes = slices.Clone(r.Attributes)
	slices.SortFunc(out.Attributes, func(a, b AttributeData) int { return byName(a.Name, b.Name) })
	out.SubjectSets = slices.Clone(r.SubjectSets)
	slices.SortFunc(out.SubjectSets, func(a, b SubjectBindingData) int { return byName(a.Name, b.Name) })
	out.SubjectValues = slices.Clone(r.SubjectValues)
	slices.SortFunc(out.SubjectValues, func(a, b SubjectBindingData) int { return byName(a.Name, b.Name) })

	return out
}

// SetResourceBindings registers a resource's binding vocabulary, replacing
// any previous registration for that resource — the generator computes one
// vocabulary per resource.
func (b *CollectionBuilder) SetResourceBindings(scope accesstypes.PermissionScope, res accesstypes.Resource, bindings *Bindings) {
	b.g.setResourceBindings(scope, res, bindings)
}

func (g *GeneratedCollection) setResourceBindings(scope accesstypes.PermissionScope, res accesstypes.Resource, bindings *Bindings) {
	if bindings.isZero() {
		return
	}
	if g.bindings[scope] == nil {
		g.bindings[scope] = make(map[accesstypes.Resource]Bindings)
	}
	g.bindings[scope][res] = *bindings
}

// Bindings returns a resource's binding vocabulary; ok is false when the
// resource declares none.
func (g *GeneratedCollection) Bindings(scope accesstypes.PermissionScope, res accesstypes.Resource) (Bindings, bool) {
	bindings, ok := g.bindings[scope][res]

	return bindings, ok
}

// The flat vocabulary accessors below answer with basic types only, so
// deployment tooling (access.MigrateRoles) can consume the Collection through
// a structural interface without importing this package.

// AttributeComparisonType resolves an attribute's comparison type on one
// resource; ok is false when the resource declares no such attribute.
func (g *GeneratedCollection) AttributeComparisonType(scope accesstypes.PermissionScope, res accesstypes.Resource, name string) (accesstypes.AttributeType, bool) {
	attr, ok := g.attributeData(scope, res, name)
	if !ok {
		return "", false
	}

	return attr.Type, true
}

// AttributeIsColumn reports whether an attribute is a column binding on the
// resource's own row — the shape a post-image (new.attr) reference requires.
func (g *GeneratedCollection) AttributeIsColumn(scope accesstypes.PermissionScope, res accesstypes.Resource, name string) bool {
	attr, ok := g.attributeData(scope, res, name)

	return ok && len(attr.Path) == 0
}

// DeclaresSubjectSet reports whether the application declares subject.<name>
// as a set.
func (g *GeneratedCollection) DeclaresSubjectSet(name string) bool {
	_, ok := g.SubjectSet(name)

	return ok
}

// DeclaresSubjectValue reports whether the application declares
// subject.<name> as a scalar value.
func (g *GeneratedCollection) DeclaresSubjectValue(name string) bool {
	_, ok := g.SubjectValue(name)

	return ok
}

func (g *GeneratedCollection) attributeData(scope accesstypes.PermissionScope, res accesstypes.Resource, name string) (AttributeData, bool) {
	bindings, ok := g.Bindings(scope, res)
	if !ok {
		return AttributeData{}, false
	}
	for _, attr := range bindings.Attributes {
		if attr.Name == name {
			return attr, true
		}
	}

	return AttributeData{}, false
}

// validateCollectionBindings enforces the vocabulary rules the runtime can
// check without a schema: names unique within one resource's vocabulary, and
// subject names unique across the whole collection — subject.<name> is one
// application-wide namespace, so two resources anchoring the same name would
// make a condition ambiguous.
func validateCollectionBindings(resources []CollectionResource) error {
	type subjectClaim struct {
		resource accesstypes.Resource
		scope    accesstypes.PermissionScope
	}
	subjectNames := make(map[string]subjectClaim)

	for i := range resources {
		res := &resources[i]
		local := make(map[string]struct{})
		claimLocal := func(name string) error {
			if name == "" {
				return errors.Newf("resource %q declares a binding with an empty name", res.Name)
			}
			if _, taken := local[name]; taken {
				return errors.Newf("resource %q declares binding name %q twice", res.Name, name)
			}
			local[name] = struct{}{}

			return nil
		}

		for _, attr := range res.Attributes {
			if err := claimLocal(attr.Name); err != nil {
				return err
			}
			if !accesstypes.ValidAttributeType(attr.Type) {
				return errors.Newf("resource %q attribute %q carries comparison type %q, which is not in the vocabulary — regenerate the collection", res.Name, attr.Name, attr.Type)
			}
		}
		for _, subject := range slices.Concat(res.SubjectSets, res.SubjectValues) {
			if err := claimLocal(subject.Name); err != nil {
				return err
			}
			if prev, taken := subjectNames[subject.Name]; taken {
				return errors.Newf("subject vocabulary name %q is declared by both %q (scope %q) and %q (scope %q); subject.<name> is one application-wide namespace", subject.Name, prev.resource, prev.scope, res.Name, res.Scope)
			}
			subjectNames[subject.Name] = subjectClaim{resource: res.Name, scope: res.Scope}
		}
	}

	return nil
}

// SubjectAnchor is a subject-vocabulary entry with everything its rendering
// needs: the anchor table (the resource declaring it), the entry itself, and
// the anchor table's own tenancy binding — the rendered subquery filters to
// the request's partition exactly when both the request and the anchor table
// are partitioned, derived here, never authored.
type SubjectAnchor struct {
	Resource accesstypes.Resource
	Scope    accesstypes.PermissionScope
	Binding  SubjectBindingData
	Domain   *DomainBindingData
}

// SubjectSet resolves a @subjectSet name across the collection —
// subject.<name> is one application-wide namespace, validated unique at
// construction.
func (g *GeneratedCollection) SubjectSet(name string) (SubjectAnchor, bool) {
	return g.subjectAnchor(name, func(b Bindings) []SubjectBindingData { return b.SubjectSets })
}

// SubjectValue resolves a @subjectValue name across the collection.
func (g *GeneratedCollection) SubjectValue(name string) (SubjectAnchor, bool) {
	return g.subjectAnchor(name, func(b Bindings) []SubjectBindingData { return b.SubjectValues })
}

func (g *GeneratedCollection) subjectAnchor(name string, entries func(Bindings) []SubjectBindingData) (SubjectAnchor, bool) {
	for scope, store := range g.bindings {
		for res, bindings := range store {
			for _, entry := range entries(bindings) {
				if entry.Name == name {
					return SubjectAnchor{Resource: res, Scope: scope, Binding: entry, Domain: bindings.Domain}, true
				}
			}
		}
	}

	return SubjectAnchor{}, false
}
