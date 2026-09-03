package integration

// This suite pins the first-class Touch: an update carried entirely by the resource's
// update functions. NudgeWorkOrder fires the generated NewWorkOrderTouch, so the full
// update pipeline runs with no caller-set fields — UpdatedAt takes the commit
// timestamp and the change event records who nudged — while a plain update patch with
// no fields set stays a silent no-op (pinned in the resource module). The structural
// halves script their grants; the bootstrap-parity half runs the demo personas over
// the real permission engine.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
	initiator "github.com/cccteam/db-initiator"
	"google.golang.org/api/iterator"
)

// nudgeGrants scripts exactly the Execute grant the nudge decode checks.
func nudgeGrants() grants {
	return grants{
		accesstypes.Execute: {accesstypes.Resource("NudgeWorkOrder")},
	}
}

// latestChangeEvent reads the newest DataChangeEvents row for one tracked row,
// returning its event source and change-set keys.
func latestChangeEvent(t *testing.T, db *initiator.SpannerDB, tableName, rowID string) (eventSource string, changeSetKeys []string) {
	t.Helper()

	iter := db.Single().Query(t.Context(), spanner.Statement{
		SQL: `SELECT EventSource, TO_JSON_STRING(ChangeSet)
		        FROM DataChangeEvents
		       WHERE TableName = @tableName AND RowId = @rowID
		    ORDER BY EventTime DESC, Sequence DESC
		       LIMIT 1`,
		Params: map[string]any{"tableName": tableName, "rowID": rowID},
	})
	defer iter.Stop()

	row, err := iter.Next()
	if errors.Is(err, iterator.Done) {
		t.Fatalf("no DataChangeEvents row for %s %s", tableName, rowID)
	}
	if err != nil {
		t.Fatalf("DataChangeEvents query: %v", err)
	}

	var changeSetJSON string
	if err := row.Columns(&eventSource, &changeSetJSON); err != nil {
		t.Fatalf("row.Columns: %v", err)
	}

	changeSet := map[string]any{}
	if err := json.Unmarshal([]byte(changeSetJSON), &changeSet); err != nil {
		t.Fatalf("ChangeSet unmarshal: %v: %s", err, changeSetJSON)
	}
	for key := range changeSet {
		changeSetKeys = append(changeSetKeys, key)
	}

	return eventSource, changeSetKeys
}

// TestNudgeWorkOrderTouch walks one work order's stamp through its lifecycle:
// unset in the seed, stamped by the first touch, advanced by the second, with the
// change event carrying exactly the mechanical stamp. Deliberately not a table —
// each step depends on the state the previous one left behind.
func TestNudgeWorkOrderTouch(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}
	h := newTestApp(db, nudgeGrants())

	// Seed rows never carry the stamp: UpdatedAt is an output_only_update_fn, written
	// only by the update pipeline.
	if got := readColumn[spanner.NullTime](ctx, t, db, "WorkOrders", spanner.Key{woScrubberID}, "UpdatedAt"); got.Valid {
		t.Fatalf("seeded UpdatedAt = %v, want unset", got)
	}

	status, body := doRequestAs(t, h, "nudger-nkosi", http.MethodPost, "/api/waystations/ws-alpha/nudge-work-order",
		fmt.Sprintf(`{"workOrderId":%q}`, woScrubberID))
	assertStatus(t, status, http.StatusOK, body)

	first := readColumn[spanner.NullTime](ctx, t, db, "WorkOrders", spanner.Key{woScrubberID}, "UpdatedAt")
	if !first.Valid {
		t.Fatal("UpdatedAt not stamped by the touch")
	}

	// The touch changed nothing else: its change event carries exactly the mechanical
	// stamp, attributed to the nudging user.
	eventSource, changeSetKeys := latestChangeEvent(t, db, "WorkOrders", woScrubberID)
	if want := "nudger-nkosi ("; !strings.HasPrefix(eventSource, want) {
		t.Errorf("eventSource = %q, want the session-derived %q prefix", eventSource, want)
	}
	if len(changeSetKeys) != 1 || changeSetKeys[0] != "UpdatedAt" {
		t.Errorf("changeSet keys = %v, want exactly [UpdatedAt]", changeSetKeys)
	}

	// A second nudge advances the stamp: every touch is a real update.
	status, body = doRequestAs(t, h, "nudger-nkosi", http.MethodPost, "/api/waystations/ws-alpha/nudge-work-order",
		fmt.Sprintf(`{"workOrderId":%q}`, woScrubberID))
	assertStatus(t, status, http.StatusOK, body)
	second := readColumn[spanner.NullTime](ctx, t, db, "WorkOrders", spanner.Key{woScrubberID}, "UpdatedAt")
	if !second.Valid || !second.Time.After(first.Time) {
		t.Errorf("second touch UpdatedAt = %v, want after the first %v", second, first)
	}
}

