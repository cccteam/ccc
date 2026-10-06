package resources

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
	// @resource
	// @tenant
	// @order(Name asc)
	Tenant struct {
		ID   string `spanner:"Id"`
		Name string `spanner:"Name"`
	}
)
