package virtualresources

import (
	"time"

	_ "embed"

	"github.com/cccteam/ccc"
)

type (
	// OpenMissionsBySquadron is the sector-scoped virtual resource: per-squadron counts
	// of missions still moving. Its permission registrations use the domain scope, so
	// it is served under /api/sectors/{sectorID}/... and checked in that sector's
	// partition, and its bare @domain on a view column partitions the projection's
	// rows. The key is compound: the squadron and its sector.
	//
	// @virtual
	// @permissionScope(domain)
	OpenMissionsBySquadron struct {
		SquadronID   ccc.UUID `spanner:"SquadronId"   index:"true"` // @primarykey
		SquadronName string   `spanner:"SquadronName" index:"true"`
		// @domain
		SectorID     string     `spanner:"SectorId"     index:"true"` // @primarykey
		OpenMissions int64      `spanner:"OpenMissions"`
		NextDeadline *time.Time `spanner:"NextDeadline"`
	}
)

//go:embed open_missions_by_squadrons.sql
var openMissionsBySquadronsSubquery string

// Subquery provides the embedded SQL projection backing this virtual resource.
func (OpenMissionsBySquadron) Subquery() (query string, params map[string]any) {
	return openMissionsBySquadronsSubquery, nil
}
