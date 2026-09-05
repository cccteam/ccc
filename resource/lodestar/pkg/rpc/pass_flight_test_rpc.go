package rpc

import (
	"context"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
)

type (
	// PassFlightTest moves a refit flight_test -> cleared and stamps the ship's
	// LastRefitAt with the commit timestamp — the one transition that owns that
	// business event. (Marked file: see StartFlightTest.)
	//
	// @rpc
	// @permissionScope(domain)
	// @transition(Refit, from: flight_test, to: cleared)
	PassFlightTest struct {
		// @target
		RefitID ccc.UUID
	}
)

// Execute implements TxnRunner.
func (m *PassFlightTest) Execute(ctx context.Context, txn resource.ReadWriteTransaction, _ *Client) error {
	return stampShipRefitted(ctx, txn, m.RefitID)
}
