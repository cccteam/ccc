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
	// Mission is the first workflow root: StatusId carries the @state marker, so the state
	// column is structurally unwritable from the wire. Every one of the seven transitions
	// happens inside an RPC body, and what each role may do to a mission in each state is
	// a conditional grant on the uniform state binding.
	//
	// BookedBy is server-stamped from the session (output_only + default), so bookedBy =
	// subject compares against a value the client can never forge. Settlement is written by
	// CompleteMission through the Paymaster role: no persona holds an Update on it, so the
	// only writer is a body borrowing that role's checker. The attributes are the
	// vocabulary of the grants: kind, hazard, fee (the masking target), deadline (the
	// overdue clock), requiredCert, assignedSquadron, client (so a portal grant can say
	// client = subject.client), and notes.
	//
	// Mission is served on the portal outlet too: clients track their own missions under
	// a grant whose width never includes assignedSquadron, notes, or settlement. Change
	// tracking is on, feeding the ship's log. The list is ordered by deadline and paged at
	// twenty-five, the size the flight deck reads from the descriptor.
	//
	// Two fields declare which resource their picker lists (the field-scope form of
	// the enumerate annotation). ClientID is a foreign key into Clients, and it names
	// ClientRosters, a sector-scoped view over Clients keyed by the same id that carries
	// the contact and mission counts a booking picker wants; the constraint stays the
	// guard at write time. BriefingTemplateID is a plain column with no foreign key: it
	// holds an identifier from the BriefingTemplates catalog, which lives in Go, not in
	// the schema, so nothing but the declaration says what the picker lists.
	//
	// Fee is allow_filter on an unindexed column, so its metadata says filterable:
	// withIndexed — the browser offers a fee filter only beside one on an indexed
	// column, the same rule the database parse enforces. Deadline is that indexed
	// column without leading any index: it sits directly after SectorId in
	// MissionsBySectorIdDeadline (migration 000032), and the list binds the sector by
	// equality, so a deadline filter alone seeks that index; the generated list struct
	// tags Deadline index:"true" and its metadata says filterable: always.
	//
	// Deadline, the default order, is declared masking:"positional": a deadline's rank
	// is not sensitive, so the archivist's every page orders on the real column and
	// comes off the index, while a masked deadline cell would still arrive hidden. Fee
	// stays concealing — where a fee falls among the others is what the mask protects —
	// so the archivist's fee filter runs over the visible projection, and the deploy
	// warns that her pages filtered by fee sort the partition (pkg/deploy).
	//
	// Hazard is guarded at two points. The create validator answers a hazard outside 1..5
	// as 400 naming the field before anything is buffered; the update path has no
	// validator, so the schema's CK_Missions_Hazard (migration 000033) refuses the same
	// value at commit, and the library answers that as 400 naming Missions.
	//
	// Fee is NUMERIC, so its create and update requests carry sqltype:"NUMERIC": a fee
	// with ten decimals answers 400 naming the field at decode, on a create and on a
	// PATCH, while nine decimals are accepted; the schema's InvalidArgument at commit is
	// never reached from a request.
	//
	// Demonstrates: @state, @attribute, @attribute.decimal, @attribute.timestamp, @attribute.nullable-fk, output_only, default_create_fn, @defaultsCreateType, @validateCreateType, change-tracking, outlet.shared, @order, @page, cell-masking, paging.masked-sort, masking.positional, filter.typed-values, filter.validated-at-decode, condition.now, condition.not-in, condition.prefix-not, condition.subject-scalar, condition.old-vs-new, write-grouping, @attribute.join-path-global, @enumerate.plain-column, @enumerate.key-view, metadata.filterable, index.tenant-second, commit.constraint-refusal, decode.value-limit.
	//
	// @resource
	// @permissionScope(domain)
	// @outlet(default, portal)
	// @order(Deadline asc)
	// @page(default: 25, max: 200)
	// @defaultsCreateType(MissionCreateDefaults)
	// @validateCreateType(MissionCreateValidator)
	Mission struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain
		SectorID string `spanner:"SectorId"`
		// @attribute(client)
		// @enumerate(ClientRosters)
		ClientID ccc.UUID `spanner:"ClientId"`
		// @attribute(kind)
		KindID string  `spanner:"KindId"`
		Title  string  `spanner:"Title"`
		Brief  *string `spanner:"Brief"`
		// @enumerate(BriefingTemplates)
		BriefingTemplateID *string `spanner:"BriefingTemplateId"`
		// @attribute(hazard)
		Hazard int64 `spanner:"Hazard"`
		// @attribute(fee)
		// Filterable so the visible projection can be exercised: the archivist's fee is
		// masked until a mission completes, and a masked cell matches only isnull.
		Fee decimal.Decimal `spanner:"Fee" allow_filter:"true"`
		// @attribute(deadline)
		Deadline time.Time `spanner:"Deadline" masking:"positional"`
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
		Settlement decimal.NullDecimal `spanner:"Settlement"`
	}
)

// Config enables change tracking: Mission mutations write DataChangeEvents rows in the
// same transaction, which the ship's log renders.
func (Mission) Config() resource.Config {
	return defaultConfig().SetTrackChanges(true)
}

// MissionCreateDefaults is wired in by the @defaultsCreateType annotation; the generated
// create patch calls Defaults inside the mutation transaction, after the per-field
// default functions.
//
// Demonstrates: @defaultsCreateType.
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

// MissionCreateValidator is wired in by the @validateCreateType annotation; the generated
// create patch calls Validate inside the mutation transaction.
//
// Demonstrates: @validateCreateType.
type MissionCreateValidator struct{}

// Validate rejects a mission whose deadline is not in the future (someone is waiting, so
// a call sheet with a past deadline is a mistake, not a mission) and a hazard outside the
// 1..5 scale.
func (v *MissionCreateValidator) Validate(_ context.Context, _ resource.ReadWriteTransaction, p *MissionCreatePatch) error {
	if p.DeadlineIsSet() && !p.Deadline().After(time.Now()) {
		return httpio.NewBadRequestMessage("deadline must be in the future")
	}
	if p.HazardIsSet() && (p.Hazard() < 1 || p.Hazard() > 5) {
		return httpio.NewBadRequestMessage("hazard must be between 1 and 5")
	}

	return nil
}
