package rpc

import (
	"context"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
)

type (
	// CompleteMission moves a mission underway -> completed: the body brings the open
	// sorties home (ReturnedAt) and computes the settlement — fee minus every booked
	// expense — into the output_only field. The Flight Lead's Execute grant carries
	// `assignedSquadron IN subject.squadrons`: you close your own squadron's missions.
	//
	// @rpc
	// @permissionScope(domain)
	// @transition(Mission, from: underway, to: completed)
	CompleteMission struct {
		// @target
		MissionID ccc.UUID
	}
)

// Execute implements TxnRunner.
func (m *CompleteMission) Execute(ctx context.Context, txn resource.ReadWriteTransaction, _ *Client) error {
	sortieIDs, err := returnOpenSorties(ctx, txn, m.MissionID)
	if err != nil {
		return err
	}

	return settleMission(ctx, txn, m.MissionID, sortieIDs)
}
