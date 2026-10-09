package rpc

import (
	"context"

	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/httpio"
)

type (
	// IssueBulletin is the global, row-free RPC: the Bulletin Officer's Execute grant
	// carries `now < '2099-06-30T00:00:00Z'` — an authorization with an expiry, folded
	// at decode time since no row exists to evaluate against. A bulletin is short, so
	// the method declares its own request body limit, 64 KB in place of the
	// application's 2 MiB: its generated handler applies it before decoding, and a
	// longer body answers 413 naming the limit.
	//
	// Demonstrates: rpc.row-free, condition.now, @rpc.max.
	//
	// @rpc(max: 64KB)
	IssueBulletin struct {
		Announcement string
	}
)

// Execute runs inside the handler's transaction.
func (m *IssueBulletin) Execute(_ context.Context, _ resource.ReadWriteTransaction, _ *Client) error {
	if m.Announcement == "" {
		return httpio.NewBadRequestMessage("announcement is required")
	}

	return nil
}
