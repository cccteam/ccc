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

// clientStatementLine is one entry of a company's statement: a change-tracking event on
// one of the company's missions in the sector.
type clientStatementLine struct {
	MissionID   string           `json:"missionId"   spanner:"RowId"`
	Title       string           `json:"title"       spanner:"Title"`
	EventTime   time.Time        `json:"eventTime"   spanner:"EventTime"`
	EventSource string           `json:"eventSource" spanner:"EventSource"`
	ChangeSet   spanner.NullJSON `json:"changeSet"   spanner:"ChangeSet"`
}

// clientStatementLimit caps the statement at the newest events.
const clientStatementLimit = 200

// ClientStatements is the portal's per-company statement: the change log of the missions
// the caller's company booked in the sector (bookings, fee changes, completions,
// settlements). The permission is registered manually with @manualAddResource(List,
// domain) and @outlet(portal), so it reaches the portal's TypeScript constants and not the
// console's, and the check here is what that registration promises: List on
// ClientStatements in the URL sector's partition, in the members store the portal's
// session group bound. A manual resource has no bindings, so the route, not the grant,
// scopes the statement: the company is the caller's ClientContact row.
//
// Demonstrates: @manualAddResource.outlet, @manualAddResource.scope, hand-written-route, change-tracking.
func (a *App) ClientStatements() http.HandlerFunc {
	return httpio.Log(func(w http.ResponseWriter, r *http.Request) error {
		ctx := r.Context()
		domain := httpio.Param[accesstypes.Domain](r, router.Domain)
		perms := a.UserPermissions(r)

		env := accesstypes.NewEnvironment().WithNow(time.Now())
		decisions, err := perms.Check(ctx, env, accesstypes.DomainScope(domain), accesstypes.List, resources.ClientStatements)
		if err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, errors.Wrap(err, "resource.UserPermissions.Check()"))
		}
		if !decisions[resources.ClientStatements].IsGranted() {
			return httpio.NewEncoder(w).ClientMessage(ctx, httpio.NewForbiddenMessagef(
				"user %s does not have List on %s in %s", perms.User(), resources.ClientStatements, domain,
			))
		}

		txn := a.resourceClient.ReadOnlyTransaction()
		defer txn.Close()

		lines := make([]clientStatementLine, 0, clientStatementLimit)
		stmt := spanner.Statement{
			SQL: `SELECT e.RowId, m.Title, e.EventTime, e.EventSource, e.ChangeSet
			        FROM DataChangeEvents e
			        JOIN Missions m ON m.Id = e.RowId
			        JOIN ClientContacts c ON c.ClientId = m.ClientId
			       WHERE e.TableName = 'Missions'
			         AND m.SectorId = @sector
			         AND c.UserId = @caller
			       ORDER BY e.EventTime DESC, e.RowId, e.Sequence DESC
			       LIMIT @limit`,
			Params: map[string]any{"sector": string(domain), "caller": string(perms.User()), "limit": clientStatementLimit},
		}
		if err := spxscan.Select(ctx, txn.SpannerReadOnlyTransaction(), &lines, stmt); err != nil {
			return httpio.NewEncoder(w).ClientMessage(ctx, errors.Wrap(err, "spxscan.Select()"))
		}

		return httpio.NewEncoder(w).Ok(lines)
	})
}
