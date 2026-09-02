package integration

// This suite is bootstrap parity: it provisions the SHIPPED demo role config
// (cmd/bootstrap/demo_access.json) through the real permission engine — spannerstore →
// access.Client → MigrateRoles → ForUser — over the SHIPPED demo data, then asserts
// each persona's view and authority. What a human sees logging into the running demo
// is exactly what this suite pins; the demo product and the regression suite cannot
// drift apart.
//
// These assertions are FROZEN: they pin decided condition semantics over the demo
// world. A failure here means enforcement or rendering broke — or the demo config
// changed, which is a deliberate act that updates this suite in the same change.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/cccteam/access"
	"github.com/cccteam/access/spannerstore"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource/waystation/pkg/router"
	initiator "github.com/cccteam/db-initiator"
)

const demoAccessPath = "../cmd/bootstrap/demo_access.json"

// demoAccessConfig mirrors the bootstrap's committed shape: the RoleConfig
// MigrateRoles provisions plus per-persona role assignments.
type demoAccessConfig struct {
	access.RoleConfig
	Users []struct {
		User  accesstypes.User `json:"user"`
		Roles struct {
			Global  []accesstypes.Role                        `json:"global"`
			Domains map[accesstypes.Domain][]accesstypes.Role `json:"domains"`
		} `json:"roles"`
	} `json:"users"`
	ServiceAccounts []struct {
		User  accesstypes.User `json:"user"`
		Roles struct {
			Global  []accesstypes.Role                        `json:"global"`
			Domains map[accesstypes.Domain][]accesstypes.Role `json:"domains"`
		} `json:"roles"`
	} `json:"serviceAccounts"`
}

func loadDemoAccess(t *testing.T) *demoAccessConfig {
	t.Helper()

	raw, err := os.ReadFile(demoAccessPath)
	if err != nil {
		t.Fatalf("reading %s: %v", demoAccessPath, err)
	}
	var conf demoAccessConfig
	if err := json.Unmarshal(raw, &conf); err != nil {
		t.Fatalf("parsing %s: %v", demoAccessPath, err)
	}

	return &conf
}

// newDemoApp assembles the application over the real permission engine with the
// shipped demo roles and personas provisioned through the production deploy path.
func newDemoApp(ctx context.Context, t *testing.T, db *initiator.SpannerDB) http.Handler {
	t.Helper()

	return newTestAppWithAccess(db, newDemoAccessClient(ctx, t, db))
}

// newDemoAccessClient provisions the shipped demo role config and personas through
// the production deploy path and returns the live engine. The manual-resource
// parity check draws on it directly (the audit route is not part of the generated
// test router newDemoApp composes).
func newDemoAccessClient(ctx context.Context, t *testing.T, db *initiator.SpannerDB) *access.Client {
	t.Helper()

	conf := loadDemoAccess(t)

	store, err := spannerstore.New(db.Client)
	if err != nil {
		t.Fatalf("spannerstore.New() error = %v", err)
	}
	client, err := access.New(store)
	if err != nil {
		t.Fatalf("access.New() error = %v", err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Errorf("access.Client.Close() error = %v", err)
		}
	})

	if err := access.MigrateRoles(ctx, client.UserManager(), router.Collection(), &conf.RoleConfig, wsAlpha, wsBeta, wsCeres); err != nil {
		t.Fatalf("access.MigrateRoles() error = %v", err)
	}
	for _, user := range conf.Users {
		if len(user.Roles.Global) > 0 {
			if err := client.UserManager().AddUserRoles(ctx, accesstypes.GlobalScope(), user.User, user.Roles.Global...); err != nil {
				t.Fatalf("AddUserRoles(%s, global) error = %v", user.User, err)
			}
		}
		for domain, roles := range user.Roles.Domains {
			if err := client.UserManager().AddUserRoles(ctx, accesstypes.DomainScope(domain), user.User, roles...); err != nil {
				t.Fatalf("AddUserRoles(%s, %s) error = %v", user.User, domain, err)
			}
		}
	}

	waitForDemoPolicy(ctx, t, client)

	return client
}

