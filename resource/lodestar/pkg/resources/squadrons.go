package resources

import "github.com/cccteam/ccc"

type (
	// Squadron is a flight unit. Its table carries no tenancy column: the @domain binding
	// leaves through the WingId foreign key and resolves the sector one hop away (via:
	// names remote Go field segments), the single-hop form of join-path tenancy. The same
	// column is the wing attribute the Wing Commander's grant compares against
	// subject.wings.
	//
	// Demonstrates: @domain.join-path, @attribute.
	//
	// @resource
	// @permissionScope(domain)
	// @order(Name asc)
	// @page(default: 25, max: 200)
	Squadron struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain(via: SectorID)
		// @attribute(wing)
		WingID ccc.UUID `spanner:"WingId"`
		Name   string   `spanner:"Name"`
	}
)
