package resources

import (
	"github.com/cccteam/ccc"
)

type (
	// Facility is a room or bay inside a module. Its table carries no tenancy column:
	// the @domain binding leaves through the ModuleId foreign key and resolves the
	// tenant one hop away (via: names remote Go field segments), the single-hop form
	// of join-path tenancy.
	//
	// @resource
	// @permissionScope(domain)
	Facility struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain(via: WaystationID)
		ModuleID ccc.UUID `spanner:"ModuleId"`
		Name     string   `spanner:"Name"`
		Kind     string   `spanner:"Kind"`
	}
)
