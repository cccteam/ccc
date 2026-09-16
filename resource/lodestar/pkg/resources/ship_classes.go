package resources

import "github.com/cccteam/ccc"

type (
	// ShipClass is the hull catalog: a global resource with an immutable designation, an
	// enum foreign key (RoleId to ShipRoles), an int tonnage, and a bool hardened flag. It
	// is the target of a join-path attribute from a SECTOR-scoped table: Ship's shipRole
	// attribute reaches ClassId.RoleId, a join path into a global table.
	//
	// It declares no @order, the one listed table in the demo that does not: a request
	// with no sort is not sorted, the statement carries no ORDER BY and the hulls arrive
	// in whatever order Spanner returns them, and a page that does not fit them is
	// marked Page-More with no cursor until the request asks a sort. It declares no
	// maximum either, so the ship form's class picker reads the whole catalog with
	// limit=all; a resource read whole declares an order or no maximum, since a list
	// with neither cannot be walked without a sort.
	//
	// Demonstrates: immutable, @attribute.join-path-global, order.none.
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
