package resources

import (
	"context"

	"cloud.google.com/go/civil"
	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/httpio"
	"github.com/shopspring/decimal"
)

type (
	// Requisition is the purchasing workflow's root. RequestedBy is server-stamped
	// from the session; TotalCost is server-owned (starts at zero, recomputed and
	// frozen by SubmitRequisition), so the approval-limit conditions compare against
	// a total the client cannot assert. StatusId carries the @state marker —
	// transitions happen only in the Submit/Approve/Decline RPC bodies.
	//
	// Change tracking is on, feeding the audit trail page.
	//
	// @resource
	// @permissionScope(domain)
	// @defaultsCreateType(RequisitionCreateDefaults)
	// @validateCreateType(RequisitionCreateValidator)
	Requisition struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain
		WaystationID string `spanner:"WaystationId"`
		// @attribute(requestedBy)
		RequestedBy   string  `spanner:"RequestedBy"   conditions:"output_only" default_create_fn:"currentUser"`
		Justification *string `spanner:"Justification"`
		// @attribute(neededBy)
		NeededBy civil.Date `spanner:"NeededBy"`
		// @attribute(totalCost)
		TotalCost decimal.Decimal `spanner:"TotalCost" conditions:"output_only" default_create_fn:"defaultTotalCost"`
		// @state(default: draft)
		StatusID string `spanner:"StatusId"`
	}
)

// Config enables change tracking: Requisition mutations write DataChangeEvents rows
// in the same transaction.
func (Requisition) Config() resource.Config {
	return defaultConfig().SetTrackChanges(true)
}

// RequisitionCreateDefaults is wired in by the @defaultsCreateType annotation; the
// generated create patch calls Defaults inside the mutation transaction, after the
// per-field default functions.
type RequisitionCreateDefaults struct{}

// Defaults fills a missing justification so downstream views never render a hole.
func (d *RequisitionCreateDefaults) Defaults(_ context.Context, _ resource.ReadWriteTransaction, p *RequisitionCreatePatch) error {
	if !p.JustificationIsSet() {
		p.SetJustification(nil)
	}

	return nil
}

// RequisitionCreateValidator is wired in by the @validateCreateType annotation; the
// generated create patch calls Validate inside the mutation transaction.
type RequisitionCreateValidator struct{}

// Validate rejects creates whose needed-by date is unset-zero.
func (v *RequisitionCreateValidator) Validate(_ context.Context, _ resource.ReadWriteTransaction, p *RequisitionCreatePatch) error {
	if p.NeededByIsSet() && p.NeededBy().IsZero() {
		return httpio.NewBadRequestMessage("neededBy must be a real date")
	}

	return nil
}
