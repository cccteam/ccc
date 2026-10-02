package resources

type (
	// Tenant is the tenant record: its route name equals the domain route segment, so
	// /api/tenants lists the tenants while /api/tenants/{tenantID}/... serves the
	// tenant-scoped routes. The application derives its domain universe from this
	// table rather than a fixed in-code list — a session's tenant list is this roster
	// filtered by the engine's foothold answer (resource.SessionPermissions), and the
	// DomainVisible seam checks it — so the tenant list is data. The roles need no row
	// per tenant: a domain role is held in every tenant domain.
	//
	// The primary key is a human-readable slug, not a UUID: tenant identifiers appear
	// in every tenant-scoped URL and in role provisioning, and the schema enforces the
	// slug shape with a CHECK constraint. Creating a tenant therefore supplies its key.
	//
	// Tenant itself is a GLOBAL resource — administering the tenant list is a global
	// concern. Its list comes in name order (@order) when the request names no sort.
	//
	// @resource
	// @order(Name asc)
	Tenant struct {
		ID   string `spanner:"Id"`
		Name string `spanner:"Name"`
	}
)
