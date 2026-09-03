package resources

import (
	"context"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/httpio"
)

type (
	// IncidentReport is the field-direction showcase: ReporterContact is PII
	// (rejected in URL filters, flagged in TS metadata) and nullable — a persona
	// whose Create grant excludes it (the technician) files reports without it,
	// RawStatement is input_only (accepted on mutations, never serialized back),
	// and CaseNumber is output_only with a server-issued default — supplying it
	// is a 400, reading it always works.
	// The update path runs both a defaults type and a validator type, the update-side
	// twins of Requisition's create-side pair.
	//
	// @resource
	// @permissionScope(domain)
	// @defaultsUpdateType(IncidentReportUpdateDefaults)
	// @validateUpdateType(IncidentReportUpdateValidator)
	IncidentReport struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain
		WaystationID    string  `spanner:"WaystationId"`
		Summary         string  `spanner:"Summary"`
		Severity        int64   `spanner:"Severity"`
		ReporterContact *string `spanner:"ReporterContact" conditions:"pii"`
		RawStatement    *string `spanner:"RawStatement"    conditions:"input_only"`
		CaseNumber      string  `spanner:"CaseNumber"      conditions:"output_only" default_create_fn:"defaultCaseNumber"`
	}
)

// IncidentReportUpdateDefaults is wired in by the @defaultsUpdateType annotation; the
// generated update patch calls Defaults inside the mutation transaction.
type IncidentReportUpdateDefaults struct{}

// Defaults clamps severity into the 1..5 scale rather than rejecting out-of-range
// updates outright.
func (d *IncidentReportUpdateDefaults) Defaults(_ context.Context, _ resource.ReadWriteTransaction, p *IncidentReportUpdatePatch) error {
	if p.SeverityIsSet() {
		switch {
		case p.Severity() < 1:
			p.SetSeverity(1)
		case p.Severity() > 5:
			p.SetSeverity(5)
		}
	}

	return nil
}

// IncidentReportUpdateValidator is wired in by the @validateUpdateType annotation; the
// generated update patch calls Validate inside the mutation transaction.
type IncidentReportUpdateValidator struct{}

// Validate rejects updates that blank the summary.
func (v *IncidentReportUpdateValidator) Validate(_ context.Context, _ resource.ReadWriteTransaction, p *IncidentReportUpdatePatch) error {
	if p.SummaryIsSet() && p.Summary() == "" {
		return httpio.NewBadRequestMessage("summary cannot be empty")
	}

	return nil
}
