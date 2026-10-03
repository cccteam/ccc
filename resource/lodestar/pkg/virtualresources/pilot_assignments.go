package virtualresources

import (
	_ "embed"

	"github.com/cccteam/ccc"
)

type (
	// PilotAssignment is the pilot page's list: one row per membership of the pilot in
	// the selected sector, carrying the squadron's and the wing's names; the squadron id
	// names Squadrons so a row opens the squadron's page. It is the second view over
	// SquadronMemberships, and the two together show the direction of the declaration:
	// each view names the table, and the table names no view. The view declares its
	// backing table, a create goes into the table, and the new row shows up in the view
	// on the next list because the view's SQL reads that table.
	//
	// Demonstrates: @rowsOf, @rowsOf.association, @enumerate, list.row-route, @virtual, virtual.domain.
	//
	// @virtual
	// @rowsOf(SquadronMemberships)
	// @permissionScope(domain)
	// @order(SquadronName asc)
	// @page(default: 25, max: 200)
	PilotAssignment struct {
		// @primarykey
		// @enumerate(Squadrons)
		SquadronID ccc.UUID `spanner:"SquadronId" index:"true"`
		// @primarykey
		UserID string `spanner:"UserId" index:"true"`
		// @domain
		SectorID     string `spanner:"SectorId"     index:"true"`
		SquadronName string `spanner:"SquadronName" index:"true"`
		WingName     string `spanner:"WingName"     index:"true"`
	}
)

//go:embed pilot_assignments.sql
var pilotAssignmentsSubquery string

// Subquery provides the embedded SQL projection backing this virtual resource.
func (PilotAssignment) Subquery() (query string, params map[string]any) {
	return pilotAssignmentsSubquery, nil
}
