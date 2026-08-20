package rpc

import (
	"context"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/httpio"
)

type (
	// AuthorizeDocking authorizes a ship to dock at a berth of the station named in the
	// URL. It is the domain-scoped RPC demo: the Execute permission is checked in the
	// permission partition of the station route parameter, not the global domain. Like
	// AuthorizeLaunch, the implementation is intentionally minimal.
	//
	// @rpc
	// @permissionScope(domain)
	AuthorizeDocking struct {
		BerthID     ccc.UUID
		DockingCode string
	}
)

// Execute implements TxnRunner.
func (a *AuthorizeDocking) Execute(_ context.Context, _ resource.ReadWriteTransaction, _ *Client) error {
	if a.DockingCode == "" {
		return httpio.NewBadRequestMessage("dockingCode is required")
	}

	return nil
}
