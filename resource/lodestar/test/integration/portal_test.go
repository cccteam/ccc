package integration

// portal_test (design plan §9): a client session under /portal lists only its
// company's missions and its own filed calls; a refit, ship, or squadron route under
// /portal 404s as a non-member; StandDown fires on an open own-company mission and
// refuses on another company's; the portal digest names four resources and no
// others; the portal's user-domains lists the sectors where the ClientPortal role is
// held.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
)

func TestClientPortal(t *testing.T) {
	t.Parallel()

	_, h, _ := sharedWorld(t)

	t.Run("cleo sees only Halvard's missions, with the portal width", func(t *testing.T) {
		t.Parallel()

		status, body := doRequestAs(t, h, "client", http.MethodGet, "/portal/sectors/"+anvil+"/missions", "")
		assertStatus(t, status, http.StatusOK, body)
		rows := decodeRows(t, body)
		want := []string{missionHaulerID, missionPodID, missionTowID, missionQuarantineID}
		slices.Sort(want)
		if got := idsOf(t, rows); !slices.Equal(got, want) {
			t.Errorf("rows = %v, want Halvard's %v", got, want)
		}
		for _, row := range rows {
			for _, hidden := range []string{"assignedSquadronId", "notes", "settlement", "bookedBy"} {
				if _, ok := row[hidden]; ok {
					t.Errorf("portal row %v carries %s, outside the portal grant's width", row["id"], hidden)
				}
			}
			for _, shown := range []string{"title", "kindId", "statusId", "hazard", "fee", "deadline"} {
				if _, ok := row[shown]; !ok {
					t.Errorf("portal row %v lacks %s", row["id"], shown)
				}
			}
		}
	})

	t.Run("cleo reads back the calls she filed and nobody else's", func(t *testing.T) {
		t.Parallel()

		status, body := doRequestAs(t, h, "client", http.MethodGet, "/portal/sectors/"+anvil+"/distress-calls", "")
		assertStatus(t, status, http.StatusOK, body)
		if got := idsOf(t, decodeRows(t, body)); !slices.Equal(got, []string{callBeaconID}) {
			t.Errorf("calls = %v, want only the beacon call Cleo filed", got)
		}
	})

	t.Run("non-members 404 under the portal prefix", func(t *testing.T) {
		t.Parallel()

		for _, target := range []string{
			"/portal/sectors/" + anvil + "/refits",
			"/portal/sectors/" + anvil + "/ships",
			"/portal/sectors/" + anvil + "/squadrons",
			"/portal/sectors/" + anvil + "/sorties",
			"/portal/pilots",
		} {
			status, body := doRequestAs(t, h, "client", http.MethodGet, target, "")
			if status != http.StatusNotFound && status != http.StatusMethodNotAllowed {
				t.Errorf("%s: status = %d, want 404/405: %s", target, status, body)
			}
		}
	})

	t.Run("the portal digest names the portal's members", func(t *testing.T) {
		t.Parallel()

		status, body := doRequestAs(t, h, "client", http.MethodGet, "/portal/permission-digest?domain="+anvil, "")
		assertStatus(t, status, http.StatusOK, body)
		var digest accesstypes.PermissionDigest
		if err := json.Unmarshal(body, &digest); err != nil {
			t.Fatalf("decoding digest: %v", err)
		}
		if got := digest["Missions"]["List"]; got != accesstypes.DigestConditional {
			t.Errorf("digest[Missions][List] = %q, want conditional (client = subject.client)", got)
		}
		if got := digest["DistressCalls"]["Create"]; got != accesstypes.DigestGranted {
			t.Errorf("digest[DistressCalls][Create] = %q, want granted", got)
		}
		if got := digest["StandDownMission"]["Execute"]; got != accesstypes.DigestConditional {
			t.Errorf("digest[StandDownMission][Execute] = %q, want conditional", got)
		}
		for _, absent := range []accesstypes.Resource{"Refits", "Ships", "Squadrons", "Sorties", "ClaimMission"} {
			if _, ok := digest[absent]; ok {
				t.Errorf("digest carries %s; the portal role holds nothing on it", absent)
			}
		}

		status, body = doRequestAs(t, h, "client", http.MethodGet, "/portal/permission-digest", "")
		assertStatus(t, status, http.StatusOK, body)
		if err := json.Unmarshal(body, &digest); err != nil {
			t.Fatalf("decoding global digest: %v", err)
		}
		if got := digest["ClientContacts"]["Read"]; got != accesstypes.DigestConditional {
			t.Errorf("digest[ClientContacts][Read] = %q, want conditional (userId = subject)", got)
		}
	})

	t.Run("the portal's user-domains lists where the portal role is held", func(t *testing.T) {
		t.Parallel()

		status, body := doRequestAs(t, h, "client", http.MethodGet, "/portal/user-domains", "")
		assertStatus(t, status, http.StatusOK, body)
		var domains []accesstypes.Domain
		if err := json.Unmarshal(body, &domains); err != nil {
			t.Fatalf("decoding domains: %v", err)
		}
		if !slices.Equal(domains, []accesstypes.Domain{anvil}) {
			t.Errorf("domains = %v, want [anvil]", domains)
		}
	})

	t.Run("cleo reads her own contact record and no other", func(t *testing.T) {
		t.Parallel()

		status, body := doRequestAs(t, h, "client", http.MethodGet, "/portal/client-contacts", "")
		assertStatus(t, status, http.StatusOK, body)
		rows := decodeRows(t, body)
		if len(rows) != 1 || rows[0]["displayName"] != "Client Cleo" {
			t.Errorf("contacts = %v, want Cleo's own record", rows)
		}
	})
}

