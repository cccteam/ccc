package integration

// This suite pins E2's write-side structural row tenancy end to end (design
// plan §06): a mutation locates its target row WITHIN the tenant predicate,
// so a cross-station primary key is NotFound — indistinguishable from a row
// that does not exist — even under unconditional grants; a create's proposed
// parent must land in the route's partition (the join-path proof); and the
// stamped tenant key means a create needs no tenant field on the wire at all
// (the read-side half lives in the query-parameter, structural, and
// conditions suites).

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
	"google.golang.org/grpc/codes"
)

func TestWriteTenancy(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}

	fullWorkOrderMutation := grants{
		accesstypes.Update: {
			workOrdersResource,
			fieldResource(workOrdersResource, "priority"),
		},
		accesstypes.Delete: {workOrdersResource},
		accesstypes.Create: {
			accesstypes.Resource("WorkOrderTasks"),
			fieldResource(accesstypes.Resource("WorkOrderTasks"), "instructions"),
			fieldResource(accesstypes.Resource("WorkOrderTasks"), "done"),
		},
	}

	tests := []struct {
		name       string
		body       string
		wantStatus int
		verify     func(t *testing.T)
	}{
		{
			name:       "update via another station's route is NotFound",
			body:       fmt.Sprintf(`[{"op":"patch","path":"/waystations/ws-beta/work-orders/%s","value":{"priority":5}}]`, woScrubberID),
			wantStatus: http.StatusNotFound,
			verify: func(t *testing.T) {
				t.Helper()
				if priority := readColumn[int64](ctx, t, db, "WorkOrders", spanner.Key{woScrubberID}, "Priority"); priority != 2 {
					t.Errorf("WorkOrder priority = %d, want unchanged 2", priority)
				}
			},
		},
		{
			name:       "delete via another station's route is NotFound",
			body:       fmt.Sprintf(`[{"op":"remove","path":"/waystations/ws-alpha/work-orders/%s"}]`, woBetaAirID),
			wantStatus: http.StatusNotFound,
			verify: func(t *testing.T) {
				t.Helper()
				if _, err := db.Single().ReadRow(ctx, "WorkOrders", spanner.Key{woBetaAirID}, []string{"Title"}); err != nil {
					t.Errorf("beta work order should survive the cross-station delete: %v", err)
				}
			},
		},
		{
			name:       "create under another station's parent is NotFound",
			body:       fmt.Sprintf(`[{"op":"add","path":"/waystations/ws-alpha/work-order-tasks/%s/9","value":{"instructions":"Smuggled task","done":false}}]`, woBetaAirID),
			wantStatus: http.StatusNotFound,
			verify: func(t *testing.T) {
				t.Helper()
				_, err := db.Single().ReadRow(ctx, "WorkOrderTasks", spanner.Key{woBetaAirID, 9}, []string{"Instructions"})
				if spanner.ErrCode(err) != codes.NotFound {
					t.Errorf("cross-station task should not exist, ReadRow err = %v", err)
				}
			},
		},
		{
			name:       "the same mutations in the row's own station succeed",
			body:       fmt.Sprintf(`[{"op":"patch","path":"/waystations/ws-alpha/work-orders/%s","value":{"priority":5}}]`, woScrubberID),
			wantStatus: http.StatusOK,
			verify: func(t *testing.T) {
				t.Helper()
				if priority := readColumn[int64](ctx, t, db, "WorkOrders", spanner.Key{woScrubberID}, "Priority"); priority != 5 {
					t.Errorf("WorkOrder priority = %d, want updated 5", priority)
				}
			},
		},
	}

	// The cases share seeded rows and run sequentially: the final case mutates
	// the row the first case proved unreachable.
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newTestApp(db, fullWorkOrderMutation)

			status, body := doRequest(t, h, http.MethodPatch, "/api/resources", tt.body)
			assertStatus(t, status, tt.wantStatus, body)
			if tt.wantStatus == http.StatusNotFound && !strings.Contains(string(body), "not found") {
				t.Errorf("expected a not-found message, got: %s", body)
			}
			tt.verify(t)
		})
	}
}
