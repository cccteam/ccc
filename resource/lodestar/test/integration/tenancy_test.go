package integration

// This suite pins write-side structural row tenancy end to end: a mutation locates
// its target row WITHIN the tenant predicate, so a cross-sector primary key is
// NotFound — indistinguishable from a row that does not exist — even under
// unconditional grants; a create's proposed parent must land in the route's partition
// (the join-path proof, two hops deep for a sortie expense); and the stamped tenant
// key means a create needs no tenant field on the wire at all.

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

	full := grants{
		accesstypes.Update: withFields(missionsResource, "hazard"),
		accesstypes.Delete: {missionsResource},
		accesstypes.Create: withFields("SortieExpenses", "sortieId", "category", "amount"),
	}

	tests := []struct {
		name       string
		body       string
		wantStatus int
		verify     func(t *testing.T)
	}{
		{
			name:       "update via another sector's route is NotFound",
			body:       fmt.Sprintf(`[{"op":"patch","path":%q,"value":{"hazard":5}}]`, opPath(bastion, "missions/"+missionHaulerID)),
			wantStatus: http.StatusNotFound,
			verify: func(t *testing.T) {
				t.Helper()
				if hazard := readColumn[int64](ctx, t, db, "Missions", spanner.Key{missionHaulerID}, "Hazard"); hazard != 2 {
					t.Errorf("Mission hazard = %d, want unchanged 2", hazard)
				}
			},
		},
		{
			name:       "delete via another sector's route is NotFound",
			body:       fmt.Sprintf(`[{"op":"remove","path":%q}]`, opPath(anvil, "missions/"+missionBeaconID)),
			wantStatus: http.StatusNotFound,
			verify: func(t *testing.T) {
				t.Helper()
				if _, err := db.Single().ReadRow(ctx, "Missions", spanner.Key{missionBeaconID}, []string{"Title"}); err != nil {
					t.Errorf("the Bastion mission should survive the cross-sector delete: %v", err)
				}
			},
		},
		{
			name:       "create under another sector's parent, two hops deep, is NotFound",
			body:       fmt.Sprintf(`[{"op":"add","path":%q,"value":{"sortieId":%q,"category":"fuel","amount":1}}]`, opPath(bastion, "sortie-expenses"), sortieConvoyID),
			wantStatus: http.StatusNotFound,
			verify: func(t *testing.T) {
				t.Helper()
				iter := db.Single().Query(ctx, spanner.Statement{SQL: "SELECT COUNT(*) FROM SortieExpenses WHERE SortieId = @s AND Category = 'fuel' AND Amount = 1", Params: map[string]any{"s": sortieConvoyID}})
				defer iter.Stop()
				row, err := iter.Next()
				if err != nil {
					t.Fatalf("count: %v", err)
				}
				var n int64
				if err := row.Columns(&n); err != nil {
					t.Fatal(err)
				}
				if n != 0 {
					t.Errorf("cross-sector expense exists (%d), want none", n)
				}
			},
		},
		{
			name:       "the same mutations in the row's own sector succeed",
			body:       fmt.Sprintf(`[{"op":"patch","path":%q,"value":{"hazard":3}}]`, opPath(anvil, "missions/"+missionHaulerID)),
			wantStatus: http.StatusOK,
			verify: func(t *testing.T) {
				t.Helper()
				if hazard := readColumn[int64](ctx, t, db, "Missions", spanner.Key{missionHaulerID}, "Hazard"); hazard != 3 {
					t.Errorf("Mission hazard = %d, want updated 3", hazard)
				}
			},
		},
	}

	// The cases share seeded rows and run sequentially: the final case mutates the
	// row the first case proved unreachable.
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newTestApp(db, full)

			status, body := doRequest(t, h, http.MethodPatch, "/api/resources", tt.body)
			assertStatus(t, status, tt.wantStatus, body)
			if tt.wantStatus == http.StatusNotFound && !strings.Contains(string(body), "not found") {
				t.Errorf("expected a not-found message, got: %s", body)
			}
			tt.verify(t)
		})
	}

	_ = codes.NotFound
}
