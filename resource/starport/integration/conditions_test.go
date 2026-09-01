package integration

// This suite is the ABAC conditions demo (design plan §11): conditional grants
// exercised end-to-end through the REAL permission engine — spannerstore →
// access.Client → ForUser → the resource pipeline's condition rendering —
// against seeded rows. It pins the read rules (row disappearance, cell
// masking with absent wire keys, implication pruning making the common case a
// plain filtered read), the write rules (the in-transaction check-SELECT
// admitting and forbidding by row data), and the deploy path (MigrateRoles
// landing conditional grants from a RoleConfig).
//
// The generated authorization matrix stays unconditional (its cases assert
// binary allow/deny per endpoint); conditional outcomes depend on row data
// and live here, with seeded rows — the invariant-suite discipline, extended.
//
// The seeded crates (testdata/seed):
//
//	Coolant Cells  qty 40  priority 2  sealed       badge INSP-77
//	Ration Packs   qty 500 priority 1  provisioned  badge NULL
//	Coolant Cells  qty 12  priority 3  quarantined  badge INSP-81
//
// These assertions are FROZEN: they pin decided condition semantics. A
// failure here means enforcement or rendering broke, not that the suite needs
// updating.

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/access"
	"github.com/cccteam/access/spannerstore"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource/starport/app"
	"github.com/cccteam/ccc/resource/starport/pkg/router"
	initiator "github.com/cccteam/db-initiator"
)

// conditionRoles is the suite's RoleConfig: each role is one grant object, the
// pairing invariant putting one condition on the grant's whole field set.
func conditionRoles() *access.RoleConfig {
	return &access.RoleConfig{Roles: []*access.Role{
		{
			// SealedAuditor reads crate contents, but only of sealed crates:
			// the same condition on every column, so every CASE prunes and
			// failing rows disappear entirely.
			Name: "SealedAuditor",
			Permissions: map[accesstypes.Permission][]access.Grant{
				accesstypes.List: {{Resource: supplyCratesResource, Fields: []accesstypes.Tag{"label", "quantity", "status"}, Condition: "status = 'sealed'"}},
				accesstypes.Read: {{Resource: supplyCratesResource, Fields: []accesstypes.Tag{"label", "quantity", "status"}, Condition: "status = 'sealed'"}},
			},
		},
		{
			// OwnInspector reads the crates the requester inspected: a
			// subject-fact condition over the badge column.
			Name: "OwnInspector",
			Permissions: map[accesstypes.Permission][]access.Grant{
				accesstypes.List: {{Resource: supplyCratesResource, Fields: []accesstypes.Tag{"label", "inspectorBadge"}, Condition: "inspectorBadge = subject"}},
			},
		},
		{
			// CrateHandler mutates by row state: quantity edits only while a
			// crate is provisioned, deletion only of high-priority crates.
			Name: "CrateHandler",
			Permissions: map[accesstypes.Permission][]access.Grant{
				accesstypes.Update: {{Resource: supplyCratesResource, Fields: []accesstypes.Tag{"quantity"}, Condition: "status = 'provisioned'"}},
				accesstypes.Delete: {{Resource: supplyCratesResource, Condition: "priority > 2"}},
			},
		},
	}}
}

// newConditionsApp assembles the application over the real permission engine:
// the emulator database holds both the domain rows and the policy store, the
// RoleConfig migrates through the production deploy path, and every request
// checks through Client.ForUser.
func newConditionsApp(ctx context.Context, t *testing.T, db *initiator.SpannerDB, probePerm accesstypes.Permission, userRoles map[accesstypes.User][]accesstypes.Role) http.Handler {
	t.Helper()

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

	if err := access.MigrateRoles(ctx, client.UserManager(), router.Collection(), conditionRoles()); err != nil {
		t.Fatalf("access.MigrateRoles() error = %v", err)
	}
	for user, roles := range userRoles {
		if err := client.UserManager().AddUserRoles(ctx, accesstypes.GlobalScope(), user, roles...); err != nil {
			t.Fatalf("AddUserRoles(%s) error = %v", user, err)
		}
	}

	waitForPolicy(ctx, t, client, probePerm, userRoles)

	return router.NewTestRouter(app.New(&testConfigurer{
		db:           db,
		access:       client,
		domainExists: stationsExist,
	}))
}

