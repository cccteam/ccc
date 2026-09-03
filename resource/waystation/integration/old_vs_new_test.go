package integration

// Old-vs-new comparisons (design plan §05, decided 2026-09-03): the foreman's
// WorkOrders Update grant carries `... AND new.priority <= priority` — a
// foreman may re-prioritize only downward; raising priority is the chief's
// call (their grant is unconditional). The term compares the proposed value
// against the row's pre-image inside the mutation's own check-SELECT, and an
// update that leaves priority untouched degenerates to the tautology — the
// mutation is judged only by what it changes.

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
)

// TestOldVsNewPriority walks the may-lower-never-raise rule over the seeded
// draft work order (priority 4). Deliberately not a table — each step depends
// on the priority the previous one left behind.
func TestOldVsNewPriority(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}
	h := newDemoApp(ctx, t, db)

	patchPriority := func(user accesstypes.User, priority int) (int, []byte) {
		return doRequestAs(t, h, user, http.MethodPatch, "/api/resources",
			fmt.Sprintf(`[{"op":"patch","path":"/waystations/ws-alpha/work-orders/%s","value":{"priority":%d}}]`, woManifoldID, priority))
	}

	// An edit that leaves priority untouched carries the tautology: the
	// untouched new.priority reads the existing column, so the old-vs-new
	// term never blocks what the foreman may otherwise change.
	status, body := doRequestAs(t, h, "foreman-okafor", http.MethodPatch, "/api/resources",
		fmt.Sprintf(`[{"op":"patch","path":"/waystations/ws-alpha/work-orders/%s","value":{"dueAt":"2026-09-25T09:00:00Z"}}]`, woManifoldID))
	assertStatus(t, status, http.StatusOK, body)

	// Lowering holds: 2 <= 4.
	status, body = patchPriority("foreman-okafor", 2)
	assertStatus(t, status, http.StatusOK, body)

	// Raising is refused by the same grant: 5 > 2 fails the check-SELECT
	// inside the mutation's transaction, and nothing commits.
	status, body = patchPriority("foreman-okafor", 5)
	assertStatus(t, status, http.StatusForbidden, body)

	// Raising priority is the chief's call — their Update grant carries no
	// old-vs-new conjunct.
	status, body = patchPriority("chief-alpha", 5)
	assertStatus(t, status, http.StatusOK, body)
}
