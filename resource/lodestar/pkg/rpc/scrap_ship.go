package rpc

import (
	"context"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
)

type (
	// ScrapShip is the second multi-from edge, on the second workflow: three bays
	// (inspected, in_refit, flight_test) send a ship to the scrapyard. Only the
	// Sector Marshal holds it.
	//
	// Demonstrates: @transition.multi-from.
	//
	// @rpc
	// @permissionScope(domain)
	// @transition(Refit, from: inspected, in_refit, flight_test, to: scrapped)
	ScrapShip struct {
		// @target
		RefitID ccc.UUID
	}
)

// Execute runs inside the handler's transaction.
func (m *ScrapShip) Execute(context.Context, resource.ReadWriteTransaction, *Client) error {
	return nil
}
