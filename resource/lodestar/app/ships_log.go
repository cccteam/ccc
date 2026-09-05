package app

import (
	"net/http"
	"time"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource/lodestar/pkg/resources"
	"github.com/cccteam/ccc/resource/lodestar/pkg/router"
	"github.com/cccteam/httpio"
	"github.com/cccteam/spxscan"
	"github.com/go-playground/errors/v5"
)

// shipsLogEntry is one DataChangeEvents row on the wire. The change-tracked resources
// (Missions, Refits, Ships) write these inside their mutation transactions; the ship's
// log renders them newest-first, per sector.
type shipsLogEntry struct {
	TableName   string           `json:"tableName"   spanner:"TableName"`
	RowID       string           `json:"rowId"       spanner:"RowId"`
	Sequence    int64            `json:"sequence"    spanner:"Sequence"`
	EventTime   time.Time        `json:"eventTime"   spanner:"EventTime"`
	EventSource string           `json:"eventSource" spanner:"EventSource"`
	ChangeSet   spanner.NullJSON `json:"changeSet"   spanner:"ChangeSet"`
}

// shipsLogLimit caps the log page at the newest events; the demo carries no pagination
// on this surface.
const shipsLogLimit = 200

// ShipsLogEntries lists the sector's change-tracking events, newest first. The handler
// is hand-written — DataChangeEvents is library infrastructure, not a schema resource,
// so no handler is generated — and its permission is declared to the generator via
// @manualAddResource(List, domain) on resources.ShipsLogEntries. The check here is
// what that registration promises: List on ShipsLogEntries in the URL sector's
// partition, fail-closed. A Conditional decision is refused too: a hand-written
// surface has no bindings for the engine's conditions to reference, so only an
// unconditional grant can open it. The sector's rows are the missions booked in it,
// and the refits and ships whose hangar is in it — DataChangeEvents carries no tenant
// column, so the sector predicate joins through the tracked tables.
func (a *App) ShipsLogEntries() http.HandlerFunc {
	return httpio.Log(func(w http.ResponseWriter, r *http.Request) error {
		ctx := r.Context()
		domain := httpio.Param[accesstypes.Domain](r, router.Domain)

		env := accesstypes.NewEnvironment().WithNow(time.Now())
		decisions, err := a.UserPermissions(r).Check(ctx, env, accesstypes.DomainScope(domain), accesstypes.List, resources.ShipsLogEntries)
		if err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, errors.Wrap(err, "resource.UserPermissions.Check()"))
		}
		if !decisions[resources.ShipsLogEntries].IsGranted() {
			return httpio.NewEncoder(w).ClientMessage(ctx, httpio.NewForbiddenMessagef(
				"user %s does not have List on %s in %s", a.UserPermissions(r).User(), resources.ShipsLogEntries, domain,
			))
		}

		txn := a.resourceClient.ReadOnlyTransaction()
		defer txn.Close()

		entries := make([]shipsLogEntry, 0, shipsLogLimit)
		stmt := spanner.Statement{
			SQL: `SELECT e.TableName, e.RowId, e.Sequence, e.EventTime, e.EventSource, e.ChangeSet
			        FROM DataChangeEvents e
			       WHERE (e.TableName = 'Missions' AND e.RowId IN (SELECT m.Id FROM Missions m WHERE m.SectorId = @sector))
			          OR (e.TableName = 'Ships' AND e.RowId IN (
			                SELECT s.Id FROM Ships s JOIN Hangars h ON h.Id = s.HangarId WHERE h.SectorId = @sector))
			          OR (e.TableName = 'Refits' AND e.RowId IN (
			                SELECT r.Id FROM Refits r JOIN Ships s ON s.Id = r.ShipId JOIN Hangars h ON h.Id = s.HangarId WHERE h.SectorId = @sector))
			       ORDER BY e.EventTime DESC, e.TableName, e.RowId, e.Sequence DESC
			       LIMIT @limit`,
			Params: map[string]any{"sector": string(domain), "limit": shipsLogLimit},
		}
		if err := spxscan.Select(ctx, txn.SpannerReadOnlyTransaction(), &entries, stmt); err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, errors.Wrap(err, "spxscan.Select()"))
		}

		return httpio.NewEncoder(w).Ok(entries)
	})
}
