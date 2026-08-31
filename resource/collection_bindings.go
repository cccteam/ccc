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

// AttributeData is one attribute binding: the vocabulary name grant
// conditions reference, resolved to data. Column is the anchor column on the
// resource's own table — the attribute itself when Path is empty, or the
// foreign key the join path leaves through.
type AttributeData struct {
	Name   string
	Column string
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
