package integration

// This suite pins the manual-resource seam end to end: AuditTrailEntries has no
// generated handler — @manualAddResource registers its List permission into the
// generated collection, and app.AuditTrailEntries is the hand-written surface that
// checks it. The suite drives a change-tracked mutation through the generated
// consolidated handler, then reads it back through the hand-written route, mounted
// here exactly as the production router mounts it (router.NewTestRouter serves only
// generated routes, so the manual route gets the same one-line mount main gets).

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource/waystation/app"
	"github.com/cccteam/ccc/resource/waystation/pkg/resources"
	"github.com/cccteam/ccc/resource/waystation/pkg/router"
	initiator "github.com/cccteam/db-initiator"
	"github.com/cccteam/httpio"
	"github.com/go-chi/chi/v5"
)

// newAuditApp builds the application with the given grants and mounts both the
// generated routes and the hand-written audit route on one handler.
func newAuditApp(db *initiator.SpannerDB, g grants) http.Handler {
	a := app.New(&testConfigurer{
		db:           db,
		access:       &staticAccess{g: g},
		domainExists: demoDomainsExist,
	})

	r := chi.NewRouter()
	r.Use(httpio.WithParams)
	r.Get("/api/audit-trail-entries", a.AuditTrailEntries())
	r.Mount("/", router.NewTestRouter(a))

	return r
}

func TestManualResourceAuditTrail(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}

	auditGrants := grants{
		accesstypes.List: {resources.AuditTrailEntries},
		accesstypes.Create: {
			workOrdersResource,
			fieldResource(workOrdersResource, "waystationId"),
			fieldResource(workOrdersResource, "assetId"),
			fieldResource(workOrdersResource, "title"),
			fieldResource(workOrdersResource, "priority"),
		},
	}

	h := newAuditApp(db, auditGrants)

	// A tracked mutation through the generated surface produces the event the
	// hand-written surface serves.
	status, body := doRequestAs(t, h, "audit-actor", http.MethodPatch, "/api/resources",
		fmt.Sprintf(`[{"op":"add","path":"/waystations/ws-alpha/work-orders","value":{"waystationId":"ws-alpha","assetId":%q,"title":"Audited create","priority":1}}]`, assetRecyclerID))
	assertStatus(t, status, http.StatusOK, body)

	t.Run("granted List serves the change events newest first", func(t *testing.T) {
		t.Parallel()

		status, body := doRequest(t, h, http.MethodGet, "/api/audit-trail-entries", "")
		assertStatus(t, status, http.StatusOK, body)

		rows := decodeRows(t, body)
		if len(rows) == 0 {
			t.Fatalf("audit trail = 0 rows, want at least the tracked create: %s", body)
		}
		first := rows[0]
		if got := first["tableName"]; got != "WorkOrders" {
			t.Errorf("newest event tableName = %v, want WorkOrders", got)
		}
		if src, _ := first["eventSource"].(string); src == "" {
			t.Errorf("newest event eventSource is empty, want the session-derived source")
		}
		if _, ok := first["changeSet"]; !ok {
			t.Errorf("newest event has no changeSet key: %v", first)
		}
	})

	t.Run("without the grant the surface is forbidden", func(t *testing.T) {
		t.Parallel()

		denied := newAuditApp(db, grants{})
		status, body := doRequest(t, denied, http.MethodGet, "/api/audit-trail-entries", "")
		assertStatus(t, status, http.StatusForbidden, body)
	})
}

// TestManualResourceBootstrapParity runs the audit surface against the REAL engine
// provisioned with the shipped demo config: auditor-voss holds the global
// RecordsAuditor role and reaches the page, the foreman holds no audit grant and is
// refused. MigrateRoles validating the RecordsAuditor grant against the generated
// collection is what the @manualAddResource registration buys.
func TestManualResourceBootstrapParity(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}

	a := app.New(&testConfigurer{
		db:           db,
		access:       newDemoAccessClient(ctx, t, db),
		domainExists: demoDomainsExist,
	})
	r := chi.NewRouter()
	r.Use(httpio.WithParams)
	r.Get("/api/audit-trail-entries", a.AuditTrailEntries())

	status, body := doRequestAs(t, r, "auditor-voss", http.MethodGet, "/api/audit-trail-entries", "")
	assertStatus(t, status, http.StatusOK, body)

	status, body = doRequestAs(t, r, "foreman-okafor", http.MethodGet, "/api/audit-trail-entries", "")
	assertStatus(t, status, http.StatusForbidden, body)
}
