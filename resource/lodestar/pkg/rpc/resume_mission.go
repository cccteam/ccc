package rpc

import (
	"context"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
)

type (
	// ResumeMission moves a mission on_hold -> underway: the back half of the
	// hold/resume loop. The declared edge is the whole effect.
	//
	// Demonstrates: @transition.loop.
	//
	// @rpc
	// @permissionScope(domain)
	// @transition(Mission, from: on_hold, to: underway)
	ResumeMission struct {
		// @target
		MissionID ccc.UUID
	}
)

// Execute runs inside the handler's transaction. Resuming has no effect beyond the declared edge.
func (m *ResumeMission) Execute(context.Context, resource.ReadWriteTransaction, *Client) error {
	return nil
}
