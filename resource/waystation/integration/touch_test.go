package integration

// This suite pins the first-class Touch: an update carried entirely by the resource's
// update functions. NudgeWorkOrder fires the generated NewWorkOrderTouch, so the full
// update pipeline runs with no caller-set fields — UpdatedAt takes the commit
// timestamp and the change event records who nudged — while a plain update patch with
// no fields set stays a silent no-op (pinned in the resource module). The structural
// half scripts its grants; the bootstrap-parity half runs the demo personas over the
// real permission engine.

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

func TestNudgeWorkOrderTouch(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}
	h := newTestApp(db, grants{
		accesstypes.Execute: {accesstypes.Resource("NudgeWorkOrder")},
	})

	// The suite mutates one work order's stamp step by step, so the sections run in
	// order rather than as parallel subtests.

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

	// A finished work order has no one left to get its attention.
	status, body = doRequest(t, h, http.MethodPost, "/api/waystations/ws-alpha/nudge-work-order",
		fmt.Sprintf(`{"workOrderId":%q}`, woCraneID))
	if status != http.StatusBadRequest {
		t.Errorf("nudge a completed order: status = %d, want 400: %s", status, body)
	}

	// A missing row is a 404, not a silent success.
	status, body = doRequest(t, h, http.MethodPost, "/api/waystations/ws-alpha/nudge-work-order",
		`{"workOrderId":"80000000-0000-4000-8000-00000000dead"}`)
	if status != http.StatusNotFound {
		t.Errorf("nudge a missing order: status = %d, want 404: %s", status, body)
	}
}

// TestNudgeWorkOrderTouchUngranted pins the decode-time Execute check: without the
// grant the touch never runs, and the row keeps its unset stamp.
func TestNudgeWorkOrderTouchUngranted(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}
	h := newTestApp(db, grants{})

	status, body := doRequest(t, h, http.MethodPost, "/api/waystations/ws-alpha/nudge-work-order",
		fmt.Sprintf(`{"workOrderId":%q}`, woScrubberID))
	if status != http.StatusForbidden {
		t.Fatalf("ungranted nudge: status = %d, want 403: %s", status, body)
	}
	if got := readColumn[spanner.NullTime](ctx, t, db, "WorkOrders", spanner.Key{woScrubberID}, "UpdatedAt"); got.Valid {
		t.Errorf("UpdatedAt = %v after a refused nudge, want unset", got)
	}
}

// TestNudgeWorkOrderBootstrapParity runs the nudge over the real permission engine
// with the committed demo roles: the foreman chases work on their own station, the
// quartermaster holds no nudge grant at all, and a role scoped to ws-alpha buys
// nothing on the ws-beta route.
func TestNudgeWorkOrderBootstrapParity(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}
	h := newDemoApp(ctx, t, db)

	t.Run("foreman nudges an open order on their station", func(t *testing.T) {
		t.Parallel()

		status, body := doRequestAs(t, h, "foreman-okafor", http.MethodPost, "/api/waystations/ws-alpha/nudge-work-order",
			fmt.Sprintf(`{"workOrderId":%q}`, woOvenID))
		assertStatus(t, status, http.StatusOK, body)
	})

	t.Run("quartermaster holds no nudge grant", func(t *testing.T) {
		t.Parallel()

		status, body := doRequestAs(t, h, "quartermaster-idris", http.MethodPost, "/api/waystations/ws-alpha/nudge-work-order",
			fmt.Sprintf(`{"workOrderId":%q}`, woScrubberID))
		if status != http.StatusForbidden {
			t.Errorf("quartermaster nudge: status = %d, want 403: %s", status, body)
		}
	})

	t.Run("foreman's role is scoped to ws-alpha", func(t *testing.T) {
		t.Parallel()

		status, body := doRequestAs(t, h, "foreman-okafor", http.MethodPost, "/api/waystations/ws-beta/nudge-work-order",
			fmt.Sprintf(`{"workOrderId":%q}`, woBetaAirID))
		if status != http.StatusForbidden {
			t.Errorf("foreman on ws-beta: status = %d, want 403: %s", status, body)
		}
	})
}
