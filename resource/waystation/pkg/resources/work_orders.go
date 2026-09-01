package resources

import (
	"time"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
)

type (
	// WorkOrder is the maintenance workflow's root: StatusId carries the @state
	// marker, so the state column is structurally unwritable from the wire — every
	// transition happens inside an RPC body (ScheduleWorkOrder, StartWorkOrder,
	// CompleteWorkOrder) and what each role may do to the data in each state is a
	// conditional grant on the uniform `state` binding.
	//
	// CreatedBy is server-stamped from the session (output_only + default), so
	// `author = subject` conditions compare against a value the client can never
	// forge. Change tracking is on: WorkOrder mutations write DataChangeEvents rows
	// in the same transaction, which the audit trail page renders.
	//
	// UpdatedAt is the mechanical enforcement stamp — its meaning is "this row was
	// updated", nothing more — so it is an output_only_update_fn, stamped on every
	// update. Declaring it also gives the resource the generated NewWorkOrderTouch:
	// an update carried entirely by the update functions, which NudgeWorkOrder fires
	// (contrast Asset.LastServicedAt, a timestamp with domain meaning written as an
	// explicit update by the one transition that owns the business event).
	//
	// @resource
	// @permissionScope(domain)
	WorkOrder struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain
		WaystationID string   `spanner:"WaystationId"`
		AssetID      ccc.UUID `spanner:"AssetId"`
		Title        string   `spanner:"Title"`
		Summary      *string  `spanner:"Summary"`
		// @attribute(priority)
		Priority int64 `spanner:"Priority"`
		// @state(default: draft)
		StatusID string `spanner:"StatusId"`
		// @attribute(author)
		CreatedBy string `spanner:"CreatedBy" conditions:"output_only" default_create_fn:"currentUser"`
		// @attribute(assignedTeam)
		AssignedTeamID ccc.NullUUID `spanner:"AssignedTeamId"`
		// @attribute(dueAt)
		DueAt     *time.Time `spanner:"DueAt"`
		UpdatedAt *time.Time `spanner:"UpdatedAt" output_only_update_fn:"resource.CommitTimestampPtr"`
	}
)

// Config enables change tracking: WorkOrder mutations write DataChangeEvents rows in
// the same transaction.
func (WorkOrder) Config() resource.Config {
	return defaultConfig().SetTrackChanges(true)
}
