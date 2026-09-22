package rpc

import (
	"context"
	"time"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/lodestar/pkg/telemetry"
	"github.com/cccteam/httpio"
)

type (
	// IngestDroidReports is the machine-only RPC: @outlet(droids) replaces the default
	// outlet, so the route exists only behind the API key. The payload is flat, one
	// reading per call (a batch is the droid script calling it in a loop); tenancy
	// resolves through the ship's hangar, so the payload never asserts its own sector. It
	// has no target, so its Execute grant stays row-free. Frame is the reading's raw
	// frame, a telemetry.Frame the request carries as the JSON the firmware wrote and
	// the row stores as it came.
	//
	// Demonstrates: outlet.exclusive, machine-identity, rpc.row-free, typescript.types-package.
	//
	// @rpc
	// @permissionScope(domain)
	// @outlet(droids)
	IngestDroidReports struct {
		ShipID    ccc.UUID
		Subsystem string
		Reading   float64
		// RecordedAt is when the droid took the reading; the ingest time when zero.
		RecordedAt time.Time
		// Frame is the raw frame behind the reading; absent from a droid on old
		// firmware, and then the row's column is NULL.
		Frame telemetry.Frame
	}
)

// Execute runs inside the handler's transaction.
func (m *IngestDroidReports) Execute(ctx context.Context, txn resource.ReadWriteTransaction, _ *Client) error {
	if m.Subsystem == "" {
		return httpio.NewBadRequestMessage("subsystem is required")
	}

	return ingestDroidReport(ctx, txn, m.ShipID, m.Subsystem, m.Reading, m.RecordedAt, m.Frame)
}
