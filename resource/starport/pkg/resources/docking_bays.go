package resources

import (
	"github.com/cccteam/ccc"
)

type (
	// DockingBay is a fail-open resource: none of its fields carry a perm tag, so
	// field-level access follows the package default. The fail-open pinning suite
	// asserts the current behavior of these fields.
	//
	// @resource
	DockingBay struct {
		ID         ccc.UUID `spanner:"Id"`
		Name       string   `spanner:"Name"`
		DeckLevel  int64    `spanner:"DeckLevel"`
		MaxTonnage int64    `spanner:"MaxTonnage"`
	}
)
