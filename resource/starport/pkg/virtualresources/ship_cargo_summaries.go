// Package virtualresources provides the view-backed virtual resources for the
// starport: read-only projections over the base tables. A virtual resource has no
// table metadata — its rows come from an embedded subquery, indexed fields are
// declared with index/uniqueindex tags, and its read identity comes from primarykey
// field annotations.
package virtualresources

import (
	_ "embed"

	"github.com/cccteam/ccc"
)

type (
	// ShipCargoSummary is a per-ship cargo rollup: one row per ship with its docking
	// bay and aggregated manifest totals. Its single-field key is declared with the
	// primarykey annotation, so the generated list request struct carries the
	// perm:"-" exemption on ShipID from an annotation instead of schema metadata.
	// It is also served on the automation outlet, exercising a read-only projection
	// behind the machine REST API.
	//
	// @virtual
	// @outlet(default, automation)
	ShipCargoSummary struct {
		ShipID             ccc.UUID `spanner:"ShipId"             uniqueindex:"true"` // @primarykey
		ShipName           string   `spanner:"ShipName"           index:"true"`
		DockingBayName     *string  `spanner:"DockingBayName"     index:"true"`
		ManifestLines      int64    `spanner:"ManifestLines"`
		TotalDeclaredValue int64    `spanner:"TotalDeclaredValue"`
	}
)

//go:embed ship_cargo_summaries.sql
var shipCargoSummariesSubquery string

// Subquery provides the embedded SQL projection backing this virtual resource.
func (ShipCargoSummary) Subquery() (query string, params map[string]any) {
	return shipCargoSummariesSubquery, nil
}
