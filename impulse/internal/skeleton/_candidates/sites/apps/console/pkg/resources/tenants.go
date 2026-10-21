package resources

import "github.com/cccteam/ccc/resource"

type (
	// Tenant is the tenant record (@tenant): the global resource whose rows are the
	// tenants. Its route name is the segment the tenant-scoped routes are served under,
	// so /api/tenants lists the tenants while /api/tenants/{tenantID}/... serves them,
	// and its key is the domain in every tenant-scoped URL. The generator derives the
	// segment from it and emits NewTenantRoster, the constructor of the tenant roster the
	// shared data level builds and starts: the tenants as every site's generated guard
	// knows them, kept current on every instance by this record's generated write paths
	// (a create or a delete reaches the roster after its commit and publishes the
	// tenants signal), so a tenant created here is usable at once, on both sites,
	// without a restart. The application thus derives its domain universe from this
	// table rather than a fixed in-code list, and a session's tenant list is the roster
	// filtered by the engine's foothold answer (resource.SessionPermissions). The roles
	// need no row per tenant: a domain role is held in every tenant domain. The portal
	// declares the same record with its handlers and routes suppressed, since a site's
	// generator reads one package and the console alone administers the tenants.
	//
	// The primary key is a human-readable slug, not a UUID: tenant identifiers appear
	// in every tenant-scoped URL and in role provisioning, and the schema enforces the
	// slug shape with a CHECK constraint. Creating a tenant therefore supplies its key.
	//
	// Tenant itself is a GLOBAL resource — administering the tenant list is a global
	// concern. Its list comes in name order (@order) when the request names no sort.
	//
	// Who may enter a tenant is the record's declaration, data of the application
	// rather than deploy configuration: the directories the tenant admits its people
	// through (TenantDirectory, one row each) and whether its people enter through them
	// only (DirectoryOnly). The staff auth's sign-in policy reads it in the sign-in's
	// own transaction (NewTenantDeclarationReader, this site's, handed to the auth both
	// sites share) and the shared roster keeps a copy beside the keys (NewTenantRoster,
	// Declarations), which this record's and the directories' generated write paths
	// reload after their commit; each site's App offers a tenant to a session only
	// when the session may enter it. The record's change events (DataChangeEvents,
	// Config) are the audit of every edit.
	//
	// @resource
	// @tenant
	// @order(Name asc)
	Tenant struct {
		ID   string `spanner:"Id"`
		Name string `spanner:"Name"`
		// DirectoryOnly says the tenant's people enter through its directories only: a
		// password does not reach the tenant. Set by whoever holds the record's Update
		// grant on the column (Administrator_Global), never at creation: a new tenant
		// declares nothing.
		// @directoryOnly
		DirectoryOnly bool `spanner:"DirectoryOnly"`
	}
)

// Config enables change tracking: Tenant mutations write DataChangeEvents rows in the
// same transaction, the audit of every change to what a tenant declares.
func (Tenant) Config() resource.Config {
	return defaultConfig().SetTrackChanges(true)
}
