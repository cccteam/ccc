package integration

// impersonation_ops_test (design plan §9): the watch desk lists a minted session with its
// two-hour cap, revocation refuses its next request, a forged write on a read-only
// session is refused by the middleware before any handler runs, and the desk itself is
// gated by the manual Execute registration.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// watchDeskEntry is one live impersonated session as the watch desk lists it.
type watchDeskEntry struct {
	SessionID string    `json:"sessionId"`
	Actor     string    `json:"actor"`
	Principal string    `json:"principal"`
	Kind      string    `json:"kind"`
	StartedAt time.Time `json:"startedAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// Demonstrates: impersonation.active-list, impersonation.revoke, impersonation.max-duration, impersonation.read-only-backstop.
func TestImpersonationOperator(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	s := newServed(ctx, t)

	// The marshal views as Cass: the minted session replaces the marshal's cookie.
	maren := newBrowser(t, s, consoleAPI)
	maren.signIn(ctx, "marshal")
	status, body := maren.impersonate(ctx, `{"kind":"user","principal":"cadet","reason":"watch desk walkthrough"}`)
	assertStatus(t, status, http.StatusOK, body)
	var minted struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(body, &minted); err != nil {
		t.Fatal(err)
	}

	// The governor runs the watch desk from a session of their own.
	greer := newBrowser(t, s, consoleAPI)
	greer.signIn(ctx, "governor")

	t.Run("the desk lists the live session with its actor, principal, and two-hour cap", func(t *testing.T) {
		status, body := greer.do(ctx, http.MethodGet, "/api/impersonations", nil)
		assertStatus(t, status, http.StatusOK, body)
		var entries []watchDeskEntry
		if err := json.Unmarshal(body, &entries); err != nil {
			t.Fatal(err)
		}
		var found *watchDeskEntry
		for i := range entries {
			if entries[i].SessionID == minted.SessionID {
				found = &entries[i]
			}
		}
		if found == nil {
			t.Fatalf("watch desk = %v, want the session %s the marshal minted", entries, minted.SessionID)
		}
		if found.Actor != "marshal" || found.Principal != "cadet" || found.Kind != "user" {
			t.Errorf("entry = %+v, want marshal viewing as cadet", found)
		}
		if lifetime := found.ExpiresAt.Sub(found.StartedAt); lifetime != 2*time.Hour {
			t.Errorf("lifetime = %v, want the two-hour cap the mint route sets", lifetime)
		}
	})

	t.Run("a forged write on the read-only session is stopped by the middleware", func(t *testing.T) {
		status, body := maren.do(ctx, http.MethodPost, sectorPath(anvil, "claim-mission"), fmt.Appendf(nil, `{"missionId":%q,"squadronId":%q}`, missionHaulerID, squadronTongsID))
		assertStatus(t, status, http.StatusForbidden, body)
		if want := "this session is read-only"; !strings.Contains(string(body), want) {
			t.Errorf("refusal = %s, want the backstop's %q", body, want)
		}
		// The read-only session still reads.
		status, body = maren.do(ctx, http.MethodGet, sectorPath(anvil, "missions"), nil)
		assertStatus(t, status, http.StatusOK, body)
	})

	t.Run("the cadet may not operate the desk", func(t *testing.T) {
		cass := newBrowser(t, s, consoleAPI)
		cass.signIn(ctx, "cadet")
		status, body := cass.do(ctx, http.MethodGet, "/api/impersonations", nil)
		assertStatus(t, status, http.StatusForbidden, body)
		status, body = cass.do(ctx, http.MethodDelete, "/api/impersonations/"+minted.SessionID, nil)
		assertStatus(t, status, http.StatusForbidden, body)
	})

	t.Run("revocation refuses the revoked console's next request", func(t *testing.T) {
		status, body := greer.do(ctx, http.MethodDelete, "/api/impersonations/"+minted.SessionID, nil)
		assertStatus(t, status, http.StatusNoContent, body)

		status, body = maren.do(ctx, http.MethodGet, sectorPath(anvil, "missions"), nil)
		assertStatus(t, status, http.StatusUnauthorized, body)

		status, body = greer.do(ctx, http.MethodGet, "/api/impersonations", nil)
		assertStatus(t, status, http.StatusOK, body)
		var entries []watchDeskEntry
		if err := json.Unmarshal(body, &entries); err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if entry.SessionID == minted.SessionID {
				t.Errorf("the revoked session %s is still listed", minted.SessionID)
			}
		}
	})
}
