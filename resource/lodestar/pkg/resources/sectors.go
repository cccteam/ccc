package resources

import "cloud.google.com/go/civil"

type (
	// Sector is the tenant record: its route name equals the domain route segment, so
	// /api/sectors lists the sectors while /api/sectors/{sectorID}/... serves the
	// sector-scoped routes. The application derives its domain universe from this table
	// rather than a fixed in-code list: the deployment reads it for MigrateRoles and the
	// DomainVisible seam checks it, so adding a sector is a data change, not a release.
	//
	// The primary key is a human-readable slug, not a UUID: sector identifiers appear in
	// every sector-scoped URL and in role provisioning, and the schema enforces the slug
	// shape with a CHECK constraint. Creating a sector therefore supplies its key.
	//
	// Sector itself is a GLOBAL resource: administering the sector list is a global
	// concern. The star chart's dark sectors are the ones a login holds no grant in.
	//
	// Demonstrates: tenancy.tenant-record, tenancy.concealed, @order, @page.
	//
	// @resource
	// @order(Name asc)
	// @page(default: 25, max: 200)
	Sector struct {
		ID          string     `spanner:"Id"`
		Name        string     `spanner:"Name"`
		Region      string     `spanner:"Region"`
		Established civil.Date `spanner:"Established"`
	}
)
