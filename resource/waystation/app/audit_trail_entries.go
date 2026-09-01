package app

import (
	"net/http"
	"time"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource/waystation/pkg/resources"
	"github.com/cccteam/httpio"
	"github.com/cccteam/spxscan"
	"github.com/go-playground/errors/v5"
)

// auditTrailEntry is one DataChangeEvents row on the wire. The change-tracked
// resources (WorkOrders, Requisitions) write these inside their mutation
// transactions; the audit page renders them newest-first.
type auditTrailEntry struct {
	TableName   string           `json:"tableName"   spanner:"TableName"`
	RowID       string           `json:"rowId"       spanner:"RowId"`
	Sequence    int64            `json:"sequence"    spanner:"Sequence"`
	EventTime   time.Time        `json:"eventTime"   spanner:"EventTime"`
	EventSource string           `json:"eventSource" spanner:"EventSource"`
	ChangeSet   spanner.NullJSON `json:"changeSet"   spanner:"ChangeSet"`
}

// auditTrailLimit caps the audit page at the newest events; the demo carries no
// pagination on this surface.
const auditTrailLimit = 200

// AuditTrailEntries lists the change-tracking events, newest first. The handler is
// hand-written — DataChangeEvents is library infrastructure, not a schema resource,
// so no handler is generated — and its permission is declared to the generator via
// the @manualAddResource annotation on resources.AuditTrailEntries. The check here
// is what that registration promises: List on AuditTrailEntries in the global
// scope, fail-closed. A Conditional decision is refused too: a hand-written surface
// has no bindings for the engine's conditions to reference, so only an
// unconditional grant can open it.
func (a *App) AuditTrailEntries() http.HandlerFunc {
	return httpio.Log(func(w http.ResponseWriter, r *http.Request) error {
		ctx := r.Context()

		env := accesstypes.NewEnvironment().WithNow(time.Now())
		decisions, err := a.UserPermissions(r).Check(ctx, env, accesstypes.GlobalScope(), accesstypes.List, resources.AuditTrailEntries)
		if err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, errors.Wrap(err, "resource.UserPermissions.Check()"))
		}
		if !decisions[resources.AuditTrailEntries].IsGranted() {
			return httpio.NewEncoder(w).ClientMessage(ctx, httpio.NewForbiddenMessagef(
				"user %s does not have List on %s", a.UserPermissions(r).User(), resources.AuditTrailEntries,
			))
		}

		txn := a.resourceClient.ReadOnlyTransaction()
		defer txn.Close()

		entries := make([]auditTrailEntry, 0, auditTrailLimit)
		stmt := spanner.Statement{
			SQL: `SELECT TableName, RowId, Sequence, EventTime, EventSource, ChangeSet
			        FROM DataChangeEvents
			       ORDER BY EventTime DESC, TableName, RowId, Sequence DESC
			       LIMIT @limit`,
			Params: map[string]any{"limit": auditTrailLimit},
		}
		if err := spxscan.Select(ctx, txn.SpannerReadOnlyTransaction(), &entries, stmt); err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, errors.Wrap(err, "spxscan.Select()"))
		}

		return httpio.NewEncoder(w).Ok(entries)
	})
}
