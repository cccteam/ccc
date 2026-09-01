package resources

import (
	"cloud.google.com/go/civil"
	"github.com/cccteam/ccc"
)

type (
	// InventoryLot is stock on hand: Quantity is filterable via allow_filter (the
	// column is unindexed), and ExpiresOn is a date-typed attribute the
	// quartermaster's delete grant conditions on — expired lots can be purged,
	// unexpired stock cannot, enforced by the delete path's check-SELECT against
	// the base-resource decision.
	//
	// @resource
	// @permissionScope(domain)
	InventoryLot struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain
		WaystationID  string   `spanner:"WaystationId"`
		CatalogItemID ccc.UUID `spanner:"CatalogItemId"`
		Quantity      int64    `spanner:"Quantity"      allow_filter:"true"`
		// @attribute(expiresOn)
		ExpiresOn   *civil.Date `spanner:"ExpiresOn"`
		BinLocation string      `spanner:"BinLocation"`
	}
)
