package resource

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/session/sessioninfo"
	"github.com/cccteam/session/signin"
	"github.com/google/go-cmp/cmp"
)

// invite adds a standing invitation to the fixture's account store, as an earlier
// invite would have.
func (f *userFixture) invite(t *testing.T, email, tenant string, roles ...string) ccc.UUID {
	t.Helper()
	inv, err := f.accounts.Invite(t.Context(), &UserInvitationRequest{Email: email, Tenant: tenant, Roles: roles, InvitedBy: ccc.NullUUID{UUID: f.user(t, "admin").ID, Valid: true}})
	if err != nil {
		t.Fatal(err)
	}

	return inv.ID
}

// TestUserSurface_invitations pins the invitations of both partitions: each lists its
// own, a create judges the grant's condition once per role and records the caller, and
// a revoke withdraws one of the partition's.
func TestUserSurface_invitations(t *testing.T) {
	t.Parallel()

	listGrant := func(p *scriptedPermissions) {
		p.grant(accesstypes.List, "CrewInvitations", "CrewInvitations.email", "CrewInvitations.roles", "CrewInvitations.invitedBy").
			grant(accesstypes.List, "CrewSectorInvitations", "CrewSectorInvitations.email", "CrewSectorInvitations.roles", "CrewSectorInvitations.invitedBy")
	}
	writeGrant := func(p *scriptedPermissions) {
		p.grant(accesstypes.Create, "CrewInvitations", "CrewInvitations.email", "CrewInvitations.roles").
			grant(accesstypes.Delete, "CrewInvitations").
			grant(accesstypes.Create, "CrewSectorInvitations", "CrewSectorInvitations.email", "CrewSectorInvitations.roles").
			grant(accesstypes.Delete, "CrewSectorInvitations")
	}
	tests := []struct {
		name     string
		grant    func(p *scriptedPermissions)
		method   string
		target   string
		body     func(f *userFixture) string
		seed     func(t *testing.T, f *userFixture)
		wantCode int
		check    func(t *testing.T, f *userFixture, body []byte)
	}{
		{
			name:   "the global partition lists the invitations into it, with their sender",
			grant:  listGrant,
			method: http.MethodGet,
			target: "/crew-invitations",
			seed: func(t *testing.T, f *userFixture) {
				t.Helper()
				f.invite(t, "zed@lakeside.edu", "", "Administrator")
				f.invite(t, "amy@lakeside.edu", "anvil", "Clerk")
			},
			wantCode: http.StatusOK,
			check: func(t *testing.T, _ *userFixture, body []byte) {
				t.Helper()
				rows := decodeRows(t, body)
				if len(rows) != 1 || rows[0]["email"] != "zed@lakeside.edu" || rows[0]["invitedBy"] != "admin" || !cmp.Equal(rows[0]["roles"], []any{"Administrator"}) {
					t.Errorf("global invitations = %v, want zed's alone, sent by admin", rows)
				}
			},
		},
		{
			name:   "the tenant partition lists the invitations into the sector",
			grant:  listGrant,
			method: http.MethodGet,
			target: "/sectors/anvil/crew-sector-invitations",
			seed: func(t *testing.T, f *userFixture) {
				t.Helper()
				f.invite(t, "zed@lakeside.edu", "", "Administrator")
				f.invite(t, "amy@lakeside.edu", "anvil", "Clerk", "Pilot")
				f.invite(t, "bob@lakeside.edu", "bastion", "Clerk")
			},
			wantCode: http.StatusOK,
			check: func(t *testing.T, _ *userFixture, body []byte) {
				t.Helper()
				rows := decodeRows(t, body)
				if len(rows) != 1 || rows[0]["email"] != "amy@lakeside.edu" || !cmp.Equal(rows[0]["roles"], []any{"Clerk", "Pilot"}) {
					t.Errorf("anvil invitations = %v, want amy's alone", rows)
				}
			},
		},
		{
			name: "a list condition on role holds only when it holds for every role carried",
			grant: func(p *scriptedPermissions) {
				p.when(accesstypes.List, "CrewSectorInvitations", "role IN ('Clerk')").grant(accesstypes.List, "CrewSectorInvitations.email")
			},
			method: http.MethodGet,
			target: "/sectors/anvil/crew-sector-invitations",
			seed: func(t *testing.T, f *userFixture) {
				t.Helper()
				f.invite(t, "amy@lakeside.edu", "anvil", "Clerk", "Pilot")
				f.invite(t, "cal@lakeside.edu", "anvil", "Clerk")
			},
			wantCode: http.StatusOK,
			check: func(t *testing.T, _ *userFixture, body []byte) {
				t.Helper()
				if rows := decodeRows(t, body); len(rows) != 1 || rows[0]["email"] != "cal@lakeside.edu" {
					t.Errorf("rows = %v, want cal's invitation alone: amy's carries Pilot too", rows)
				}
			},
		},
		{
			name:   "a global invitation records the caller and the partition",
			grant:  writeGrant,
			method: http.MethodPatch,
			target: "/crew-invitations",
			body: func(*userFixture) string {
				return `[{"op":"add","path":"/","value":{"email":"zed@lakeside.edu","roles":["Administrator","Pilot"]}}]`
			},
			wantCode: http.StatusOK,
			check: func(t *testing.T, f *userFixture, _ []byte) {
				t.Helper()
				if len(f.accounts.invitations) != 1 {
					t.Fatalf("invitations = %v, want one", f.accounts.invitations)
				}
				inv := f.accounts.invitations[0]
				if inv.Email != "zed@lakeside.edu" || inv.Tenant != "" || !slices.Equal(inv.Roles, []string{"Administrator", "Pilot"}) || !inv.InvitedBy.Valid || inv.InvitedBy.UUID != f.user(t, "admin").ID {
					t.Errorf("invitation = %+v, want zed into the global partition as Administrator and Pilot, sent by admin", inv)
				}
			},
		},
		{
			name:   "a tenant invitation carries the sector",
			grant:  writeGrant,
			method: http.MethodPatch,
			target: "/sectors/anvil/crew-sector-invitations",
			body: func(*userFixture) string {
				return `[{"op":"add","path":"/","value":{"email":"amy@lakeside.edu","roles":["Clerk"]}}]`
			},
			wantCode: http.StatusOK,
			check: func(t *testing.T, f *userFixture, _ []byte) {
				t.Helper()
				if len(f.accounts.invitations) != 1 || f.accounts.invitations[0].Tenant != "anvil" {
					t.Errorf("invitations = %+v, want one into anvil", f.accounts.invitations)
				}
			},
		},
		{
			name: "a create's condition on role is judged once per role",
			grant: func(p *scriptedPermissions) {
				p.when(accesstypes.Create, "CrewSectorInvitations", "new.role IN ('Clerk')").grant(accesstypes.Create, "CrewSectorInvitations.email", "CrewSectorInvitations.roles")
			},
			method: http.MethodPatch,
			target: "/sectors/anvil/crew-sector-invitations",
			body: func(*userFixture) string {
				return `[{"op":"add","path":"/","value":{"email":"amy@lakeside.edu","roles":["Clerk","Pilot"]}}]`
			},
			wantCode: http.StatusForbidden,
		},
		{
			name: "a create within the condition is permitted",
			grant: func(p *scriptedPermissions) {
				p.when(accesstypes.Create, "CrewSectorInvitations", "new.role IN ('Clerk')").grant(accesstypes.Create, "CrewSectorInvitations.email", "CrewSectorInvitations.roles")
			},
			method: http.MethodPatch,
			target: "/sectors/anvil/crew-sector-invitations",
			body: func(*userFixture) string {
				return `[{"op":"add","path":"/","value":{"email":"amy@lakeside.edu","roles":["Clerk"]}}]`
			},
			wantCode: http.StatusOK,
		},
		{
			name:   "a role the partition does not seat is refused",
			grant:  writeGrant,
			method: http.MethodPatch,
			target: "/sectors/anvil/crew-sector-invitations",
			body: func(*userFixture) string {
				return `[{"op":"add","path":"/","value":{"email":"amy@lakeside.edu","roles":["Administrator"]}}]`
			},
			wantCode: http.StatusBadRequest,
		},
		{
			name:   "an invitation without roles is refused",
			grant:  writeGrant,
			method: http.MethodPatch,
			target: "/crew-invitations",
			body: func(*userFixture) string {
				return `[{"op":"add","path":"/","value":{"email":"zed@lakeside.edu","roles":[]}}]`
			},
			wantCode: http.StatusBadRequest,
		},
		{
			name:   "an invitation is not updated",
			grant:  writeGrant,
			method: http.MethodPatch,
			target: "/crew-invitations",
			body: func(f *userFixture) string {
				return fmt.Sprintf(`[{"op":"patch","path":"/%s","value":{"email":"x@lakeside.edu"}}]`, f.invite(t, "zed@lakeside.edu", "", "Administrator"))
			},
			wantCode: http.StatusBadRequest,
		},
		{
			name:   "a revoke withdraws the partition's invitation",
			grant:  writeGrant,
			method: http.MethodPatch,
			target: "/sectors/anvil/crew-sector-invitations",
			body: func(f *userFixture) string {
				return fmt.Sprintf(`[{"op":"remove","path":"/%s"}]`, f.invite(t, "amy@lakeside.edu", "anvil", "Clerk"))
			},
			wantCode: http.StatusOK,
			check: func(t *testing.T, f *userFixture, _ []byte) {
				t.Helper()
				if len(f.accounts.invitations) != 0 || len(f.accounts.revoked) != 1 {
					t.Errorf("invitations = %v after the revoke, want none", f.accounts.invitations)
				}
			},
		},
		{
			name:   "a revoke of another partition's invitation is not found",
			grant:  writeGrant,
			method: http.MethodPatch,
			target: "/sectors/anvil/crew-sector-invitations",
			body: func(f *userFixture) string {
				return fmt.Sprintf(`[{"op":"remove","path":"/%s"}]`, f.invite(t, "zed@lakeside.edu", "", "Administrator"))
			},
			wantCode: http.StatusNotFound,
		},
		{
			name:   "a revoke's condition is judged over the invitation",
			grant:  func(p *scriptedPermissions) { p.when(accesstypes.Delete, "CrewSectorInvitations", "role IN ('Clerk')") },
			method: http.MethodPatch,
			target: "/sectors/anvil/crew-sector-invitations",
			body: func(f *userFixture) string {
				return fmt.Sprintf(`[{"op":"remove","path":"/%s"}]`, f.invite(t, "amy@lakeside.edu", "anvil", "Clerk", "Pilot"))
			},
			wantCode: http.StatusForbidden,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newUserFixture(t)
			tt.grant(f.perms)
			if tt.seed != nil {
				tt.seed(t, f)
			}
			body := ""
			if tt.body != nil {
				body = tt.body(f)
			}

			code, answer := f.do(t, tt.method, tt.target, body)
			if code != tt.wantCode {
				t.Fatalf("status = %d, want %d: %s", code, tt.wantCode, answer)
			}
			if tt.check != nil {
				tt.check(t, f, answer)
			}
		})
	}
}

