package resources

import (
	"context"
	"time"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/httpio"
	"github.com/shopspring/decimal"
)

type (
	// Mission is the first workflow root: StatusId carries the @state marker, so the
	// state column is structurally unwritable from the wire — every one of the seven
	// transitions happens inside an RPC body, and what each role may do to a mission
	// in each state is a conditional grant on the uniform `state` binding.
	//
	// BookedBy is server-stamped from the session (output_only + default), so
	// `bookedBy = subject` compares against a value the client can never forge.
	// Settlement is output_only, written by CompleteMission (fee minus expenses). The
	// attributes are the vocabulary of §7's grants: kind, hazard, fee (the masking
	// target), deadline (the overdue clock), requiredCert, assignedSquadron, client
	// (so a portal grant can say `client = subject.client`), and notes.
	//
	// Mission is served on the portal outlet too: clients track their own missions
	// under a grant whose width never includes assignedSquadron, notes, or settlement.
	// Change tracking is on, feeding the ship's log.
	//
	// @resource
	// @permissionScope(domain)
	// @outlet(default, portal)
	// @defaultsCreateType(MissionCreateDefaults)
	// @validateCreateType(MissionCreateValidator)
	Mission struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain
		SectorID string `spanner:"SectorId"`
		// @attribute(client)
		ClientID ccc.UUID `spanner:"ClientId"`
		// @attribute(kind)
		KindID string  `spanner:"KindId"`
		Title  string  `spanner:"Title"`
		Brief  *string `spanner:"Brief"`
		// @attribute(hazard)
		Hazard int64 `spanner:"Hazard"`
		// @attribute(fee)
		Fee decimal.Decimal `spanner:"Fee"`
		// @attribute(deadline)
		Deadline time.Time `spanner:"Deadline"`
		// @attribute(requiredCert)
		RequiredCertID *string `spanner:"RequiredCertId"`
		// @attribute(bookedBy)
		BookedBy string `spanner:"BookedBy" conditions:"output_only" default_create_fn:"currentUser"`
		// @attribute(assignedSquadron)
		AssignedSquadronID ccc.NullUUID `spanner:"AssignedSquadronId"`
		// @state(default: open)
		StatusID string `spanner:"StatusId"`
		// @attribute(notes)
		Notes *string `spanner:"Notes"`
		// @attribute(settlement)
		Settlement decimal.NullDecimal `spanner:"Settlement" conditions:"output_only"`
	}
)

// Config enables change tracking: Mission mutations write DataChangeEvents rows in
// the same transaction, which the ship's log renders.
func (Mission) Config() resource.Config {
	return defaultConfig().SetTrackChanges(true)
}

// MissionCreateDefaults is wired in by the @defaultsCreateType annotation; the
// generated create patch calls Defaults inside the mutation transaction, after the
// per-field default functions.
type MissionCreateDefaults struct{}

// Defaults fills a missing brief so call sheets never render a hole, and starts an
// unbooked hazard at the lowest class.
func (d *MissionCreateDefaults) Defaults(_ context.Context, _ resource.ReadWriteTransaction, p *MissionCreatePatch) error {
	if !p.BriefIsSet() {
		p.SetBrief(nil)
	}
	if !p.HazardIsSet() {
		p.SetHazard(1)
	}

	return nil
}

// MissionCreateValidator is wired in by the @validateCreateType annotation; the
// generated create patch calls Validate inside the mutation transaction.
type MissionCreateValidator struct{}

// Validate rejects a mission whose deadline is not in the future — someone is
// waiting, so a call sheet with a past deadline is a mistake, not a mission — and a
// hazard outside the 1..5 scale.
func (v *MissionCreateValidator) Validate(_ context.Context, _ resource.ReadWriteTransaction, p *MissionCreatePatch) error {
	if p.DeadlineIsSet() && !p.Deadline().After(time.Now()) {
		return httpio.NewBadRequestMessage("deadline must be in the future")
	}
	if p.HazardIsSet() && (p.Hazard() < 1 || p.Hazard() > 5) {
		return httpio.NewBadRequestMessage("hazard must be between 1 and 5")
	}

	return nil
}
