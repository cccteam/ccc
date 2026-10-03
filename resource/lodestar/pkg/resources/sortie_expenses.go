package resources

import (
	"github.com/cccteam/ccc"
	"github.com/shopspring/decimal"
)

type (
	// SortieExpense is the SECOND hop: fuel, supplies, and tow gear booked against a
	// sortie. @stateRoot(Mission) on SortieId declares its immediate hop, and the chain
	// resolver composes Sortie's hop to reach the root, so the Quartermaster's state =
	// 'underway' grant evaluates two hops deep, through Sortie to Mission. Tenancy runs
	// the same two hops (Sortie, Mission.SectorId).
	//
	// Memo is the purser's line note, renamed from Note: the Go field and the wire name
	// moved, the column stayed, and @formerly keeps the old name answered beside the new
	// one for consoles built before the rename (a body, columns, sort or filter naming
	// note is read as memo, a body naming both is refused, and every row carries both
	// keys), while the role file and the console's code name memo alone and the permission
	// digest mirrors memo's entry under note. The annotation leaves, and the old name with
	// it, once the console outlet's oldest answered release (cmd/generate) passes the
	// release that renamed it.
	//
	// Demonstrates: @stateRoot.two-hop, @domain.join-path, create-under-parent, api.formerly.
	//
	// @resource
	// @permissionScope(domain)
	// @order(Amount desc)
	// @page(default: 10, max: 100)
	SortieExpense struct {
		ID ccc.UUID `spanner:"Id"`
		// @stateRoot(Mission)
		// @domain(via: MissionID.SectorID)
		SortieID ccc.UUID `spanner:"SortieId"`
		Category string   `spanner:"Category"`
		// @attribute(amount)
		Amount decimal.Decimal `spanner:"Amount"`
		// @formerly(Note)
		Memo *string `spanner:"Note"`
	}
)
