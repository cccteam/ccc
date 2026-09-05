package integration

// Bootstrap parity for the permission digest endpoint: the shipped personas fetch
// their structural grant enumeration through the real engine — the service card's
// source. The digest is advisory and non-folding, so these pins are pure functions of
// the shipped role config.

import (
	"encoding/json"
	"maps"
	"net/http"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
)

func TestPermissionDigest(t *testing.T) {
	t.Parallel()

	_, h, _ := sharedWorld(t)

	tests := []struct {
		name        string
		user        accesstypes.User
		target      string
		wantEntries accesstypes.PermissionDigest
		absentKeys  []accesstypes.Resource
		wantEmpty   bool
	}{
		{
			name:   "the cadet's Anvil digest carries the tri-state per grant",
			user:   "cadet",
			target: "/api/permission-digest?domain=" + anvil,
			wantEntries: accesstypes.PermissionDigest{
				"Missions":              {"List": accesstypes.DigestConditional, "Read": accesstypes.DigestConditional},
				"Missions.hazard":       {"List": accesstypes.DigestConditional, "Read": accesstypes.DigestConditional},
				"DistressCalls":         {"Create": accesstypes.DigestGranted, "List": accesstypes.DigestConditional, "Read": accesstypes.DigestConditional},
				"DistressCalls.summary": {"Create": accesstypes.DigestGranted, "List": accesstypes.DigestConditional, "Read": accesstypes.DigestConditional},
				"ClaimMission":          {"Execute": accesstypes.DigestConditional},
				"Wings":                 {"List": accesstypes.DigestGranted, "Read": accesstypes.DigestGranted},
			},
			// The partial-width Create grant enumerates exactly its fields: the PII
			// contact and transcript are absent, so the call-log form renders two inputs.
			absentKeys: []accesstypes.Resource{"DistressCalls.callerContact", "DistressCalls.transcript", "Refits", "Sectors", "Missions.settlement"},
		},
		{
			name:   "the dispatcher's two update groups both read conditional",
			user:   "dispatcher",
			target: "/api/permission-digest?domain=" + anvil,
			wantEntries: accesstypes.PermissionDigest{
				"Missions.assignedSquadronId": {"List": accesstypes.DigestConditional, "Read": accesstypes.DigestConditional, "Update": accesstypes.DigestConditional},
				"Missions.notes":              {"List": accesstypes.DigestConditional, "Read": accesstypes.DigestConditional, "Update": accesstypes.DigestConditional},
			},
		},
		{
			name:   "the dockmaster's windowed grant reads conditional, with a clock",
			user:   "dock",
			target: "/api/permission-digest?domain=" + anvil,
			wantEntries: accesstypes.PermissionDigest{
				"Refits": {"List": accesstypes.DigestConditional, "Read": accesstypes.DigestConditional},
			},
		},
		{
			name:   "the marshal's global digest is the global roles' structure",
			user:   "marshal",
			target: "/api/permission-digest",
			wantEntries: accesstypes.PermissionDigest{
				"Sectors":       {"List": accesstypes.DigestGranted, "Read": accesstypes.DigestGranted},
				"IssueBulletin": {"Execute": accesstypes.DigestConditional},
				"ViewAsUser":    {"Execute": accesstypes.DigestGranted},
				"AssumeRole":    {"Execute": accesstypes.DigestGranted},
				"Clients":       {"List": accesstypes.DigestConditional},
			},
			absentKeys: []accesstypes.Resource{"Missions", "Refits"},
		},
		{
			name:      "a sector without a foothold digests to nothing",
			user:      "cadet",
			target:    "/api/permission-digest?domain=" + cinder,
			wantEmpty: true,
		},
		{
			name:      "an unknown sector digests to nothing, indistinguishably",
			user:      "cadet",
			target:    "/api/permission-digest?domain=nowhere",
			wantEmpty: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status, body := doRequestAs(t, h, tt.user, http.MethodGet, tt.target, "")
			assertStatus(t, status, http.StatusOK, body)

			var got accesstypes.PermissionDigest
			if err := json.Unmarshal(body, &got); err != nil {
				t.Fatalf("unmarshaling digest %s: %v", body, err)
			}
			if tt.wantEmpty && len(got) != 0 {
				t.Errorf("digest = %v, want empty", got)
			}
			for res, want := range tt.wantEntries {
				if !maps.Equal(want, got[res]) {
					t.Errorf("digest[%s] = %v, want %v", res, got[res], want)
				}
			}
			for _, res := range tt.absentKeys {
				if _, ok := got[res]; ok {
					t.Errorf("digest carries %s = %v, want absent (denied is absence)", res, got[res])
				}
			}
		})
	}
}
