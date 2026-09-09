package integration

// impersonation_test (design plan §9, P9): the two impersonation moments through the
// SERVED stack (session cookies, XSRF, the hand-written mint route) over the real
// engines. View as: mint as Cass with a List, Read mask and assert the flight deck's
// Execute list is empty, a PATCH is refused, and the digest carries no write
// permission. Act as a role: mint as role Dispatcher and assert the change event's
// source names the actor and the role, a deadline extension succeeds while a squadron
// assignment is refused because subject resolved to Greer, not to a dispatcher. Minting
// from an impersonated session is refused.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
)

// The suite runs through the SERVED stack (newServed): the crew auth with the
// impersonation record attached, the real engines provisioned from the shipped roles,
// and the production router composition.

// impersonate posts the mint route's body as this browser.
func (b *browser) impersonate(ctx context.Context, body string) (status int, respBody []byte) {
	b.t.Helper()

	return b.do(ctx, http.MethodPost, "/api/impersonate", []byte(body))
}

// Demonstrates: impersonation.view-as, impersonation.act-as-role, impersonation.mask, impersonation.identity-proof, impersonation.end, event-source, @manualAddResource.execute, impersonation.session-permissions.
func TestImpersonatedSessions(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	s := newServed(ctx, t)
	db := s.db

	t.Run("view as: the marshal sees Cass's board read-only", func(t *testing.T) {
		t.Parallel()

		maren := newBrowser(t, s, consoleAPI)
		maren.signIn(ctx, "marshal")

		status, body := maren.impersonate(ctx, `{"kind":"user","principal":"cadet","reason":"walkthrough"}`)
		assertStatus(t, status, http.StatusOK, body)

		// The console is now Cass's console...
		info := maren.session(ctx)
		if info["username"] != "cadet" {
			t.Errorf("session username = %v, want cadet", info["username"])
		}
		imp, _ := info["impersonation"].(map[string]any)
		if imp["actor"] != "marshal" || imp["principal"] != "cadet" {
			t.Errorf("impersonation record = %v, want actor marshal, principal cadet", imp)
		}

		// ...the same two-hazard board...
		status, body = maren.do(ctx, http.MethodGet, sectorPath(anvil, "missions?capabilities=Execute&limit=200"), nil)
		assertStatus(t, status, http.StatusOK, body)
		rows := decodeRows(t, body)
		if len(rows) != 14 {
			t.Errorf("rows = %d, want Cass's 14 (hazard 1 and 2 in Anvil)", len(rows))
		}
		// ...with every edge unlit, because the mask strips Execute before policy.
		for _, row := range rows {
			caps, _ := row["zzCapabilities"].(map[string]any)
			if list, _ := caps["Execute"].([]any); len(list) != 0 {
				t.Errorf("row %v Execute = %v, want empty under the mask", row["id"], list)
			}
		}

		// The digest carries no write permission.
		status, body = maren.do(ctx, http.MethodGet, "/api/permission-digest?domain="+anvil, nil)
		assertStatus(t, status, http.StatusOK, body)
		var digest accesstypes.PermissionDigest
		if err := json.Unmarshal(body, &digest); err != nil {
			t.Fatal(err)
		}
		for res, perms := range digest {
			for perm := range perms {
				if perm != accesstypes.List && perm != accesstypes.Read {
					t.Errorf("digest[%s] carries %s under a List, Read mask", res, perm)
				}
			}
		}

		// A write is refused by the mask.
		status, body = maren.do(ctx, http.MethodPost, sectorPath(anvil, "claim-mission"), fmt.Appendf(nil, `{"missionId":%q,"squadronId":%q}`, missionHaulerID, squadronTongsID))
		assertStatus(t, status, http.StatusForbidden, body)

		// Chaining is refused by the library.
		status, body = maren.impersonate(ctx, `{"kind":"user","principal":"pilot"}`)
		if status == http.StatusOK {
			t.Errorf("minting from an impersonated session succeeded: %s", body)
		}
	})

	t.Run("act as a role: the governor works as Dispatcher, subject stays Greer", func(t *testing.T) {
		t.Parallel()

		greer := newBrowser(t, s, consoleAPI)
		greer.signIn(ctx, "governor")

		status, body := greer.impersonate(ctx, `{"kind":"role","principal":"Dispatcher","reason":"walkthrough"}`)
		assertStatus(t, status, http.StatusOK, body)

		info := greer.session(ctx)
		if info["username"] != "governor" {
			t.Errorf("session username = %v, want the actor governor", info["username"])
		}

		// The board is the dispatcher's board: the terminal missions are gone.
		status, body = greer.do(ctx, http.MethodGet, sectorPath(anvil, "missions?limit=200"), nil)
		assertStatus(t, status, http.StatusOK, body)
		if rows := decodeRows(t, body); len(rows) != 23 {
			t.Errorf("rows = %d, want the dispatcher's 23 (every Anvil mission not finished)", len(rows))
		}

		// A deadline extension lands (grant B)...
		status, body = greer.do(ctx, http.MethodPatch, "/api/resources",
			fmt.Appendf(nil, `[{"op":"patch","path":%q,"value":{"deadline":"2026-10-02T08:00:00Z"}}]`, opPath(anvil, "missions/"+missionConvoyID)))
		assertStatus(t, status, http.StatusOK, body)
		// ...and the change event names the actor and the role.
		source, _ := latestChangeEvent(t, db, "Missions", missionConvoyID)
		if !strings.Contains(source, "governor as role Dispatcher") {
			t.Errorf("eventSource = %q, want 'governor as role Dispatcher'", source)
		}

		// The identity proof: grant A reads subject.squadrons against Greer's own
		// memberships (none), so the assignment is refused.
		status, body = greer.do(ctx, http.MethodPatch, "/api/resources",
			fmt.Appendf(nil, `[{"op":"patch","path":%q,"value":{"assignedSquadronId":%q}}]`, opPath(anvil, "missions/"+missionHaulerID), squadronHammerID))
		assertStatus(t, status, http.StatusForbidden, body)

		// The role session's digest is the role's.
		status, body = greer.do(ctx, http.MethodGet, "/api/permission-digest?domain="+anvil, nil)
		assertStatus(t, status, http.StatusOK, body)
		var digest accesstypes.PermissionDigest
		if err := json.Unmarshal(body, &digest); err != nil {
			t.Fatal(err)
		}
		if _, ok := digest["ScrapShip"]; ok {
			t.Error("digest carries ScrapShip: the role session must not inherit the governor's own grants")
		}
		if got := digest["Missions"]["Update"]; got != accesstypes.DigestConditional {
			t.Errorf("digest[Missions][Update] = %q, want the dispatcher's conditional", got)
		}
	})

	t.Run("return to self: the marshal ends the view and is Maren again", func(t *testing.T) {
		t.Parallel()

		maren := newBrowser(t, s, consoleAPI)
		maren.signIn(ctx, "marshal")
		status, body := maren.impersonate(ctx, `{"kind":"user","principal":"cadet","reason":"walkthrough"}`)
		assertStatus(t, status, http.StatusOK, body)
		if info := maren.session(ctx); info["username"] != "cadet" {
			t.Fatalf("session username = %v, want cadet before ending", info["username"])
		}

		// Ending the minted session hands the browser the actor's own session back:
		// a local actor whose source session is still live is restored, not logged out.
		status, body = maren.do(ctx, http.MethodPost, "/api/impersonate/end", nil)
		assertStatus(t, status, http.StatusOK, body)
		var ended struct {
			Restored bool `json:"restored"`
		}
		if err := json.Unmarshal(body, &ended); err != nil {
			t.Fatal(err)
		}
		if !ended.Restored {
			t.Fatalf("restored = false, want the marshal's own session back: %s", body)
		}

		info := maren.session(ctx)
		if info["username"] != "marshal" {
			t.Errorf("session username = %v, want marshal", info["username"])
		}
		if info["impersonation"] != nil {
			t.Errorf("impersonation record = %v, want none after ending", info["impersonation"])
		}

		// The mask is gone with the minted session: the marshal's own digest carries
		// the write permissions the view withheld.
		status, body = maren.do(ctx, http.MethodGet, "/api/permission-digest?domain="+anvil, nil)
		assertStatus(t, status, http.StatusOK, body)
		var digest accesstypes.PermissionDigest
		if err := json.Unmarshal(body, &digest); err != nil {
			t.Fatal(err)
		}
		if _, ok := digest["Missions"][accesstypes.Update]; !ok {
			t.Errorf("digest[Missions] = %v, want Update back for the marshal", digest["Missions"])
		}

		// Ending from a session that is not impersonated has nothing to end.
		status, body = maren.do(ctx, http.MethodPost, "/api/impersonate/end", nil)
		if status == http.StatusOK {
			t.Errorf("ending an ordinary session succeeded: %s", body)
		}
	})

	t.Run("the gates are the manual Execute registrations", func(t *testing.T) {
		t.Parallel()

		cass := newBrowser(t, s, consoleAPI)
		cass.signIn(ctx, "cadet")
		status, body := cass.impersonate(ctx, `{"kind":"user","principal":"marshal"}`)
		assertStatus(t, status, http.StatusForbidden, body)
		status, body = cass.impersonate(ctx, `{"kind":"role","principal":"SectorMarshal"}`)
		assertStatus(t, status, http.StatusForbidden, body)
	})
}
