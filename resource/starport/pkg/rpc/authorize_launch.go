package rpc

import (
	"context"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/httpio"
)

type (
	// AuthorizeLaunch authorizes a ship for departure. The starport only exercises the
	// generated wiring and the Execute permission gate, so the implementation is
	// intentionally minimal.
	//
	// @rpc
	AuthorizeLaunch struct {
		ShipID     ccc.UUID
		LaunchCode string
	}
)

// Execute implements TxnRunner.
func (a *AuthorizeLaunch) Execute(_ context.Context, _ resource.ReadWriteTransaction, _ *Client) error {
	if a.LaunchCode == "" {
		return httpio.NewBadRequestMessage("launchCode is required")
	}

	return nil
}