// waitForPolicy blocks until the engine's snapshot reflects the migrated
// policy: the store writes signal a reload, but the swap is asynchronous, so
// the suite polls one configured user's probe permission until the decision
// stops being Denied.
func waitForPolicy(ctx context.Context, t *testing.T, client *access.Client, probePerm accesstypes.Permission, userRoles map[accesstypes.User][]accesstypes.Role) {
	t.Helper()

	var probe accesstypes.User
	for user := range userRoles {
		probe = user

		break
	}
	checker := client.ForUser(probe)

	deadline := time.Now().Add(15 * time.Second)
	for {
		decisions, err := checker.Check(ctx, accesstypes.NewEnvironment().WithNow(time.Now()), accesstypes.GlobalScope(), probePerm, supplyCratesResource)
		if err == nil && !decisions[supplyCratesResource].IsDenied() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("policy snapshot never became visible; last decisions: %v (err %v)", decisions, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestConditions_readRules(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db, err := prepareDatabase(ctx, t, "file://../schema/migrations", "file://testdata/seed")
	if err != nil {
		t.Fatal(err)
	}

	h := newConditionsApp(ctx, t, db, accesstypes.List, map[accesstypes.User][]accesstypes.Role{
		"INSP-77":             {"SealedAuditor"},
		"INSP-81":             {"SealedAuditor", "OwnInspector"},
		"BADGELESS-INSPECTOR": {"OwnInspector"},
	})

	t.Run("one condition on every column drops failing rows entirely", func(t *testing.T) {
		t.Parallel()

		status, body := doRequestAs(t, h, "INSP-77", http.MethodGet, "/api/supply-crates", "")
		assertStatus(t, status, http.StatusOK, body)
		rows := decodeRows(t, body)
		if len(rows) != 1 {
			t.Fatalf("rows = %d, want only the sealed crate: %s", len(rows), body)
		}
		assertKeys(t, rows[0], []string{"id", "label", "quantity", "status"})
		if rows[0]["status"] != "sealed" || rows[0]["quantity"] != float64(40) {
			t.Errorf("row = %v, want the sealed Coolant Cells crate", rows[0])
		}
	})

	t.Run("unioned roles mask per cell and the shared column prunes", func(t *testing.T) {
		t.Parallel()

		// INSP-81 holds both roles: the sealed crate is visible through
		// SealedAuditor (badge INSP-77 masks inspectorBadge), the quarantined
		// crate through OwnInspector (quantity and status mask); label is
		// covered by both conditions, so it shows on every surviving row.
		status, body := doRequestAs(t, h, "INSP-81", http.MethodGet, "/api/supply-crates?sort=quantity", "")
		assertStatus(t, status, http.StatusOK, body)
		rows := decodeRows(t, body)
		if len(rows) != 2 {
			t.Fatalf("rows = %d, want the sealed and the own-badge crates: %s", len(rows), body)
		}

		quarantined, sealed := rows[0], rows[1] // sorted by quantity: 12, 40
		assertKeys(t, quarantined, []string{"id", "inspectorBadge", "label"})
		if quarantined["inspectorBadge"] != "INSP-81" {
			t.Errorf("own crate inspectorBadge = %v, want INSP-81", quarantined["inspectorBadge"])
		}
		assertKeys(t, sealed, []string{"id", "label", "quantity", "status"})
	})

	t.Run("a read of a row the condition fails is not found", func(t *testing.T) {
		t.Parallel()

		status, body := doRequestAs(t, h, "INSP-77", http.MethodGet, "/api/supply-crates/"+crateRationsID, "")
		assertStatus(t, status, http.StatusNotFound, body)

		status, body = doRequestAs(t, h, "INSP-77", http.MethodGet, "/api/supply-crates/"+crateCoolant40ID, "")
		assertStatus(t, status, http.StatusOK, body)
		assertKeys(t, decodeRow(t, body), []string{"id", "label", "quantity", "status"})
	})

	t.Run("a subject-fact condition matches nothing for a stranger", func(t *testing.T) {
		t.Parallel()

		status, body := doRequestAs(t, h, "BADGELESS-INSPECTOR", http.MethodGet, "/api/supply-crates", "")
		assertStatus(t, status, http.StatusOK, body)
		if rows := decodeRows(t, body); len(rows) != 0 {
			t.Errorf("rows = %d, want none for a badge matching no crate: %s", len(rows), body)
		}
	})
}

func TestConditions_writeRules(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db, err := prepareDatabase(ctx, t, "file://../schema/migrations", "file://testdata/seed")
	if err != nil {
		t.Fatal(err)
	}

	h := newConditionsApp(ctx, t, db, accesstypes.Update, map[accesstypes.User][]accesstypes.Role{
		"HANDLER-1": {"CrateHandler"},
	})

	// The write stages run in order — each step's outcome is the next step's
	// starting state, so these are sequential sections, not subtests.

	// An update whose condition holds buffers and commits.
	body := fmt.Sprintf(`[{"op":"patch","path":"/supply-crates/%s","value":{"quantity":499}}]`, crateRationsID)
	status, respBody := doRequestAs(t, h, "HANDLER-1", http.MethodPatch, "/api/resources", body)
	assertStatus(t, status, http.StatusOK, respBody)
	if qty := readColumn[int64](ctx, t, db, "SupplyCrates", spanner.Key{crateRationsID}, "Quantity"); qty != 499 {
		t.Errorf("Quantity = %d, want 499", qty)
	}

	// An update whose condition fails is forbidden and commits nothing.
	body = fmt.Sprintf(`[{"op":"patch","path":"/supply-crates/%s","value":{"quantity":39}}]`, crateCoolant40ID)
	status, respBody = doRequestAs(t, h, "HANDLER-1", http.MethodPatch, "/api/resources", body)
	assertStatus(t, status, http.StatusForbidden, respBody)
	if qty := readColumn[int64](ctx, t, db, "SupplyCrates", spanner.Key{crateCoolant40ID}, "Quantity"); qty != 40 {
		t.Errorf("Quantity = %d, want unchanged 40 (sealed crates do not take quantity edits)", qty)
	}

	// A delete is carried by the base-resource condition: priority 2 fails
	// priority > 2 — forbidden, the row stays.
	body = fmt.Sprintf(`[{"op":"remove","path":"/supply-crates/%s"}]`, crateCoolant40ID)
	status, respBody = doRequestAs(t, h, "HANDLER-1", http.MethodPatch, "/api/resources", body)
	assertStatus(t, status, http.StatusForbidden, respBody)
	if count := countTableRows(ctx, t, db, "SupplyCrates"); count != 3 {
		t.Fatalf("SupplyCrates rows = %d, want 3 after the forbidden delete", count)
	}

	// Priority 3 passes: the row deletes.
	body = fmt.Sprintf(`[{"op":"remove","path":"/supply-crates/%s"}]`, crateCoolant12ID)
	status, respBody = doRequestAs(t, h, "HANDLER-1", http.MethodPatch, "/api/resources", body)
	assertStatus(t, status, http.StatusOK, respBody)
	if count := countTableRows(ctx, t, db, "SupplyCrates"); count != 2 {
		t.Errorf("SupplyCrates rows = %d, want 2 after the permitted delete", count)
	}
}

// countTableRows counts a table's rows directly in the database.
func countTableRows(ctx context.Context, t *testing.T, db *initiator.SpannerDB, table string) int64 {
	t.Helper()

	it := db.Single().Query(ctx, spanner.Statement{SQL: "SELECT COUNT(*) FROM " + table})
	defer it.Stop()

	row, err := it.Next()
	if err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	var count int64
	if err := row.Columns(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}

	return count
}
