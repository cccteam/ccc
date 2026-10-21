package resources

type (
	// Tenant is the tenant record (@tenant) as the portal declares it: the table the
	// console serves, declared here too because a site's generator reads one package and
	// derives the tenant segment (/api/tenants/{tenantID}/...) and the roster constructor
	// (NewTenantRoster) from the record it finds there. The portal administers no
	// tenants, so its handlers and routes are suppressed: the console's generated write
	// paths keep the roster current, and this site's generated guard asks the same
	// roster, built once by the shared data level. What a tenant declares about who may
	// enter it (its directories, whether they are the only way in) is the console's
	// record too: the shared roster's copy of it filters this site's sessions' tenant
	// lists, and the auth both sites share reads it at sign-in through the console's
	// generated reader.
	//
	// @resource
	// @tenant
	// @suppress(allHandlers)
	// @suppress(allRoutes)
	Tenant struct {
		ID   string `spanner:"Id"`
		Name string `spanner:"Name"`
	}
)
