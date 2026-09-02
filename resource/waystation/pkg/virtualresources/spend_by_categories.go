// Package virtualresources provides the view-backed virtual resources for the
// waystation: read-only projections over the base tables. A virtual resource has no
// table metadata — its rows come from an embedded subquery, indexed fields are
// declared with index/uniqueindex tags, and its read identity comes from primarykey
// field annotations.
package virtualresources

import (
	_ "embed"

	"github.com/shopspring/decimal"
)

type (
	// SpendByCategory rolls approved-and-fulfilled requisition spend up by catalog
	// category. Its subquery exercises two virtual-resource variants: a WITH clause
	// (extractWithClause) and a named subquery parameter
	// (@minLineCost, bound by Subquery's params map).
	//
	// @virtual
	SpendByCategory struct {
		Category      string          `spanner:"Category"      uniqueindex:"true"` // @primarykey
		LineCount     int64           `spanner:"LineCount"`
		TotalQuantity int64           `spanner:"TotalQuantity"`
		TotalSpend    decimal.Decimal `spanner:"TotalSpend"`
	}
)

//go:embed spend_by_categories.sql
var spendByCategoriesSubquery string

// Subquery provides the embedded SQL projection backing this virtual resource. The
// named parameter demonstrates Subquery params (names must not start with "_", which
// is reserved for the runtime's own parameters).
func (SpendByCategory) Subquery() (query string, params map[string]any) {
	return spendByCategoriesSubquery, map[string]any{"minLineCost": 0}
}
