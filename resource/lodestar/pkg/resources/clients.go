package resources

import (
	"github.com/cccteam/ccc"
)

type (
	// Client is who books missions: a global resource with PII contact fields and a
	// bool attribute. The ClientBrowser role lists clients under a `trusted = true`
	// condition, so untrusted outfits disappear from booking pickers while
	// headquarters (unconditional grants) still sees and manages them.
	//
	// Client is excluded from the consolidated patch handler, keeping a global
	// standalone PATCH surface in the demo.
	//
	// ContactEmail is additionally allow_filter: PII fields may never be filtered
	// through the URL (the value would land in access logs), but a POST-body filter
	// is accepted — the query-parameter suite pins both directions.
	//
	// @resource
	Client struct {
		ID           ccc.UUID `spanner:"Id"`
		Name         string   `spanner:"Name"`
		ContactName  string   `spanner:"ContactName"  conditions:"pii"`
		ContactEmail string   `spanner:"ContactEmail" allow_filter:"true" conditions:"pii"`
		// @attribute(trusted)
		Trusted bool `spanner:"Trusted"`
	}
)
