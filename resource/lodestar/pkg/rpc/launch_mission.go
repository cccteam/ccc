package rpc

import (
	"context"
	"time"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/lodestar/pkg/resources"
	"github.com/cccteam/httpio"
	"github.com/go-playground/errors/v5"
)

type (
	// LaunchMission moves a mission claimed -> underway and creates its first sortie
	// (ship and pilot) in the same transaction — a transition with a visible side
	// effect one hop down the workflow.
	//
	// @rpc
	// @permissionScope(domain)
	// @transition(Mission, from: claimed, to: underway)
	LaunchMission struct {
		// @target
		MissionID   ccc.UUID
		ShipID      ccc.UUID `enumerated:"Ships"`
		PilotUserID string
	}
)

// Execute implements TxnRunner.
func (m *LaunchMission) Execute(ctx context.Context, txn resource.ReadWriteTransaction, _ *Client) error {
	if m.PilotUserID == "" {
		return httpio.NewBadRequestMessage("pilotUserId is required")
	}

	patch, err := resources.NewSortieCreatePatch()
	if err != nil {
		return errors.Wrap(err, "resources.NewSortieCreatePatch()")
	}
	patch.SetMissionID(m.MissionID).
		SetShipID(m.ShipID).
		SetPilotUserID(m.PilotUserID).
		SetLaunchedAt(time.Now().UTC())
	if err := patch.Buffer(ctx, txn, resource.UserEvent(ctx)); err != nil {
		return errors.Wrap(err, "resources.SortieCreatePatch.Buffer()")
	}

	return nil
}
