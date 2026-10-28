package resources

import "github.com/cccteam/ccc/resource"

type (
	// Tenant is the tenant record (@tenant): the global resource whose rows are the
	// tenants. Its route name is the segment the tenant-scoped routes are served under,
	// so /api/tenants lists the tenants while /api/tenants/{tenantID}/... serves them,
	// and its key is the domain in every tenant-scoped URL. The generator derives the
	// segment from it and emits NewTenantRoster, the constructor of the tenant roster the
	// data level builds and starts: the tenants as the generated guard knows them, kept
	// current on every instance by this record's generated write paths (a create or a
	// delete reaches the roster after its commit and publishes the tenants signal), so
	// a tenant created here is usable at once, without a restart. The application thus
	// derives its domain universe from this table rather than a fixed in-code list, and
	// a session's tenant list is the roster filtered by the engine's foothold answer
	// (resource.SessionPermissions). The roles need no row per tenant: a domain role is
	// held in every tenant domain.
	//
	// The primary key is a human-readable slug, not a UUID: tenant identifiers appear
	// in every tenant-scoped URL and in role provisioning, and the schema enforces the
	// slug shape with a CHECK constraint. Creating a tenant therefore supplies its key.
	//
	// Tenant itself is a GLOBAL resource — administering the tenant list is a global
	// concern. Its list comes in name order (@order) when the request names no sort.
	//
	// Who may create a tenant is declared here, apart from who may enter one
	// (@tenant(creation: ...)): an administrator alone (@tenant, the default: the create
	// path above under the Administrator role's Create grant), the person who
	// becomes its administrator (self-service), or both, as here. Self-service is one
	// generated route, POST /api/create-tenant: a stranger signs in first, through the
	// staff auth's google registration (any Google account, open sign-up), holds no
	// tenant, and names theirs; the route writes the row, seats the person with the
	// roles TenantAdministratorRoles names (app) and hands the tenant to the roster, so
	// their next request stands in it. A person who already holds a tenant is refused:
	// one tenant per person, and an administrator of one never creates another.
	//
	// Who may enter a tenant is the record's declaration, data of the application
	// rather than deploy configuration: the directories the tenant admits its people
	// through (TenantDirectory, one row each) and whether its people enter through them
	// only (DirectoryOnly). The sign-in policy reads it in the sign-in's own transaction
	// (NewTenantDeclarationReader, handed to the staff auth) and every instance's roster
	// keeps a copy beside the keys (NewTenantRoster, Declarations), which the record's
	// and the directories' generated write paths reload after their commit; a tenant is
	// offered to a session only when the session may enter it. The record's change
	// events (DataChangeEvents, Config) are the audit of every edit.
	//
	// @resource
	// @tenant(creation: both)
	// @order(Name asc)
	Tenant struct {
		ID   string `spanner:"Id"`
		Name string `spanner:"Name"`
		// DirectoryOnly says the tenant's people enter through its directories only: a
		// password does not reach the tenant. Set by whoever holds the record's Update
		// grant on the column (Administrator), never at creation: a new tenant
		// declares nothing beyond its creator's directory.
		// @directoryOnly
		DirectoryOnly bool `spanner:"DirectoryOnly"`
	}
)

// Config enables change tracking: Tenant mutations write DataChangeEvents rows in the
// same transaction, the audit of every change to what a tenant declares.
func (Tenant) Config() resource.Config {
	return defaultConfig().SetTrackChanges(true)
}
