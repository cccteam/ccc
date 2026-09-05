// Package virtualresources provides the view-backed virtual resources for Lodestar:
// read-only projections over the base tables. A virtual resource has no table
// metadata — its rows come from an embedded subquery, indexed fields are declared
// with index/uniqueindex tags, and its read identity comes from primarykey field
// annotations.
package virtualresources

import (
	_ "embed"

	"github.com/shopspring/decimal"
)

type (
	// FeeByKind rolls booked mission fees up by mission kind across every sector — a
	// headquarters view. Its subquery exercises two virtual-resource variants: a WITH
	// clause and a named subquery parameter (@minFee, bound by Subquery's params map).
	//
	// @virtual
	FeeByKind struct {
		KindID       string          `spanner:"KindId"       uniqueindex:"true"` // @primarykey
		MissionCount int64           `spanner:"MissionCount"`
		TotalFee     decimal.Decimal `spanner:"TotalFee"`
		TopFee       decimal.Decimal `spanner:"TopFee"`
	}
)

//go:embed fee_by_kinds.sql
var feeByKindsSubquery string

// Subquery provides the embedded SQL projection backing this virtual resource. The
// named parameter demonstrates Subquery params (names must not start with "_", which
// is reserved for the runtime's own parameters).
func (FeeByKind) Subquery() (query string, params map[string]any) {
	return feeByKindsSubquery, map[string]any{"minFee": 0}
}
