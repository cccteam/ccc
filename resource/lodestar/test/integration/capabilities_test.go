package integration

// Bootstrap parity for the capability envelope: per-row write affordances riding the
// read response, opt-in via ?capabilities=, evaluated through the real engine against
// the same row image the response carries. Update and Delete here; Execute lives in
// execute_conditions_test; Create (the create-under-parent affordance, F10) below.

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
)

func TestCapabilityEnvelope(t *testing.T) {
	t.Parallel()

	_, h, _ := sharedWorld(t)

	type wantCapability struct {
		update []any
		del    bool
	}

	tests := []struct {
		name   string
		user   accesstypes.User
		target string
		want   map[string]wantCapability
	}{
		{
			// Marshal's Update grant is unconditional (pure RBAC): the same positive
			// list on every row, delete live everywhere.
			name:   "marshal's unconditional update lists every field on every row",
			user:   "marshal",
			target: sectorPath(anvil, "missions?capabilities=Update,Delete"),
			want: map[string]wantCapability{
				missionHaulerID: {update: []any{"clientId", "kindId", "title", "brief", "hazard", "fee", "deadline", "requiredCertId", "assignedSquadronId", "notes"}, del: true},
				missionPodID:    {update: []any{"clientId", "kindId", "title", "brief", "hazard", "fee", "deadline", "requiredCertId", "assignedSquadronId", "notes"}, del: true},
			},
		},
		{
			// Booking Agent's fee update is limited to open missions; the delete too.
			name:   "booking's conditional update and delete follow each row's state",
			user:   "booking",
			target: sectorPath(anvil, "missions?capabilities=Update,Delete"),
			want: map[string]wantCapability{
				missionHaulerID:  {update: []any{"fee"}, del: true}, // open
				missionCorvidID:  {update: []any{}, del: false},     // claimed
				missionBullionID: {update: []any{}, del: false},     // stood_down
			},
		},
		{
			// Dispatcher's two grants: the assignment group's new. term counts
			// potentially-true per term while the state term narrows; the notes/deadline
			// group is live on every non-terminal row.
			name:   "dispatcher's two update groups answer per row",
			user:   "dispatcher",
			target: sectorPath(anvil, "missions?capabilities=Update"),
			want: map[string]wantCapability{
				missionHaulerID: {update: []any{"deadline", "assignedSquadronId", "notes"}}, // open, projection order
				missionConvoyID: {update: []any{"deadline", "notes"}},                       // underway: assignment closed
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status, body := doRequestAs(t, h, tt.user, http.MethodGet, tt.target, "")
			assertStatus(t, status, http.StatusOK, body)

			seen := make(map[string]bool, len(tt.want))
			for _, row := range decodeRows(t, body) {
				id, _ := row["id"].(string)
				want, pinned := tt.want[id]
				if !pinned {
					continue
				}
				seen[id] = true
				caps, _ := row["zzCapabilities"].(map[string]any)
				if caps == nil {
					t.Errorf("row %s carries no zzCapabilities property", id)

					continue
				}
				if got := caps["Update"]; !reflect.DeepEqual(got, want.update) {
					t.Errorf("row %s Update = %v, want %v", id, got, want.update)
				}
				if del, asked := caps["Delete"]; asked && del != want.del {
					t.Errorf("row %s Delete = %v, want %v", id, del, want.del)
				}
			}
			for id := range tt.want {
				if !seen[id] {
					t.Errorf("row %s missing from the response", id)
				}
			}
		})
	}

	// The create-under-parent affordance (F10): each row lists the workflow member
	// resources the user may create beneath it, the member grant's state residue
	// evaluated against the parent row's own uniform state binding.
	createTests := []struct {
		name   string
		user   accesstypes.User
		target string
		want   map[string][]any
	}{
		{
			name:   "lead's add-sortie affordance follows the mission's state",
			user:   "lead",
			target: sectorPath(anvil, "missions?capabilities=Create"),
			want: map[string][]any{
				missionConvoyID: {"Sorties"}, // underway
				missionCorvidID: {},          // claimed
				missionPodID:    {},          // completed
			},
		},
		{
			name:   "quartermaster's add-expense affordance is answered at the immediate parent",
			user:   "quartermaster",
			target: sectorPath(anvil, "sorties?capabilities=Create"),
			want: map[string][]any{
				sortieConvoyID:  {"SortieExpenses"}, // the convoy is underway
				sortieCourierID: {},                 // on hold
				sortiePodID:     {},                 // completed
			},
		},
		{
			name:   "engineer's add-task affordance follows the refit's bay",
			user:   "engineer",
			target: sectorPath(anvil, "refits?capabilities=Create"),
			want: map[string][]any{
				refitLanternID:   {},             // docked
				refitMuleID:      {"RefitTasks"}, // inspected
				refitSamaritanID: {"RefitTasks"}, // in_refit
			},
		},
	}
	for _, tt := range createTests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status, body := doRequestAs(t, h, tt.user, http.MethodGet, tt.target, "")
			assertStatus(t, status, http.StatusOK, body)

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
}
