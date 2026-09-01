package resources

import (
	"github.com/cccteam/ccc"
	"github.com/shopspring/decimal"
)

type (
	// RequisitionLine is a line item: interleaved in Requisitions with a compound
	// primary key and a @stateRoot member of the Requisition workflow. The requester
	// role's Create/Update/Delete grants on lines carry `state = 'draft'` — the
	// moment a requisition is submitted, its lines go structurally read-only, proven
	// by the in-transaction check-SELECT rather than any handler code.
	//
	// UnitCostSnapshot is the auditor-facing masking target: their Read grant on it
	// is conditioned on the requisition being approved.
	//
	// @resource
	// @permissionScope(domain)
	RequisitionLine struct {
		// The parent-key column is named Id because Spanner interleaving requires the
		// child's leading key column to carry the parent's key column name; the Go
		// field keeps the readable name.
		//
		// @stateRoot(Requisition)
		RequisitionID    ccc.UUID        `spanner:"Id"`
		LineNumber       int64           `spanner:"LineNumber"`
		CatalogItemID    ccc.UUID        `spanner:"CatalogItemId"`
		Quantity         int64           `spanner:"Quantity"`
		UnitCostSnapshot decimal.Decimal `spanner:"UnitCostSnapshot"`
	}
)
