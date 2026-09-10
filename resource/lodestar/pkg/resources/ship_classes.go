package resources

import "github.com/cccteam/ccc"

type (
	// ShipClass is the hull catalog: a global resource with an immutable designation, an
	// enum foreign key (RoleId to ShipRoles), an int tonnage, and a bool hardened flag. It
	// is the target of a join-path attribute from a SECTOR-scoped table: Ship's shipRole
	// attribute reaches ClassId.RoleId, a join path into a global table.
	//
	// Demonstrates: immutable, @attribute.join-path-global.
	//
	// @resource
	// @order(Designation asc)
	// @page(default: 25, max: 200)
	ShipClass struct {
		ID          ccc.UUID `spanner:"Id"`
		Designation string   `spanner:"Designation" conditions:"immutable"`
		RoleID      string   `spanner:"RoleId"`
		Tonnage     int64    `spanner:"Tonnage"`
		Hardened    bool     `spanner:"Hardened"`
	}
)
