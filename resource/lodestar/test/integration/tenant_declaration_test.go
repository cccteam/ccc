//go:build skipAuth

package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	cloudspanner "cloud.google.com/go/spanner"
	"github.com/cccteam/ccc"
	"github.com/cccteam/session/sessioninfo"
)

// TestTenantDeclaration_whoMayEnterASector proves the Sector record's declaration end to
// end on the served stack. The seed declares the fleet's directory (google, example) on
// every sector and makes Bastion admit its people through its directories only. The
// rigger, by password, holding SectorCrew in Anvil and Bastion and no global role,
// charts Anvil alone and Bastion answers her as a sector that does not exist; the bosun,
// whose only sector is Bastion, is refused at sign-in naming the rule; the navigator,
// through the fleet's directory in a role held in every sector and no global one, charts
// all three, Bastion included. The governor withdraws Anvil's directory and the
// navigator's next request stands outside Anvil; declared again, he is back; the same
// declaration twice is a conflict. Headquarters opens Bastion to passwords and the
// rigger's chart lights it and the bosun signs in. Every edit leaves a change event. The
// directory sign-in sets a process variable, so the test does not run in parallel with
// the other directory sign-ins (simulatedLogin).
//
// Demonstrates: tenancy.declaration, tenancy.directory-only.
func TestTenantDeclaration_whoMayEnterASector(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	s := newServed(ctx, t)

	governor := newBrowser(t, s, consoleAPI)
	governor.signIn(ctx, "governor")

	// The rigger: a password session, admitted since Anvil admits passwords; Bastion,
	// directory only, is not hers to enter.
	rigger := newBrowser(t, s, consoleAPI)
	rigger.signIn(ctx, "rigger")
	if got, want := rigger.domains(ctx), []string{anvil}; !slices.Equal(got, want) {
		t.Errorf("the rigger's chart = %v, want %v: Bastion admits its people through its directory only", got, want)
	}
	if status, body := rigger.do(ctx, http.MethodGet, sectorPath(bastion, "wings"), nil); status != http.StatusNotFound {
		t.Errorf("GET Bastion's wings as the rigger: status %d, want 404: %s", status, body)
	}
	if status, body := rigger.do(ctx, http.MethodGet, sectorPath(anvil, "wings"), nil); status != http.StatusOK {
		t.Errorf("GET Anvil's wings as the rigger: status %d, want 200: %s", status, body)
	}

	// The bosun: every sector of hers is directory only, so the password is refused.
	bosun := newBrowser(t, s, consoleAPI)
	if status, body := bosun.do(ctx, http.MethodGet, consoleAPI+"/user/session", nil); status != http.StatusOK {
		t.Fatalf("GET session before the bosun's login: status %d: %s", status, body)
	}
	status, body := bosun.login(ctx, "bosun", personaPassword)
	if status != http.StatusUnauthorized {
		t.Fatalf("the bosun's password sign-in: status %d, want 401: %s", status, body)
	}
	var refusal struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(body, &refusal); err != nil || refusal.Code != string(sessioninfo.RefusedDirectoryOnly) {
		t.Errorf("the bosun's refusal code = %q (err %v), want %q: %s", refusal.Code, err, sessioninfo.RefusedDirectoryOnly, body)
	}

	// The navigator: through the fleet's directory, which every sector declares, in a
	// role held in every sector and no global one. The crew's roles are the
	// application's, so the first sign-in provisions him holding nothing, and the
	// governor seats him as Pilot in the global partition, a domain role held in every
	// sector.
	navigator := newBrowser(t, s, consoleAPI)
	if status, body := navigator.do(ctx, http.MethodGet, consoleAPI+"/user/session", nil); status != http.StatusOK {
		t.Fatalf("GET session before the navigator's login: status %d: %s", status, body)
	}
	if status, location := navigator.loginConsoleGoogle(ctx, navigatorUser); status != http.StatusFound || location != consolePage {
		t.Fatalf("the navigator's Google login: status %d to %q, want %d to %q", status, location, http.StatusFound, consolePage)
	}
	if got := navigator.domains(ctx); len(got) != 0 {
		t.Fatalf("the navigator's chart before any seat = %v, want none", got)
	}
	seat := seatBody(crewUserID(ctx, t, governor, navigatorUser), "Pilot")
	if status, body := governor.do(ctx, http.MethodPatch, consoleAPI+"/"+crewPartitions().Global.SeatsRoute, []byte(seat)); status != http.StatusOK {
		t.Fatalf("seating the navigator as Pilot: status %d: %s", status, body)
	}
	if got, want := navigator.domains(ctx), []string{anvil, bastion, cinder}; !slices.Equal(got, want) {
		t.Errorf("the navigator's chart = %v, want %v: every sector declares the fleet's directory", got, want)
	}

	// The governor withdraws Anvil's directory: the navigator's next request stands
	// outside Anvil, and it answers him as a sector that does not exist.
	declared := directories(ctx, t, governor, anvil)
	if len(declared) != 1 || declared[0].Method != string(sessioninfo.MethodGoogle) || declared[0].Connection != "example" {
		t.Fatalf("Anvil's directories = %+v, want the seeded fleet directory alone", declared)
	}
	withdraw := `[{"op":"remove","path":"/sectors/` + anvil + `/sector-directories/` + declared[0].ID + `"}]`
	if status, body := governor.do(ctx, http.MethodPatch, consoleAPI+"/resources", []byte(withdraw)); status != http.StatusOK {
		t.Fatalf("withdrawing Anvil's directory: status %d: %s", status, body)
	}
	if got, want := navigator.domains(ctx), []string{bastion, cinder}; !slices.Equal(got, want) {
		t.Errorf("the navigator's chart once Anvil's directory is withdrawn = %v, want %v", got, want)
	}
	if status, body := navigator.do(ctx, http.MethodGet, sectorPath(anvil, "wings"), nil); status != http.StatusNotFound {
		t.Errorf("GET Anvil's wings as the navigator once the directory is withdrawn: status %d, want 404: %s", status, body)
	}
	// Declared again, he is back; a second identical declaration is a conflict.
	declare := `[{"op":"add","path":"/sectors/` + anvil + `/sector-directories","value":{"method":"google","connection":"example"}}]`
	if status, body := governor.do(ctx, http.MethodPatch, consoleAPI+"/resources", []byte(declare)); status != http.StatusOK {
		t.Fatalf("declaring Anvil's directory again: status %d: %s", status, body)
	}
	if got, want := navigator.domains(ctx), []string{anvil, bastion, cinder}; !slices.Equal(got, want) {
		t.Errorf("the navigator's chart once Anvil's directory is declared again = %v, want %v", got, want)
	}
	if status, body := governor.do(ctx, http.MethodPatch, consoleAPI+"/resources", []byte(declare)); status != http.StatusConflict {
		t.Errorf("declaring the same directory twice: status %d, want 409: %s", status, body)
	}

	// Headquarters opens Bastion to passwords: the rigger's chart lights it and the
	// bosun signs in.
	openBastion := `[{"op":"patch","path":"/sectors/` + bastion + `","value":{"directoryOnly":false}}]`
	if status, body := governor.do(ctx, http.MethodPatch, consoleAPI+"/resources", []byte(openBastion)); status != http.StatusOK {
		t.Fatalf("opening Bastion to passwords: status %d: %s", status, body)
	}
	if got, want := rigger.domains(ctx), []string{anvil, bastion}; !slices.Equal(got, want) {
		t.Errorf("the rigger's chart once Bastion admits passwords = %v, want %v", got, want)
	}
	if status, body := bosun.login(ctx, "bosun", personaPassword); status != http.StatusOK {
		t.Errorf("the bosun's password sign-in once Bastion admits passwords: status %d, want 200: %s", status, body)
	}

	// Every edit is audited: the records' change events carry the declaration's writes.
	events := changeEvents(ctx, t, s, "Sectors", "SectorDirectories")
	if events["Sectors"] < 1 || events["SectorDirectories"] < 2 {
		t.Errorf("change events = %v, want the flag write on Sectors and the withdrawal and declaration on SectorDirectories", events)
	}
}

