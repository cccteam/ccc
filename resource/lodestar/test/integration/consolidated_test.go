package integration

// This suite pins the consolidated mutation surface's batch semantics: one PATCH
// /api/resources body is one transaction even when its operations span sectors, and
// the wire vocabulary is closed — add, patch, and remove are the only operations.

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

func countMissionsTitled(ctx context.Context, t *testing.T, db *initiator.SpannerDB, title string) int64 {
	t.Helper()

	iter := db.Single().Query(ctx, spanner.Statement{
		SQL:    "SELECT COUNT(*) FROM Missions WHERE Title = @title",
		Params: map[string]any{"title": title},
	})
	defer iter.Stop()

	row, err := iter.Next()
	if err != nil && !stderrors.Is(err, iterator.Done) {
		t.Fatalf("counting missions: %v", err)
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

	createGrants := grants{accesstypes.Create: withFields(missionsResource, "clientId", "kindId", "title", "hazard", "fee", "deadline")}
	value := func(title string) string {
		return fmt.Sprintf(`{"clientId":%q,"kindId":"courier","title":%q,"hazard":1,"fee":100,"deadline":"2027-01-01T00:00:00Z"}`, clientHalvardID, title)
	}

	tests := []struct {
		name       string
		ops        string
		wantStatus int
		title      string
		wantCount  int64
	}{
		{
			name:       "the operation vocabulary is closed: upsert is not expressible",
			ops:        fmt.Sprintf(`[{"op":"upsert","path":%q,"value":%s}]`, opPath(anvil, "missions"), value("upsert probe")),
			wantStatus: http.StatusBadRequest,
			title:      "upsert probe",
			wantCount:  0,
		},
		{
			name:       "one batch spans sectors atomically",
			ops:        fmt.Sprintf(`[{"op":"add","path":%q,"value":%s},{"op":"add","path":%q,"value":%s}]`, opPath(anvil, "missions"), value("cross-sector batch"), opPath(bastion, "missions"), value("cross-sector batch")),
			wantStatus: http.StatusOK,
			title:      "cross-sector batch",
			wantCount:  2,
		},
		{
			name:       "a refused operation rolls back the whole batch",
			ops:        fmt.Sprintf(`[{"op":"add","path":%q,"value":%s},{"op":"add","path":%q,"value":{"name":"Rollback Wing"}}]`, opPath(anvil, "missions"), value("atomic probe"), opPath(bastion, "wings")),
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
			if got := countMissionsTitled(ctx, t, db, tt.title); got != tt.wantCount {
				t.Errorf("missions titled %q = %d, want %d", tt.title, got, tt.wantCount)
			}
		})
	}
}
