package integration

// impersonation_test (design plan §9, P8): the two impersonation moments through the
// SERVED stack — session cookies, XSRF, the hand-written mint route — over the real
// engine. View as: mint as Cass with a List, Read mask and assert the flight deck's
// Execute list is empty, a PATCH is refused, and the digest carries no write
// permission. Act as a role: mint as role Dispatcher and assert the change event's
// source names the actor and the role, a deadline extension succeeds while a squadron
// assignment is refused because subject resolved to Greer, not to a dispatcher. Minting
// from an impersonated session is refused.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/cccteam/access"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource/lodestar/app"
	"github.com/cccteam/ccc/resource/lodestar/pkg/config"
	"github.com/cccteam/ccc/resource/lodestar/pkg/router"
	initiator "github.com/cccteam/db-initiator"
	"github.com/cccteam/session"
)

// servedStack stands the full router up over the test database: the same
// PasswordAuth the demo runs (impersonation table attached), the real engine, and the
// production router composition.
func servedStack(t *testing.T, db *initiator.SpannerDB, controller access.Controller) *httptest.Server {
	t.Helper()

	cookieKey, err := config.EphemeralCookieKey()
	if err != nil {
		t.Fatal(err)
	}
	passwordAuth, err := config.NewPasswordAuth(db.Client, cookieKey)
	if err != nil {
		t.Fatal(err)
	}
	for _, user := range []string{"governor", "cadet", "marshal", "dispatcher"} {
		password := "lodestar"
		if _, err := passwordAuth.API().CreateSessionUser(context.Background(), &session.CreateUserRequest{Username: user, Password: &password}); err != nil {
			t.Fatalf("CreateSessionUser(%s): %v", user, err)
		}
	}

	a := app.New(&testConfigurer{
		db:            db,
		access:        controller,
		session:       passwordAuth,
		domainVisible: domainVisibleVia(controller),
	})
	srv := httptest.NewServer(router.New(a))
	t.Cleanup(srv.Close)

	return srv
}

// browser is one persona's cookie-carrying client with the XSRF handshake.
type browser struct {
	t      *testing.T
	client *http.Client
	base   string
}

func newBrowser(t *testing.T, srv *httptest.Server) *browser {
	t.Helper()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}

	return &browser{t: t, client: &http.Client{Jar: jar}, base: srv.URL}
}

// xsrf reads the XSRF token cookie the session middleware set on the last response.
func (b *browser) xsrf() string {
	for _, c := range b.client.Jar.Cookies(mustURL(b.t, b.base)) {
		if c.Name == "XSRF-TOKEN" {
			return c.Value
		}
	}

	return ""
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()

	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}

	return u
}

