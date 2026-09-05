package rpc

import (
	"context"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/lodestar/pkg/resources"
	"github.com/cccteam/httpio"
)

type (
	// FailMission moves a mission underway -> failed. The reason is validated in the
	// body against the FailReasons constants (enum tables are not referencable by
	// enumerated: today — finding 4); the open sorties come home.
	//
	// @rpc
	// @permissionScope(domain)
	// @transition(Mission, from: underway, to: failed)
	FailMission struct {
		// @target
		MissionID ccc.UUID
		ReasonID  string
	}
)

// Execute implements TxnRunner.
func (m *FailMission) Execute(ctx context.Context, txn resource.ReadWriteTransaction, _ *Client) error {
	switch resources.FailReason(m.ReasonID) {
	case resources.UnrecoverableFailReason, resources.SolarWeatherFailReason, resources.AbortedFailReason, resources.RecalledFailReason:
	default:
		return httpio.NewBadRequestMessagef("reasonId %q is not a fail reason", m.ReasonID)
	}

	if _, err := returnOpenSorties(ctx, txn, m.MissionID); err != nil {
		return err
	}

	return appendMissionNote(ctx, txn, m.MissionID, "Failed: "+m.ReasonID)
}
