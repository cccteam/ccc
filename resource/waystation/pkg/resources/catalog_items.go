package resources

import (
	"github.com/cccteam/ccc"
	"github.com/shopspring/decimal"
)

type (
	// CatalogItem is the orderable-goods registry: a global resource whose CategoryId
	// is a foreign key to the ItemCategories enum table, giving the gui its enum
	// dropdown, and whose UnitCost is the cell-masking target — roles without an
	// unconditional grant see the cost column masked.
	//
	// @resource
	CatalogItem struct {
		ID   ccc.UUID `spanner:"Id"`
		Sku  string   `spanner:"Sku"  conditions:"immutable"`
		Name string   `spanner:"Name"`
		// @attribute(category)
		CategoryID string `spanner:"CategoryId"`
		// @attribute(unitCost)
		UnitCost decimal.Decimal `spanner:"UnitCost"`
		// @attribute(hazardous)
		Hazardous bool `spanner:"Hazardous"`
	}
)
