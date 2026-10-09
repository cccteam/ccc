package app

import (
	"net/http"
	"time"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource/lodestar/pkg/router"
	"github.com/cccteam/httpio"
)

// beaconPulse is a beacon's answer: the sector it stands in and when it answered.
type beaconPulse struct {
	Sector accesstypes.Domain `json:"sector"`
	At     time.Time          `json:"at"`
}

// SectorBeacon answers a droid asking whether a sector is charted: 200 with the pulse
// for a sector the tenant roster holds, 404 for any other. The route sits outside every
// outlet (hooks.Root), with no session and no key, since a beacon tells a droid where it
// is before it holds anything else, and it is asked all day by every droid in the sky.
// That is why the generator program declares the prefix quiet: a pulse answered 200
// writes no request log entry and records no span, while a pulse answered 404, a droid
// asking after a sector that is not charted, is an event and writes its entry.
//
// Demonstrates: generation.request-log, generation.traces.
func (a *App) SectorBeacon() http.HandlerFunc {
	return httpio.Log(func(w http.ResponseWriter, r *http.Request) error {
		ctx := r.Context()
		sector := httpio.Param[accesstypes.Domain](r, router.Domain)
		if !a.tenants.Has(sector) {
			return httpio.NewEncoder(w).ClientMessage(ctx, httpio.NewNotFoundMessagef("no beacon stands in sector %s", sector))
		}

		return httpio.NewEncoder(w).Ok(beaconPulse{Sector: sector, At: time.Now().UTC()})
	})
}
