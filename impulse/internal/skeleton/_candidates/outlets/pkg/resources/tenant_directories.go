package resources

import (
	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
)

type (
	// TenantDirectory is a directory a tenant admits its people through
	// (@tenantDirectory), one row per declaration: the sign-in method the directory is
	// reached through (google, azure or workos, as the session library spells them) and
	// the connection the method's identities carry, which is what the session's first
	// auth event records: a Microsoft Entra tenant by its tenant id (the portal's members
	// come through the Entra directory the members auth registers, which the development
	// seed declares on both tenants), a Google Workspace by its domain. One domain, one
	// directory by default; a second directory is an explicit second row. A tenant's own
	// administrator declares its directories on the console under the record's Create
	// and Delete grants (Administrator_Domain), and the change events are the audit.
	//
	// DefaultSeat is what a stranger the directory vouches for gets at their first
	// sign-in: the tenant's role they are seated with, or NULL, and the tenant admits
	// them by invitation only (they are refused with no account, and an invitation is
	// the way in). The tenant's administrator sets it on the Sign-in page, a picker
	// over the tenant's roles.
	//
	// The tenancy column is on the row itself (the bare @domain binding), as the
	// directories record requires: a directory is the tenant's own declaration, stamped
	// from the request's tenant on create, never written by the wire. The record is on
	// the console alone, like the tenant record it declares for.
	//
	// @resource
	// @permissionScope(domain)
	// @tenantDirectory
	// @order(Method asc, Connection asc)
	TenantDirectory struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain
		TenantID string `spanner:"TenantId"`
		// @directoryMethod
		Method string `spanner:"Method"`
		// @directoryConnection
		Connection string `spanner:"Connection"`
		// @directoryDefaultSeat
		DefaultSeat *string `spanner:"DefaultSeat"`
	}
)

// Config enables change tracking: TenantDirectory mutations write DataChangeEvents rows
// in the same transaction, the audit of every directory declared or withdrawn.
func (TenantDirectory) Config() resource.Config {
	return defaultConfig().SetTrackChanges(true)
}
