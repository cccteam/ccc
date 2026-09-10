package resources

type (
	// Tenant is the tenant record: its route name equals the domain route segment, so
	// /api/tenants lists the tenants while /api/tenants/{tenantID}/... serves the
	// tenant-scoped routes. The application derives its domain universe from this
	// table rather than a fixed in-code list — the deployment reads it for
	// MigrateRoles and the DomainVisible seam checks it — so the tenant list is data.
	//
	// The primary key is a human-readable slug, not a UUID: tenant identifiers appear
	// in every tenant-scoped URL and in role provisioning, and the schema enforces the
	// slug shape with a CHECK constraint. Creating a tenant therefore supplies its key.
	//
	// Tenant itself is a GLOBAL resource — administering the tenant list is a global
	// concern.
	//
	// @resource
	Tenant struct {
		ID   string `spanner:"Id"`
		Name string `spanner:"Name"`
	}
)
