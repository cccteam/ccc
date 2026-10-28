package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	cloudspanner "cloud.google.com/go/spanner"
	"github.com/cccteam/session/sessioninfo"
)

// TestDeclaration_whoMayEnterATenant proves the Tenant record's declaration end to end
// on both sites' served stacks, which share the auth and the roster. The staff sign in
// by password, so the declared directories admit nobody yet: a directory declared on
// north leaves the member's password session where it was. North admitting its people
// through its directories only refuses the member's password session at reach time on
// the console and on the portal alike, at once, since the console's write reloads the
// shared roster; a fresh password sign-in is refused naming the rule, since every
// tenant of the member is directory only; the administrator, a global role, keeps every
// tenant. Every edit leaves a change event.
func TestDeclaration_whoMayEnterATenant(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	s := newServed(ctx, t)
	admin := signedIn(ctx, t, s, s.console, adminUser)
	member := signedIn(ctx, t, s, s.console, memberUser)
	portalMember := signedIn(ctx, t, s, s.portal, memberUser)

	// A directory declared on north admits the people who come through it; the member,
	// who came by password, keeps north as before.
	declareDirectory := `[{"op":"add","path":"/tenants/` + north + `/tenant-directories","value":{"method":"google","connection":"north.example"}}]`
	status, body := admin.do(ctx, http.MethodPatch, "/api/resources", []byte(declareDirectory))
	if status != http.StatusOK {
		t.Fatalf("declaring a directory on north: status %d: %s", status, body)
	}
	if domains := chart(ctx, t, member); !slices.Equal(domains, []string{north}) {
		t.Fatalf("the member's chart with north declaring a directory = %v, want [%s]: a password is admitted", domains, north)
	}
	if directories := directories(ctx, t, admin, north); len(directories) != 1 || directories[0].Connection != "north.example" {
		t.Errorf("north's directories = %+v, want the one declared", directories)
	}
	// The same declaration twice is refused as a conflict: one row per directory.
	if status, body := admin.do(ctx, http.MethodPatch, "/api/resources", []byte(declareDirectory)); status != http.StatusConflict {
		t.Errorf("declaring the same directory twice: status %d, want 409: %s", status, body)
	}

	// North admits its people through its directories only: the member loses north on
	// both sites at once, and a fresh password sign-in is refused naming the rule. The
	// administrator keeps every tenant.
	closeNorth := `[{"op":"patch","path":"/tenants/` + north + `","value":{"directoryOnly":true}}]`
	if status, body := admin.do(ctx, http.MethodPatch, "/api/resources", []byte(closeNorth)); status != http.StatusOK {
		t.Fatalf("setting north directory only: status %d: %s", status, body)
	}
	for _, site := range []struct {
		name    string
		browser *browser
	}{{"console", member}, {"portal", portalMember}} {
		if domains := chart(ctx, t, site.browser); len(domains) != 0 {
			t.Errorf("the member's chart on the %s once north is directory only = %v, want none", site.name, domains)
		}
		if status, body := site.browser.do(ctx, http.MethodGet, "/api/tenants/"+north+"/announcements", nil); status != http.StatusNotFound {
			t.Errorf("GET north announcements on the %s as the member once north is directory only: status %d, want 404: %s", site.name, status, body)
		}
	}
	if domains := chart(ctx, t, admin); !slices.Equal(domains, []string{north, south}) {
		t.Errorf("the administrator's chart = %v, want both: a global role reaches every tenant", domains)
	}
	fresh := newBrowser(t, s, s.console)
	if status, body := fresh.do(ctx, http.MethodGet, "/api/user/session", nil); status != http.StatusOK {
		t.Fatalf("GET session before the refused login: status %d: %s", status, body)
	}
	status, body = fresh.login(ctx, memberUser, adminPassword)
	if status != http.StatusUnauthorized {
		t.Fatalf("the member's password sign-in with every tenant directory only: status %d, want 401: %s", status, body)
	}
	var refusal struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(body, &refusal); err != nil || refusal.Code != string(sessioninfo.RefusedDirectoryOnly) {
		t.Errorf("the refusal's code = %q (err %v), want %q: %s", refusal.Code, err, sessioninfo.RefusedDirectoryOnly, body)
	}

	// Back to passwords: the member signs in again, on the portal too.
	openNorth := `[{"op":"patch","path":"/tenants/` + north + `","value":{"directoryOnly":false}}]`
	if status, body := admin.do(ctx, http.MethodPatch, "/api/resources", []byte(openNorth)); status != http.StatusOK {
		t.Fatalf("setting north back to passwords: status %d: %s", status, body)
	}
	if status, body := fresh.login(ctx, memberUser, adminPassword); status != http.StatusOK {
		t.Errorf("the member's password sign-in once north admits passwords again: status %d, want 200: %s", status, body)
	}
	if domains := chart(ctx, t, portalMember); !slices.Equal(domains, []string{north}) {
		t.Errorf("the member's chart on the portal once north admits passwords again = %v, want [%s]", domains, north)
	}

	// Every edit is audited: the records' change events carry the declaration's writes.
	events := changeEvents(ctx, t, s, "Tenants", "TenantDirectories")
	if events["Tenants"] < 2 || events["TenantDirectories"] < 1 {
		t.Errorf("change events = %v, want the two flag writes on Tenants and the directory on TenantDirectories", events)
	}
}

