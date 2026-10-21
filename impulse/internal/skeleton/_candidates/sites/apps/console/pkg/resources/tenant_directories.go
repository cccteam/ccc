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
	// auth event records: a Google Workspace by its domain, a Microsoft Entra tenant by
	// its tenant id. The staff auth signs in by password until a directory method joins
	// it (impulse add method); a declared directory admits the people who come through
	// it from then on. One domain, one directory by default; a second directory is an
	// explicit second row. A tenant's own administrator declares its directories under
	// the record's Create and Delete grants (Administrator_Domain), and the change events
	// are the audit.
	//
	// The tenancy column is on the row itself (the bare @domain binding), as the
	// directories record requires: a directory is the tenant's own declaration, stamped
	// from the request's tenant on create, never written by the wire. The console
	// administers the tenants, so the record is this site's alone, like the tenant
	// record it declares for.
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
	}
)

// Config enables change tracking: TenantDirectory mutations write DataChangeEvents rows
// in the same transaction, the audit of every directory declared or withdrawn.
func (TenantDirectory) Config() resource.Config {
	return defaultConfig().SetTrackChanges(true)
}
