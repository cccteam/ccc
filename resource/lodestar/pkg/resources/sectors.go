package resources

import (
	"cloud.google.com/go/civil"
)

type (
	// Sector is the tenant record: its route name equals the domain route segment, so
	// /api/sectors lists the tenants while /api/sectors/{sectorID}/... serves the
	// sector-scoped routes. Lodestar derives its domain universe from this table
	// rather than a fixed in-code list — the bootstrap reads it for MigrateRoles and
	// the DomainVisible seam queries the startup cache of it — so adding a sector is a
	// data change, not a release.
	//
	// The primary key is a human-readable slug (anvil, bastion, cinder), not a UUID:
	// tenant identifiers appear in every sector-scoped URL and in role provisioning,
	// and the schema enforces the slug shape with a CHECK constraint. Creating a
	// sector therefore supplies its key (no server-generated UUID).
	//
	// Sector itself is a GLOBAL resource — administering the tenant list is a
	// headquarters concern. The star chart's "chart every sector" toggle reads it.
	//
	// @resource
	Sector struct {
		ID          string     `spanner:"Id"`
		Name        string     `spanner:"Name"`
		Region      string     `spanner:"Region"`
		Established civil.Date `spanner:"Established"`
	}
)
