package virtualresources

import (
	"time"

	_ "embed"

	"github.com/cccteam/ccc"
)

type (
	// MissionBoard is the Missions console page's list: every mission in the sector with
	// the client's name, the assigned squadron's name, and the days left to its deadline,
	// computed in the SQL. The view declares its backing table, a create goes into the
	// table, and the new row shows up in the view on the next list because the view's
	// SQL reads that table. Its key is the mission's own id under the table's column
	// name, so a row opens the mission's page, an edit patches Missions, and a delete
	// removes the mission; the view's other columns are its own and never written.
	//
	// Demonstrates: @rowsOf, @rowsOf.same-row, @virtual, virtual.domain.
	//
	// @virtual
	// @rowsOf(Missions)
	// @permissionScope(domain)
	// @order(Deadline asc)
	// @page(default: 25, max: 200)
	MissionBoard struct {
		// @primarykey
		ID ccc.UUID `spanner:"Id" index:"true"`
		// @domain
		SectorID     string    `spanner:"SectorId"     index:"true"`
		Title        string    `spanner:"Title"        index:"true"`
		ClientName   string    `spanner:"ClientName"   index:"true"`
		SquadronName *string   `spanner:"SquadronName" index:"true"`
		KindID       string    `spanner:"KindId"       index:"true"`
		StatusID     string    `spanner:"StatusId"     index:"true"`
		Deadline     time.Time `spanner:"Deadline"     index:"true"`
		DaysLeft     int64     `spanner:"DaysLeft"`
	}
)

//go:embed mission_boards.sql
var missionBoardsSubquery string

// Subquery provides the embedded SQL projection backing this virtual resource.
func (MissionBoard) Subquery() (query string, params map[string]any) {
	return missionBoardsSubquery, nil
}
