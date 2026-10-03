package resources

import (
	"time"

	"cloud.google.com/go/civil"
	"github.com/cccteam/ccc"
)

type (
	// Consignment is salvaged cargo held in bond until its owner claims it. Mass is
	// filterable via allow_filter (the column is unindexed), ExpiresOn is a date-typed
	// attribute the Supercargo's delete grant conditions on, and ReleasedAt doubles as the
	// lifecycle marker: NULL means still in bond, and both the Supercargo's and the
	// droid's ReleaseConsignment grants carry releasedAt IS NULL, so a second release is
	// policy, not code. BondCode is immutable.
	//
	// The hold is listed by release date, a NULLABLE sort column, in Spanner's own NULL
	// placement: unreleased cargo (NULL) is placed first ascending and last descending, the
	// ORDER BY is the plain direction so an index can serve it, and the cursor walks across
	// the NULL boundary in both directions without a repeated or a skipped row.
	//
	// Served on the default and droids outlets: a droid releases a consignment by API key
	// through the same generated surface humans use.
	//
	// ReleasedAt sits directly after SectorId in the ConsignmentsBySectorIdReleasedAt
	// index (migration 000032): the list binds the sector by equality, so a filter on
	// ReleasedAt alone seeks that index, and its metadata says filterable: always.
	//
	// Demonstrates: allow_filter, @attribute.date, immutable, outlet.shared, paging.nullable-sort, @order, @page, index.tenant-second.
	//
	// @resource
	// @permissionScope(domain)
	// @outlet(default, droids)
	// @order(ReleasedAt desc)
	// @page(default: 10, max: 100)
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
