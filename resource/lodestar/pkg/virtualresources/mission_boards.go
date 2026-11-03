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
	// DaysLeft is computed in the SQL and indexed by nothing, so it is allow_filter:
	// its metadata says filterable: withIndexed, and the browser offers a days-left
	// filter only beside one on an indexed column, the rule the database parse enforces.
	//
	// The board's box searches two tables' indexes: Title names Missions.Title and
	// ClientName names Clients.Name, the SQL projects both token columns, and a box over
	// the board gathers each term's key set per index and joins it back on the key; the
	// box then joins the rest of the filter with AND alone.
	//
	// The board is live without a declaration: the generator reads the tables its query
	// names off mission_boards.sql (Missions, Clients, Squadrons), writes them into the
	// handler package's live derivations, and marks the board published in its metadata.
	// A mission's write reaches the board's row and list subscribers under the mission's
	// own key, as Missions' own do; a client's or a squadron's write reaches every
	// subscriber of the board as one resource document, in every sector for Clients,
	// which is global.
	//
	// Demonstrates: @rowsOf, @rowsOf.same-row, @virtual, virtual.domain, metadata.filterable, search.view, search.key-set, live.derived.
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
		Title        string    `spanner:"Title"        index:"true"        search:"Missions.Title"`
		ClientName   string    `spanner:"ClientName"   index:"true"        search:"Clients.Name"`
		SquadronName *string   `spanner:"SquadronName" index:"true"`
		KindID       string    `spanner:"KindId"       index:"true"`
		StatusID     string    `spanner:"StatusId"     index:"true"`
		Deadline     time.Time `spanner:"Deadline"     index:"true"`
		DaysLeft     int64     `spanner:"DaysLeft"     allow_filter:"true"`
	}
)

//go:embed mission_boards.sql
var missionBoardsSubquery string

// Subquery provides the embedded SQL projection backing this virtual resource.
func (MissionBoard) Subquery() (query string, params map[string]any) {
	return missionBoardsSubquery, nil
}
