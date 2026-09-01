package rpc

import (
	"context"

	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/httpio"
)

type (
	// RunSafetyDrill is the global RPC and the row-free Execute-condition demo: the
	// safety officer's Execute grant carries `now < '2027-06-30T00:00:00Z'` — a
	// drill authorization with an expiry, folded at decode time (no row exists to
	// evaluate against).
	//
	// @rpc
	RunSafetyDrill struct {
		Announcement string
	}
)

// Execute implements TxnRunner.
func (m *RunSafetyDrill) Execute(_ context.Context, _ resource.ReadWriteTransaction, _ *Client) error {
	if m.Announcement == "" {
		return httpio.NewBadRequestMessage("announcement is required")
	}

	return nil
}