// waitForDemoPolicy blocks until the engine's snapshot reflects the migrated policy:
// the store writes signal a reload, but the swap is asynchronous, so the suite polls
// the last-provisioned persona's most specific authority until it stops being Denied.
func waitForDemoPolicy(ctx context.Context, t *testing.T, client *access.Client) {
	t.Helper()

	checker := client.ForUser("quartermaster-idris")
	deadline := time.Now().Add(15 * time.Second)
	for {
		decisions, err := checker.Check(ctx, accesstypes.NewEnvironment().WithNow(time.Now()),
			accesstypes.DomainScope(wsAlpha), accesstypes.Execute, "ReceiveShipment")
		if err == nil && !decisions["ReceiveShipment"].IsDenied() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("policy snapshot never became visible; last decisions: %v (err %v)", decisions, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestDemoPersonaViews(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}
	h := newDemoApp(ctx, t, db)

	tests := []struct {
		name       string
		user       accesstypes.User
		method     string // GET when empty
		target     string
		body       string
		wantStatus int
		wantRows   int                                 // list-row count, asserted when > 0
		wantIDs    []string                            // ids that must be present in the list
		banIDs     []string                            // ids that must be suppressed from the list
		check      func(t *testing.T, respBody []byte) // bespoke shape assertions
	}{
		{
			// Four rows: the commander's grant is unconditional, but structural row
			// tenancy (E2) keeps ws-beta's work order out of the alpha partition.
			name:       "commander sees every station work order in the partition",
			user:       "commander",
			target:     "/api/waystations/ws-alpha/work-orders",
			wantStatus: http.StatusOK,
			wantRows:   4,
		},
		{
			// tech-rivera is on Alpha Mechanical (ws-alpha) and Beta Maintenance
			// (ws-beta). In the ws-alpha partition subject.teams is tenancy-filtered to
			// the alpha membership, so the beta team's work order never qualifies.
			name:       "technician sees own teams' work orders with derived tenancy",
			user:       "tech-rivera",
			target:     "/api/waystations/ws-alpha/work-orders",
			wantStatus: http.StatusOK,
			wantRows:   2,
			wantIDs:    []string{woScrubberID, woCraneID},
		},
		{
			name:       "technician assets exclude the reactor zone through the join path",
			user:       "tech-rivera",
			target:     "/api/waystations/ws-alpha/assets",
			wantStatus: http.StatusOK,
			wantRows:   4, // 4 of 7: the reactor-zone condition and row tenancy (E2) each drop rows
			banIDs:     []string{assetManifoldID},
		},
		{
			name:       "foreman sees only requisitions they requested",
			user:       "foreman-okafor",
			target:     "/api/waystations/ws-alpha/requisitions",
			wantStatus: http.StatusOK,
			wantRows:   3,
			wantIDs:    []string{reqScrubberID, reqPumpID, reqTorchID},
		},
		{
			// The over-limit overhaul (7120 > 5000) and the non-submitted rows are
			// suppressed by conditions, and ws-beta's submitted row by structural
			// row tenancy (E2) — one alpha row survives both.
			name:       "approver queue is submitted work within their limit",
			user:       "procurement-chen",
			target:     "/api/waystations/ws-alpha/requisitions",
			wantStatus: http.StatusOK,
			wantRows:   1,
			banIDs:     []string{reqOverhaulID},
		},
		{
			name:       "auditor sees cost cells only on approved requisitions",
			user:       "auditor-voss",
			target:     "/api/waystations/ws-alpha/requisition-lines",
			wantStatus: http.StatusOK,
			wantRows:   4, // ws-beta's line is out: row tenancy resolves through the interleaved parent
			check: func(t *testing.T, respBody []byte) {
				t.Helper()
				for _, row := range decodeRows(t, respBody) {
					_, hasCost := row["unitCostSnapshot"]
					isApproved := row["requisitionId"] == reqTorchID
					if hasCost != isApproved {
						t.Errorf("line %v/%v: cost visible = %v, want %v", row["requisitionId"], row["lineNumber"], hasCost, isApproved)
					}
				}
			},
		},
		{
			name:       "auditor sees only terminal work orders",
			user:       "auditor-voss",
			target:     "/api/waystations/ws-alpha/work-orders",
			wantStatus: http.StatusOK,
			wantRows:   1,
			wantIDs:    []string{woCraneID},
		},
		{
			// The uniform state binding reads identically on the interleaved member: the
			// same condition text admits only the completed work order's tasks (2 of 5).
			name:       "auditor tasks follow the member's uniform state binding",
			user:       "auditor-voss",
			target:     "/api/waystations/ws-alpha/work-order-tasks",
			wantStatus: http.StatusOK,
			wantRows:   2,
		},
		{
			name:       "auditor incident view carries no reporter PII field",
			user:       "auditor-voss",
			target:     "/api/waystations/ws-alpha/incident-reports/" + incidentCoolantID,
			wantStatus: http.StatusOK,
			check: func(t *testing.T, respBody []byte) {
				t.Helper()
				if _, ok := decodeRow(t, respBody)["reporterContact"]; ok {
					t.Errorf("reporterContact visible to auditor: %s", respBody)
				}
			},
		},
		{
			name:       "requester supplier list hides inactive vendors",
			user:       "foreman-okafor",
			target:     "/api/suppliers",
			wantStatus: http.StatusOK,
			wantRows:   2, // the 2 active
			banIDs:     []string{supplierRedlineID},
		},
		{
			name:       "no role in a domain means the domain is concealed",
			user:       "foreman-okafor",
			target:     "/api/waystations/ws-beta/requisitions",
			wantStatus: http.StatusNotFound,
		},
		{
			// chief-alpha holds SafetyOfficer, whose Execute grant carries a row-free
			// now-condition still in the future.
			name:       "safety drill authorization folds its expiry at decode",
			user:       "chief-alpha",
			method:     http.MethodPost,
			target:     "/api/run-safety-drill",
			body:       `{"announcement":"Drill: seal ring three"}`,
			wantStatus: http.StatusOK,
		},
		{
			name:       "safety drill without the authorization is refused",
			user:       "tech-rivera",
			method:     http.MethodPost,
			target:     "/api/run-safety-drill",
			body:       `{"announcement":"Unauthorized drill"}`,
			wantStatus: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			method := tt.method
			if method == "" {
				method = http.MethodGet
			}
			status, body := doRequestAs(t, h, tt.user, method, tt.target, tt.body)
			if status != tt.wantStatus {
				t.Fatalf("status = %d, want %d: %s", status, tt.wantStatus, body)
			}

			if tt.wantRows > 0 || len(tt.wantIDs) > 0 || len(tt.banIDs) > 0 {
				rows := decodeRows(t, body)
				if tt.wantRows > 0 && len(rows) != tt.wantRows {
					t.Fatalf("rows = %d, want %d: %s", len(rows), tt.wantRows, body)
				}
				indexed := rowsByID(t, rows, "id")
				for _, id := range tt.wantIDs {
					if _, ok := indexed[id]; !ok {
						t.Errorf("missing row %s", id)
					}
				}
				for _, id := range tt.banIDs {
					if _, ok := indexed[id]; ok {
						t.Errorf("row %s visible, want suppressed", id)
					}
				}
			}

			if tt.check != nil {
				tt.check(t, body)
			}
		})
	}
}

func TestDemoWorkflowEnforcement(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}
	h := newDemoApp(ctx, t, db)

	// Deliberately not a table: the sections mutate shared workflow state and each
	// step depends on what the previous one left behind, so they run in order rather
	// than as parallel subtests.

	// Draft lines are editable by their requester...
	status, body := doRequestAs(t, h, "foreman-okafor", http.MethodPatch, "/api/resources",
		fmt.Sprintf(`[{"op":"patch","path":"/waystations/ws-alpha/requisition-lines/%s/1","value":{"quantity":2}}]`, reqPumpID))
	assertStatus(t, status, http.StatusOK, body)

	// ...and submit recomputes and freezes the total from the lines (2 x 890.00).
	status, body = doRequestAs(t, h, "foreman-okafor", http.MethodPost, "/api/waystations/ws-alpha/submit-requisition",
		fmt.Sprintf(`{"requisitionId":%q}`, reqPumpID))
	assertStatus(t, status, http.StatusOK, body)
	status, body = doRequestAs(t, h, "foreman-okafor", http.MethodGet, "/api/waystations/ws-alpha/requisitions/"+reqPumpID, "")
	assertStatus(t, status, http.StatusOK, body)
	if row := decodeRow(t, body); row["statusId"] != "submitted" || row["totalCost"] != "1780" {
		t.Fatalf("after submit: status %v total %v, want submitted 1780", row["statusId"], row["totalCost"])
	}

	// Once submitted, the same edit is refused by the in-transaction check-SELECT.
	status, body = doRequestAs(t, h, "foreman-okafor", http.MethodPatch, "/api/resources",
		fmt.Sprintf(`[{"op":"patch","path":"/waystations/ws-alpha/requisition-lines/%s/1","value":{"quantity":9}}]`, reqPumpID))
	assertStatus(t, status, http.StatusForbidden, body)

	// The approver can approve within their limit (1780 <= 5000)...
	status, body = doRequestAs(t, h, "procurement-chen", http.MethodPost, "/api/waystations/ws-alpha/approve-requisition",
		fmt.Sprintf(`{"requisitionId":%q}`, reqPumpID))
	assertStatus(t, status, http.StatusOK, body)

	// ...but a directly addressed over-limit approval is refused by the RPC body.
	status, body = doRequestAs(t, h, "procurement-chen", http.MethodPost, "/api/waystations/ws-alpha/approve-requisition",
		fmt.Sprintf(`{"requisitionId":%q}`, reqOverhaulID))
	assertStatus(t, status, http.StatusForbidden, body)

	// Creating work orders is condition-gated on the proposed row itself.
	status, body = doRequestAs(t, h, "foreman-okafor", http.MethodPatch, "/api/resources",
		fmt.Sprintf(`[{"op":"add","path":"/waystations/ws-alpha/work-orders","value":{"assetId":%q,"title":"Illegally critical","priority":5}}]`, assetRecyclerID))
	assertStatus(t, status, http.StatusForbidden, body)
	status, body = doRequestAs(t, h, "foreman-okafor", http.MethodPatch, "/api/resources",
		fmt.Sprintf(`[{"op":"add","path":"/waystations/ws-alpha/work-orders","value":{"assetId":%q,"title":"Routine filter check","priority":2}}]`, assetRecyclerID))
	assertStatus(t, status, http.StatusOK, body)

	// Task checklists open only while work is running: the uniform state binding on
	// the member gates the technician's update.
	status, body = doRequestAs(t, h, "tech-rivera", http.MethodPatch, "/api/resources",
		fmt.Sprintf(`[{"op":"patch","path":"/waystations/ws-alpha/work-order-tasks/%s/2","value":{"done":true}}]`, woScrubberID))
	assertStatus(t, status, http.StatusOK, body)
	status, body = doRequestAs(t, h, "tech-rivera", http.MethodPatch, "/api/resources",
		fmt.Sprintf(`[{"op":"patch","path":"/waystations/ws-alpha/work-order-tasks/%s/1","value":{"done":true}}]`, woOvenID))
	assertStatus(t, status, http.StatusForbidden, body)

	// Deletion rides the base-resource decision: drafts may go, history may not.
	status, body = doRequestAs(t, h, "chief-alpha", http.MethodPatch, "/api/resources",
		fmt.Sprintf(`[{"op":"remove","path":"/waystations/ws-alpha/work-orders/%s"}]`, woManifoldID))
	assertStatus(t, status, http.StatusOK, body)
	status, body = doRequestAs(t, h, "chief-alpha", http.MethodPatch, "/api/resources",
		fmt.Sprintf(`[{"op":"remove","path":"/waystations/ws-alpha/work-orders/%s"}]`, woCraneID))
	assertStatus(t, status, http.StatusForbidden, body)

	// Expiry-gated deletes over a date attribute: the expired lot goes, the fresh lot
	// and the never-expiring lot (NULL fails closed) stay.
	status, body = doRequestAs(t, h, "quartermaster-idris", http.MethodPatch, "/api/resources",
		fmt.Sprintf(`[{"op":"remove","path":"/waystations/ws-alpha/inventory-lots/%s"}]`, lotExpiredID))
	assertStatus(t, status, http.StatusOK, body)
	for _, keep := range []string{lotFreshID, lotNoExpiryID} {
		status, body = doRequestAs(t, h, "quartermaster-idris", http.MethodPatch, "/api/resources",
			fmt.Sprintf(`[{"op":"remove","path":"/waystations/ws-alpha/inventory-lots/%s"}]`, keep))
		assertStatus(t, status, http.StatusForbidden, body)
	}

	// Shipments go read-only once they arrive: the IS NULL condition on updates.
	status, body = doRequestAs(t, h, "quartermaster-idris", http.MethodPatch, "/api/resources",
		fmt.Sprintf(`[{"op":"patch","path":"/waystations/ws-alpha/shipments/%s","value":{"supplierId":%q}}]`, shipmentArrivedID, supplierRedlineID))
	assertStatus(t, status, http.StatusForbidden, body)
	status, body = doRequestAs(t, h, "quartermaster-idris", http.MethodPatch, "/api/resources",
		fmt.Sprintf(`[{"op":"patch","path":"/waystations/ws-alpha/shipments/%s","value":{"supplierId":%q}}]`, shipmentPendingID, supplierRedlineID))
	assertStatus(t, status, http.StatusOK, body)
}
