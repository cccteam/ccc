package virtualresources

import (
	_ "embed"

	"github.com/cccteam/ccc"
)

type (
	// ManifestLine joins each cargo manifest line to its ship's name. Its compound key
	// is declared with two primarykey annotations, which form the key in declaration
	// order — both key fields carry the perm:"-" exemption in the generated list
	// request struct.
	//
	// @virtual
	ManifestLine struct {
		ShipID        ccc.UUID `spanner:"ShipId"        index:"true"` // @primarykey
		LineNumber    int64    `spanner:"LineNumber"`                 // @primarykey
		ShipName      string   `spanner:"ShipName"      index:"true"`
		Details       string   `spanner:"Details"`
		Quantity      int64    `spanner:"Quantity"`
		DeclaredValue int64    `spanner:"DeclaredValue"`
	}
)

//go:embed manifest_lines.sql
var manifestLinesSubquery string

// Subquery provides the embedded SQL projection backing this virtual resource.
func (ManifestLine) Subquery() (query string, params map[string]any) {
	return manifestLinesSubquery, nil
}
