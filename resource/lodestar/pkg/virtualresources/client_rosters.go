package virtualresources

import (
	_ "embed"

	"github.com/cccteam/ccc"
)

type (
	// ClientRoster is the view a sector books clients from: every client, in every
	// sector, with the columns a booking picker wants and the Clients table lacks — how
	// many contacts the client has and how many missions it holds in this sector. It is
	// keyed by the client id alone, so its rows key into the same space as Clients, and
	// Mission.ClientID names it with a field-scope @enumerate: the schema's foreign key
	// stays the guard at write time, and the picker lists this view instead of the
	// target. The bare @domain on SectorId partitions the rows, so the roster a picker
	// shows is the selected sector's. Mission is served on the portal outlet too, and a
	// declared enumeration must be on every outlet its field is, so the roster is as well.
	//
	// The roster declares a maximum page size, so it is a bounded picker source: the
	// picker pages it one server page at a time and reads the chosen client by key, and
	// the view serves that read through the keyed read route its @primarykey gives it
	// (a view with no key lists only). A bounded source with a suppressed read fails
	// generation naming the two ways out.
	//
	// Demonstrates: @enumerate.key-view, @virtual, virtual.domain, virtual.keyed-read, outlet.shared.
	//
	// @virtual
	// @permissionScope(domain)
	// @outlet(default, portal)
	// @order(Name asc)
	// @page(default: 25, max: 200)
	ClientRoster struct {
		// @primarykey
		ID ccc.UUID `spanner:"Id" index:"true"`
		// @domain
		SectorID       string `spanner:"SectorId"       index:"true"`
		Name           string `spanner:"Name"           index:"true"`
		Trusted        bool   `spanner:"Trusted"`
		ContactCount   int64  `spanner:"ContactCount"`
		SectorMissions int64  `spanner:"SectorMissions"`
	}
)

//go:embed client_rosters.sql
var clientRostersSubquery string

// Subquery provides the embedded SQL projection backing this virtual resource.
func (ClientRoster) Subquery() (query string, params map[string]any) {
	return clientRostersSubquery, nil
}