// TestNudgeWorkOrderRefusals pins every way a nudge is refused — and that a refused
// nudge never runs the pipeline: the target row's stamp stays unset.
func TestNudgeWorkOrderRefusals(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name        string
		grants      grants
		workOrderID string
		rowExists   bool
		wantStatus  int
		wantStamped bool
	}{
		{
			// The terminal rule is policy, not code (§12): the demo grants
			// carry `state NOT IN ('completed', 'cancelled')`, but this
			// scripted grant is unconditional — so the nudge lands even on a
			// finished order. The demo-role refusal pins in the
			// bootstrap-parity test below.
			name:        "an unconditional grant nudges even a finished order",
			grants:      nudgeGrants(),
			workOrderID: woCraneID, // completed
			rowExists:   true,
			wantStatus:  http.StatusOK,
			wantStamped: true,
		},
		{
			// The generated frame locates the @target row before the body runs.
			name:        "a missing row is a 404, not a silent success",
			grants:      nudgeGrants(),
			workOrderID: "80000000-0000-4000-8000-00000000dead",
			wantStatus:  http.StatusNotFound,
		},
		{
			name:        "with no grants at all the domain is concealed",
			grants:      grants{},
			workOrderID: woScrubberID, // in_progress, nudgeable if any grant were held
			rowExists:   true,
			wantStatus:  http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newTestApp(db, tt.grants)

			status, body := doRequest(t, h, http.MethodPost, "/api/waystations/ws-alpha/nudge-work-order",
				fmt.Sprintf(`{"workOrderId":%q}`, tt.workOrderID))
			if status != tt.wantStatus {
				t.Fatalf("status = %d, want %d: %s", status, tt.wantStatus, body)
			}

			if tt.rowExists {
				got := readColumn[spanner.NullTime](ctx, t, db, "WorkOrders", spanner.Key{tt.workOrderID}, "UpdatedAt")
				if got.Valid != tt.wantStamped {
					t.Errorf("UpdatedAt = %v, want stamped = %v", got, tt.wantStamped)
				}
			}
		})
	}
}

// TestNudgeWorkOrderBootstrapParity runs the nudge over the real permission engine
// with the committed demo roles.
func TestNudgeWorkOrderBootstrapParity(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}
	h := newDemoApp(ctx, t, db)

	tests := []struct {
		name        string
		user        accesstypes.User
		domain      string
		workOrderID string
		wantStatus  int
	}{
		{
			name:        "foreman nudges an open order on their station",
			user:        "foreman-okafor",
			domain:      wsAlpha,
			workOrderID: woOvenID,
			wantStatus:  http.StatusOK,
		},
		{
			name:        "quartermaster holds no nudge grant",
			user:        "quartermaster-idris",
			domain:      wsAlpha,
			workOrderID: woScrubberID,
			wantStatus:  http.StatusForbidden,
		},
		{
			name:        "foreman's role is scoped to ws-alpha, so ws-beta is concealed from them",
			user:        "foreman-okafor",
			domain:      wsBeta,
			workOrderID: woBetaAirID,
			wantStatus:  http.StatusNotFound,
		},
		{
			// The demo grant's condition — state NOT IN ('completed',
			// 'cancelled') — evaluates against the located row inside the
			// frame's transaction (§12): a finished order refuses the nudge
			// with the frame's uniform Forbidden.
			name:        "the grant's condition refuses a finished order",
			user:        "foreman-okafor",
			domain:      wsAlpha,
			workOrderID: woCraneID, // completed
			wantStatus:  http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status, body := doRequestAs(t, h, tt.user, http.MethodPost,
				fmt.Sprintf("/api/waystations/%s/nudge-work-order", tt.domain),
				fmt.Sprintf(`{"workOrderId":%q}`, tt.workOrderID))
			if status != tt.wantStatus {
				t.Errorf("status = %d, want %d: %s", status, tt.wantStatus, body)
			}
		})
	}
}
