package generation

import (
	"fmt"
	"slices"
	"strings"

	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/generation/parser/genlang"
	"github.com/go-playground/errors/v5"
)

// pagingDecl is a resource's declared paging contract, read off the @order and
// @page annotations and carried to the generated list handler as its decoder's
// resource.Paging. The zero value declares nothing: the decoder lists by primary
// key with the generator-wide page size and no maximum.
type pagingDecl struct {
	// DeclaredOrder is the @order list, each entry a Go field name and direction.
	DeclaredOrder []resource.SortField
	// PageDefault and PageMax are the @page sizes; 0 means undeclared.
	PageDefault uint64
	PageMax     uint64
}

// DeclaresPaging reports whether any paging annotation is present.
func (p *pagingDecl) DeclaresPaging() bool {
	return len(p.DeclaredOrder) > 0 || p.PageDefault != 0 || p.PageMax != 0
}

// PagingOption renders the WithPaging call the list handler chains onto its
// decoder, or nothing when the resource declares no paging contract.
func (p *pagingDecl) PagingOption() string {
	if !p.DeclaresPaging() {
		return ""
	}

	var b strings.Builder
	b.WriteString(".\n\t\tWithPaging(resource.Paging{")
	var parts []string
	if len(p.DeclaredOrder) > 0 {
		fields := make([]string, 0, len(p.DeclaredOrder))
		for _, sf := range p.DeclaredOrder {
			direction := "resource.SortAscending"
			if sf.Direction == resource.SortDescending {
				direction = "resource.SortDescending"
			}
			fields = append(fields, fmt.Sprintf("{Field: %q, Direction: %s}", sf.Field, direction))
		}
		parts = append(parts, "Order: []resource.SortField{"+strings.Join(fields, ", ")+"}")
	}
	if p.PageDefault != 0 {
		parts = append(parts, fmt.Sprintf("DefaultLimit: %d", p.PageDefault))
	}
	if p.PageMax != 0 {
		parts = append(parts, fmt.Sprintf("MaxLimit: %d", p.PageMax))
	}
	b.WriteString(strings.Join(parts, ", "))
	b.WriteString("})")

	return b.String()
}

// resolveOrder compiles an @order annotation: a comma list of `Field [asc|desc]`
// entries naming Go fields of the struct, ascending when the direction is
// omitted. sortable answers whether a named field can be ordered by (a flat
// field of a supported type); the primary key is appended at runtime, so
// naming it here is allowed but unnecessary.
func resolveOrder(arg genlang.Arg, sortable func(field string) error) ([]resource.SortField, error) {
	var order []resource.SortField
	for invocation := range arg.Seq() {
		for part := range strings.SplitSeq(invocation, ",") {
			words := strings.Fields(part)
			switch len(words) {
			case 0:
				return nil, errors.Newf("@%s(%s) contains an empty entry", orderKeyword, invocation)
			case 1, 2:
			default:
				return nil, errors.Newf("@%s(%s): entry %q must be a field name followed by an optional asc or desc", orderKeyword, invocation, strings.TrimSpace(part))
			}

			field := words[0]
			if err := sortable(field); err != nil {
				return nil, errors.Wrapf(err, "@%s(%s)", orderKeyword, invocation)
			}
			if slices.ContainsFunc(order, func(sf resource.SortField) bool { return sf.Field == field }) {
				return nil, errors.Newf("@%s(%s) names %s twice", orderKeyword, invocation, field)
			}

			direction := resource.SortAscending
			if len(words) == 2 {
				switch words[1] {
				case string(resource.SortAscending):
				case string(resource.SortDescending):
					direction = resource.SortDescending
				default:
					return nil, errors.Newf("@%s(%s): direction %q on %s must be asc or desc", orderKeyword, invocation, words[1], field)
				}
			}
			order = append(order, resource.SortField{Field: field, Direction: direction})
		}
	}

	return order, nil
}

// resolveResourcePaging applies the paging annotations of a table or view
// resource. Every field of a resource is a column, so every field is sortable.
func resolveResourcePaging(res *resourceInfo, annotations genlang.StructAnnotations) error {
	if !annotations.Struct.Has(orderKeyword) {
		return nil
	}

	order, err := resolveOrder(annotations.Struct.Get(orderKeyword), func(field string) error {
		if !slices.ContainsFunc(res.Fields, func(f *resourceField) bool { return f.Name() == field }) {
			return errors.Newf("%s is not a field of %s", field, res.Name())
		}

		return nil
	})
	if err != nil {
		return errors.Wrapf(err, "on %s", res.Name())
	}
	res.DeclaredOrder = order

	return nil
}

// resolveComputedPaging applies the paging annotations of a computed resource. A
// nested field is opaque and never a sort key, so only a leaf field may be named.
func resolveComputedPaging(res *computedResource, annotations genlang.StructAnnotations) error {
	if !annotations.Struct.Has(orderKeyword) {
		return nil
	}

	order, err := resolveOrder(annotations.Struct.Get(orderKeyword), func(field string) error {
		i := slices.IndexFunc(res.Fields, func(f *computedField) bool { return f.Name() == field })
		if i < 0 {
			return errors.Newf("%s is not a field of %s", field, res.Name())
		}
		if f := res.Fields[i]; f.wire != nil && !f.wire.IsLeaf() {
			return errors.Newf("%s is a nested field of %s; a nested field is opaque and never a sort key", field, res.Name())
		}

		return nil
	})
	if err != nil {
		return errors.Wrapf(err, "on %s", res.Name())
	}
	res.DeclaredOrder = order

	return nil
}
