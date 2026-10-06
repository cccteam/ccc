package virtualresources

import (
	_ "embed"

	"github.com/cccteam/ccc"
)

type (
	// SquadronRoster is the squadron page's list: one row per membership, carrying the
	// pilot's display name from the personnel registry and the pilot's id, which names
	// Pilots so a row opens the pilot's page. The view declares its backing table, a
	// create goes into the table, and the new row shows up in the view on the next list
	// because the view's SQL reads that table. Its key is SquadronMemberships' compound
	// key under the table's own column names, so a row is deleted from the list through
	// the table and a create associates a pilot through the table's form. The bare
	// @domain on SectorId partitions the rows the way the table's two-hop tenancy does.
	//
	// Demonstrates: @rowsOf, @rowsOf.association, @enumerate, list.row-route, @virtual, virtual.domain.
	//
	// @virtual
	// @rowsOf(SquadronMemberships)
	// @permissionScope(domain)
	// @order(PilotName asc)
	// @page(default: 25, max: 200)
	SquadronRoster struct {
		// @primarykey
		SquadronID ccc.UUID `spanner:"SquadronId" index:"true"`
		// @primarykey
		UserID string `spanner:"UserId" index:"true"`
		// @domain
		SectorID     string  `spanner:"SectorId"     index:"true"`
		SquadronName string  `spanner:"SquadronName" index:"true"`
		PilotName    *string `spanner:"PilotName"    index:"true"`
		// @enumerate(Pilots)
		PilotID ccc.NullUUID `spanner:"PilotId"`
	}
)

//go:embed squadron_rosters.sql
var squadronRostersSubquery string

// Subquery provides the embedded SQL projection backing this virtual resource.
func (SquadronRoster) Subquery() (query string, params map[string]any) {
	return squadronRostersSubquery, nil
}
