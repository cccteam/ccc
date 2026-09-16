package resources

import "github.com/cccteam/ccc"

type (
	// ShipClass is the hull catalog: a global resource with an immutable designation, an
	// enum foreign key (RoleId to ShipRoles), an int tonnage, and a bool hardened flag. It
	// is the target of a join-path attribute from a SECTOR-scoped table: Ship's shipRole
	// attribute reaches ClassId.RoleId, a join path into a global table.
	//
	// It declares no @order, the one listed table in the demo that does not, and no
	// maximum, so the ship form's class picker reads the whole catalog with limit=all:
	// that whole read is not sorted, the statement carries no ORDER BY and the hulls
	// arrive in whatever order Spanner returns them. Every paged request needs an order,
	// so a bare GET or a limit on the catalog without a sort is refused with a 400 naming
	// the resource and the two ways out (a sort, or limit=all), and the console's Ship
	// Classes page names its sort; a requested sort pages it by cursor as any list.
	//
	// Demonstrates: immutable, @attribute.join-path-global, order.none, order.required.
	//
	// @resource
	// @page(default: 25)
	ShipClass struct {
		ID          ccc.UUID `spanner:"Id"`
		Designation string   `spanner:"Designation" conditions:"immutable"`
		RoleID      string   `spanner:"RoleId"`
		Tonnage     int64    `spanner:"Tonnage"`
		Hardened    bool     `spanner:"Hardened"`
	}
)
