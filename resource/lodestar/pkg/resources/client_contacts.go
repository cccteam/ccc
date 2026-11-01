package resources

import "github.com/cccteam/ccc"

type (
	// ClientContact is a client company's login and the SECOND subject-value anchor:
	// subject.client is the company the requesting user speaks for, so a portal grant can
	// say client = subject.client on Missions. It is served on the portal outlet too, so
	// Client Cleo's portal reads her own record (userId = subject) for its header.
	//
	// DisplayName has a token column under ClientContactsSearch, which stores ClientId:
	// Client's @searchAlso(ClientContact.DisplayName) searches it from a client's box, and
	// the index alone yields the matching contact's client. The contact's own box searches
	// the display name too.
	//
	// Demonstrates: @subjectValue.second-anchor, outlet.shared, @attribute, @searchAlso, search.box.
	//
	// @resource
	// @outlet(default, portal)
	// @order(DisplayName asc)
	// @page(default: 25, max: 200)
	ClientContact struct {
		ID ccc.UUID `spanner:"Id"`
		// @subjectValue(client, value: ClientID)
		// @attribute(userId)
		UserID      string   `spanner:"UserId"`
		ClientID    ccc.UUID `spanner:"ClientId"`
		DisplayName string   `spanner:"DisplayName"`
	}
)
