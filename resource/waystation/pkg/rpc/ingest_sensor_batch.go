package rpc

import (
	"context"
	"time"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/httpio"
)

type (
	// IngestSensorBatch is the machine-only RPC: @outlet(automation) replaces the
	// default outlet, so the route exists only behind the API key. It writes a
	// sensor reading for a facility.
	//
	// @rpc
	// @permissionScope(domain)
	// @outlet(automation)
	IngestSensorBatch struct {
		FacilityID ccc.UUID
		Metric     string
		Reading    float64
		RecordedAt time.Time
	}
)

// Execute implements TxnRunner.
func (m *IngestSensorBatch) Execute(ctx context.Context, txn resource.ReadWriteTransaction, _ *Client) error {
	if m.Metric == "" {
		return httpio.NewBadRequestMessage("metric is required")
	}

	return ingestSensorReading(ctx, txn, m.FacilityID, m.Metric, m.Reading, m.RecordedAt)
}