// TestUserSurface_orphanRule pins the orphan rule and the tenant's delete: a person
// whose last seat anywhere goes is deleted with the account; one with a seat left
// stays; a tenant's delete removes the seats there alone.
func TestUserSurface_orphanRule(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		grant    func(p *scriptedPermissions)
		target   string
		body     func(f *userFixture) string
		dryRun   bool
		wantCode int
		check    func(t *testing.T, f *userFixture)
	}{
		{
			name:   "unseating a person's only seat deletes the account",
			grant:  func(p *scriptedPermissions) { p.grant(accesstypes.Delete, "CrewSectorSeats") },
			target: "/sectors/anvil/crew-sector-seats",
			body: func(f *userFixture) string {
				return fmt.Sprintf(`[{"op":"remove","path":"/%s/Clerk"}]`, f.user(t, "clerk").ID)
			},
			wantCode: http.StatusOK,
			check: func(t *testing.T, f *userFixture) {
				t.Helper()
				for i := range f.accounts.users {
					if f.accounts.users[i].Username == "clerk" {
						t.Errorf("clerk still exists after losing the only seat")
					}
				}
			},
		},
		{
			name:   "unseating a person who holds a seat elsewhere keeps the account",
			grant:  func(p *scriptedPermissions) { p.grant(accesstypes.Delete, "CrewSeats") },
			target: "/crew-seats",
			body: func(f *userFixture) string {
				return fmt.Sprintf(`[{"op":"remove","path":"/%s/Marshal"}]`, f.user(t, "marshal").ID)
			},
			wantCode: http.StatusOK,
			check: func(t *testing.T, f *userFixture) {
				t.Helper()
				f.user(t, "marshal")
				if !slices.Contains(f.seats.held[accesstypes.DomainPolicyScope(bastion)]["Clerk"], "marshal") {
					t.Errorf("marshal lost the Clerk seat at bastion too")
				}
			},
		},
		{
			name:   "a tenant's delete removes the seats there and the orphaned account",
			grant:  func(p *scriptedPermissions) { p.grant(accesstypes.Delete, "CrewSectorUsers") },
			target: "/sectors/anvil/crew-sector-users",
			body: func(f *userFixture) string {
				return fmt.Sprintf(`[{"op":"remove","path":"/%s"}]`, f.user(t, "clerk").ID)
			},
			wantCode: http.StatusOK,
			check: func(t *testing.T, f *userFixture) {
				t.Helper()
				if slices.Contains(f.seats.held[accesstypes.DomainPolicyScope(anvil)]["Clerk"], "clerk") {
					t.Errorf("clerk still seated at anvil")
				}
				for i := range f.accounts.users {
					if f.accounts.users[i].Username == "clerk" {
						t.Errorf("clerk still exists: no seat remained anywhere")
					}
				}
			},
		},
		{
			name: "a tenant's delete of a person seated elsewhere keeps the account",
			grant: func(p *scriptedPermissions) {
				p.grant(accesstypes.Delete, "CrewSectorUsers")
			},
			target: "/sectors/bastion/crew-sector-users",
			body: func(f *userFixture) string {
				f.seats.seat(global, "Auditor", "clerk")
				f.seats.seat(accesstypes.DomainPolicyScope(bastion), "Pilot", "clerk")

				return fmt.Sprintf(`[{"op":"remove","path":"/%s"}]`, f.user(t, "clerk").ID)
			},
			wantCode: http.StatusOK,
			check: func(t *testing.T, f *userFixture) {
				t.Helper()
				f.user(t, "clerk")
				if slices.Contains(f.seats.held[accesstypes.DomainPolicyScope(bastion)]["Pilot"], "clerk") {
					t.Errorf("clerk still seated at bastion")
				}
				if !slices.Contains(f.seats.held[accesstypes.DomainPolicyScope(anvil)]["Clerk"], "clerk") || !slices.Contains(f.seats.held[global]["Auditor"], "clerk") {
					t.Errorf("clerk's seats elsewhere went with bastion's delete")
				}
			},
		},
		{
			name:   "a tenant's delete of a person holding a role in every sector is refused",
			grant:  func(p *scriptedPermissions) { p.grant(accesstypes.Delete, "CrewSectorUsers") },
			target: "/sectors/bastion/crew-sector-users",
			body: func(f *userFixture) string {
				return fmt.Sprintf(`[{"op":"remove","path":"/%s"}]`, f.user(t, "marshal").ID)
			},
			wantCode: http.StatusConflict,
		},
		{
			name:   "a dry run of a tenant's delete removes nothing",
			grant:  func(p *scriptedPermissions) { p.grant(accesstypes.Delete, "CrewSectorUsers") },
			target: "/sectors/anvil/crew-sector-users",
			dryRun: true,
			body: func(f *userFixture) string {
				return fmt.Sprintf(`[{"op":"remove","path":"/%s"}]`, f.user(t, "clerk").ID)
			},
			wantCode: http.StatusOK,
			check: func(t *testing.T, f *userFixture) {
				t.Helper()
				f.user(t, "clerk")
				if !slices.Contains(f.seats.held[accesstypes.DomainPolicyScope(anvil)]["Clerk"], "clerk") {
					t.Errorf("the dry run unseated clerk")
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newUserFixture(t)
			tt.grant(f.perms)

			req := httptest.NewRequestWithContext(t.Context(), http.MethodPatch, tt.target, strings.NewReader(tt.body(f)))
			req.Header.Set("Content-Type", "application/json")
			if tt.dryRun {
				req.Header.Set(DryRunHeader, "true")
			}
			rr := httptest.NewRecorder()
			f.router.ServeHTTP(rr, req)
			if rr.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d: %s", rr.Code, tt.wantCode, rr.Body.String())
			}
			if tt.check != nil {
				tt.check(t, f)
			}
		})
	}
}

