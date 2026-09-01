package resources

import (
	"time"

	"cloud.google.com/go/civil"
	"github.com/cccteam/ccc"
)

type (
	// Asset is maintained machinery. Its foreign key to Facility carries both a
	// two-hop @domain binding (Asset -> Facility -> Module.WaystationId) and a
	// two-hop @attribute join path (zone = the containing module's zone), so a grant
	// condition like `zone != 'reactor'` evaluates an attribute two tables away —
	// evaluation through a foreign key, not disclosure of it.
	//
	// LastServicedAt is domain data owned by the CompleteWorkOrder transition, which
	// stamps it with the commit timestamp as an explicit update; output_only keeps
	// clients from writing it. It is deliberately NOT an output_only_update_fn:
	// update functions enforce mechanical stamps like UpdatedAt on every update,
	// while a timestamp with domain meaning is written explicitly by the code that
	// owns the business event.
	//
	// @resource
	// @permissionScope(domain)
	Asset struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain(via: ModuleID.WaystationID)
		// @attribute(zone, via: ModuleID.Zone)
		FacilityID     ccc.UUID   `spanner:"FacilityId"`
		SerialNumber   string     `spanner:"SerialNumber"   conditions:"immutable"`
		Name           string     `spanner:"Name"`
		CommissionedOn civil.Date `spanner:"CommissionedOn"`
		LastServicedAt *time.Time `spanner:"LastServicedAt" conditions:"output_only"`
	}
)