// TestClientPortalActions is the mutating half: filing a call and standing down.
func TestClientPortalActions(t *testing.T) {
	t.Parallel()

	_, _, h := demoWorld(t)

	t.Run("cleo files a call with her contact — the one PII field a client writes", func(t *testing.T) {
		t.Parallel()

		status, body := doRequestAs(t, h, "client", http.MethodPatch, "/portal/resources",
			fmt.Sprintf(`[{"op":"add","path":%q,"value":{"summary":"Hauler drifting off the lane","severity":4,"callerContact":"cleo@halvard.example"}}]`, opPath(anvil, "distress-calls")))
		assertStatus(t, status, http.StatusOK, body)
		ids, _ := decodeRow(t, body)["distressCalls"].([]any)
		if len(ids) != 1 {
			t.Fatalf("created ids = %v, want one call id: %s", ids, body)
		}
		status, body = doRequestAs(t, h, "client", http.MethodGet, fmt.Sprintf("/portal/sectors/%s/distress-calls/%s", anvil, ids[0]), "")
		assertStatus(t, status, http.StatusOK, body)
		row := decodeRow(t, body)
		if row["callerContact"] != "cleo@halvard.example" {
			t.Errorf("callerContact = %v, want the contact Cleo wrote", row["callerContact"])
		}
		if cn, _ := row["caseNumber"].(string); len(cn) < 4 || cn[:3] != "DC-" {
			t.Errorf("caseNumber = %v, want a server-issued DC- number", row["caseNumber"])
		}
	})

	t.Run("stand down fires on an own-company open mission and refuses another company's", func(t *testing.T) {
		t.Parallel()

		status, body := doRequestAs(t, h, "client", http.MethodPost, "/portal/sectors/"+anvil+"/stand-down-mission",
			fmt.Sprintf(`{"missionId":%q}`, missionConvoyID)) // Meridian's
		assertStatus(t, status, http.StatusForbidden, body)
		status, body = doRequestAs(t, h, "client", http.MethodPost, "/portal/sectors/"+anvil+"/stand-down-mission",
			fmt.Sprintf(`{"missionId":%q}`, missionHaulerID)) // Halvard's, open
		assertStatus(t, status, http.StatusOK, body)
		// The same method under the console prefix is a foreign surface for Cleo's
		// role too — the grant is the same, the outlet is not the guard.
		status, body = doRequestAs(t, h, "client", http.MethodPost, sectorPath(anvil, "stand-down-mission"),
			fmt.Sprintf(`{"missionId":%q}`, missionQuarantineID))
		assertStatus(t, status, http.StatusOK, body)
	})
}
