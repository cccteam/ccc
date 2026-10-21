//go:build skipAuth

package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	cloudspanner "cloud.google.com/go/spanner"
	"github.com/cccteam/session/sessioninfo"
)

// TestDeclaration_whoMayEnterATenant proves the Tenant record's declaration end to end
// on the served stack, over both auths. The portal's member, who came through the
// members auth's directory, reaches a tenant only while the tenant declares that
// directory (the development seed declares it on both), and a fresh directory sign-in
// is refused naming the rule when none of their tenants does; a tenant whose people
// enter through its directories only refuses a console password session at reach time
// and, when every tenant of the person is such, at sign-in, while the directory session
// keeps the tenant; the administrator, who holds a global role, keeps every tenant. A
// declaration written through the API decides the next request, since the records' write
// paths reload the roster after their commit, and every edit leaves a change event. The
// portal's simulated sign-in sets a process variable, so the test does not run in
// parallel.
func TestDeclaration_whoMayEnterATenant(t *testing.T) {
	ctx := t.Context()
	s := newServed(ctx, t)

	admin := newBrowser(t, s, consoleAPI)
	if status, body := admin.do(ctx, http.MethodGet, consoleAPI+"/user/session", nil); status != http.StatusOK {
		t.Fatalf("GET console session before login: status %d: %s", status, body)
	}
	if status, body := admin.login(ctx, adminUser, adminPassword); status != http.StatusOK {
		t.Fatalf("the administrator's login: status %d: %s", status, body)
	}

	// The member reaches north through the seeded declaration of the directory.
	client := newBrowser(t, s, portalAPI)
	if status, body := client.do(ctx, http.MethodGet, portalAPI+"/user/session", nil); status != http.StatusOK {
		t.Fatalf("GET portal session before login: status %d: %s", status, body)
	}
	if status, location := client.loginPortal(ctx, clientUser); status != http.StatusFound || location != portalPage {
		t.Fatalf("the member's portal login: status %d to %q, want %d to %q", status, location, http.StatusFound, portalPage)
	}
	if domains := chart(ctx, t, client, portalAPI); !slices.Equal(domains, []string{north}) {
		t.Fatalf("the member's chart with north declaring the directory = %v, want [%s]", domains, north)
	}

	// The administrator withdraws north's directory on the console: the member's next
	// request stands in no tenant, and a fresh sign-in through the directory is refused,
	// since no tenant of theirs declares it.
	declared := directories(ctx, t, admin, north)
	if len(declared) != 1 || declared[0].Method != string(sessioninfo.MethodAzure) {
		t.Fatalf("north's directories = %+v, want the seeded azure directory alone", declared)
	}
	withdraw := `[{"op":"remove","path":"/tenants/` + north + `/tenant-directories/` + declared[0].ID + `"}]`
	if status, body := admin.do(ctx, http.MethodPatch, consoleAPI+"/resources", []byte(withdraw)); status != http.StatusOK {
		t.Fatalf("withdrawing north's directory: status %d: %s", status, body)
	}
	if domains := chart(ctx, t, client, portalAPI); len(domains) != 0 {
		t.Errorf("the member's chart once north's directory is withdrawn = %v, want none", domains)
	}
	if status, body := client.do(ctx, http.MethodGet, portalAPI+"/tenants/"+north+"/announcements", nil); status != http.StatusNotFound {
		t.Errorf("GET north announcements as the member once the directory is withdrawn: status %d, want 404: %s", status, body)
	}
	refused := newBrowser(t, s, portalAPI)
	if status, body := refused.do(ctx, http.MethodGet, portalAPI+"/user/session", nil); status != http.StatusOK {
		t.Fatalf("GET portal session before the refused login: status %d: %s", status, body)
	}
	if status, location := refused.loginPortal(ctx, clientUser); status != http.StatusFound || !strings.Contains(location, "code="+string(sessioninfo.RefusedConnectionNotInTenant)) {
		t.Errorf("a directory sign-in with no tenant declaring the directory: status %d to %q, want %d to the login page naming %s", status, location, http.StatusFound, sessioninfo.RefusedConnectionNotInTenant)
	}

	// Declared again, the member's next request stands in north.
	declare := `[{"op":"add","path":"/tenants/` + north + `/tenant-directories","value":{"method":"` + declared[0].Method + `","connection":"` + declared[0].Connection + `"}}]`
	if status, body := admin.do(ctx, http.MethodPatch, consoleAPI+"/resources", []byte(declare)); status != http.StatusOK {
		t.Fatalf("declaring north's directory again: status %d: %s", status, body)
	}
	if domains := chart(ctx, t, client, portalAPI); !slices.Equal(domains, []string{north}) {
		t.Errorf("the member's chart once the directory is declared again = %v, want [%s]", domains, north)
	}

	// North admits its people through its directories only: the staff member, signed in
	// by password and seated in north alone, loses north at once (not listed, answered as
	// a tenant that does not exist), and a fresh password sign-in is refused naming the
	// rule, since every tenant of theirs is directory only. The administrator, a global
	// role, keeps every tenant, and the portal's member, who came through the directory,
	// keeps north.
	member := newBrowser(t, s, consoleAPI)
	if status, body := member.do(ctx, http.MethodGet, consoleAPI+"/user/session", nil); status != http.StatusOK {
		t.Fatalf("GET console session before the member's login: status %d: %s", status, body)
	}
	if status, body := member.login(ctx, memberUser, adminPassword); status != http.StatusOK {
		t.Fatalf("the staff member's login: status %d: %s", status, body)
	}
	if domains := chart(ctx, t, member, consoleAPI); !slices.Equal(domains, []string{north}) {
		t.Fatalf("the staff member's chart before = %v, want [%s]", domains, north)
	}
	closeNorth := `[{"op":"patch","path":"/tenants/` + north + `","value":{"directoryOnly":true}}]`
	if status, body := admin.do(ctx, http.MethodPatch, consoleAPI+"/resources", []byte(closeNorth)); status != http.StatusOK {
		t.Fatalf("setting north directory only: status %d: %s", status, body)
	}
	if domains := chart(ctx, t, member, consoleAPI); len(domains) != 0 {
		t.Errorf("the staff member's chart once north is directory only = %v, want none", domains)
	}
	if status, body := member.do(ctx, http.MethodGet, consoleAPI+"/tenants/"+north+"/announcements", nil); status != http.StatusNotFound {
		t.Errorf("GET north announcements as the staff member once north is directory only: status %d, want 404: %s", status, body)
	}
	if domains := chart(ctx, t, admin, consoleAPI); !slices.Equal(domains, []string{north, south}) {
		t.Errorf("the administrator's chart = %v, want both: a global role reaches every tenant", domains)
	}
	if domains := chart(ctx, t, client, portalAPI); !slices.Equal(domains, []string{north}) {
		t.Errorf("the portal member's chart once north is directory only = %v, want [%s]: the directory session is admitted", domains, north)
	}
	fresh := newBrowser(t, s, consoleAPI)
	if status, body := fresh.do(ctx, http.MethodGet, consoleAPI+"/user/session", nil); status != http.StatusOK {
		t.Fatalf("GET console session before the refused login: status %d: %s", status, body)
	}
	status, body := fresh.login(ctx, memberUser, adminPassword)
	if status != http.StatusUnauthorized {
		t.Fatalf("the staff member's password sign-in with every tenant directory only: status %d, want 401: %s", status, body)
	}
	var refusal struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(body, &refusal); err != nil || refusal.Code != string(sessioninfo.RefusedDirectoryOnly) {
		t.Errorf("the refusal's code = %q (err %v), want %q: %s", refusal.Code, err, sessioninfo.RefusedDirectoryOnly, body)
	}

	// Back to passwords: the staff member signs in again.
	openNorth := `[{"op":"patch","path":"/tenants/` + north + `","value":{"directoryOnly":false}}]`
	if status, body := admin.do(ctx, http.MethodPatch, consoleAPI+"/resources", []byte(openNorth)); status != http.StatusOK {
		t.Fatalf("setting north back to passwords: status %d: %s", status, body)
	}
	if status, body := fresh.login(ctx, memberUser, adminPassword); status != http.StatusOK {
		t.Errorf("the staff member's password sign-in once north admits passwords again: status %d, want 200: %s", status, body)
	}

	// Every edit is audited: the records' change events carry the declaration's writes.
	events := changeEvents(ctx, t, s, "Tenants", "TenantDirectories")
	if events["Tenants"] < 2 || events["TenantDirectories"] < 2 {
		t.Errorf("change events = %v, want the two flag writes on Tenants and the withdrawal and declaration on TenantDirectories", events)
	}
}

// chart reads the session's tenant list under the outlet's prefix.
func chart(ctx context.Context, t *testing.T, b *browser, prefix string) []string {
	t.Helper()

	status, body := b.do(ctx, http.MethodGet, prefix+"/user-domains", nil)
	if status != http.StatusOK {
		t.Fatalf("GET %s/user-domains: status %d: %s", prefix, status, body)
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

	status, body := b.do(ctx, http.MethodGet, consoleAPI+"/tenants/"+tenant+"/tenant-directories", nil)
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
