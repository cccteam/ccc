package resources

import (
	"time"

	"github.com/cccteam/ccc"
)

type (
	// Reading is a measurement a device reports, and it is outlet-exclusive: naming
	// only the devices outlet below replaces the default outlet, so these routes exist
	// only behind the staff auth's token check — the generated router tests prove the
	// browser paths 404.
	// A list without a sort comes newest first (@order), so a paged request is never
	// refused for want of one; the index on (TenantId, RecordedAt DESC) serves every page.
	//
	// @resource
	// @permissionScope(domain)
	// @outlet(devices)
	// @order(RecordedAt desc)
	Reading struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain
		TenantID   string    `spanner:"TenantId"`
		Source     string    `spanner:"Source"`
		Value      float64   `spanner:"Value"`
		RecordedAt time.Time `spanner:"RecordedAt"`
	}
)
