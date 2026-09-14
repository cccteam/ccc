package resources

import "github.com/cccteam/ccc"

type (
	// RefitTask is a checklist item: interleaved in Refits with a compound primary key,
	// so creates supply the full key (no server-generated UUID). The @stateRoot marker on
	// the anchoring foreign key makes it a member of the Refit workflow one hop from the
	// root, and its tenancy is THREE hops away: Refit, Ship, Hangar, SectorId.
	//
	// TaskNumber trails the parent key in the compound key with nothing bound before it
	// (the tenancy is a join path, so no column on the row is bound by equality): it is
	// not indexed, so the generated list struct carries no index tag on it, its metadata
	// says nothing about filtering, and a filter naming it alone is refused.
	//
	// Demonstrates: interleaved-table, compound-key, client-supplied-key, @stateRoot, @domain.join-path, create-under-parent, index.trailing-key.
	//
	// @resource
	// @permissionScope(domain)
	// @order(TaskNumber asc)
	// @page(default: 25, max: 200)
	RefitTask struct {
		// The parent-key column is named Id because Spanner interleaving requires the
		// child's leading key column to carry the parent's key column name; the Go field
		// keeps the readable name.
		//
		// @stateRoot(Refit)
		// @domain(via: ShipID.HangarID.SectorID)
		RefitID      ccc.UUID `spanner:"Id"`
		TaskNumber   int64    `spanner:"TaskNumber"`
		Instructions string   `spanner:"Instructions"`
		Done         bool     `spanner:"Done"`
		Notes        *string  `spanner:"Notes"`
	}
)
