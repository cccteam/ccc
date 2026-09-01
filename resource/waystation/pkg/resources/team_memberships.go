package resources

import (
	"github.com/cccteam/ccc"
)

type (
	// TeamMembership is the subject-set anchor: `subject.teams` in a grant condition
	// is the set of TeamId values on rows whose UserId matches the requesting user
	// (`assignedTeam IN subject.teams`). The anchor table is itself domain-scoped, so
	// the rendered subject subquery is filtered to the request's domain — membership
	// at one station never leaks authority into another.
	//
	// A user with no memberships gets the empty set, which fails IN conditions
	// closed for free.
	//
	// @resource
	// @permissionScope(domain)
	TeamMembership struct {
		// @domain(via: WaystationID)
		TeamID ccc.UUID `spanner:"TeamId"`
		// @subjectSet(teams, value: TeamID)
		UserID string `spanner:"UserId"`
	}
)
