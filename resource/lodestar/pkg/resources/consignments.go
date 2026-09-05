package resources

import (
	"time"

	"cloud.google.com/go/civil"
	"github.com/cccteam/ccc"
)

type (
	// Consignment is salvaged cargo held in bond until its owner claims it. Mass is
	// filterable via allow_filter (the column is unindexed), ExpiresOn is a date-typed
	// attribute the Supercargo's delete grant conditions on, and ReleasedAt doubles
	// as the lifecycle marker: NULL means still in bond, and both the Supercargo's
	// and the droid's ReleaseConsignment grants carry `releasedAt IS NULL`, so a
	// second release is policy, not code. BondCode is immutable.
	//
	// Served on the default and droids outlets: a droid releases a consignment by API
	// key through the same generated surface humans use.
	//
	// @resource
	// @permissionScope(domain)
	// @outlet(default, droids)
	Consignment struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain
		SectorID    string   `spanner:"SectorId"`
		ClientID    ccc.UUID `spanner:"ClientId"`
		BondCode    string   `spanner:"BondCode"    conditions:"immutable"`
		Description string   `spanner:"Description"`
		Mass        float64  `spanner:"Mass"        allow_filter:"true"`
		// @attribute(expiresOn)
		ExpiresOn civil.Date `spanner:"ExpiresOn"`
		// @attribute(releasedAt)
		ReleasedAt *time.Time `spanner:"ReleasedAt"`
	}
)