// sectorDirectory is a row of the directories record as the console lists it.
type sectorDirectory struct {
	ID         string `json:"id"`
	Method     string `json:"method"`
	Connection string `json:"connection"`
}

// directories lists the sector's declared directories on the console.
func directories(ctx context.Context, t *testing.T, b *browser, sector string) []sectorDirectory {
	t.Helper()

	status, body := b.do(ctx, http.MethodGet, sectorPath(sector, "sector-directories"), nil)
	if status != http.StatusOK {
		t.Fatalf("GET %s's directories: status %d: %s", sector, status, body)
	}
	var rows []sectorDirectory
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

// crewUserID finds a crew member's id by username through the console's global users
// route, as the browser b (one who may list the crew) reads it.
func crewUserID(ctx context.Context, t *testing.T, b *browser, username string) ccc.UUID {
	t.Helper()

	status, body := b.do(ctx, http.MethodGet, consoleAPI+"/"+crewPartitions().Global.UsersRoute, nil)
	if status != http.StatusOK {
		t.Fatalf("GET the crew's users: status %d: %s", status, body)
	}
	var rows []struct {
		ID       ccc.UUID `json:"id"`
		Username string   `json:"username"`
	}
	if err := json.Unmarshal(body, &rows); err != nil {
		t.Fatalf("the crew's users are not a list: %v: %s", err, body)
	}
	for _, row := range rows {
		if row.Username == username {
			return row.ID
		}
	}
	t.Fatalf("no crew member %q among %d rows", username, len(rows))

	return ccc.UUID{}
}
