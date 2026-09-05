package resources

import (
	"time"

	"github.com/cccteam/ccc"
)

type (
	// Reading is a measurement a machine reports, and it is outlet-exclusive: naming
	// only the machines outlet below replaces the default outlet, so these routes exist
	// only behind the API key — the generated router tests prove the browser paths 404.
	//
	// @resource
	// @permissionScope(domain)
	// @outlet(machines)
	Reading struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain
		TenantID   string    `spanner:"TenantId"`
		Source     string    `spanner:"Source"`
		Value      float64   `spanner:"Value"`
		RecordedAt time.Time `spanner:"RecordedAt"`
	}
)
