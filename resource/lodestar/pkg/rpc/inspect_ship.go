package rpc

import (
	"context"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
)

type (
	// InspectShip moves a refit docked -> inspected and writes Refit.InspectedAt
	// explicitly — transition-owned domain data, not an update function. The
	// Engineer's estimate grant carries `inspectedAt IS NOT NULL`, so the stamp is
	// what unlocks the estimate.
	//
	// Refit's tenancy is a two-hop join path (Ship → Hangar → SectorId): the frame
	// verifies it with one check-SELECT in this transaction (c2b62ab).
	//
	// @rpc
	// @permissionScope(domain)
	// @transition(Refit, from: docked, to: inspected)
	InspectShip struct {
		// @target
		RefitID ccc.UUID
	}
)

// Execute implements TxnRunner.
func (m *InspectShip) Execute(ctx context.Context, txn resource.ReadWriteTransaction, _ *Client) error {
	return stampRefitInspected(ctx, txn, m.RefitID)
}
