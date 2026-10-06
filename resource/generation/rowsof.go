package generation

import (
	"fmt"
	"slices"
	"strings"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/cccteam/ccc/resource/generation/parser/genlang"
	"github.com/go-playground/errors/v5"
)

// The struct-scope @rowsOf: the view declares its backing table, a create goes into
// the table, and the new row shows up in the view on the next list because the view's
// SQL reads that table. What the declaration states is row identity, not columns:
// every row of the view is one row of the named table under the same key, and the
// view's other columns are its own — joined from other tables, aggregated, or computed
// in the projection; none is compared to the table. The generator checks the key, the
// scope, and the outlets, and the generated metadata carries the table's name
// (rowsOf), so a page listing the view knows where its writes and its row navigation
// go. A view without the declaration is a read-only list. One backing table: two
// tables sharing one key (a table and its one-to-one extension) is the door left open
// and not built; a parent joined to its children is never one to one with anything.

// rowsOfDecl records a view's struct-scope @rowsOf; both view kinds carry one.
type rowsOfDecl struct {
	// rowsOfArg is the argument as written, resolved once every kind is extracted
	// (resolveRowsOf); nil when the view declares nothing.
	rowsOfArg *genlang.Arg
	// RowsOf is the resolved table resource's collection name; empty for an
	// undeclared view.
	RowsOf string
}

// HasRowsOf reports whether the view declares, and the generator resolved, the table
// whose rows it carries.
func (d *rowsOfDecl) HasRowsOf() bool {
	return d.RowsOf != ""
}

// declareRowsOf records a view's @rowsOf argument for resolveRowsOf, which runs once
// every kind is extracted.
func declareRowsOf(annotations genlang.StructAnnotations, dest *rowsOfDecl) {
	if !annotations.Struct.Has(rowsOfKeyword) {
		return
	}
	arg := annotations.Struct.Get(rowsOfKeyword)
	dest.rowsOfArg = &arg
}

// rejectRowsOf fails a struct of a kind that is no view: a table-backed resource's
// rows are its own, and an RPC method has none.
func rejectRowsOf(pStruct *parser.Struct, annotations genlang.StructAnnotations, kind string) error {
	if !annotations.Struct.Has(rowsOfKeyword) {
		return nil
	}

	return errors.Newf("struct %s: @%s is only valid on @%s and @%s structs; this %s is not a view over a table", pStruct.Name(), rowsOfKeyword, virtualKeyword, computedKeyword, kind)
}

// keyColumn is one column of a primary key as the check compares it: the Go field,
// its Go type (nullability rides in the type), and the Spanner column, which a
// computed view has none of.
type keyColumn struct {
	Field  string
	Type   string
	Column string
}

// String renders the column for a message: `DockID ccc.UUID (DockId)`, the column
// shown when it is spelled differently from the field.
func (k keyColumn) String() string {
	if k.Column != "" && k.Column != k.Field {
		return fmt.Sprintf("%s %s (%s)", k.Field, k.Type, k.Column)
	}

	return fmt.Sprintf("%s %s", k.Field, k.Type)
}

// describeKey renders a key for a message, its columns in key order.
func describeKey(key []keyColumn) string {
	parts := make([]string, 0, len(key))
	for _, k := range key {
		parts = append(parts, k.String())
	}

	return strings.Join(parts, ", ")
}

// rowsOfView is a view as the @rowsOf checks see it, whichever kind it is.
type rowsOfView struct {
	name    string
	plural  string
	virtual bool
	scope   accesstypes.PermissionScope
	outlets *outletMembership
	key     []keyColumn
	decl    *rowsOfDecl
}

// tableKey is a table-backed resource's primary key in key order.
func tableKey(res *resourceInfo) []keyColumn {
	fields := make([]*resourceField, 0, res.PkCount)
	for _, f := range res.PrimaryKeys() {
		fields = append(fields, f)
	}
	slices.SortStableFunc(fields, func(a, b *resourceField) int {
		return int(a.KeyOrdinalPosition - b.KeyOrdinalPosition)
	})
	key := make([]keyColumn, 0, len(fields))
	for _, f := range fields {
		column, _ := f.LookupTag(spannerTagKey)
		key = append(key, keyColumn{Field: f.Name(), Type: f.Type(), Column: column})
	}

	return key
}

// virtualKey is a virtual resource's declared key in declaration order.
func virtualKey(res *resourceInfo) []keyColumn {
	key := make([]keyColumn, 0, len(res.Fields))
	for _, f := range res.PrimaryKeys() {
		column, _ := f.LookupTag(spannerTagKey)
		key = append(key, keyColumn{Field: f.Name(), Type: f.Type(), Column: column})
	}

	return key
}

// computedKey is a computed resource's declared key in declaration order.
func computedKey(res *computedResource) []keyColumn {
	key := make([]keyColumn, 0, len(res.Fields))
	for _, f := range res.PrimaryKeys() {
		key = append(key, keyColumn{Field: f.Name(), Type: f.Type()})
	}

	return key
}

