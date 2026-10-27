// Package tenantfixture provides parsed-struct fixtures for the tenant record's capture
// rules (@tenant): the well-formed record beside a tenant-scoped resource, and each
// shape the annotation refuses. The schema behind the table-backed structs is synthetic
// (tenantFixtureTables in the tests); Docks is keyed twice there.
package tenantfixture

import (
	"cloud.google.com/go/civil"

	"github.com/cccteam/ccc"
)

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

	// Station declares the self-service creation shape: the person who becomes its
	// administrator creates it, through the generated route, and no administrator does.
	// Beside the key it carries a plain column, a nullable one, a column of a type from
	// another package and a server-owned one.
	//
	// @resource
	// @tenant(creation: self-service)
	Station struct {
		ID      string     `spanner:"Id"`
		Name    string     `spanner:"Name"`
		Motto   *string    `spanner:"Motto"`
		Opened  civil.Date `spanner:"Opened"`
		Derived string     `spanner:"Derived" conditions:"output_only"`
	}

	// Colony declares both creation shapes.
	//
	// @resource
	// @tenant(creation: both)
	Colony struct {
		ID   string `spanner:"Id"`
		Name string `spanner:"Name"`
	}

	// Enclave spells the default shape out.
	//
	// @resource
	// @tenant(creation: administrator)
	Enclave struct {
		ID string `spanner:"Id"`
	}

	// Outland names a creation shape the generator does not know.
	//
	// @resource
	// @tenant(creation: nowhere)
	Outland struct {
		ID string `spanner:"Id"`
	}

	// Province passes the shape without its key.
	//
	// @resource
	// @tenant(self-service)
	Province struct {
		ID string `spanner:"Id"`
	}

	// CreateStation is a method named as the Station record's create-your-station
	// handler would be.
	//
	// @rpc
	CreateStation struct {
		Input string
	}

	// Bastion names an argument @tenant does not take.
	//
	// @resource
	// @tenant(shape: both)
	Bastion struct {
		ID string `spanner:"Id"`
	}

	// Harbor is a tenant record that declares who may enter a harbor: its people enter
	// through its directories only when DirectoryOnly says so (@directoryOnly), and
	// HarborDirectory holds the directories.
	//
	// @resource
	// @tenant(creation: both)
	Harbor struct {
		ID   string `spanner:"Id"`
		Name string `spanner:"Name"`
		// @directoryOnly
		DirectoryOnly bool `spanner:"DirectoryOnly"`
	}

	// HarborDirectory is the well-formed directories record: tenant-scoped through the
	// bare @domain column, with its method and connection columns and the default seat
	// a stranger it vouches for gets at their first sign-in.
	//
	// @resource
	// @permissionScope(domain)
	// @tenantDirectory
	HarborDirectory struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain
		HarborID string `spanner:"HarborId"`
		// @directoryMethod
		Method string `spanner:"Method"`
		// @directoryConnection
		Connection string `spanner:"Connection"`
		// @directoryDefaultSeat
		DefaultSeat *string `spanner:"DefaultSeat"`
	}

	// Breakwater is a well-formed directories record without the default seat column:
	// every harbor admits the strangers its directories vouch for by invitation only.
	//
	// @resource
	// @permissionScope(domain)
	// @tenantDirectory
	Breakwater struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain
		HarborID string `spanner:"HarborId"`
		// @directoryMethod
		Method string `spanner:"Method"`
		// @directoryConnection
		Connection string `spanner:"Connection"`
	}

	// Slipway declares @directoryDefaultSeat on a resource that is not the directories
	// record.
	//
	// @resource
	// @permissionScope(domain)
	Slipway struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain
		HarborID string `spanner:"HarborId"`
		// @directoryDefaultSeat
		Seat string `spanner:"Seat"`
	}

	// Wharf is a directories record whose default seat is a BOOL.
	//
	// @resource
	// @permissionScope(domain)
	// @tenantDirectory
	Wharf struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain
		HarborID string `spanner:"HarborId"`
		// @directoryMethod
		Method string `spanner:"Method"`
		// @directoryConnection
		Connection string `spanner:"Connection"`
		// @directoryDefaultSeat
		Seated bool `spanner:"Seated"`
	}

	// Bollard is a directories record declaring @directoryDefaultSeat on two columns.
	//
	// @resource
	// @permissionScope(domain)
	// @tenantDirectory
	Bollard struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain
		HarborID string `spanner:"HarborId"`
		// @directoryMethod
		Method string `spanner:"Method"`
		// @directoryConnection
		Connection string `spanner:"Connection"`
		// @directoryDefaultSeat
		Seat string `spanner:"Seat"`
		// @directoryDefaultSeat
		Rank string `spanner:"Rank"`
	}

	// Pier declares @directoryOnly on a resource that is not the tenant record.
	//
	// @resource
	Pier struct {
		ID ccc.UUID `spanner:"Id"`
		// @directoryOnly
		Closed bool `spanner:"Closed"`
	}

	// Quay declares @directoryOnly on a string column of a tenant record.
	//
	// @resource
	// @tenant
	Quay struct {
		ID string `spanner:"Id"`
		// @directoryOnly
		Mode string `spanner:"Mode"`
	}

	// Beacon declares @tenantDirectory on a global resource.
	//
	// @resource
	// @tenantDirectory
	Beacon struct {
		ID         ccc.UUID `spanner:"Id"`
		Method     string   `spanner:"Method"`
		Connection string   `spanner:"Connection"`
	}

	// Buoy declares @directoryMethod on a resource that is not the directories record.
	//
	// @resource
	// @permissionScope(domain)
	Buoy struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain
		HarborID string `spanner:"HarborId"`
		// @directoryMethod
		Method string `spanner:"Method"`
	}

	// Mooring is a directories record with no connection column.
	//
	// @resource
	// @permissionScope(domain)
	// @tenantDirectory
	Mooring struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain
		HarborID string `spanner:"HarborId"`
		// @directoryMethod
		Method     string `spanner:"Method"`
		Connection string `spanner:"Connection"`
	}

	// Jetty declares @directoryMethod on two columns.
	//
	// @resource
	// @permissionScope(domain)
	// @tenantDirectory
	Jetty struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain
		HarborID string `spanner:"HarborId"`
		// @directoryMethod
		Method string `spanner:"Method"`
		// @directoryMethod
		Kind string `spanner:"Kind"`
		// @directoryConnection
		Connection string `spanner:"Connection"`
	}

	// Anchorage is a directories record whose tenant is resolved through a join path,
	// through Berth's own hop.
	//
	// @resource
	// @permissionScope(domain)
	// @tenantDirectory
	Anchorage struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain(via: SectorID)
		BerthID ccc.UUID `spanner:"BerthId"`
		// @directoryMethod
		Method string `spanner:"Method"`
		// @directoryConnection
		Connection string `spanner:"Connection"`
	}
)
