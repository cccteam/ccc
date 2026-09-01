package resources

import (
	"github.com/cccteam/ccc"
)

type (
	// Module is a station section (habitat, cargo, reactor...). It is domain-scoped
	// with the tenancy column on the row itself, so the @domain binding is the bare
	// column form. Module is excluded from the consolidated patch handler, keeping a
	// domain-scoped standalone PATCH surface in the demo.
	//
	// Zone is the attribute the equipment condition reaches through a two-hop join
	// path (Equipment -> Facility -> Module.Zone).
	//
	// @resource
	// @permissionScope(domain)
	Module struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain
		WaystationID  string `spanner:"WaystationId"`
		Name          string `spanner:"Name"`
		Zone          string `spanner:"Zone"`
		PressureRated bool   `spanner:"PressureRated"`
	}
)
