package resources

import (
	"github.com/cccteam/ccc"
)

type (
	// SquadronMembership is the sector-scoped subject-set anchor, carrying TWO subject
	// sets on one user column: `subject.squadrons` is the set of SquadronId values on
	// rows whose UserId matches the requesting user (`assignedSquadron IN
	// subject.squadrons`), and `subject.wings` continues through the squadron's
	// foreign key to its wing — a dotted value path (`wing IN subject.wings`).
	//
	// The anchor is two-hop tenancy (Squadron → Wing → SectorId), so the rendered
	// subject subquery is filtered to the request's sector — a squadron roster at
	// Bastion never leaks authority into Anvil. A user with no memberships gets the
	// empty set, which fails IN conditions closed for free.
	//
	// @resource
	// @permissionScope(domain)
	SquadronMembership struct {
		// @domain(via: WingID.SectorID)
		SquadronID ccc.UUID `spanner:"SquadronId"`
		// @subjectSet(squadrons, value: SquadronID)
		// @subjectSet(wings, value: SquadronID.WingID)
		UserID string `spanner:"UserId"`
	}
)
