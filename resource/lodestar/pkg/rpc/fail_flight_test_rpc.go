package rpc

import (
	"context"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
)

type (
	// FailFlightTest is the backward edge: flight_test -> in_refit, so the ship
	// slides back a bay and the in_refit state is entered twice. (Marked file: see
	// StartFlightTest.)
	//
	// Demonstrates: @transition.loop, test-suffixed-method.
	//
	// @rpc
	// @permissionScope(domain)
	// @transition(Refit, from: flight_test, to: in_refit)
	FailFlightTest struct {
		// @target
		RefitID ccc.UUID
	}
)

// Execute runs inside the handler's transaction.
func (m *FailFlightTest) Execute(context.Context, resource.ReadWriteTransaction, *Client) error {
	return nil
}
