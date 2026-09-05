package resources

import (
	"github.com/cccteam/ccc"
)

type (
	// DistressCall is an incoming call before it becomes a mission, and the
	// field-direction showcase: CallerContact is PII (rejected in URL filters,
	// flagged in TS metadata) and nullable — the Cadet's partial-width Create grant
	// (summary, severity) files calls without it, so the column must accept the
	// narrowed create (§7's width rule); Transcript is input_only (accepted on
	// mutations, never serialized back); CaseNumber is output_only with a
	// server-issued DC- default; FiledBy is output_only, stamped from the session, and
	// the attribute the portal's `filedBy = subject` grant reads.
	//
	// Served on the portal outlet too: Client Cleo files calls through a three-field
	// form — the one PII field an external user writes.
	//
	// @resource
	// @permissionScope(domain)
	// @outlet(default, portal)
	DistressCall struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain
		SectorID      string  `spanner:"SectorId"`
		Summary       string  `spanner:"Summary"`
		Severity      int64   `spanner:"Severity"`
		CallerContact *string `spanner:"CallerContact" conditions:"pii"`
		Transcript    *string `spanner:"Transcript"    conditions:"input_only"`
		CaseNumber    string  `spanner:"CaseNumber"    conditions:"output_only" default_create_fn:"defaultCaseNumber"`
		// @attribute(filedBy)
		FiledBy string `spanner:"FiledBy" conditions:"output_only" default_create_fn:"currentUser"`
	}
)
