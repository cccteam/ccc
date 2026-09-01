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
	// ContactEmail is additionally allow_filter: PII fields may never be filtered
	// through the URL (the value would land in access logs), but a POST-body filter
	// is accepted — the query-parameter suite pins both directions.
	//
	// @resource
	Supplier struct {
		ID           ccc.UUID `spanner:"Id"`
		Name         string   `spanner:"Name"`
		ContactName  string   `spanner:"ContactName"  conditions:"pii"`
		ContactEmail string   `spanner:"ContactEmail" allow_filter:"true" conditions:"pii"`
		// @attribute(active)
		Active bool `spanner:"Active"`
	}
)
