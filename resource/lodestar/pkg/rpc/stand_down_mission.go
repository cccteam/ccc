package rpc

import (
	"context"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
)

type (
	// StandDownMission is the three-source edge: the client stands the call down from
	// open, claimed, or on_hold — never from underway, which must be held first; the
	// generated frame refuses that with the transition's name. It is the only method
	// the portal serves: Client Cleo fires it from a portal session under a grant
	// carrying `client = subject.client`, while the Booking Agent's grant carries
	// `bookedBy = subject`.
	//
	// @rpc
	// @permissionScope(domain)
	// @outlet(default, portal)
	// @transition(Mission, from: open, claimed, on_hold, to: stood_down)
	StandDownMission struct {
		// @target
		MissionID ccc.UUID
	}
)

// Execute implements TxnRunner. Standing down has no effect beyond the declared edge.
func (m *StandDownMission) Execute(context.Context, resource.ReadWriteTransaction, *Client) error {
	return nil
}