// resolveRowsOf resolves every view's @rowsOf once every kind is extracted. The named
// resource must be a table-backed @resource: a view cannot back a view, and a struct
// backing an @enumerate table is refused as a contradiction, since its rows are the
// program's constants and a create button for them would be policy about nothing. The
// view's @primarykey fields must be the table's key — the same count, order, Go field
// names, and Go types, and on a virtual the same Spanner columns — the two must share
// a permission scope, and the table must be served on every outlet the view is served
// on, so the metadata's rowsOf names a resource that outlet's client carries. Every
// refusal names the fix.
func (c *client) resolveRowsOf(resources []*resourceInfo, computed []*computedResource) error {
	var errs []error
	for _, res := range resources {
		if res.rowsOfArg == nil || !res.IsVirtual {
			continue
		}
		view := rowsOfView{
			name:    res.Name(),
			plural:  c.pluralize(res.Name()),
			virtual: true,
			scope:   res.PermissionScope,
			outlets: &res.outletMembership,
			key:     virtualKey(res),
			decl:    &res.rowsOfDecl,
		}
		if err := c.resolveViewRowsOf(&view); err != nil {
			errs = append(errs, err)
		}
	}
	for _, res := range computed {
		if res.rowsOfArg == nil {
			continue
		}
		view := rowsOfView{
			name:    res.Name(),
			plural:  c.pluralize(res.Name()),
			scope:   res.PermissionScope,
			outlets: &res.outletMembership,
			key:     computedKey(res),
			decl:    &res.rowsOfDecl,
		}
		if err := c.resolveViewRowsOf(&view); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return errors.Wrapf(errors.Join(errs...), "struct-scope @%s", rowsOfKeyword)
	}

	return nil
}

// resolveViewRowsOf runs the checks for one view and records the table on success.
func (c *client) resolveViewRowsOf(view *rowsOfView) error {
	name := strings.TrimSpace(string(*view.decl.rowsOfArg))
	if strings.ContainsAny(name, ", ") {
		return errors.Newf("struct %s: @%s takes one argument, the table resource whose rows the view carries; got %q", view.name, rowsOfKeyword, name)
	}
	refuse := func(format string, a ...any) error {
		return errors.Newf("struct %s: @%s(%s): %s", view.name, rowsOfKeyword, name, fmt.Sprintf(format, a...))
	}

	if name == view.plural {
		return refuse("the view names itself; name the table resource whose rows it carries")
	}
	if typeName, ok := c.enumerationOf(name); ok {
		return refuse("%s is an enumeration table (@%s on type %s), whose rows are the program's constants; a view over them is a read-only list, so remove the annotation", name, enumerateKeyword, typeName)
	}
	table, err := c.rowsOfTable(name, refuse)
	if err != nil {
		return err
	}

	if err := matchRowsOfKey(view, name, tableKey(table), refuse); err != nil {
		return err
	}

	if view.scope != table.PermissionScope {
		return refuse("the view is %s-scoped and %s is %s-scoped; a view carries its table's permission scope, so declare @%s(%s) on both", scopeName(view.scope), name, scopeName(table.PermissionScope), permissionScopeKeyword, scopeName(table.PermissionScope))
	}

	outlets := view.outlets.OutletNames
	if len(outlets) == 0 {
		outlets = []string{defaultOutletName}
	}
	for _, outlet := range outlets {
		if !table.OnOutlet(outlet) {
			return refuse("the view is served on outlet %q and %s is not; attach %s to the outlet via @%s(%s), or take the view off it", outlet, name, name, outletKeyword, outlet)
		}
	}

	view.decl.RowsOf = name

	return nil
}

// rowsOfTable finds the table-backed resource a declaration names, refusing a view of
// either kind and an unknown name.
func (c *client) rowsOfTable(name string, refuse func(string, ...any) error) (*resourceInfo, error) {
	for _, res := range c.resources {
		if c.pluralize(res.Name()) != name {
			continue
		}
		if res.IsVirtual {
			return nil, refuse("%s is a virtual resource, and a view cannot back a view; name the table resource its rows come from", name)
		}

		return res, nil
	}
	for _, res := range c.computedResources {
		if c.pluralize(res.Name()) == name {
			return nil, refuse("%s is a computed resource, and a view cannot back a view; name the table resource its rows come from", name)
		}
	}

	return nil, refuse("resource %q does not exist; name a table-backed @%s by its collection name", name, resourceKeyword)
}

// matchRowsOfKey checks that the view's declared key is the table's: the same count,
// order, Go field names, and Go types, and on a virtual the same Spanner columns. The
// message names the first mismatch and the table's key.
func matchRowsOfKey(view *rowsOfView, table string, key []keyColumn, refuse func(string, ...any) error) error {
	if len(view.key) == 0 {
		return refuse("the view declares no @%s; declare %s's key on the view, in this order: %s", primarykeyKeyword, table, describeKey(key))
	}
	if len(view.key) != len(key) {
		return refuse("the view's key spans %d columns and %s's spans %d; declare %s's key on the view, in this order: %s", len(view.key), table, len(key), table, describeKey(key))
	}
	for i, want := range key {
		got := view.key[i]
		switch {
		case got.Field != want.Field:
			return refuse("key column %d is field %s on the view and %s on %s; the view's key fields carry %s's Go field names in its key order: %s", i+1, got.Field, want.Field, table, table, describeKey(key))
		case got.Type != want.Type:
			return refuse("key field %s is %s on the view and %s on %s; the view's key fields carry %s's Go types, nullability included: %s", got.Field, got.Type, want.Type, table, table, describeKey(key))
		case view.virtual && got.Column != want.Column:
			return refuse("key field %s reads column %q on the view and %q on %s; the view's projection names %s's key columns: %s", got.Field, got.Column, want.Column, table, table, describeKey(key))
		}
	}

	return nil
}

// scopeName is a permission scope for a message, the generator's global default spelled out.
func scopeName(scope accesstypes.PermissionScope) accesstypes.PermissionScope {
	if scope == "" {
		return accesstypes.GlobalPermissionScope
	}

	return scope
}
