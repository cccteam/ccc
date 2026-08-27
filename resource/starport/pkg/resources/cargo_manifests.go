package resources

import (
	"github.com/cccteam/ccc"
)

type (
	// CargoManifest is a structurally enforced resource with a composite primary key:
	// both key fields are exempt (their readability follows the resource-level grant)
	// and every other field requires its own field grant. The fail-closed pinning
	// suite asserts both halves.
	//
	// @resource
	CargoManifest struct {
		ShipID        ccc.UUID `spanner:"ShipId"`
		LineNumber    int64    `spanner:"LineNumber"`
		Details       string   `spanner:"Details"`
		Quantity      int64    `spanner:"Quantity"`
		DeclaredValue int64    `spanner:"DeclaredValue"`
	}
)
