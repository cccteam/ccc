package resources

import (
	"github.com/cccteam/ccc"
)

type (
	// Wing groups squadrons. It is deliberately the BARE resource: a sector-scoped
	// resource whose only field annotation is the mandatory @domain binding. It pins
	// structural fail-closed enforcement — a resource-only grant exposes nothing
	// beyond the primary key.
	//
	// @resource
	// @permissionScope(domain)
	Wing struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain
		SectorID string `spanner:"SectorId"`
		Name     string `spanner:"Name"`
	}
)
