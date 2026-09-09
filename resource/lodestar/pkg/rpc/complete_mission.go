package rpc

import (
	"context"
	"fmt"
	"net/http"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/lodestar/pkg/resources"
	"github.com/cccteam/session/sessioninfo"
	"github.com/go-playground/errors/v5"
	"github.com/shopspring/decimal"
)

type (
	// CompleteMission moves a mission underway -> completed: the body brings the open
	// sorties home (ReturnedAt) and computes the settlement, fee minus every booked
	// expense. The Flight Lead's Execute grant carries `assignedSquadron IN
	// subject.squadrons`: you close your own squadron's missions.
	//
	// It is the method that acts THROUGH A ROLE: the lead who completes a mission holds no
	// Update on Missions.settlement, so the settlement write is armed through caller.As
	// composed over the Paymaster role (RolePrincipalPermissions over the engine's
	// ForRole), a role nobody signs in as. The identity stays the lead's; only the checker
	// changes, and the change event reads "lead as role Paymaster", the same actor-aware
	// event an act-as-role session produces.
	//
	// It is also the method that reads a DECISION AS DATA: it asks the caller's checker
	// (resource.Caller.Check) whether the caller may Update Missions.notes and records a
	// completion note only when the answer is not a denial. The write itself is armed, so a
	// conditional grant is still decided against the row.
	//
	// And it CHOOSES ITS STATUS: it declares @answers(200, 409) and answers with a
	// Settlement, whose HTTPStatus picks 409 when the booked expenses exceed the fee. The
	// 409 is the method's own refusal with the figures as its body; the frame rolls the
	// transaction back, so the sorties stay out and the mission stays underway.
	//
	// Demonstrates: @answers, rpc.as-role, rpc.decision-as-data, rpc.armed-write, rpc.typed-result, @transition, execute-condition.
	//
	// @rpc
	// @permissionScope(domain)
	// @transition(Mission, from: underway, to: completed)
	// @answers(200, 409)
	CompleteMission struct {
		// @target
		MissionID ccc.UUID
	}

	// Settlement is CompleteMission's answer: the fee, the expenses booked against the
	// mission's sorties, and the net. A negative net is the 409 (the completion was
	// refused because the expenses exceed the fee), and the sorties carry each one's share
	// so the caller can see where the money went.
	Settlement struct {
		Fee      decimal.Decimal
		Expenses decimal.Decimal
		Net      decimal.Decimal
		Sorties  []SortieCost
	}

	// SortieCost is one sortie's booked expenses.
	SortieCost struct {
		SortieID ccc.UUID
		Expenses decimal.Decimal
	}
)

// HTTPStatus chooses the response: 409 when the expenses exceed the fee, 200 otherwise.
// The frame checks the choice against @answers(200, 409).
func (s *Settlement) HTTPStatus() int {
	if s.Net.IsNegative() {
		return http.StatusConflict
	}

	return http.StatusOK
}

// Execute runs inside the handler's transaction.
func (m *CompleteMission) Execute(ctx context.Context, txn resource.ReadWriteTransaction, client *Client) (*Settlement, error) {
	sortieIDs, err := returnOpenSorties(ctx, txn, m.MissionID)
	if err != nil {
		return nil, err
	}
	settlement, err := settleMission(ctx, txn, m.MissionID, sortieIDs)
	if err != nil {
		return nil, err
	}
	if settlement.HTTPStatus() != http.StatusOK {
		// The refusal: the frame rolls back everything above.
		return settlement, nil
	}

	caller := resource.CallerFrom(ctx)
	if err := postSettlementAsPaymaster(ctx, txn, caller, client, m.MissionID, settlement.Net); err != nil {
		return nil, err
	}

	decisions, err := caller.Check(ctx, accesstypes.Update, missionNotes)
	if err != nil {
		return nil, errors.Wrap(err, "resource.Caller.Check()")
	}
	if decisions[missionNotes].IsDenied() {
		return settlement, nil
	}
	if err := appendMissionNoteAs(ctx, txn, caller, m.MissionID, fmt.Sprintf("Completed: %d sortie(s) returned", len(sortieIDs))); err != nil {
		return nil, err
	}

	return settlement, nil
}

// postSettlementAsPaymaster writes the settlement through the Paymaster role's checker:
// the caller's identity, the role's grants. The event source names both, as an
// act-as-role session's would.
func postSettlementAsPaymaster(ctx context.Context, txn resource.ReadWriteTransaction, caller *resource.Caller, client *Client, missionID ccc.UUID, net decimal.Decimal) error {
	actor := caller.Permissions.User()
	paymaster := caller.As(resource.RolePrincipalPermissions(client.ForRole(PaymasterRole), actor))
	event := fmt.Sprintf("%s as role %s (%s)", actor, PaymasterRole, sessioninfo.FromCtx(ctx).ID)

	if err := resources.NewMissionUpdatePatch(missionID).SetSettlement(decimal.NewNullDecimal(net)).Enforce(paymaster).Buffer(ctx, txn, event); err != nil {
		return errors.Wrap(err, "resources.MissionUpdatePatch.Buffer()")
	}

	return nil
}
