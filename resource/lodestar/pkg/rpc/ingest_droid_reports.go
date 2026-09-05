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
	// outlet, so the route exists only behind the API key. It writes one telemetry
	// reading per call (the RPC payload grammar is flat; a batch is the droid script
	// calling it in a loop), resolving tenancy through the ship's hangar. It has no
	// target, so its Execute grant stays row-free.
	//
	// @rpc
	// @permissionScope(domain)
	// @outlet(droids)
	IngestDroidReports struct {
		ShipID     ccc.UUID
		Subsystem  string
		Reading    float64
		RecordedAt time.Time
	}
)

// Execute implements TxnRunner.
func (m *IngestDroidReports) Execute(ctx context.Context, txn resource.ReadWriteTransaction, _ *Client) error {
	if m.Subsystem == "" {
		return httpio.NewBadRequestMessage("subsystem is required")
	}

	return ingestDroidReport(ctx, txn, m.ShipID, m.Subsystem, m.Reading, m.RecordedAt)
}
