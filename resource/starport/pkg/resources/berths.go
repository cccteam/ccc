package resources

import (
	"github.com/cccteam/ccc"
)

type (
	// Berth is the domain-scoped demo resource: its permission registrations use the
	// domain scope (the domain is the station in the URL), so every check runs inside
	// one station's permission partition. The table is deliberately domain-BLIND — no
	// StationId column — pinning that @permissionScope partitions permissions, not
	// data; structural row tenancy is a separate, later change.
	//
	// Berth is structurally enforced (see Ship), so the domain-partition integration
	// suite is invariant across the fail-open to fail-closed migration.
	//
	// @resource
	// @permissionScope(domain)
	Berth struct {
		ID          ccc.UUID `spanner:"Id"`
		Designation string   `spanner:"Designation" conditions:"immutable"`
		SizeClass   int64    `spanner:"SizeClass"`
		Occupied    bool     `spanner:"Occupied"`
	}
)
