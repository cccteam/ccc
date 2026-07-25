package resources

import (
	"github.com/cccteam/ccc"
)

type (
	// CargoManifest is a mixed resource with a composite primary key: DeclaredValue is
	// explicitly tagged while Details and Quantity follow the field-permission
	// default. The fail-open pinning suite asserts the current behavior of the
	// untagged fields.
	//
	// @resource
	CargoManifest struct {
		ShipID        ccc.UUID `spanner:"ShipId"`
		LineNumber    int64    `spanner:"LineNumber"`
		Details       string   `spanner:"Details"`
		Quantity      int64    `spanner:"Quantity"`
		DeclaredValue int64    `spanner:"DeclaredValue" perm:"Read,List,Create,Update"`
	}
)
