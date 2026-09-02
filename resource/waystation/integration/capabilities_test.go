package integration

// Bootstrap parity for the capability envelope (design plan §13): per-row
// write affordances riding the read response, opt-in via ?capabilities=,
// evaluated through the real engine against the same row image the response
// carries. The answers are advisory; the write-path suites prove the
// enforcement these hints predict.

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
)

func TestCapabilityEnvelope(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}
	h := newDemoApp(ctx, t, db)

	// wantCapability is one row's expected envelope: the Update list in
	// projection order and the Delete boolean.
	type wantCapability struct {
		update []any
		del    bool
	}

	tests := []struct {
		name   string
		user   accesstypes.User
		target string
		// want maps row id → expected envelope; every listed row must appear.
		want map[string]wantCapability
	}{
		{
			// StationChief's Update grant is unconditional (pure RBAC): the
			// same positive list on every row with no extra SQL paid, while
			// the conditional Delete (state = 'draft') answers per row.
			name:   "chief's unconditional update lists every field, delete follows state",
			user:   "chief-alpha",
			target: "/api/waystations/ws-alpha/work-orders?capabilities=Update,Delete",
			want: map[string]wantCapability{
				woScrubberID: {update: []any{"title", "summary", "priority", "assignedTeamId", "dueAt"}, del: false}, // in_progress
				woOvenID:     {update: []any{"title", "summary", "priority", "assignedTeamId", "dueAt"}, del: false}, // scheduled
				woManifoldID: {update: []any{"title", "summary", "priority", "assignedTeamId", "dueAt"}, del: true},  // draft
				woCraneID:    {update: []any{"title", "summary", "priority", "assignedTeamId", "dueAt"}, del: false}, // completed
			},
		},
		{
			// Foreman's Update grant is condition-limited (state IN
			// ('draft', 'scheduled')) and narrower: the editable list appears
			// only where the row's state admits it, and with no Delete grant
			// at all the button is dead everywhere.
			name:   "foreman's conditional update follows each row's state",
			user:   "foreman-okafor",
			target: "/api/waystations/ws-alpha/work-orders?capabilities=Update,Delete",
			want: map[string]wantCapability{
				woScrubberID: {update: []any{}, del: false},                                      // in_progress
				woOvenID:     {update: []any{"priority", "assignedTeamId", "dueAt"}, del: false}, // scheduled
				woManifoldID: {update: []any{"priority", "assignedTeamId", "dueAt"}, del: false}, // draft
				woCraneID:    {update: []any{}, del: false},                                      // completed
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status, body := doRequestAs(t, h, tt.user, http.MethodGet, tt.target, "")
			if status != http.StatusOK {
				t.Fatalf("GET %s status = %d, want %d (body %s)", tt.target, status, http.StatusOK, body)
			}

			rows := decodeRows(t, body)
			seen := make(map[string]bool, len(tt.want))
			for _, row := range rows {
				id, _ := row["id"].(string)
				want, pinned := tt.want[id]
				if !pinned {
					continue
				}
				seen[id] = true

				capsAny, ok := row["zzCapabilities"]
				if !ok {
					t.Errorf("row %s carries no zzCapabilities property: %v", id, row)

					continue
				}
				caps, _ := capsAny.(map[string]any)
				if got := caps["Update"]; !reflect.DeepEqual(got, tt.want[id].update) {
					t.Errorf("row %s Update = %v, want %v", id, got, want.update)
				}
				if got := caps["Delete"]; got != want.del {
					t.Errorf("row %s Delete = %v, want %v", id, got, want.del)
				}
			}
			for id := range tt.want {
				if !seen[id] {
					t.Errorf("row %s missing from the response", id)
				}
			}
		})
	}

	t.Run("a capability-free request carries no envelope", func(t *testing.T) {
		t.Parallel()

		status, body := doRequestAs(t, h, "chief-alpha", http.MethodGet, "/api/waystations/ws-alpha/work-orders", "")
		if status != http.StatusOK {
			t.Fatalf("GET status = %d, want %d (body %s)", status, http.StatusOK, body)
		}
		for _, row := range decodeRows(t, body) {
			if _, ok := row["zzCapabilities"]; ok {
				t.Fatalf("row %v carries zzCapabilities without opting in", row["id"])
			}
		}
	})

	t.Run("an unsupported capability is a bad request", func(t *testing.T) {
		t.Parallel()

		status, body := doRequestAs(t, h, "chief-alpha", http.MethodGet, "/api/waystations/ws-alpha/work-orders?capabilities=Create", "")
		if status != http.StatusBadRequest {
			t.Fatalf("GET status = %d, want %d (body %s)", status, http.StatusBadRequest, body)
		}
	})
}
