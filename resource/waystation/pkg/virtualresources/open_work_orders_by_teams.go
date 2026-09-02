package virtualresources

import (
	"time"

	_ "embed"

	"github.com/cccteam/ccc"
)

type (
	// OpenWorkOrdersByTeam is the domain-scoped virtual resource: per-team counts of
	// open work orders. Its permission registrations use the domain scope, so it is
	// served under /api/waystations/{waystationID}/... and checked in that station's
	// permission partition.
	//
	// @virtual
	// @permissionScope(domain)
	OpenWorkOrdersByTeam struct {
		TeamID   ccc.UUID `spanner:"TeamId"   uniqueindex:"true"` // @primarykey
		TeamName string   `spanner:"TeamName" index:"true"`
		// @domain
		WaystationID string     `spanner:"WaystationId" index:"true"`
		OpenOrders   int64      `spanner:"OpenOrders"`
		NextDue      *time.Time `spanner:"NextDue"`
	}
)

//go:embed open_work_orders_by_teams.sql
var openWorkOrdersByTeamsSubquery string

// Subquery provides the embedded SQL projection backing this virtual resource.
func (OpenWorkOrdersByTeam) Subquery() (query string, params map[string]any) {
	return openWorkOrdersByTeamsSubquery, nil
}
