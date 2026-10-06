package resources

import (
	"time"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource/lodestar/pkg/telemetry"
)

type (
	// DroidReport is ship telemetry, and it is outlet-exclusive: @outlet(droids) replaces
	// the default outlet, so these routes exist only behind the API key; the generated
	// router tests prove the browser paths 404. Humans see telemetry through the
	// SectorHazardBoard computed resource instead. The read handler is suppressed: the
	// droid surface is list-and-write only.
	//
	// The tenant column is stamped by IngestDroidReports from the ship's hangar; the
	// payload never asserts its own tenancy.
	//
	// The table has no index leading with SectorId then RecordedAt on purpose: the
	// resource is the registered demonstration of the generator's index warning, which
	// names the CREATE INDEX the list wants at every generate. Every other bare-tenant
	// resource that lists in a declared order carries that index (migration 000032).
	//
	// Frame is the reading's raw frame, a telemetry.Frame: the type lives in the droid
	// link's own package, which cmd/generate names with WithTypes, so the generator
	// writes the frame's JSON and Spanner methods there and the type stays where the
	// link is modeled.
	//
	// Demonstrates: outlet.exclusive, @suppress, machine-identity, warning.tenant-index, typescript.types-package.
	//
	// @resource
	// @permissionScope(domain)
	// @outlet(droids)
	// @suppress(readHandler)
	// @order(RecordedAt desc)
	// @page(default: 25, max: 200)
	DroidReport struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain
		SectorID   string    `spanner:"SectorId"`
		ShipID     ccc.UUID  `spanner:"ShipId"`
		Subsystem  string    `spanner:"Subsystem"`
		Reading    float64   `spanner:"Reading"`
		RecordedAt time.Time `spanner:"RecordedAt"`
		// Frame is nullable: the seeded readings and a droid on old firmware send none.
		Frame *telemetry.Frame `spanner:"Frame"`
	}
)
