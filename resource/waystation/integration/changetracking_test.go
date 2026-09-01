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

	tests := []struct {
		name       string
		target     string
		ops        string
		table      string // the DataChangeEvents table probed after the mutation
		wantEvents int64
	}{
		{
			name:       "a tracked resource's create writes its change event in the same transaction",
			target:     "/api/resources",
			ops:        fmt.Sprintf(`[{"op":"add","path":"/waystations/ws-alpha/work-orders","value":{"waystationId":"ws-alpha","assetId":%q,"title":"Tracked create","priority":1}}]`, assetRecyclerID),
			table:      "WorkOrders",
			wantEvents: 1,
		},
		{
			// Module is excluded from consolidation, so its standalone PATCH surface
			// carries the mutation — covering that route shape in the same breath.
			name:       "an untracked resource writes none",
			target:     "/api/waystations/ws-alpha/modules",
			ops:        `[{"op":"add","path":"/","value":{"waystationId":"ws-alpha","name":"Annex","zone":"cargo","pressureRated":false}}]`,
			table:      "Modules",
			wantEvents: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status, body := doRequestAs(t, h, "tracked-user", http.MethodPatch, tt.target, tt.ops)
			assertStatus(t, status, http.StatusOK, body)
			if got := countChangeEvents(ctx, t, db, tt.table); got != tt.wantEvents {
				t.Errorf("%s change events = %d, want %d", tt.table, got, tt.wantEvents)
			}
		})
	}
}
