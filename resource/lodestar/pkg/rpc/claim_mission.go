package rpc

import (
	"context"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/lodestar/pkg/resources"
	"github.com/go-playground/errors/v5"
)

type (
	// ClaimMission moves a mission open -> claimed and assigns the squadron that
	// claimed it. Eligibility is NOT in the body: the Cadet's grant carries
	// `hazard IN (1, 2)` and the Pilot's `hazard <= subject.clearance AND
	// (requiredCert IS NULL OR requiredCert IN subject.certifications)`, evaluated by
	// the generated frame against the located row inside this transaction (E5). The
	// body only records who claimed.
	//
	// @rpc
	// @permissionScope(domain)
	// @transition(Mission, from: open, to: claimed)
	ClaimMission struct {
		// @target
		MissionID  ccc.UUID
		SquadronID ccc.UUID `enumerated:"Squadrons"`
	}
)

// Execute implements TxnRunner.
func (m *ClaimMission) Execute(ctx context.Context, txn resource.ReadWriteTransaction, _ *Client) error {
	squadron := ccc.NullUUID{UUID: m.SquadronID, Valid: true}
	if err := resources.NewMissionUpdatePatch(m.MissionID).SetAssignedSquadronID(squadron).Buffer(ctx, txn, resource.UserEvent(ctx)); err != nil {
		return errors.Wrap(err, "resources.MissionUpdatePatch.Buffer()")
	}

	return nil
}
