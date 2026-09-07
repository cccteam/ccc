package rpc

import (
	"context"
	"time"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/httpio"
)

type (
	// IngestDroidReports is the machine-only RPC: @outlet(droids) replaces the default
	// outlet, so the route exists only behind the API key. One call carries a BATCH
	// of readings for one ship, so the request is a NESTED shape: the generator walks
	// DroidReading into a local mirror in the handler and builds this struct from
	// the decoded mirror through a pinned view, so a change here is a compile error
	// in the handler until the generator runs again. Tenancy resolves through the
	// ship's hangar. It has no target, so its Execute grant stays row-free.
	//
	// @rpc
	// @permissionScope(domain)
	// @outlet(droids)
	IngestDroidReports struct {
		ShipID   ccc.UUID
		Readings []DroidReading
	}

	// DroidReading is one reading in the batch: the subsystem it measures, the value,
	// and when the droid recorded it (the ingest time when zero).
	DroidReading struct {
		Subsystem  string
		Reading    float64
		RecordedAt time.Time
	}
)

// Execute runs inside the handler's transaction: every reading in the batch lands
// or none does.
func (m *IngestDroidReports) Execute(ctx context.Context, txn resource.ReadWriteTransaction, _ *Client) error {
	if len(m.Readings) == 0 {
		return httpio.NewBadRequestMessage("at least one reading is required")
	}
	for _, r := range m.Readings {
		if r.Subsystem == "" {
			return httpio.NewBadRequestMessage("subsystem is required on every reading")
		}
		if err := ingestDroidReport(ctx, txn, m.ShipID, r.Subsystem, r.Reading, r.RecordedAt); err != nil {
			return err
		}
	}

	return nil
}
