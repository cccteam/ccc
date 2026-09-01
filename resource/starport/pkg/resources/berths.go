package resources

import (
	"github.com/cccteam/ccc"
)

type (
	// Berth is the domain-scoped demo resource: its permission registrations use the
	// domain scope (the domain is the station in the URL), so every check runs inside
	// one station's permission partition, and the mandatory @domain binding on
	// StationId partitions the rows themselves (structural row tenancy, E2) — the
	// column is stamped from the URL's station on create and injected into every
	// query as the tenant predicate.
	//
	// Berth is structurally enforced (see Ship), so the domain-partition integration
	// suite is invariant across the fail-open to fail-closed migration.
	//
	// @resource
	// @permissionScope(domain)
	Berth struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain
		StationID   string   `spanner:"StationId"`
		Designation string   `spanner:"Designation" conditions:"immutable"`
		SizeClass   int64    `spanner:"SizeClass"`
		Occupied    bool     `spanner:"Occupied"`
	}
)
