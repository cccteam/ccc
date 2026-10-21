//go:build skipAuth

package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	cloudspanner "cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/session/sessioninfo"
)

// TestDeclaration_whoMayEnterATenant proves the Tenant record's declaration end to end
// on the served stack: a tenant admits a session established through a directory only
// when it declares that directory, and a tenant whose people enter through its
// directories only refuses a password session at reach time and, when every tenant of
// the person is such, at sign-in. A declaration written through the API decides the
// next request, since the records' write paths reload the roster after their commit;
// the administrator, who holds a global role, keeps every tenant; and every edit leaves
// a change event. Under the session library's skipAuth build tag Google is simulated:
// the callback signs in whoever APP_GOOGLE_USERNAME names, as a Workspace account whose
// connection is the simulated Workspace's domain. The test sets that variable, so it
// does not run in parallel.
func TestDeclaration_whoMayEnterATenant(t *testing.T) {
	ctx := t.Context()
	s := newServed(ctx, t)
	admin := signedIn(ctx, t, s, adminUser)

	// A person who signed in through Google, seated in south: south declares nothing,
	// which admits a password and no directory, so their chart is empty.
	t.Setenv("APP_GOOGLE_USERNAME", "traveler")
	traveler := signedInThroughGoogle(ctx, t, s)
	seat(ctx, t, s, "traveler", south)
	if domains := userDomains(ctx, t, traveler); len(domains) != 0 {
		t.Fatalf("the traveler's chart before south declares a directory = %v, want none", domains)
	}
	if status, body := traveler.do(ctx, http.MethodGet, "/api/tenants/"+south+"/announcements", nil); status != http.StatusNotFound {
		t.Fatalf("GET south announcements as the traveler before the declaration: status %d, want 404: %s", status, body)
	}

	// The administrator declares the directory the traveler came through on south (the
	// method and connection the identities table recorded), through the consolidated
	// write route like every write, and the traveler's next request stands in south.
	method, connection := directoryOf(ctx, t, s, "traveler")
	declare := `[{"op":"add","path":"/tenants/` + south + `/tenant-directories","value":{"method":"` + method + `","connection":"` + connection + `"}}]`
	status, body := admin.do(ctx, http.MethodPatch, "/api/resources", []byte(declare))
	if status != http.StatusOK {
		t.Fatalf("declaring %s/%s on south: status %d: %s", method, connection, status, body)
	}
	var created struct {
		TenantDirectories []string `json:"tenantDirectories"`
	}
	if err := json.Unmarshal(body, &created); err != nil || len(created.TenantDirectories) != 1 {
		t.Fatalf("the declaration's answer = %s (err %v), want the new row's id", body, err)
	}
	if domains := userDomains(ctx, t, traveler); !slices.Equal(domains, []string{south}) {
		t.Errorf("the traveler's chart after south declares the directory = %v, want [%s]", domains, south)
	}
	if status, body := traveler.do(ctx, http.MethodGet, "/api/tenants/"+south+"/announcements", nil); status != http.StatusOK {
		t.Errorf("GET south announcements as the traveler after the declaration: status %d, want 200: %s", status, body)
	}
	// The same declaration twice is refused as a conflict: one row per directory.
	if status, body := admin.do(ctx, http.MethodPatch, "/api/resources", []byte(declare)); status != http.StatusConflict {
		t.Errorf("declaring the same directory twice: status %d, want 409: %s", status, body)
	}
	// Withdrawn, the directory admits nobody: the traveler's chart is empty again.
	withdraw := `[{"op":"remove","path":"/tenants/` + south + `/tenant-directories/` + created.TenantDirectories[0] + `"}]`
	if status, body := admin.do(ctx, http.MethodPatch, "/api/resources", []byte(withdraw)); status != http.StatusOK {
		t.Fatalf("withdrawing the directory: status %d: %s", status, body)
	}
	if domains := userDomains(ctx, t, traveler); len(domains) != 0 {
		t.Errorf("the traveler's chart once the directory is withdrawn = %v, want none", domains)
	}
	if status, body := admin.do(ctx, http.MethodPatch, "/api/resources", []byte(declare)); status != http.StatusOK {
		t.Fatalf("declaring the directory again: status %d: %s", status, body)
	}
	if domains := userDomains(ctx, t, traveler); !slices.Equal(domains, []string{south}) {
		t.Errorf("the traveler's chart once the directory is declared again = %v, want [%s]", domains, south)
	}

	// North admits its people through its directories only: the member, signed in by
	// password and seated in north alone, loses north at once (not listed, answered as a
	// tenant that does not exist), and a fresh password sign-in is refused naming the
	// rule, since every tenant of the member is directory only. The administrator, a
	// global role, keeps north.
	member := signedIn(ctx, t, s, memberUser)
	if domains := userDomains(ctx, t, member); !slices.Equal(domains, []string{north}) {
		t.Fatalf("the member's chart before = %v, want [%s]", domains, north)
	}
	closeNorth := `[{"op":"patch","path":"/tenants/` + north + `","value":{"directoryOnly":true}}]`
	if status, body := admin.do(ctx, http.MethodPatch, "/api/resources", []byte(closeNorth)); status != http.StatusOK {
		t.Fatalf("setting north directory only: status %d: %s", status, body)
	}
	if domains := userDomains(ctx, t, member); len(domains) != 0 {
		t.Errorf("the member's chart once north is directory only = %v, want none", domains)
	}
	if status, body := member.do(ctx, http.MethodGet, "/api/tenants/"+north+"/announcements", nil); status != http.StatusNotFound {
		t.Errorf("GET north announcements as the member once north is directory only: status %d, want 404: %s", status, body)
	}
	if domains := userDomains(ctx, t, admin); !slices.Equal(domains, []string{north, south}) {
		t.Errorf("the administrator's chart = %v, want both: a global role reaches every tenant", domains)
	}
	fresh := newBrowser(t, s)
	if status, body := fresh.do(ctx, http.MethodGet, "/api/user/session", nil); status != http.StatusOK {
		t.Fatalf("GET /api/user/session before the refused login: status %d: %s", status, body)
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

	// Back to passwords: the member signs in again.
	openNorth := `[{"op":"patch","path":"/tenants/` + north + `","value":{"directoryOnly":false}}]`
	if status, body := admin.do(ctx, http.MethodPatch, "/api/resources", []byte(openNorth)); status != http.StatusOK {
		t.Fatalf("setting north back to passwords: status %d: %s", status, body)
	}
	if status, body := fresh.login(ctx, memberUser, adminPassword); status != http.StatusOK {
		t.Errorf("the member's password sign-in once north admits passwords again: status %d, want 200: %s", status, body)
	}

	// Every edit is audited: the records' change events carry the declaration's writes.
	events := changeEvents(ctx, t, s, "Tenants", "TenantDirectories")
	if events["Tenants"] < 2 || events["TenantDirectories"] < 3 {
		t.Errorf("change events = %v, want the two flag writes on Tenants and the directory's three writes on TenantDirectories", events)
	}
}

// signedInThroughGoogle returns a browser signed in through the staff auth's google
// registration, simulated: the login route sends the browser to the callback, which
// signs in whoever APP_GOOGLE_USERNAME names.
func signedInThroughGoogle(ctx context.Context, t *testing.T, s *served) *browser {
	t.Helper()

	b := newBrowser(t, s)
	status, location := b.redirect(ctx, "/api/user/google/login?returnUrl="+url.QueryEscape("/dashboard"))
	if status != http.StatusFound {
		t.Fatalf("GET /api/user/google/login: status %d, want %d to the directory", status, http.StatusFound)
	}
	if status, location = b.redirect(ctx, location); status != http.StatusFound || !strings.HasSuffix(location, "/dashboard") {
		t.Fatalf("the callback answered %d to %q, want %d to /dashboard", status, location, http.StatusFound)
	}

	return b
}

// seat gives the user the tenant's administrator role in the tenant and waits for the
// engine to answer it.
func seat(ctx context.Context, t *testing.T, s *served, user accesstypes.User, tenant accesstypes.Domain) {
	t.Helper()

	if err := s.access.UserManager().AddUserRoles(ctx, accesstypes.DomainPolicyScope(tenant), user, "Administrator_Domain"); err != nil {
		t.Fatalf("AddUserRoles(%s, %s) error = %v", user, tenant, err)
	}
	waitForDomains(ctx, t, s.access, user, []accesstypes.Domain{tenant})
}

// directoryOf reads the directory the user's identity came through, as the identities
// table recorded it: the method and the connection a tenant declares.
func directoryOf(ctx context.Context, t *testing.T, s *served, user string) (method, connection string) {
	t.Helper()

	iter := s.db.Client.Single().Query(ctx, cloudspanner.Statement{
		SQL:    "SELECT i.Method, i.Connection FROM StaffSessionIdentities i JOIN StaffSessionUsers u ON u.Id = i.UserId WHERE u.Username = @user",
		Params: map[string]any{"user": user},
	})
	defer iter.Stop()
	row, err := iter.Next()
	if err != nil {
		t.Fatalf("reading %s's identity: %v", user, err)
	}
	if err := row.Columns(&method, &connection); err != nil {
		t.Fatalf("reading %s's identity columns: %v", user, err)
	}

	return method, connection
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
