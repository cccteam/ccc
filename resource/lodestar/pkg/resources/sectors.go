package resources

import "cloud.google.com/go/civil"

type (
	// Sector is the tenant record (@tenant): the table whose rows are the sectors, the
	// permission domains every sector-scoped URL names. The route segment and the route
	// parameter derive from it (/console/api/sectors lists the sectors while
	// /console/api/sectors/{sectorID}/... serves the sector-scoped routes), and every
	// instance holds a roster of its keys (resource.TenantRoster, built by the generated
	// NewSectorRoster) that the generated DomainGuard asks before a sector-scoped request
	// runs. The application derives its domain universe from this table rather than a
	// fixed in-code list, and the roster keeps up without a restart: the generated create
	// and delete paths add and remove the sector in the writing instance's roster after
	// the commit and signal the tenants kind, so every other instance reloads at once, and
	// a role held in every sector reaches the new sector with nothing written for it.
	// Charting a sector is a data change, not a release, a migration or a new login.
	//
	// The primary key is a human-readable slug, not a UUID: sector identifiers appear in
	// every sector-scoped URL and in role provisioning, and the schema enforces the slug
	// shape with a CHECK constraint. Creating a sector therefore supplies its key, and the
	// key cannot change on update.
	//
	// Sector itself is a GLOBAL resource: administering the sector list is a global
	// concern. The star chart's dark sectors are the ones a login holds no grant in.
	//
	// Demonstrates: tenancy.tenant-record, tenancy.run-time-tenant, tenancy.concealed, @order, @page.
	//
	// @resource
	// @tenant
	// @order(Name asc)
	// @page(default: 25, max: 200)
	Sector struct {
		ID          string     `spanner:"Id"`
		Name        string     `spanner:"Name"`
		Region      string     `spanner:"Region"`
		Established civil.Date `spanner:"Established"`
	}
)
