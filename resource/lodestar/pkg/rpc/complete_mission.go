package rpc

import (
	"context"
	"fmt"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/go-playground/errors/v5"
)

type (
	// CompleteMission moves a mission underway -> completed: the body brings the open
	// sorties home (ReturnedAt) and computes the settlement — fee minus every booked
	// expense — into the output_only field. The Flight Lead's Execute grant carries
	// `assignedSquadron IN subject.squadrons`: you close your own squadron's missions.
	//
	// It is the method that reads a DECISION AS DATA: it asks the caller's checker
	// (resource.Caller.Check) whether the caller may Update Missions.notes and
	// records a completion note only when the answer is not a denial — the Marshal
	// and the Dispatcher leave a note, the Flight Lead completes without one. The
	// write itself is armed, so a conditional grant is still decided against the row.
	//
	// @rpc
	// @permissionScope(domain)
	// @transition(Mission, from: underway, to: completed)
	CompleteMission struct {
		// @target
		MissionID ccc.UUID
	}
)

// Execute runs inside the handler's transaction.
func (m *CompleteMission) Execute(ctx context.Context, txn resource.ReadWriteTransaction, _ *Client) error {
	sortieIDs, err := returnOpenSorties(ctx, txn, m.MissionID)
	if err != nil {
		return err
	}
	if err := settleMission(ctx, txn, m.MissionID, sortieIDs); err != nil {
		return err
	}

	caller := resource.CallerFrom(ctx)
	decisions, err := caller.Check(ctx, accesstypes.Update, missionNotes)
	if err != nil {
		return errors.Wrap(err, "resource.Caller.Check()")
	}
	if decisions[missionNotes].IsDenied() {
		return nil
	}

	return appendMissionNoteAs(ctx, txn, caller, m.MissionID, fmt.Sprintf("Completed: %d sortie(s) returned", len(sortieIDs)))
}
