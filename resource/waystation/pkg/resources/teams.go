package resources

import (
	"github.com/cccteam/ccc"
)

type (
	// Team is deliberately bare: a domain-scoped resource with no field annotations
	// and no bindings. It pins two things at once — structural fail-closed
	// enforcement (a resource-only grant exposes nothing beyond the primary key) and
	// that @permissionScope partitions permissions even when the table carries a
	// tenancy column the framework was never told about.
	//
	// @resource
	// @permissionScope(domain)
	Team struct {
		ID           ccc.UUID `spanner:"Id"`
		WaystationID string   `spanner:"WaystationId"`
		Name         string   `spanner:"Name"`
		Specialty    string   `spanner:"Specialty"`
	}
)
