package resources

import (
	"time"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
)

type (
	// Ship is a hull in the service's fleet. Its HangarId foreign key carries both a
	// one-hop @domain binding (Ship to Hangar.SectorId, no denormalized tenant column,
	// which is legal on a plain @target root) and a one-hop @attribute join path
	// (hangarZone = the hangar's zone). Its ClassId carries a SECOND join-path attribute
	// into a GLOBAL table (shipRole = the class's role).
	//
	// Registry is immutable. LastRefitAt is domain data owned by the PassFlightTest
	// transition, which stamps it with the commit timestamp as an explicit update;
	// output_only keeps clients from writing it. UpdatedAt is the mechanical enforcement
	// stamp, an output_only_update_fn, which also gives the resource the generated
	// NewShipTouch that HailShip fires. Change tracking is on so a Hail lands in the
	// ship's log with every field unchanged, and so a refused delete of a ship under
	// refit proves the change event buffered beside it is not named in the 409.
	//
	// No column on a ship names its sector, so a list of ships scans the whole table,
	// every sector, and no index on Ships changes that; the generator says so at every
	// generate, for this and every other join-path resource. The fleet is small and
	// parent-scoped, which is what a join path suits.
	//
	// Registry is also unique across every ship (ShipsByRegistry): a create that reuses a
	// seeded registry reaches the commit and is refused there as a duplicate unique value.
	//
	// Registry is STRING(16), the fleet's only sized text column: the generated create
	// request carries sqltype:"STRING(16)", so a seventeen-character registry answers 400
	// naming the field at decode, before anything is buffered, and the console's create
	// form refuses it first from the metadata's maxLength. Registry is immutable, so the
	// create is the only path.
	//
	// CargoBays is the tonnage each cargo bay takes, in order: an ARRAY<INT64> column typed
	// []int64, number[] in the console's interface and in its metadata, one member of the
	// display-type vocabulary the client's union lists, so the console builds against what
	// the generator emits. NOT NULL with an empty array as the column's default, so a ship
	// with no bays carries [] and a create that says nothing about bays gets none. It
	// carries no allow_filter: a filter compares single values and Spanner cannot index an
	// array, so the generator would refuse the tag; a sort naming it answers 400.
	//
	// Demonstrates: @domain.join-path, @attribute.join-path, @attribute.join-path-global, immutable, output_only_update_fn, transition-owned-timestamp, change-tracking, commit.referential-refusal, commit.constraint-refusal, warning.join-path-list, decode.value-limit, typescript.array-column.
	//
	// @resource
	// @permissionScope(domain)
	// @order(Name asc)
	// @page(default: 10, max: 100)
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
		CargoBays   []int64    `spanner:"CargoBays"`
	}
)

// Config enables change tracking: Ship mutations (including the Hail touch) write
// DataChangeEvents rows in the same transaction.
func (Ship) Config() resource.Config {
	return defaultConfig().SetTrackChanges(true)
}
