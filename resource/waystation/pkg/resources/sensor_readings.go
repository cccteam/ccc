package resources

import (
	"time"

	"github.com/cccteam/ccc"
)

type (
	// SensorReading is telemetry, and it is outlet-exclusive: @outlet(automation)
	// replaces the default outlet, so these routes exist only behind the API key —
	// the generated router tests prove the browser-path 404s. Humans see sensor data
	// through the StationStatusBoard computed resource instead. The read handler is
	// suppressed: the automation surface is list-and-write only.
	//
	// @resource
	// @permissionScope(domain)
	// @outlet(automation)
	// @suppress(readHandler)
	SensorReading struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain
		WaystationID string    `spanner:"WaystationId"`
		FacilityID   ccc.UUID  `spanner:"FacilityId"`
		Metric       string    `spanner:"Metric"`
		Reading      float64   `spanner:"Reading"`
		RecordedAt   time.Time `spanner:"RecordedAt"`
	}
)
