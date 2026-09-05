package resources

import (
	"github.com/cccteam/ccc"
)

type (
	// Hangar is where ships park. It is sector-scoped with the tenancy column on the
	// row itself (the bare @domain form), and it is excluded from the consolidated
	// patch handler, keeping a sector-scoped standalone PATCH surface in the demo.
	//
	// Zone is the attribute Ship reaches through a one-hop join path
	// (`hangarZone != 'quarantine'`).
	//
	// @resource
	// @permissionScope(domain)
	Hangar struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain
		SectorID string `spanner:"SectorId"`
		Name     string `spanner:"Name"`
		Zone     string `spanner:"Zone"`
	}
)