func (b *browser) do(method, path, body string) (statusCode int, respBody []byte) {
	b.t.Helper()

	req, err := http.NewRequestWithContext(b.t.Context(), method, b.base+path, strings.NewReader(body))
	if err != nil {
		b.t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-XSRF-TOKEN", b.xsrf())
	resp, err := b.client.Do(req)
	if err != nil {
		b.t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)

	return resp.StatusCode, out
}

func (b *browser) login(user string) {
	b.t.Helper()

	// The handshake: the first request answers with the XSRF token cookie.
	b.do(http.MethodGet, "/api/user/session", "")
	status, body := b.do(http.MethodPost, "/api/user/login", fmt.Sprintf(`{"username":%q,"password":"lodestar"}`, user))
	if status != http.StatusOK {
		b.t.Fatalf("login %s: status = %d: %s", user, status, body)
	}
}

func (b *browser) session() map[string]any {
	b.t.Helper()

	status, body := b.do(http.MethodGet, "/api/user/session", "")
	if status != http.StatusOK {
		b.t.Fatalf("session: status = %d: %s", status, body)
	}
	var info map[string]any
	if err := json.Unmarshal(body, &info); err != nil {
		b.t.Fatalf("decoding session: %v", err)
	}

	return info
}

func TestImpersonatedSessions(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}
	controller := newDemoAccessClient(ctx, t, db)
	srv := servedStack(t, db, controller)

	t.Run("view as: the marshal sees Cass's board read-only", func(t *testing.T) {
		t.Parallel()

		maren := newBrowser(t, srv)
		maren.login("marshal")

		status, body := maren.do(http.MethodPost, "/api/impersonate", `{"kind":"user","principal":"cadet","reason":"walkthrough"}`)
		assertStatus(t, status, http.StatusOK, body)

		// The console is now Cass's console...
		info := maren.session()
		if info["username"] != "cadet" {
			t.Errorf("session username = %v, want cadet", info["username"])
		}
		imp, _ := info["impersonation"].(map[string]any)
		if imp["actor"] != "marshal" || imp["principal"] != "cadet" {
			t.Errorf("impersonation record = %v, want actor marshal, principal cadet", imp)
		}

		// ...the same two-hazard board...
		status, body = maren.do(http.MethodGet, sectorPath(anvil, "missions?capabilities=Execute"), "")
		assertStatus(t, status, http.StatusOK, body)
		rows := decodeRows(t, body)
		if len(rows) != 4 {
			t.Errorf("rows = %d, want Cass's 4", len(rows))
		}
		// ...with every edge unlit, because the mask strips Execute before policy.
		for _, row := range rows {
			caps, _ := row["zzCapabilities"].(map[string]any)
			if list, _ := caps["Execute"].([]any); len(list) != 0 {
				t.Errorf("row %v Execute = %v, want empty under the mask", row["id"], list)
			}
		}

		// The digest carries no write permission.
		status, body = maren.do(http.MethodGet, "/api/permission-digest?domain="+anvil, "")
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
		status, body = maren.do(http.MethodPost, sectorPath(anvil, "claim-mission"), fmt.Sprintf(`{"missionId":%q,"squadronId":%q}`, missionHaulerID, squadronTongsID))
		assertStatus(t, status, http.StatusForbidden, body)

		// Chaining is refused by the library.
		status, body = maren.do(http.MethodPost, "/api/impersonate", `{"kind":"user","principal":"pilot"}`)
		if status == http.StatusOK {
			t.Errorf("minting from an impersonated session succeeded: %s", body)
		}
	})

	t.Run("act as a role: the governor works as Dispatcher, subject stays Greer", func(t *testing.T) {
		t.Parallel()

		greer := newBrowser(t, srv)
		greer.login("governor")

		status, body := greer.do(http.MethodPost, "/api/impersonate", `{"kind":"role","principal":"Dispatcher","reason":"walkthrough"}`)
		assertStatus(t, status, http.StatusOK, body)

		info := greer.session()
		if info["username"] != "governor" {
			t.Errorf("session username = %v, want the actor governor", info["username"])
		}

		// The board is the dispatcher's board: the terminal missions are gone.
		status, body = greer.do(http.MethodGet, sectorPath(anvil, "missions"), "")
		assertStatus(t, status, http.StatusOK, body)
		if rows := decodeRows(t, body); len(rows) != 5 {
			t.Errorf("rows = %d, want the dispatcher's 5", len(rows))
		}

		// A deadline extension lands (grant B)...
		status, body = greer.do(http.MethodPatch, "/api/resources",
			fmt.Sprintf(`[{"op":"patch","path":%q,"value":{"deadline":"2026-10-02T08:00:00Z"}}]`, opPath(anvil, "missions/"+missionConvoyID)))
		assertStatus(t, status, http.StatusOK, body)
		// ...and the change event names the actor and the role.
		source, _ := latestChangeEvent(t, db, "Missions", missionConvoyID)
		if !strings.Contains(source, "governor as role Dispatcher") {
			t.Errorf("eventSource = %q, want 'governor as role Dispatcher'", source)
		}

		// The identity proof: grant A reads subject.squadrons against Greer's own
		// memberships (none), so the assignment is refused.
		status, body = greer.do(http.MethodPatch, "/api/resources",
			fmt.Sprintf(`[{"op":"patch","path":%q,"value":{"assignedSquadronId":%q}}]`, opPath(anvil, "missions/"+missionHaulerID), squadronHammerID))
		assertStatus(t, status, http.StatusForbidden, body)

		// The role session's digest is the role's.
		status, body = greer.do(http.MethodGet, "/api/permission-digest?domain="+anvil, "")
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

	t.Run("the gates are the manual Execute registrations", func(t *testing.T) {
		t.Parallel()

		cass := newBrowser(t, srv)
		cass.login("cadet")
		status, body := cass.do(http.MethodPost, "/api/impersonate", `{"kind":"user","principal":"marshal"}`)
		assertStatus(t, status, http.StatusForbidden, body)
		status, body = cass.do(http.MethodPost, "/api/impersonate", `{"kind":"role","principal":"SectorMarshal"}`)
		assertStatus(t, status, http.StatusForbidden, body)
	})
}
