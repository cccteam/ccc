package rpc

import (
	"context"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/lodestar/pkg/resources"
	"github.com/cccteam/httpio"
)

type (
	// FailMission moves a mission underway -> failed; the open sorties come home. The
	// reason names the FailReasons enum table with a field-scope @enumerate, so the
	// browser's picker renders the table's values from the generated metadata — no
	// request, no List grant — and the body validates the reason it is sent against the
	// same constants, since a picker is a convenience and never the guard.
	//
	// Demonstrates: @transition, rpc.trusted-body, @enumerate.enum-table.
	//
	// @rpc
	// @permissionScope(domain)
	// @transition(Mission, from: underway, to: failed)
	FailMission struct {
		// @target
		MissionID ccc.UUID
		// @enumerate(FailReasons)
		ReasonID string
	}
)

// Execute runs inside the handler's transaction.
func (m *FailMission) Execute(ctx context.Context, txn resource.ReadWriteTransaction, _ *Client) error {
	switch resources.FailReason(m.ReasonID) {
	case resources.UnrecoverableFailReason, resources.SolarWeatherFailReason, resources.AbortedFailReason, resources.RecalledFailReason:
	default:
		return httpio.NewBadRequestMessagef("reasonId %q is not a fail reason", m.ReasonID)
	}

	if _, err := returnOpenSorties(ctx, txn, m.MissionID); err != nil {
		return err
	}

	// The trusted default: the frame's Execute grant admitted the caller, and the
	// body writes the note as the application — no grant on the notes is consulted.
	// HoldMission is the armed contrast.
	return appendMissionNote(ctx, txn, m.MissionID, "Failed: "+m.ReasonID)
}
