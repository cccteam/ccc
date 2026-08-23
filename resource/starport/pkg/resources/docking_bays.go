package resources

import (
	"github.com/cccteam/ccc"
)

type (
	// DockingBay is the fail-closed demo resource: no field carries any permission
	// annotation, and every non-primary-key field is enforced structurally with the
	// endpoint's permission. The fail-closed pinning suite asserts that a resource-only
	// grant exposes nothing beyond the primary key.
	//
	// @resource
	DockingBay struct {
		ID         ccc.UUID `spanner:"Id"`
		Name       string   `spanner:"Name"`
		DeckLevel  int64    `spanner:"DeckLevel"`
		MaxTonnage int64    `spanner:"MaxTonnage"`
	}
)
