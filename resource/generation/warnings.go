package generation

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/cccteam/ccc/resource"
)

// Warning is a schema finding generation raises about a resource it generated or an
// enumeration it baked: informational, never a refusal, since an index's presence and
// a baked table's size are performance matters the application decides. Every Warning
// is one of the kinds in this file; a runner prints each one on its own line after a
// successful run (Generator.Warnings).
type Warning interface {
	fmt.Stringer
	warning()
}

// IndexWarning says a listed table-backed resource with a bare @domain column and a
// declared @order has no index whose key leads with the tenant column and then the
// order columns, in declared order and direction. Spanner then drives the list from
// the foreign-key backing index on the tenant column and sorts the tenant's whole
// partition for every page. Index is the CREATE INDEX statement the resource wants; the
// primary key is not named because Spanner appends it to every secondary index, which
// is what serves the keyset predicate.
type IndexWarning struct {
	Resource     string
	Table        string
	TenantColumn string
	// Order is the declared order as column names and directions.
	Order []resource.SortField
	Index string
}

func (IndexWarning) warning() {}

// String renders the one-line warning, names verbatim from the schema.
func (w IndexWarning) String() string {
	return fmt.Sprintf("%s lists in %s order with no index leading with those columns, so every page sorts the tenant's partition; wanted: %s",
		w.Resource, strings.Join(indexKeyColumns(w.TenantColumn, w.Order), ", "), w.Index)
}

// JoinPathWarning says a listed table-backed resource resolves its tenant through
// @domain(via: ...): its lists scan the whole table, every tenant, with a lookup per row
// up the path, and no index on the table changes that because no column on the row
// names the tenant. Column is the foreign key the path leaves through and Path its hops.
type JoinPathWarning struct {
	Resource string
	Table    string
	Column   string
	Path     []resource.BindingHop
}

func (JoinPathWarning) warning() {}

// String renders the one-line warning, names verbatim from the schema.
func (w JoinPathWarning) String() string {
	path := make([]string, 0, len(w.Path)+1)
	path = append(path, w.Column)
	for _, hop := range w.Path {
		path = append(path, hop.Table+"."+hop.Column)
	}

	return fmt.Sprintf("%s resolves its tenant through %s, so its lists scan all of %s, every tenant, and no index on %s changes that; a table listed at volume carries the tenant key on the row",
		w.Resource, strings.Join(path, ", "), w.Table, w.Table)
}

// maxBakedEnumerationRows is the row count above which an @enumerate table raises an
// EnumerationSizeWarning. Every field keyed into the table carries the whole table in
// the TypeScript metadata, a copy per field, and at about fifty bytes a row 500 rows
// is some 25 KB each; past that a runtime resource serves a picker better.
const maxBakedEnumerationRows = 500

// EnumerationSizeWarning says an @enumerate table holds more than
// maxBakedEnumerationRows rows: every field keyed into the table carries all of them in
// the TypeScript metadata, Bytes of them, one copy per field. Fields are the
// Struct.Field names that bake the table, in extraction order (resources, computed
// resources, RPC methods); empty when none keys into it yet, since the first one bakes
// every row. The alternative is a runtime resource: drop @enumerate from the type (the
// generated constants go with it) and expose the table with a @resource struct, whose
// picker reads it whole with no @page maximum or paged under one.
type EnumerationSizeWarning struct {
	Type   string
	Table  string
	Rows   int
	Bytes  int
	Fields []string
}

func (EnumerationSizeWarning) warning() {}

// String renders the one-line warning, names verbatim from the schema.
func (w EnumerationSizeWarning) String() string {
	fields := "none yet, and the first one bakes every row"
	if len(w.Fields) > 0 {
		fields = strings.Join(w.Fields, ", ")
	}

	return fmt.Sprintf("%s enumerates %d rows of %s, above the %d a baked table may hold without a warning, so %d bytes of enumeration ride in the metadata of each field keyed into it: %s; drop @%s from %s (the generated constants go with it) and expose %s with a @%s struct, whose picker reads it whole with no @%s maximum or paged under one",
		w.Type, w.Rows, w.Table, maxBakedEnumerationRows, w.Bytes, fields, enumerateKeyword, w.Type, w.Table, resourceKeyword, pageKeyword)
}

// schemaWarnings reads the extracted table-backed resources against the table map for
// the two shapes the perf study measured (README section 9): a listed bare-tenant
// resource whose declared order no index serves, and a listed join-path resource. A
// virtual resource is out (its index facts are tags, not schema), so is a resource whose
// list handler is suppressed, and so is a bare-tenant resource with no @order, whose
// sort-less list is not sorted and is served by the tenant column's foreign-key backing
// index.
// Warnings come in resource-name order.
func (c *client) schemaWarnings(resources []*resourceInfo) []Warning {
	sorted := slices.Clone(resources)
	sortResources(sorted)

	var warnings []Warning
	for _, res := range sorted {
		if res.IsVirtual || res.ListHandlerDisabled() || res.DomainBinding == nil {
			continue
		}
		table := c.pluralize(res.Name())

		if len(res.DomainBinding.Path) > 0 {
			warnings = append(warnings, JoinPathWarning{
				Resource: res.Name(),
				Table:    table,
				Column:   fieldColumn(res.DomainBinding.Anchor),
				Path:     collectionHops(res.DomainBinding.Path),
			})

			continue
		}

		if len(res.DeclaredOrder) == 0 {
			continue
		}

		tenantColumn := fieldColumn(res.DomainBinding.Anchor)
		order := declaredOrderColumns(res)
		if meta, ok := c.tableMap[table]; ok && meta.servesOrder(tenantColumn, order) {
			continue
		}

		warnings = append(warnings, IndexWarning{
			Resource:     res.Name(),
			Table:        table,
			TenantColumn: tenantColumn,
			Order:        order,
			Index:        wantedIndex(table, tenantColumn, order),
		})
	}

	return warnings
}

