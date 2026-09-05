package resources

import (
	"time"

	"github.com/cccteam/ccc"
)

type (
	// Sortie is one flight on a mission and the FIRST hop of the mission workflow's
	// membership chain: @stateRoot on the anchoring foreign key makes it a member, so
	// the uniform `state` binding is synthesized as a join path through MissionId and
	// `state = 'underway'` reads identically here and on the root. Its tenancy is its
	// mission's sector, declared as a join-path @domain through the same key.
	//
	// Sortie has its own UUID key and an ordinary foreign key rather than being
	// interleaved, so the second hop (SortieExpense) is a single-column foreign key.
	//
	// @resource
	// @permissionScope(domain)
	Sortie struct {
		ID ccc.UUID `spanner:"Id"`
		// @stateRoot(Mission)
		// @domain(via: SectorID)
		MissionID   ccc.UUID   `spanner:"MissionId"`
		ShipID      ccc.UUID   `spanner:"ShipId"`
		PilotUserID string     `spanner:"PilotUserId"`
		LaunchedAt  time.Time  `spanner:"LaunchedAt"`
		ReturnedAt  *time.Time `spanner:"ReturnedAt"`
		Debrief     *string    `spanner:"Debrief"`
	}
)
