package integration

// subject_anchors_test (design plan §9): the global certification set is unfiltered
// across sectors; the squadron set is partitioned per sector; wings resolve via the
// dotted path; both subject values on one anchor decide.

import (
	"fmt"
	"net/http"
	"slices"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
)

func TestSubjectAnchors(t *testing.T) {
	t.Parallel()

	_, h, _ := sharedWorld(t)

	tests := []struct {
		name    string
		user    accesstypes.User
		target  string
		wantIDs []string
	}{
		{
			// pilot's salvage certification, earned once, admits the Bastion patrol?
			// No: hazard 4 exceeds clearance 3. The beacon repair (hazard 2, no cert)
			// is in; the global set is unfiltered but the value threshold still bites.
			name:    "global certification set travels to Bastion, clearance still applies",
			user:    "pilot",
			target:  sectorPath(bastion, "missions"),
			wantIDs: []string{missionBeaconID},
		},
		{
			// dispatcher flies with Hammer and Tongs at Anvil and Portcullis at
			// Bastion: subject.squadrons is partitioned per sector, so at Bastion the
			// patrol (Portcullis) is assignable and Anvil's squadrons are not.
			name:    "squadron set is partitioned per sector",
			user:    "lead",
			target:  sectorPath(bastion, "missions"),
			wantIDs: nil, // lead holds no role at Bastion: concealed
		},
		{
			// wingco flies with Hammer, so subject.wings = {Forge}: both Forge
			// squadrons and none of Bastion's.
			name:    "wings via the dotted path",
			user:    "wingco",
			target:  sectorPath(anvil, "squadrons"),
			wantIDs: []string{squadronHammerID, squadronTongsID},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status, body := doRequestAs(t, h, tt.user, http.MethodGet, tt.target, "")
			if tt.wantIDs == nil {
				assertStatus(t, status, http.StatusNotFound, body)

				return
			}
			assertStatus(t, status, http.StatusOK, body)
			want := slices.Clone(tt.wantIDs)
			slices.Sort(want)
			if got := idsOf(t, decodeRows(t, body)); !slices.Equal(got, want) {
				t.Errorf("rows = %v, want %v", got, want)
			}
		})
	}
}

// TestSubjectAnchorWrites is the mutating half.
func TestSubjectAnchorWrites(t *testing.T) {
	t.Parallel()

	_, _, h := demoWorld(t)

	t.Run("the squadron set partitions the dispatcher's assignments per sector", func(t *testing.T) {
		t.Parallel()

		// At Bastion, Dunn flies with Portcullis: assigning the open beacon repair to
		// Portcullis passes grant A; assigning it to Hammer (an Anvil squadron Dunn
		// also flies with) fails, because Hammer is not in Bastion's partition of
		// subject.squadrons.
		status, body := doRequestAs(t, h, "dispatcher", http.MethodPatch, "/api/resources",
			fmt.Sprintf(`[{"op":"patch","path":%q,"value":{"assignedSquadronId":%q}}]`, opPath(bastion, "missions/"+missionBeaconID), squadronHammerID))
		assertStatus(t, status, http.StatusForbidden, body)
		status, body = doRequestAs(t, h, "dispatcher", http.MethodPatch, "/api/resources",
			fmt.Sprintf(`[{"op":"patch","path":%q,"value":{"assignedSquadronId":%q}}]`, opPath(bastion, "missions/"+missionBeaconID), squadronPortcullisID))
		assertStatus(t, status, http.StatusOK, body)
	})

	t.Run("both subject values on the Pilot anchor decide", func(t *testing.T) {
		t.Parallel()

		// booking's feeLimit (25000) and clearance (0) live on one row: the clearance
		// keeps Bex off the Pilot's board (Bex holds no Pilot role anyway), while the
		// fee limit admits a 25000 booking and refuses 25001.
		status, body := doRequestAs(t, h, "booking", http.MethodPatch, "/api/resources",
			fmt.Sprintf(`[{"op":"add","path":%q,"value":{"clientId":%q,"kindId":"escort","title":"At the limit","hazard":2,"fee":25000,"deadline":"2027-02-01T00:00:00Z"}}]`, opPath(anvil, "missions"), clientMeridianID))
		assertStatus(t, status, http.StatusOK, body)
		status, body = doRequestAs(t, h, "booking", http.MethodPatch, "/api/resources",
			fmt.Sprintf(`[{"op":"add","path":%q,"value":{"clientId":%q,"kindId":"escort","title":"Over the limit","hazard":2,"fee":25001,"deadline":"2027-02-01T00:00:00Z"}}]`, opPath(anvil, "missions"), clientMeridianID))
		assertStatus(t, status, http.StatusForbidden, body)
	})
}
