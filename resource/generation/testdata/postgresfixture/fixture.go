// Package postgresfixture provides source structs for an application on PostgreSQL: each
// field names its column by the postgres tag alone, over the tables of
// testdata/postgresmigrations.
package postgresfixture

import "time"

type (
	// @resource
	Tenant struct {
		ID string `postgres:"Id"`
	}

	// @resource
	Order struct {
		ID          string    `postgres:"Id"`
		TenantID    string    `postgres:"TenantId"`
		PlacedAt    time.Time `postgres:"PlacedAt"`
		Reference   string    `postgres:"Reference"`
		ExternalRef *string   `postgres:"ExternalRef"`
		Note        *string   `postgres:"Note"`
	}

	// @resource
	Seat struct {
		OrderID string  `postgres:"OrderId"`
		UserID  string  `postgres:"UserId"`
		Row     int64   `postgres:"Row"`
		Note    *string `postgres:"Note"`
	}
)