// TestUserSurface_directSeat pins the second door into a tenant: a person who entered
// through a directory the tenant declares is seated there directly; anyone else is
// invited or seated from the global surface.
func TestUserSurface_directSeat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		declare  *signin.TenantDeclaration
		user     string
		wantCode int
	}{
		{name: "a person the sector's directory vouched for is seated", declare: &signin.TenantDeclaration{Directories: []signin.Directory{{Method: "azure", Connection: "lakeside"}}}, user: "cadet", wantCode: http.StatusOK},
		{name: "a person through a directory the sector does not declare is refused", declare: &signin.TenantDeclaration{Directories: []signin.Directory{{Method: "google", Connection: "google"}}}, user: "cadet", wantCode: http.StatusBadRequest},
		{name: "a sector that declares no directory seats its own people alone", declare: nil, user: "cadet", wantCode: http.StatusBadRequest},
		{name: "a password user with no directory is refused however the sector declares", declare: &signin.TenantDeclaration{Directories: []signin.Directory{{Method: "azure", Connection: "lakeside"}}}, user: "admin", wantCode: http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newUserFixture(t)
			f.perms.grant(accesstypes.Create, "CrewSectorSeats")
			if tt.declare != nil {
				f.declarations[anvil] = tt.declare
			}

			code, body := f.do(t, http.MethodPatch, "/sectors/anvil/crew-sector-seats", fmt.Sprintf(`[{"op":"add","path":"/%s/Pilot","value":{}}]`, f.user(t, tt.user).ID))
			if code != tt.wantCode {
				t.Fatalf("status = %d, want %d: %s", code, tt.wantCode, body)
			}
			if seated := slices.Contains(f.seats.held[accesstypes.DomainPolicyScope(anvil)]["Pilot"], accesstypes.User(tt.user)); seated != (tt.wantCode == http.StatusOK) {
				t.Errorf("%s seated at anvil = %v", tt.user, seated)
			}
		})
	}
}

