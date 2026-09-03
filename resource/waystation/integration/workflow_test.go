package integration

// This suite pins the stateful-resource pattern's structural half with a scripted
// permission engine (the grants-only half lives in conditions_test.go against the
// real engine): the state column is unwritable from the wire, transitions run only
// through the Execute-gated RPC methods and enforce edge legality, completing a work
// order stamps the asset, and the interleaved member creates through a
// client-supplied key path.

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
)

// workflowGrants scripts everything the workflow walk needs; enforcement nuances are
// not under test here.
func workflowGrants() grants {
	return grants{
		accesstypes.Create: {
			workOrdersResource,
			fieldResource(workOrdersResource, "assetId"),
			fieldResource(workOrdersResource, "title"),
			fieldResource(workOrdersResource, "priority"),
			accesstypes.Resource("WorkOrderTasks"),
			fieldResource("WorkOrderTasks", "instructions"),
			fieldResource("WorkOrderTasks", "done"),
		},
		accesstypes.Read: {
			workOrdersResource,
			fieldResource(workOrdersResource, "statusId"),
			fieldResource(workOrdersResource, "createdBy"),
			fieldResource(workOrdersResource, "title"),
		},
		accesstypes.Execute: {
			accesstypes.Resource("ScheduleWorkOrder"),
			accesstypes.Resource("StartWorkOrder"),
			accesstypes.Resource("CompleteWorkOrder"),
		},
	}
}

func TestWorkOrderWorkflow(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}
	h := newTestApp(db, workflowGrants())

	// Deliberately not a table: the walk mutates one work order through its
	// lifecycle, so each section depends on the state the previous one left behind
	// and they run in order rather than as parallel subtests.

	// The wire cannot express a state write: the patch decode is closed over statusId.
	status, body := doRequest(t, h, http.MethodPatch, "/api/resources",
		fmt.Sprintf(`[{"op":"add","path":"/waystations/ws-alpha/work-orders","value":{"assetId":%q,"title":"Smuggled state","priority":1,"statusId":"completed"}}]`, assetRecyclerID))
	if status != http.StatusBadRequest {
		t.Fatalf("statusId on the wire: status = %d, want 400: %s", status, body)
	}

	// Create a draft as a specific user: CreatedBy is server-stamped from the session
	// and the initial state comes from the @state default, never the client.
	status, body = doRequestAs(t, h, "workflow-author", http.MethodPatch, "/api/resources",
		fmt.Sprintf(`[{"op":"add","path":"/waystations/ws-alpha/work-orders","value":{"assetId":%q,"title":"Recycler tune-up","priority":2}}]`, assetRecyclerID))
	assertStatus(t, status, http.StatusOK, body)
	created := decodeRow(t, body)
	ids, _ := created["workOrders"].([]any)
	if len(ids) != 1 {
		t.Fatalf("created ids = %v, want one work order id: %s", created, body)
	}
	woID, _ := ids[0].(string)

	if got := readColumn[string](ctx, t, db, "WorkOrders", spanner.Key{woID}, "CreatedBy"); got != "workflow-author" {
		t.Errorf("CreatedBy = %q, want the session user", got)
	}
	if got := readColumn[string](ctx, t, db, "WorkOrders", spanner.Key{woID}, "StatusId"); got != "draft" {
		t.Errorf("StatusId = %q, want the @state default draft", got)
	}

	// The interleaved member creates through a client-supplied key path.
	status, body = doRequest(t, h, http.MethodPatch, "/api/resources",
		fmt.Sprintf(`[{"op":"add","path":"/waystations/ws-alpha/work-order-tasks/%s/1","value":{"instructions":"Swap filters","done":false}}]`, woID))
	assertStatus(t, status, http.StatusOK, body)

	// Illegal edge: a draft cannot start. The declared transition (@transition)
	// answers Forbidden, naming the edge.
	status, body = doRequest(t, h, http.MethodPost, "/api/waystations/ws-alpha/start-work-order",
		fmt.Sprintf(`{"workOrderId":%q}`, woID))
	if status != http.StatusForbidden {
		t.Fatalf("start a draft: status = %d, want 403: %s", status, body)
	}
	if !strings.Contains(string(body), "StartWorkOrder runs from a scheduled WorkOrder") {
		t.Fatalf("start a draft: refusal must name the transition and its from set: %s", body)
	}

	// Legal walk: schedule -> start -> complete.
	status, body = doRequest(t, h, http.MethodPost, "/api/waystations/ws-alpha/schedule-work-order",
		fmt.Sprintf(`{"workOrderId":%q,"assignedTeamId":%q,"dueAt":%q}`, woID, teamAlphaMechID, time.Now().UTC().Add(48*time.Hour).Format(time.RFC3339)))
	assertStatus(t, status, http.StatusOK, body)

	status, body = doRequest(t, h, http.MethodPost, "/api/waystations/ws-alpha/start-work-order",
		fmt.Sprintf(`{"workOrderId":%q}`, woID))
	assertStatus(t, status, http.StatusOK, body)

	status, body = doRequest(t, h, http.MethodPost, "/api/waystations/ws-alpha/complete-work-order",
		fmt.Sprintf(`{"workOrderId":%q}`, woID))
	assertStatus(t, status, http.StatusOK, body)

	if got := readColumn[string](ctx, t, db, "WorkOrders", spanner.Key{woID}, "StatusId"); got != "completed" {
		t.Errorf("StatusId after the walk = %q, want completed", got)
	}

	// Completing stamps the asset's LastServicedAt in the same transaction — the
	// completion edge is the field's only writer.
	if got := readColumn[spanner.NullTime](ctx, t, db, "Assets", spanner.Key{assetRecyclerID}, "LastServicedAt"); !got.Valid {
		t.Error("Assets.LastServicedAt not stamped by CompleteWorkOrder")
	}
}
