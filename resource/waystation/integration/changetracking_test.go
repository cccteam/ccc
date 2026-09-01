package integration

// This suite pins change tracking: WorkOrders and Requisitions opt in
// (TrackChanges(true)), so their mutations write DataChangeEvents rows in the same
// transaction with the session-derived event source; Modules keep the default and
// write none.

import (
	"context"
	stderrors "errors"
	"fmt"
	"net/http"
	"testing"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
	initiator "github.com/cccteam/db-initiator"
	"google.golang.org/api/iterator"
)

func countChangeEvents(ctx context.Context, t *testing.T, db *initiator.SpannerDB, table string) int64 {
	t.Helper()

	iter := db.Single().Query(ctx, spanner.Statement{
		SQL:    "SELECT COUNT(*) FROM DataChangeEvents WHERE TableName = @table",
		Params: map[string]any{"table": table},
	})
	defer iter.Stop()

	row, err := iter.Next()
	if err != nil && !stderrors.Is(err, iterator.Done) {
		t.Fatalf("counting change events: %v", err)
	}
	var count int64
	if err := row.Columns(&count); err != nil {
		t.Fatalf("row.Columns: %v", err)
	}

	return count
}

func TestChangeTracking(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}

	modules := accesstypes.Resource("Modules")
	h := newTestApp(db, grants{
		accesstypes.Create: {
			workOrdersResource,
			fieldResource(workOrdersResource, "waystationId"),
			fieldResource(workOrdersResource, "assetId"),
			fieldResource(workOrdersResource, "title"),
			fieldResource(workOrdersResource, "priority"),
			modules,
			fieldResource(modules, "waystationId"),
			fieldResource(modules, "name"),
			fieldResource(modules, "zone"),
			fieldResource(modules, "pressureRated"),
		},
	})

	// A tracked resource's create writes its change event in the same transaction.
	status, body := doRequestAs(t, h, "tracked-user", http.MethodPatch, "/api/resources",
		fmt.Sprintf(`[{"op":"add","path":"/waystations/ws-alpha/work-orders","value":{"waystationId":"ws-alpha","assetId":%q,"title":"Tracked create","priority":1}}]`, assetRecyclerID))
	assertStatus(t, status, http.StatusOK, body)
	if got := countChangeEvents(ctx, t, db, "WorkOrders"); got != 1 {
		t.Errorf("WorkOrders change events = %d, want 1", got)
	}

	// An untracked resource writes none. Module is excluded from consolidation, so
	// its standalone PATCH surface carries the mutation — covering that route shape
	// in the same breath.
	status, body = doRequest(t, h, http.MethodPatch, "/api/waystations/ws-alpha/modules",
		`[{"op":"add","path":"/","value":{"waystationId":"ws-alpha","name":"Annex","zone":"cargo","pressureRated":false}}]`)
	assertStatus(t, status, http.StatusOK, body)
	if got := countChangeEvents(ctx, t, db, "Modules"); got != 0 {
		t.Errorf("Modules change events = %d, want 0 (tracking off)", got)
	}
}
