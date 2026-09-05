package resources

import (
	"github.com/cccteam/ccc"
)

type (
	// RefitTask is a checklist item: interleaved in Refits with a compound primary
	// key, so creates supply the full key (no server-generated UUID). The @stateRoot
	// marker on the anchoring foreign key makes it a member of the Refit workflow one
	// hop from the root, and its tenancy is THREE hops away: Refit → Ship → Hangar →
	// SectorId.
	//
	// @resource
	// @permissionScope(domain)
	RefitTask struct {
		// The parent-key column is named Id because Spanner interleaving requires the
		// child's leading key column to carry the parent's key column name; the Go
		// field keeps the readable name.
		//
		// @stateRoot(Refit)
		// @domain(via: ShipID.HangarID.SectorID)
		RefitID      ccc.UUID `spanner:"Id"`
		TaskNumber   int64    `spanner:"TaskNumber"`
		Instructions string   `spanner:"Instructions"`
		Done         bool     `spanner:"Done"`
		Notes        *string  `spanner:"Notes"`
	}
)
