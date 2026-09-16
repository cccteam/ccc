package generation

import (
	"fmt"
	"slices"
	"strings"

	"github.com/cccteam/ccc/resource"
)

// Warning is a schema finding generation raises about a resource it generated:
// informational, never a refusal, since index presence is a performance matter the
// application decides. Every Warning is one of the kinds in this file; a runner prints
// each one on its own line after a successful run (Generator.Warnings).
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
