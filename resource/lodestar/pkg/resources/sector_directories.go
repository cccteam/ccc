package resources

import (
	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
)

type (
	// SectorDirectory is a directory a sector admits its people through
	// (@tenantDirectory), one row per declaration: the sign-in method the directory is
	// reached through (google, azure or workos, as the session library spells them) and
	// the connection the method's identities carry, which is what the session's first
	// auth event records: the fleet's Google Workspace by the name both auths' Google
	// registrations give it (example, the connection setting), a Microsoft Entra tenant
	// by its tenant id. One domain, one directory by default; a second directory is an
	// explicit second row. The sector's marshal declares its directories under the
	// record's Create and Delete grants (SectorMarshal; the governor holds it in every
	// sector), and the change events are the audit. A self-service sector's first
	// directory is the one its creator came through, written by the create-sector route.
	//
	// DefaultSeat is what a fleet member nobody has seated gets when they first sign in
	// through the directory: the sector's role they are seated with (Cinder seats them
	// SectorCrew), or NULL, and the sector admits them by invitation only (Anvil,
	// Bastion): they are refused with no account, and an invitation is the way in. The
	// marshal sets it on the Sign-in page, a picker over the sector's roles.
	//
	// The tenancy column is on the row itself (the bare @domain binding), as the
	// directories record requires: a directory is the sector's own declaration, stamped
	// from the request's sector on create, never written by the wire.
	//
	// Demonstrates: tenancy.declaration, tenancy.creator-directory, tenancy.default-seat.
	//
	// @resource
	// @permissionScope(domain)
	// @tenantDirectory
	// @order(Method asc, Connection asc)
	SectorDirectory struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain
		SectorID string `spanner:"SectorId"`
		// @directoryMethod
		Method string `spanner:"Method"`
		// @directoryConnection
		Connection string `spanner:"Connection"`
		// @directoryDefaultSeat
		DefaultSeat *string `spanner:"DefaultSeat"`
	}
)

// Config enables change tracking: SectorDirectory mutations write DataChangeEvents rows
// in the same transaction, the audit of every directory declared or withdrawn.
func (SectorDirectory) Config() resource.Config {
	return defaultConfig().SetTrackChanges(true)
}
