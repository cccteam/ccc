package rpc

import (
	"context"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
)

type (
	// StartFlightTest moves a refit in_refit -> flight_test. Its snake-cased name ends
	// in _test, which Go would compile only under go test, so this file carries the
	// _rpc marker the generator derives (978ed79): start_flight_test_rpc.go, with
	// zz_gen_start_flight_test_rpc.go beside it.
	//
	// @rpc
	// @permissionScope(domain)
	// @transition(Refit, from: in_refit, to: flight_test)
	StartFlightTest struct {
		// @target
		RefitID ccc.UUID
	}
)

// Execute implements TxnRunner.
func (m *StartFlightTest) Execute(context.Context, resource.ReadWriteTransaction, *Client) error {
	return nil
}
