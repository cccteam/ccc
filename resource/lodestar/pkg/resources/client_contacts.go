package resources

import (
	"github.com/cccteam/ccc"
)

type (
	// ClientContact is a client company's login and the SECOND subject-value anchor:
	// `subject.client` is the company the requesting user speaks for, so a portal
	// grant can say `client = subject.client` on Missions. It is served on the portal
	// outlet too, so Client Cleo's portal reads her own record (`userId = subject`)
	// for its header.
	//
	// @resource
	// @outlet(default, portal)
	ClientContact struct {
		ID ccc.UUID `spanner:"Id"`
		// @subjectValue(client, value: ClientID)
		// @attribute(userId)
		UserID      string   `spanner:"UserId"`
		ClientID    ccc.UUID `spanner:"ClientId"`
		DisplayName string   `spanner:"DisplayName"`
	}
)
