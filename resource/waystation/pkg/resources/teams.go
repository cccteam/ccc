package resources

import (
	"github.com/cccteam/ccc"
)

type (
	// Team is deliberately minimal: a domain-scoped resource whose only field
	// annotation is the mandatory @domain binding. It pins structural fail-closed
	// enforcement — a resource-only grant exposes nothing beyond the primary key.
	//
	// @resource
	// @permissionScope(domain)
	Team struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain
		WaystationID string `spanner:"WaystationId"`
		Name         string `spanner:"Name"`
		Specialty    string `spanner:"Specialty"`
	}
)
