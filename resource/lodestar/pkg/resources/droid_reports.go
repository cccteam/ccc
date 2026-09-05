package resources

import (
	"time"

	"github.com/cccteam/ccc"
)

type (
	// DroidReport is ship telemetry, and it is outlet-exclusive: @outlet(droids)
	// replaces the default outlet, so these routes exist only behind the API key —
	// the generated router tests prove the browser paths 404. Humans see telemetry
	// through the SectorHazardBoard computed resource instead. The read handler is
	// suppressed: the droid surface is list-and-write only.
	//
	// The tenant column is stamped by IngestDroidReports from the ship's hangar; the
	// payload never asserts its own tenancy.
	//
	// @resource
	// @permissionScope(domain)
	// @outlet(droids)
	// @suppress(readHandler)
	DroidReport struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain
		SectorID   string    `spanner:"SectorId"`
		ShipID     ccc.UUID  `spanner:"ShipId"`
		Subsystem  string    `spanner:"Subsystem"`
		Reading    float64   `spanner:"Reading"`
		RecordedAt time.Time `spanner:"RecordedAt"`
	}
)