// declaredOrderColumns is the resource's @order as schema column names and directions.
func declaredOrderColumns(res *resourceInfo) []resource.SortField {
	byName := make(map[string]*resourceField, len(res.Fields))
	for _, field := range res.Fields {
		byName[field.Name()] = field
	}

	order := make([]resource.SortField, 0, len(res.DeclaredOrder))
	for _, sf := range res.DeclaredOrder {
		field, ok := byName[sf.Field]
		if !ok {
			continue
		}
		order = append(order, resource.SortField{Field: fieldColumn(field), Direction: sf.Direction})
	}

	return order
}

// servesOrder reports whether some index of the table that is not null-filtered has a
// key whose leading columns are the tenant column, then each order column in declared
// order with matching direction; trailing key columns are allowed. The primary key is
// never required in the key, since Spanner appends it to every secondary index, and the
// PRIMARY_KEY index counts like any other.
func (t *tableMetadata) servesOrder(tenantColumn string, order []resource.SortField) bool {
	wanted := make([]indexColumn, 0, len(order)+1)
	wanted = append(wanted, indexColumn{Column: tenantColumn})
	for _, sf := range order {
		wanted = append(wanted, indexColumn{Column: sf.Field, Descending: sf.Direction == resource.SortDescending})
	}

	for _, index := range t.Indexes {
		if index.NullFiltered || len(index.Key) < len(wanted) {
			continue
		}
		if slices.Equal(index.Key[:len(wanted)], wanted) {
			return true
		}
	}

	return false
}

// wantedIndex is the CREATE INDEX statement that serves the order: the tenant column
// then the order columns with their directions, named <Table>By<Columns>.
func wantedIndex(table, tenantColumn string, order []resource.SortField) string {
	name := table + "By" + tenantColumn
	for _, sf := range order {
		name += sf.Field
	}

	return fmt.Sprintf("CREATE INDEX %s ON %s(%s)", name, table, strings.Join(indexKeyColumns(tenantColumn, order), ", "))
}

// indexKeyColumns renders the tenant column and the order columns as index key terms:
// the column name, followed by DESC where the order says so.
func indexKeyColumns(tenantColumn string, order []resource.SortField) []string {
	terms := make([]string, 0, len(order)+1)
	terms = append(terms, tenantColumn)
	for _, sf := range order {
		term := sf.Field
		if sf.Direction == resource.SortDescending {
			term += " " + descendingOrdering
		}
		terms = append(terms, term)
	}

	return terms
}

// enumerationWarnings reads every @enumerate table the run registered
// (registerEnumerations) against the rows it fetched for it: a table above
// maxBakedEnumerationRows warns once, whether or not a field keys into it yet, naming
// the fields that bake it (enumerationReferences). Warnings come in table-name order.
func (c *client) enumerationWarnings() []Warning {
	references := c.enumerationReferences()

	var warnings []Warning
	for _, table := range slices.Sorted(maps.Keys(c.enumerateTables)) {
		if w, ok := enumerationSizeWarning(c.enumerateTables[table], table, c.enumValues[table], references[table]); ok {
			warnings = append(warnings, w)
		}
	}

	return warnings
}

// enumerationReferences lists, per @enumerate table, the Struct.Field names whose
// metadata bakes its rows, in extraction order: a table-backed field whose foreign key
// targets the table, which the TypeScript metadata renders inline
// (resolveKeyEnumeration), and every resource, computed, or RPC field whose
// field-scope @enumerate named the table (resolveFieldEnumerations,
// declareRPCEnumeration).
func (c *client) enumerationReferences() map[string][]string {
	references := make(map[string][]string)
	add := func(table, structName, fieldName string) {
		references[table] = append(references[table], structName+"."+fieldName)
	}
	for _, res := range c.resources {
		for _, field := range res.Fields {
			switch {
			case field.Enumeration != "":
				add(field.declaredResource, res.Name(), field.Name())
			case field.IsForeignKey:
				if _, ok := c.enumerationOf(field.ReferencedResource); ok {
					add(field.ReferencedResource, res.Name(), field.Name())
				}
			}
		}
	}
	for _, res := range c.computedResources {
		for _, field := range res.Fields {
			if field.Enumeration != "" {
				add(field.enumeratedResource, res.Name(), field.Name())
			}
		}
	}
	for _, method := range c.rpcMethods {
		for _, field := range method.Fields {
			if field.Enumeration != "" {
				add(field.enumeratedResource, method.Name(), field.Name())
			}
		}
	}

	return references
}

// enumerationSizeWarning reads one @enumerate table's rows against the line: above
// maxBakedEnumerationRows it answers the warning, with the bytes the metadata literal
// carries (enumerationLiteral) and the referencing fields as given.
func enumerationSizeWarning(typeName, table string, values []*enumData, fields []string) (EnumerationSizeWarning, bool) {
	if len(values) <= maxBakedEnumerationRows {
		return EnumerationSizeWarning{}, false
	}

	return EnumerationSizeWarning{
		Type:   typeName,
		Table:  table,
		Rows:   len(values),
		Bytes:  len(enumerationLiteral(values)),
		Fields: fields,
	}, true
}
