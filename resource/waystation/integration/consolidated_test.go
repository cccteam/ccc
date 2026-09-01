package integration

// This suite pins the consolidated mutation surface's batch semantics: one PATCH
// /api/resources body is one transaction even when its operations span waystations,
// and the wire vocabulary is closed — add, patch, and remove are the only operations,
// so an insert-or-update can never arrive over HTTP. (The programmatic PatchSet path
// does expose CreateOrUpdatePatchType; conditional grants refuse it fail-closed, which
// the resource package's write_checks unit suite pins. Between the two, upsert +
// conditional grant is unreachable end to end.)

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

func countWorkOrdersTitled(ctx context.Context, t *testing.T, db *initiator.SpannerDB, title string) int64 {
	t.Helper()

	iter := db.Single().Query(ctx, spanner.Statement{
		SQL:    "SELECT COUNT(*) FROM WorkOrders WHERE Title = @title",
		Params: map[string]any{"title": title},
	})
	defer iter.Stop()

	row, err := iter.Next()
	if err != nil && !stderrors.Is(err, iterator.Done) {
		t.Fatalf("counting work orders: %v", err)
	}
	var count int64
	if err := row.Columns(&count); err != nil {
		t.Fatalf("row.Columns: %v", err)
	}

	return count
}

func TestConsolidatedBatchSemantics(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}

	createGrants := grants{accesstypes.Create: {
		workOrdersResource,
		fieldResource(workOrdersResource, "waystationId"),
		fieldResource(workOrdersResource, "assetId"),
		fieldResource(workOrdersResource, "title"),
		fieldResource(workOrdersResource, "priority"),
	}}

	tests := []struct {
		name       string
		ops        string
		wantStatus int
		title      string // the WorkOrders title the batch tried to create
		wantCount  int64  // rows carrying that title after the batch
	}{
		{
			name: "the operation vocabulary is closed: upsert is not expressible",
			ops: fmt.Sprintf(`[{"op":"upsert","path":"/waystations/ws-alpha/work-orders","value":{"waystationId":"ws-alpha","assetId":%q,"title":"upsert probe","priority":1}}]`,
				assetRecyclerID),
			wantStatus: http.StatusBadRequest,
			title:      "upsert probe",
			wantCount:  0,
		},
		{
			// The scripted grants apply in every scope, so this user may create work
			// orders at both stations — the batch's two domains commit together.
			name: "one batch spans waystations atomically",
			ops: fmt.Sprintf(`[
				{"op":"add","path":"/waystations/ws-alpha/work-orders","value":{"waystationId":"ws-alpha","assetId":%q,"title":"cross-domain batch","priority":1}},
				{"op":"add","path":"/waystations/ws-beta/work-orders","value":{"waystationId":"ws-beta","assetId":%q,"title":"cross-domain batch","priority":1}}
			]`, assetRecyclerID, assetBetaAirID),
			wantStatus: http.StatusOK,
			title:      "cross-domain batch",
			wantCount:  2,
		},
		{
			// The second operation targets a resource the user holds no grant on, so
			// the batch fails — and the first operation's already-decoded create must
			// not survive it.
			name: "a refused operation rolls back the whole batch",
			ops: fmt.Sprintf(`[
				{"op":"add","path":"/waystations/ws-alpha/work-orders","value":{"waystationId":"ws-alpha","assetId":%q,"title":"atomic probe","priority":1}},
				{"op":"add","path":"/waystations/ws-beta/teams","value":{"waystationId":"ws-beta","name":"Rollback Crew","specialty":"none"}}
			]`, assetRecyclerID),
			wantStatus: http.StatusForbidden,
			title:      "atomic probe",
			wantCount:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newTestApp(db, createGrants)
			status, body := doRequest(t, h, http.MethodPatch, "/api/resources", tt.ops)
			assertStatus(t, status, tt.wantStatus, body)
			if got := countWorkOrdersTitled(ctx, t, db, tt.title); got != tt.wantCount {
				t.Errorf("work orders titled %q = %d, want %d", tt.title, got, tt.wantCount)
			}
		})
	}
}