// TestUserSurface_email pins the address on a user: an operator's entry at the create
// and on an update, unverified, and its refusal where the username is the address.
func TestUserSurface_email(t *testing.T) {
	t.Parallel()

	grant := func(p *scriptedPermissions) {
		p.grant(accesstypes.Create, "CrewUsers", "CrewUsers.username", "CrewUsers.password", "CrewUsers.email").
			grant(accesstypes.Update, "CrewUsers", "CrewUsers.username", "CrewUsers.email")
	}
	tests := []struct {
		name     string
		target   string
		body     func(f *userFixture) string
		wantCode int
		check    func(t *testing.T, f *userFixture)
	}{
		{
			name:   "a create enters the address unverified",
			target: "/crew-users",
			body: func(*userFixture) string {
				return `[{"op":"add","path":"/","value":{"username":"newbie","password":"temporary-1","email":"newbie@lakeside.edu"}}]`
			},
			wantCode: http.StatusOK,
			check: func(t *testing.T, f *userFixture) {
				t.Helper()
				user := f.user(t, "newbie")
				if user.Email == nil || *user.Email != "newbie@lakeside.edu" || user.EmailVerifiedAt != nil {
					t.Errorf("newbie = %+v, want the address entered, unverified", user)
				}
			},
		},
		{
			name:   "an empty address is refused",
			target: "/crew-users",
			body: func(*userFixture) string {
				return `[{"op":"add","path":"/","value":{"username":"newbie","password":"temporary-1","email":" "}}]`
			},
			wantCode: http.StatusBadRequest,
		},
		{
			name:   "an update enters or removes the address",
			target: "/crew-users",
			body: func(f *userFixture) string {
				return fmt.Sprintf(`[{"op":"patch","path":"/%s","value":{"email":"clerk@lakeside.edu"}}]`, f.user(t, "clerk").ID)
			},
			wantCode: http.StatusOK,
			check: func(t *testing.T, f *userFixture) {
				t.Helper()
				if user := f.user(t, "clerk"); user.Email == nil || *user.Email != "clerk@lakeside.edu" {
					t.Errorf("clerk = %+v, want the address entered", user)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newUserFixture(t)
			grant(f.perms)

			code, body := f.do(t, http.MethodPatch, tt.target, tt.body(f))
			if code != tt.wantCode {
				t.Fatalf("status = %d, want %d: %s", code, tt.wantCode, body)
			}
			if tt.check != nil {
				tt.check(t, f)
			}
		})
	}
}

// testEmailAuth is a marker whose password method declares the username as the
// address.
type testEmailAuth struct{}

func (testEmailAuth) UserSurfaceDeclaration() UserSurfaceDeclaration {
	return UserSurfaceDeclaration{Auth: "Staff", Shape: UserSurfaceShape{Password: true, UsernameIsEmail: true}}
}

// TestUserSurface_usernameIsEmail pins the shape where the username is the address: no
// address is entered beside it, and a rename is refused, as the session library refuses
// it.
func TestUserSurface_usernameIsEmail(t *testing.T) {
	t.Parallel()

	accounts := &fakeAccounts{users: []UserAccount{{ID: mustUUID(t), Username: "pat@lakeside.edu", HasPassword: true}}}
	seats := &fakeSeats{roles: map[accesstypes.PolicyScope][]accesstypes.Role{global: {"Administrator"}}}
	perms := &scriptedPermissions{user: "pat@lakeside.edu"}
	perms.grant(accesstypes.Create, "StaffUsers", "StaffUsers.username", "StaffUsers.password", "StaffUsers.email").
		grant(accesstypes.Update, "StaffUsers", "StaffUsers.username", "StaffUsers.email")
	b := NewCollectionBuilder()
	decl := testEmailAuth{}.UserSurfaceDeclaration()
	if err := RegisterUserSurface(b, &decl, true); err != nil {
		t.Fatal(err)
	}
	surface := NewUserSurface[testEmailAuth](accounts, seats)
	app := userTestApp{perms: perms, key: mustCursorKey(t, "c3RhZmYtdGVzdC1jb29raWUta2V5LTMyLWJ5dGVzISE=")}
	handler := surface.PatchUsers(app, b.GeneratedCollection())
	do := func(body string) int {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPatch, "/staff-users", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		return rr.Code
	}
	if code := do(`[{"op":"add","path":"/","value":{"username":"sam@lakeside.edu","password":"temporary-1","email":"sam@lakeside.edu"}}]`); code != http.StatusBadRequest {
		t.Errorf("create with an address beside the username = %d, want 400", code)
	}
	if code := do(`[{"op":"add","path":"/","value":{"username":"sam@lakeside.edu","password":"temporary-1"}}]`); code != http.StatusOK {
		t.Errorf("create under the username = %d, want 200", code)
	}
	if code := do(fmt.Sprintf(`[{"op":"patch","path":"/%s","value":{"username":"other@lakeside.edu"}}]`, accounts.users[0].ID)); code != http.StatusConflict {
		t.Errorf("rename = %d, want 409: the username is the address", code)
	}
}

// TestSeatInvited pins the acceptance hook: a tenant invitation seats in the tenant, a
// global one where each role is held, and a role that exists nowhere is an error.
func TestSeatInvited(t *testing.T) {
	t.Parallel()

	seats := &fakeSeats{roles: map[accesstypes.PolicyScope][]accesstypes.Role{global: {"Administrator"}, everywhere: {"Pilot"}, accesstypes.DomainPolicyScope(anvil): {"Clerk"}}}
	hook := SeatInvited(seats)
	if err := hook(t.Context(), ccc.NilUUID, "amy", &sessioninfo.Invitation{Email: "amy@lakeside.edu", Tenant: "anvil", RoleNames: []string{"Clerk"}}); err != nil {
		t.Fatalf("hook (tenant) error = %v", err)
	}
	if !slices.Contains(seats.held[accesstypes.DomainPolicyScope(anvil)]["Clerk"], "amy") {
		t.Errorf("amy not seated as Clerk at anvil")
	}
	if err := hook(t.Context(), ccc.NilUUID, "zed", &sessioninfo.Invitation{Email: "zed@lakeside.edu", RoleNames: []string{"Administrator", "Pilot"}}); err != nil {
		t.Fatalf("hook (global) error = %v", err)
	}
	if !slices.Contains(seats.held[global]["Administrator"], "zed") || !slices.Contains(seats.held[everywhere]["Pilot"], "zed") {
		t.Errorf("zed's seats = %v, want Administrator globally and Pilot everywhere", seats.held)
	}
	if err := hook(t.Context(), ccc.NilUUID, "zed", &sessioninfo.Invitation{Email: "zed@lakeside.edu", RoleNames: []string{"Nobody"}}); err == nil {
		t.Error("hook with a role that exists nowhere = nil, want an error")
	}
}

// TestSeatsFollowRename pins the rename hook: the seats move with the name.
func TestSeatsFollowRename(t *testing.T) {
	t.Parallel()

	seats := &fakeSeats{roles: map[accesstypes.PolicyScope][]accesstypes.Role{global: {"Administrator"}, accesstypes.DomainPolicyScope(anvil): {"Clerk"}}}
	seats.seat(global, "Administrator", "pat@lakeside.edu")
	seats.seat(accesstypes.DomainPolicyScope(anvil), "Clerk", "pat@lakeside.edu")
	if err := SeatsFollowRename(seats)(t.Context(), ccc.NilUUID, "pat@lakeside.edu", "pat.lee@lakeside.edu"); err != nil {
		t.Fatalf("hook error = %v", err)
	}
	for scope, role := range map[accesstypes.PolicyScope]accesstypes.Role{global: "Administrator", accesstypes.DomainPolicyScope(anvil): "Clerk"} {
		if slices.Contains(seats.held[scope][role], "pat@lakeside.edu") || !slices.Contains(seats.held[scope][role], "pat.lee@lakeside.edu") {
			t.Errorf("%s in %s held by %v, want the new name alone", role, scope, seats.held[scope][role])
		}
	}
}

// Test_roleVariants pins the expansion a list of roles gets: one image per role, in
// whichever image carries the list; an image with none is itself.
func Test_roleVariants(t *testing.T) {
	t.Parallel()

	pre := map[string]any{"email": "a@b", "role": []string{"Clerk", "Pilot"}}
	variants := roleVariants(pre, nil)
	if len(variants) != 2 || variants[0][0]["role"] != "Clerk" || variants[1][0]["role"] != "Pilot" || variants[0][1] != nil {
		t.Errorf("variants = %v, want one pre-image per role", variants)
	}
	post := map[string]any{"role": []string{"Clerk"}}
	variants = roleVariants(nil, post)
	if len(variants) != 1 || variants[0][1]["role"] != "Clerk" || variants[0][0] != nil {
		t.Errorf("variants = %v, want the one post-image", variants)
	}
	plain := map[string]any{"role": "Clerk"}
	if variants = roleVariants(plain, nil); len(variants) != 1 || variants[0][0]["role"] != "Clerk" {
		t.Errorf("variants = %v, want the image itself", variants)
	}
	if _, ok := pre["role"].([]string); !ok {
		t.Error("the source image was changed")
	}
}
