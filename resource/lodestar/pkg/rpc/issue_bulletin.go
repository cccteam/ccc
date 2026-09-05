package rpc

import (
	"context"

	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/httpio"
)

type (
	// IssueBulletin is the global, row-free RPC: the Bulletin Officer's Execute grant
	// carries `now < '2027-06-30T00:00:00Z'` — an authorization with an expiry, folded
	// at decode time since no row exists to evaluate against.
	//
	// @rpc
	IssueBulletin struct {
		Announcement string
	}
)

// Execute implements TxnRunner.
func (m *IssueBulletin) Execute(_ context.Context, _ resource.ReadWriteTransaction, _ *Client) error {
	if m.Announcement == "" {
		return httpio.NewBadRequestMessage("announcement is required")
	}

	return nil
}