// signedIn returns a browser signed in on the site as the development login, through the
// password and the second factor every session carries (browser.signIn).
func signedIn(ctx context.Context, t *testing.T, s *served, site *httptest.Server, user string) *browser {
	t.Helper()

	b := newBrowser(t, s, site)
	b.signIn(ctx, user)

	return b
}

// chart reads the session's tenant list.
func chart(ctx context.Context, t *testing.T, b *browser) []string {
	t.Helper()

	status, body := b.do(ctx, http.MethodGet, "/api/user-domains", nil)
	if status != http.StatusOK {
		t.Fatalf("GET /api/user-domains: status %d: %s", status, body)
	}
	var domains []string
	if err := json.Unmarshal(body, &domains); err != nil {
		t.Fatalf("user-domains is not a list: %v: %s", err, body)
	}

	return domains
}

// tenantDirectory is a row of the directories record as the console lists it.
type tenantDirectory struct {
	ID         string `json:"id"`
	Method     string `json:"method"`
	Connection string `json:"connection"`
}

// directories lists the tenant's declared directories on the console.
func directories(ctx context.Context, t *testing.T, b *browser, tenant string) []tenantDirectory {
	t.Helper()

	status, body := b.do(ctx, http.MethodGet, "/api/tenants/"+tenant+"/tenant-directories", nil)
	if status != http.StatusOK {
		t.Fatalf("GET %s's directories: status %d: %s", tenant, status, body)
	}
	var rows []tenantDirectory
	if err := json.Unmarshal(body, &rows); err != nil {
		t.Fatalf("the directories are not a list: %v: %s", err, body)
	}

	return rows
}

// changeEvents counts the change events written for each table.
func changeEvents(ctx context.Context, t *testing.T, s *served, tables ...string) map[string]int64 {
	t.Helper()

	counts := map[string]int64{}
	for _, table := range tables {
		iter := s.db.Client.Single().Query(ctx, cloudspanner.Statement{
			SQL:    "SELECT COUNT(*) FROM DataChangeEvents WHERE TableName = @table",
			Params: map[string]any{"table": table},
		})
		row, err := iter.Next()
		if err != nil {
			t.Fatalf("counting change events of %s: %v", table, err)
		}
		var n int64
		if err := row.Columns(&n); err != nil {
			t.Fatalf("reading the count of %s: %v", table, err)
		}
		iter.Stop()
		counts[table] = n
	}

	return counts
}
