package rpc

import (
	"context"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/httpio"
)

type (
	// HoldMission moves a mission underway -> on_hold, recording the reason on the
	// mission's notes. on_hold is the loop state: ResumeMission leaves it again.
	//
	// @rpc
	// @permissionScope(domain)
	// @transition(Mission, from: underway, to: on_hold)
	HoldMission struct {
		// @target
		MissionID ccc.UUID
		Reason    string
	}
)

// Execute implements TxnRunner.
func (m *HoldMission) Execute(ctx context.Context, txn resource.ReadWriteTransaction, _ *Client) error {
	if m.Reason == "" {
		return httpio.NewBadRequestMessage("reason is required")
	}

	return appendMissionNote(ctx, txn, m.MissionID, "Hold: "+m.Reason)
}
