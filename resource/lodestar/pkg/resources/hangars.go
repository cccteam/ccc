package resources

import "github.com/cccteam/ccc"

type (
	// Hangar is where ships park. It is sector-scoped with the tenancy column on the row
	// itself (the bare @domain form), and it is excluded from the consolidated patch
	// handler, keeping a sector-scoped standalone PATCH surface in the demo.
	//
	// Zone is the attribute Ship reaches through a one-hop join path (hangarZone !=
	// 'quarantine').
	//
	// Ships reference their hangar with no cascade, so deleting a hangar that still holds
	// ships is refused at commit; the refusal answers 409 naming Hangars, never the ships.
	//
	// Demonstrates: @domain, consolidation.exclusion, commit.referential-refusal.
	//
	// @resource
	// @permissionScope(domain)
	// @order(Name asc)
	// @page(default: 25, max: 200)
	Hangar struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain
		SectorID string `spanner:"SectorId"`
		Name     string `spanner:"Name"`
		Zone     string `spanner:"Zone"`
	}
)
