// Package tenantfixture provides parsed-struct fixtures for the tenant record's capture
// rules (@tenant): the well-formed record beside a tenant-scoped resource, and each
// shape the annotation refuses. The schema behind the table-backed structs is synthetic
// (tenantFixtureTables in the tests); Docks is keyed twice there.
package tenantfixture

import "github.com/cccteam/ccc"

// Struct-level annotations only parse from a TypeSpec doc comment, so the structs sit
// in a grouped type block like real project sources do.
type (
	// Sector is the well-formed tenant record: global, table-backed, one string key.
	//
	// @resource
	// @tenant
	Sector struct {
		ID   string `spanner:"Id"`
		Name string `spanner:"Name"`
	}

	// Berth is tenant-scoped: served under the record's segment.
	//
	// @resource
	// @permissionScope(domain)
	Berth struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain
		SectorID string `spanner:"SectorId"`
		Name     string `spanner:"Name"`
	}

	// Outpost declares @tenant on a tenant-scoped resource.
	//
	// @resource
	// @tenant
	// @permissionScope(domain)
	Outpost struct {
		ID string `spanner:"Id"`
		// @domain
		SectorID string `spanner:"SectorId"`
	}

	// Dock declares @tenant on a resource the schema keys twice.
	//
	// @resource
	// @tenant
	Dock struct {
		SectorID string `spanner:"SectorId"`
		Number   int64  `spanner:"Number"`
	}

	// Hangar declares @tenant on a resource whose key is not a string.
	//
	// @resource
	// @tenant
	Hangar struct {
		ID   ccc.UUID `spanner:"Id"`
		Name string   `spanner:"Name"`
	}

	// Region is a second tenant record beside Sector.
	//
	// @resource
	// @tenant
	Region struct {
		ID string `spanner:"Id"`
	}

	// Ledger declares @tenant on a view.
	//
	// @virtual
	// @tenant
	Ledger struct {
		// @primarykey
		ID   string `spanner:"Id" uniqueindex:"true"`
		Name string `spanner:"Name"`
	}

	// Tally declares @tenant on a computed resource.
	//
	// @computed
	// @tenant
	Tally struct {
		ID    string // @primarykey
		Total int64
	}

	// Reindex declares @tenant on a method.
	//
	// @rpc
	// @tenant
	Reindex struct {
		Input string
	}
)
