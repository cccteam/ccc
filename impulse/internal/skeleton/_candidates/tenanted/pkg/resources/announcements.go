package resources

import "github.com/cccteam/ccc"

type (
	// Announcement is the first tenant-scoped resource: a notice posted within one
	// tenant. It exists so tenancy is observable from the first sign-in — a login's
	// tenant list is the set of tenants where it holds at least one grant, and a grant
	// needs a tenant-scoped resource to land on. Replace or extend it as the
	// application's own tenant-scoped resources arrive.
	//
	// The tenancy column is on the row itself, so the @domain binding is the bare
	// column form: the framework stamps it from the request's tenant on create and the
	// wire can never write it.
	//
	// @resource
	// @permissionScope(domain)
	Announcement struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain
		TenantID string `spanner:"TenantId"`
		Title    string `spanner:"Title"`
		Body     string `spanner:"Body"`
	}
)
