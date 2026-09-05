package resources

import (
	"github.com/cccteam/ccc"
	"github.com/shopspring/decimal"
)

type (
	// SortieExpense is the SECOND hop: fuel, supplies, and tow gear booked against a
	// sortie. @stateRoot(Mission) on SortieId declares its immediate hop, and the
	// chain resolver composes Sortie's hop to reach the root, so the Quartermaster's
	// `state = 'underway'` grant evaluates two hops deep — through Sortie to Mission.
	// Tenancy runs the same two hops (Sortie → Mission.SectorId).
	//
	// @resource
	// @permissionScope(domain)
	SortieExpense struct {
		ID ccc.UUID `spanner:"Id"`
		// @stateRoot(Mission)
		// @domain(via: MissionID.SectorID)
		SortieID ccc.UUID `spanner:"SortieId"`
		Category string   `spanner:"Category"`
		// @attribute(amount)
		Amount decimal.Decimal `spanner:"Amount"`
		Note   *string         `spanner:"Note"`
	}
)
