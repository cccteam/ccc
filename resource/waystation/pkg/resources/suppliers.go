package resources

import (
	"github.com/cccteam/ccc"
)

type (
	// Supplier is a global resource with PII contact fields and a bool attribute:
	// the requester role lists suppliers under an `active = true` condition, so
	// deactivated vendors disappear from purchasing flows while procurement staff
	// (unconditional grants) still see and manage them.
	//
	// Supplier is excluded from the consolidated patch handler, keeping a global
	// standalone PATCH surface in the demo.
	//
	// @resource
	Supplier struct {
		ID           ccc.UUID `spanner:"Id"`
		Name         string   `spanner:"Name"`
		ContactName  string   `spanner:"ContactName"  conditions:"pii"`
		ContactEmail string   `spanner:"ContactEmail" conditions:"pii"`
		// @attribute(active)
		Active bool `spanner:"Active"`
	}
)
