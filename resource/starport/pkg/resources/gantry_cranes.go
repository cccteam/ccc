package resources

import (
	"github.com/cccteam/ccc"
)

type (
	// GantryCrane is the domain-scoped resource INSIDE the consolidated patch set
	// (contrast with Berth, which is deliberately excluded from consolidation so the
	// standalone domain-scoped surface stays exercised): its consolidated operations
	// carry the station in the operation path (/stations/{stationID}/gantry-cranes/...),
	// and every operation is checked in that station's permission partition. Like
	// Berth, the mandatory @domain binding on StationId partitions the rows
	// themselves (structural row tenancy, E2).
	//
	// GantryCrane is structurally enforced (see Ship).
	//
	// GantryCranes are also served on the automation outlet, putting a domain-scoped
	// consolidated resource behind the machine REST API: the automation outlet gets
	// its own DomainGuard-wrapped routes and its consolidated dispatcher gets the
	// domain descent case.
	//
	// @resource
	// @permissionScope(domain)
	// @outlet(default, automation)
	GantryCrane struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain
		StationID   string   `spanner:"StationId"`
		Callsign    string   `spanner:"Callsign"`
		LiftTonnage int64    `spanner:"LiftTonnage"`
		Operational bool     `spanner:"Operational"`
	}
)
