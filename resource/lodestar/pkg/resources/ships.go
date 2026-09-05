package resources

import (
	"time"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
)

type (
	// Ship is a hull in the service's fleet. Its HangarId foreign key carries both a
	// one-hop @domain binding (Ship → Hangar.SectorId — no denormalized tenant
	// column, which since c2b62ab is legal on a plain @target root) and a one-hop
	// @attribute join path (hangarZone = the hangar's zone). Its ClassId carries a
	// SECOND join-path attribute into a GLOBAL table (shipRole = the class's role).
	//
	// Registry is immutable. LastRefitAt is domain data owned by the PassFlightTest
	// transition, which stamps it with the commit timestamp as an explicit update;
	// output_only keeps clients from writing it. UpdatedAt is the mechanical
	// enforcement stamp — an output_only_update_fn — which also gives the resource the
	// generated NewShipTouch that HailShip fires. Change tracking is on so a Hail
	// lands in the ship's log with every field unchanged.
	//
	// @resource
	// @permissionScope(domain)
	Ship struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain(via: SectorID)
		// @attribute(hangarZone, via: Zone)
		HangarID ccc.UUID `spanner:"HangarId"`
		// @attribute(shipRole, via: RoleID)
		ClassID     ccc.UUID   `spanner:"ClassId"`
		Registry    string     `spanner:"Registry"    conditions:"immutable"`
		Name        string     `spanner:"Name"`
		LastRefitAt *time.Time `spanner:"LastRefitAt" conditions:"output_only"`
		UpdatedAt   *time.Time `spanner:"UpdatedAt"   output_only_update_fn:"resource.CommitTimestampPtr"`
	}
)

// Config enables change tracking: Ship mutations (including the Hail touch) write
// DataChangeEvents rows in the same transaction.
func (Ship) Config() resource.Config {
	return defaultConfig().SetTrackChanges(true)
}
