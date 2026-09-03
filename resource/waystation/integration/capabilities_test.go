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

		status, body := doRequestAs(t, h, "chief-alpha", http.MethodGet, "/api/waystations/ws-alpha/work-orders?capabilities=Read", "")
		if status != http.StatusBadRequest {
			t.Fatalf("GET status = %d, want %d (body %s)", status, http.StatusBadRequest, body)
		}
	})

	// The create-under-parent affordance (§11): each requisition row lists the
	// workflow member resources the user may create beneath it. The foreman's
	// RequisitionLines Create grant carries `state = 'draft'`, so the add-line
	// affordance follows the parent's state — present on their draft, absent
	// once submitted or approved — with no status comparison in the UI. The
	// chief holds no line-create grant, so every row answers empty.
	createTests := []struct {
		name string
		user accesstypes.User
		want map[string][]any
	}{
		{
			name: "foreman's line-create affordance follows the requisition's state",
			user: "foreman-okafor",
			want: map[string][]any{
				reqPumpID:     {"RequisitionLines"}, // draft
				reqScrubberID: {},                   // submitted
				reqTorchID:    {},                   // approved
			},
		},
		{
			name: "the chief holds no line-create grant, so every row answers empty",
			user: "chief-alpha",
			want: map[string][]any{
				reqPumpID:     {},
				reqScrubberID: {},
			},
		},
	}
	for _, tt := range createTests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status, body := doRequestAs(t, h, tt.user, http.MethodGet, "/api/waystations/ws-alpha/requisitions?capabilities=Create", "")
			if status != http.StatusOK {
				t.Fatalf("GET status = %d, want %d (body %s)", status, http.StatusOK, body)
			}

			seen := make(map[string]bool, len(tt.want))
			for _, row := range decodeRows(t, body) {
				id, _ := row["id"].(string)
				want, pinned := tt.want[id]
				if !pinned {
					continue
				}
				seen[id] = true

				caps, _ := row["zzCapabilities"].(map[string]any)
				if got := caps["Create"]; !reflect.DeepEqual(got, want) {
					t.Errorf("row %s Create = %v, want %v", id, got, want)
				}
			}
			for id := range tt.want {
				if !seen[id] {
					t.Errorf("row %s missing from the response", id)
				}
			}
		})
	}

	// The Execute affordance (§09/§13): each row lists the targeted methods
	// that apply to it — declared transitions whose from set contains the
	// pre-image state, and plain @target methods whose grant condition holds
	// (§12: Nudge's `state NOT IN ('completed', 'cancelled')` rides the grant,
	// so it appears on every unfinished row and vanishes on finished ones).
	// The chief holds every WorkOrder edge plus Nudge, the technician only
	// Start and Complete, and a finished row offers nothing to anyone.
	executeTests := []struct {
		name string
		user accesstypes.User
		want map[string][]any
	}{
		{
			name: "chief's execute list follows each row's state across every edge",
			user: "chief-alpha",
			want: map[string][]any{
				woManifoldID: {"NudgeWorkOrder", "ScheduleWorkOrder"}, // draft
				woOvenID:     {"NudgeWorkOrder", "StartWorkOrder"},    // scheduled
				woScrubberID: {"CompleteWorkOrder", "NudgeWorkOrder"}, // in_progress
				woCraneID:    {},                                      // completed: no edge, and Nudge's condition excludes it
			},
		},
		{
			// The technician's List condition (assignedTeam IN subject.teams OR
			// author = subject) already hides the unassigned rows, so only the
			// visible ones pin here: the in_progress row offers Complete (Start
			// is granted but its from set excludes the state), the completed
			// row offers nothing.
			name: "technician's execute list carries only the granted edges",
			user: "tech-rivera",
			want: map[string][]any{
				woScrubberID: {"CompleteWorkOrder"}, // in_progress
				woCraneID:    {},                    // completed
			},
		},
	}
	for _, tt := range executeTests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status, body := doRequestAs(t, h, tt.user, http.MethodGet, "/api/waystations/ws-alpha/work-orders?capabilities=Execute", "")
			if status != http.StatusOK {
				t.Fatalf("GET status = %d, want %d (body %s)", status, http.StatusOK, body)
			}

			seen := make(map[string]bool, len(tt.want))
			for _, row := range decodeRows(t, body) {
				id, _ := row["id"].(string)
				want, pinned := tt.want[id]
				if !pinned {
					continue
				}
				seen[id] = true

				caps, _ := row["zzCapabilities"].(map[string]any)
				if got := caps["Execute"]; !reflect.DeepEqual(got, want) {
					t.Errorf("row %s Execute = %v, want %v", id, got, want)
				}
			}
			for id := range tt.want {
				if !seen[id] {
					t.Errorf("row %s missing from the response", id)
				}
			}
		})
	}

	// Row conditions on Execute (§12): the chief's Approve grant carries
	// `totalCost <= subject.approvalLimit` (their limit is 2500), so the same
	// statement answers Approve row by row — present on the small submitted
	// requisition, absent on the 7120 overhaul — while the unconditional
	// Decline follows the edge alone. The Approve button simply never renders
	// where the RPC would refuse.
	t.Run("chief's approve affordance follows their approval limit per row", func(t *testing.T) {
		t.Parallel()

		status, body := doRequestAs(t, h, "chief-alpha", http.MethodGet, "/api/waystations/ws-alpha/requisitions?capabilities=Execute", "")
		if status != http.StatusOK {
			t.Fatalf("GET status = %d, want %d (body %s)", status, http.StatusOK, body)
		}

		want := map[string][]any{
			reqScrubberID: {"ApproveRequisition", "DeclineRequisition"}, // submitted, 361.50 <= 2500
			reqOverhaulID: {"DeclineRequisition"},                       // submitted, 7120.00 over the limit
			reqPumpID:     {"SubmitRequisition"},                        // draft
			reqTorchID:    {},                                           // approved: terminal for these edges
		}
		seen := make(map[string]bool, len(want))
		for _, row := range decodeRows(t, body) {
			id, _ := row["id"].(string)
			wantList, pinned := want[id]
			if !pinned {
				continue
			}
			seen[id] = true

			caps, _ := row["zzCapabilities"].(map[string]any)
			if got := caps["Execute"]; !reflect.DeepEqual(got, wantList) {
				t.Errorf("row %s Execute = %v, want %v", id, got, wantList)
			}
		}
		for id := range want {
			if !seen[id] {
				t.Errorf("row %s missing from the response", id)
			}
		}
	})
}
