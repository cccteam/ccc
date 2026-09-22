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
	// A list without a sort comes in title order (@order), so a paged request is never
	// refused for want of one; the index on (TenantId, Title) serves every page.
	//
	// @resource
	// @permissionScope(domain)
	// @order(Title asc)
	Announcement struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain
		TenantID string `spanner:"TenantId"`
		Title    string `spanner:"Title"`
		Body     string `spanner:"Body"`
	}
)
