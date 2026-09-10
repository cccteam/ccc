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
	// It is the method with an ARMED body: the note is written as the caller
	// (appendMissionNoteAs), so who may record a reason is the caller's Update grant
	// on Missions.notes, decided inside the transaction — the Marshal always, the
	// Dispatcher while the mission is live, the Flight Lead never, though all three
	// may Execute the method. The web app shows the refusal in the grant's words.
	//
	// Demonstrates: @transition.loop, rpc.armed-write, rpc.dry-run.
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

// Execute runs inside the handler's transaction.
func (m *HoldMission) Execute(ctx context.Context, txn resource.ReadWriteTransaction, _ *Client) error {
	if m.Reason == "" {
		return httpio.NewBadRequestMessage("reason is required")
	}

	return appendMissionNoteAs(ctx, txn, resource.CallerFrom(ctx), m.MissionID, "Hold: "+m.Reason)
}
