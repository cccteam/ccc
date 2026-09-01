package resources

import (
	"cloud.google.com/go/civil"
)

type (
	// Waystation is the tenant record: its route name equals the domain route segment,
	// so /api/waystations lists the tenants while /api/waystations/{waystationID}/...
	// serves the tenant-scoped routes. Unlike starport's fixed in-code station list,
	// the waystation app derives its domain universe from this table — the bootstrap
	// reads it for MigrateRoles and the DomainExists seam queries it — so the tenant
	// list is data, the shape a real application uses.
	//
	// The primary key is a human-readable slug (ws-alpha, ws-beta, ...), not a UUID:
	// tenant identifiers appear in every domain-scoped URL and in role provisioning,
	// and the schema enforces the slug shape with a CHECK constraint. Creating a
	// waystation therefore supplies its key (no server-generated UUID).
	//
	// Waystation itself is a GLOBAL resource — administering the tenant list is a
	// global concern.
	//
	// @resource
	Waystation struct {
		ID           string     `spanner:"Id"`
		Name         string     `spanner:"Name"`
		OrbitBand    string     `spanner:"OrbitBand"`
		Commissioned civil.Date `spanner:"Commissioned"`
	}
)
