// Package indexfixture provides parsed-struct fixtures for the generation-time schema
// warnings: listed tenant-scoped resources whose table map (indexFixtureTables in the
// tests) carries the index shapes the rule reads, one struct per outcome.
package indexfixture

import (
	"time"

	"github.com/cccteam/ccc"
)

// Struct-level annotations only parse from a TypeSpec doc comment, so the structs sit
// in grouped type blocks like real project sources do.
type (
	// Served has the index it wants, with an extra trailing key column and the DESC
	// direction matched: silent.
	//
	// @resource
	// @permissionScope(domain)
	// @order(PlacedAt desc)
	Served struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain
		TenantID string    `spanner:"TenantId"`
		PlacedAt time.Time `spanner:"PlacedAt"`
		Priority int64     `spanner:"Priority"`
	}

	// Unserved has only the foreign key's backing index on the tenant column: warns,
	// naming the index it wants.
	//
	// @resource
	// @permissionScope(domain)
	// @order(PlacedAt asc, Priority desc)
	Unserved struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain
		TenantID string    `spanner:"TenantId"`
		PlacedAt time.Time `spanner:"PlacedAt"`
		Priority int64     `spanner:"Priority"`
	}

	// Misdirected has a composite index in the wrong direction: warns.
	//
	// @resource
	// @permissionScope(domain)
	// @order(PlacedAt desc)
	Misdirected struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain
		TenantID string    `spanner:"TenantId"`
		PlacedAt time.Time `spanner:"PlacedAt"`
	}

	// NullFiltered has the composite index, null-filtered: warns, since that index
	// skips rows and serves no order over every row.
	//
	// @resource
	// @permissionScope(domain)
	// @order(PlacedAt asc)
	NullFiltered struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain
		TenantID string     `spanner:"TenantId"`
		PlacedAt *time.Time `spanner:"PlacedAt"`
	}

	// Unlisted lacks the index but its list handler is suppressed: silent.
	//
	// @resource
	// @permissionScope(domain)
	// @suppress(listHandler)
	// @order(PlacedAt asc)
	Unlisted struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain
		TenantID string    `spanner:"TenantId"`
		PlacedAt time.Time `spanner:"PlacedAt"`
	}

	// Unordered has a bare @domain and no @order: silent, since its list is in
	// primary-key order and the backing index on the tenant column serves it.
	//
	// @resource
	// @permissionScope(domain)
	Unordered struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain
		TenantID string `spanner:"TenantId"`
		Name     string `spanner:"Name"`
	}

	// Keyed has its order in its primary key, tenant first: silent, since the
	// PRIMARY_KEY index counts like any other.
	//
	// @resource
	// @permissionScope(domain)
	// @order(Sequence asc)
	Keyed struct {
		// @domain
		TenantID string `spanner:"TenantId"`
		Sequence int64  `spanner:"Sequence"`
		Name     string `spanner:"Name"`
	}

	// Routed resolves its tenant through a join path: warns that its lists scan the
	// table, whatever its indexes.
	//
	// @resource
	// @permissionScope(domain)
	// @order(Name asc)
	Routed struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain(via: TenantID)
		UnservedID ccc.UUID `spanner:"UnservedId"`
		Name       string   `spanner:"Name"`
	}

	// Global has no tenant at all: silent.
	//
	// @resource
	// @order(Name asc)
	Global struct {
		ID   ccc.UUID `spanner:"Id"`
		Name string   `spanner:"Name"`
	}

	// Projected is a view with a bare @domain and no index behind its order: silent,
	// since a view's index facts are tags, not schema.
	//
	// @virtual
	// @permissionScope(domain)
	// @order(PlacedAt desc)
	Projected struct {
		// @primarykey
		ID ccc.UUID `spanner:"Id" uniqueindex:"true"`
		// @domain
		TenantID string    `spanner:"TenantId"`
		PlacedAt time.Time `spanner:"PlacedAt"`
	}
)
