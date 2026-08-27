package resources

import (
	"github.com/cccteam/ccc"
)

type (
	// Station is the tenant-record resource: its route name equals the domain route
	// segment, so /api/stations lists the tenants while /api/stations/{stationID}/...
	// serves the tenant-scoped routes — the natural REST shape for multi-tenant apps.
	// The generator supports this under two validated requirements: a single primary
	// key (its operation paths stay at depth <= 2, domain descents at depth >= 3) and
	// a read-route parameter equal to the domain route parameter (chi permits one
	// wildcard name per position).
	//
	// Station itself is a GLOBAL resource — administering the tenant list is a global
	// concern; the demo's pkg/stations directory stands in for reading this table.
	//
	// Station is structurally enforced (see Ship).
	//
	// @resource
	Station struct {
		ID   ccc.UUID `spanner:"Id"`
		Name string   `spanner:"Name"`
	}
)
