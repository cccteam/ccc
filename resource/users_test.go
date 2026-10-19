package resource

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/httpio"
	"github.com/go-chi/chi/v5"
	"github.com/go-playground/errors/v5"
	"github.com/google/go-cmp/cmp"
)

// testUserAuth is the marker of the test Auth: a password method and a Google
// registration whose directory writes the roles, over an application with sectors.
type testUserAuth struct{}

func (testUserAuth) UserSurfaceDeclaration() UserSurfaceDeclaration {
	return UserSurfaceDeclaration{
		Auth:        "Crew",
		Tenant:      "Sector",
		DomainParam: "sectorID",
		Shape: UserSurfaceShape{
			Password:      true,
			Registrations: []UserRegistration{{Name: "google", Kind: "google", Label: "Google", SyncsRoles: true}},
		},
	}
}

// userFixture is one test's world: the stores, the caller's grants, and the router the
// surface's handlers are mounted on the way the generated router mounts them.
type userFixture struct {
	accounts *fakeAccounts
	seats    *fakeSeats
	perms    *scriptedPermissions
	surface  *UserSurface[testUserAuth]
	router   chi.Router
}

type fakeAccounts struct {
	users      []UserAccount
	identities map[ccc.UUID][]UserIdentity
	resets     map[ccc.UUID]string
	unlinked   []ccc.UUID
}

func (f *fakeAccounts) Users(context.Context) ([]UserAccount, error) {
	users := slices.Clone(f.users)
	slices.SortFunc(users, func(a, b UserAccount) int {
		return strings.Compare(strings.ToLower(a.Username), strings.ToLower(b.Username))
	})

	return users, nil
}

func (f *fakeAccounts) find(id ccc.UUID) (*UserAccount, error) {
	for i := range f.users {
		if f.users[i].ID == id {
			return &f.users[i], nil
		}
	}

	return nil, httpio.NewNotFoundMessagef("user id %q does not exist", id)
}

func (f *fakeAccounts) User(_ context.Context, id ccc.UUID) (*UserAccount, error) {
	user, err := f.find(id)
	if err != nil {
		return nil, err
	}
	copied := *user

	return &copied, nil
}

func (f *fakeAccounts) CreateUser(_ context.Context, username string, password *string, disabled bool) (ccc.UUID, error) {
	id, err := ccc.NewUUID()
	if err != nil {
		return ccc.NilUUID, errors.Wrap(err, "ccc.NewUUID()")
	}
	f.users = append(f.users, UserAccount{ID: id, Username: username, HasPassword: password != nil, PasswordChangeRequired: password != nil, Disabled: disabled, CreatedAt: time.Now()})

	return id, nil
}

func (f *fakeAccounts) RenameUser(_ context.Context, id ccc.UUID, username string) error {
	user, err := f.find(id)
	if err != nil {
		return err
	}
	user.Username = username

	return nil
}

func (f *fakeAccounts) SetUserDisabled(_ context.Context, id ccc.UUID, disabled bool) error {
	user, err := f.find(id)
	if err != nil {
		return err
	}
	user.Disabled = disabled

	return nil
}

func (f *fakeAccounts) DeleteUser(_ context.Context, id ccc.UUID) error {
	if _, err := f.find(id); err != nil {
		return err
	}
	f.users = slices.DeleteFunc(f.users, func(u UserAccount) bool { return u.ID == id })

	return nil
}

func (f *fakeAccounts) ResetUserPassword(_ context.Context, id ccc.UUID, temporary string) error {
	user, err := f.find(id)
	if err != nil {
		return err
	}
	user.HasPassword, user.PasswordChangeRequired = true, true
	if f.resets == nil {
		f.resets = map[ccc.UUID]string{}
	}
	f.resets[id] = temporary

	return nil
}

func (f *fakeAccounts) Identities(_ context.Context, id ccc.UUID) ([]UserIdentity, error) {
	return slices.Clone(f.identities[id]), nil
}

func (f *fakeAccounts) UnlinkIdentity(_ context.Context, identityID ccc.UUID) error {
	f.unlinked = append(f.unlinked, identityID)
	for user, identities := range f.identities {
		f.identities[user] = slices.DeleteFunc(identities, func(i UserIdentity) bool { return i.ID == identityID })
	}

	return nil
}

// fakeSeats holds memberships by scope, role and user, with the roles that exist in
// each scope.
type fakeSeats struct {
	roles map[accesstypes.PolicyScope][]accesstypes.Role
	held  map[accesstypes.PolicyScope]map[accesstypes.Role][]accesstypes.User
}

func (f *fakeSeats) seat(scope accesstypes.PolicyScope, role accesstypes.Role, users ...accesstypes.User) {
	if f.held == nil {
		f.held = map[accesstypes.PolicyScope]map[accesstypes.Role][]accesstypes.User{}
	}
	if f.held[scope] == nil {
		f.held[scope] = map[accesstypes.Role][]accesstypes.User{}
	}
	for _, user := range users {
		if !slices.Contains(f.held[scope][role], user) {
			f.held[scope][role] = append(f.held[scope][role], user)
		}
	}
}

func (f *fakeSeats) AddUserRoles(_ context.Context, scope accesstypes.PolicyScope, user accesstypes.User, roles ...accesstypes.Role) error {
	for _, role := range roles {
		f.seat(scope, role, user)
	}

	return nil
}

func (f *fakeSeats) DeleteUserRoles(_ context.Context, scope accesstypes.PolicyScope, user accesstypes.User, roles ...accesstypes.Role) error {
	for _, role := range roles {
		f.held[scope][role] = slices.DeleteFunc(f.held[scope][role], func(u accesstypes.User) bool { return u == user })
	}

	return nil
}

func (f *fakeSeats) UserRoles(_ context.Context, user accesstypes.User, scopes ...accesstypes.PolicyScope) (accesstypes.RoleCollection, error) {
	collection := accesstypes.RoleCollection{}
	for scope, byRole := range f.held {
		if len(scopes) > 0 && !slices.Contains(scopes, scope) {
			continue
		}
		for role, users := range byRole {
			if slices.Contains(users, user) {
				collection[scope] = append(collection[scope], role)
			}
		}
	}

	return collection, nil
}

func (f *fakeSeats) RoleUsers(_ context.Context, scope accesstypes.PolicyScope, role accesstypes.Role) ([]accesstypes.User, error) {
	return slices.Clone(f.held[scope][role]), nil
}

func (f *fakeSeats) Roles(_ context.Context, scope accesstypes.PolicyScope) ([]accesstypes.Role, error) {
	return slices.Clone(f.roles[scope]), nil
}

// scriptedPermissions answers each permission from a table of decisions; a resource off
// the table is Denied.
type scriptedPermissions struct {
	user   accesstypes.User
	byPerm map[accesstypes.Permission]map[accesstypes.Resource]accesstypes.Decision
}

func (p *scriptedPermissions) grant(perm accesstypes.Permission, resources ...accesstypes.Resource) *scriptedPermissions {
	for _, res := range resources {
		p.decide(perm, res, accesstypes.Granted())
	}

	return p
}

func (p *scriptedPermissions) when(perm accesstypes.Permission, res accesstypes.Resource, source string) *scriptedPermissions {
	cond, err := accesstypes.NewCondition(source)
	if err != nil {
		panic(err)
	}
	p.decide(perm, res, accesstypes.Conditional(accesstypes.ConditionGroup{Resources: []accesstypes.Resource{res}, Condition: cond}))

	return p
}

func (p *scriptedPermissions) decide(perm accesstypes.Permission, res accesstypes.Resource, decision accesstypes.Decision) {
	if p.byPerm == nil {
		p.byPerm = map[accesstypes.Permission]map[accesstypes.Resource]accesstypes.Decision{}
	}
	if p.byPerm[perm] == nil {
		p.byPerm[perm] = map[accesstypes.Resource]accesstypes.Decision{}
	}
	p.byPerm[perm][res] = decision
}

func (p *scriptedPermissions) Check(_ context.Context, _ accesstypes.Environment, _ accesstypes.Scope, perm accesstypes.Permission, resources ...accesstypes.Resource) (accesstypes.Decisions, error) {
	out := make(accesstypes.Decisions, len(resources))
	for _, res := range resources {
		out[res] = p.byPerm[perm][res]
	}

	return out, nil
}

func (*scriptedPermissions) PermissionDigest(context.Context, accesstypes.Scope) (accesstypes.PermissionDigest, error) {
	return accesstypes.PermissionDigest{}, nil
}

func (*scriptedPermissions) HasGrants(context.Context, accesstypes.Scope) (bool, error) {
	return true, nil
}

func (*scriptedPermissions) Domains(context.Context) ([]accesstypes.Domain, error) {
	return []accesstypes.Domain{}, nil
}

func (p *scriptedPermissions) User() accesstypes.User { return p.user }

// userTestApp is the application seam: no validator, the scripted permissions, a cursor
// key and a Spanner-typed client.
type userTestApp struct {
	perms *scriptedPermissions
	key   *CursorKey
}

type userTestClient struct{ Client }

func (userTestClient) DBType() DBType { return SpannerDBType }

func (userTestApp) Validator() ValidatorFunc                        { return nil }
func (a userTestApp) UserPermissions(*http.Request) UserPermissions { return a.perms }
func (a userTestApp) CursorKey() *CursorKey                         { return a.key }
func (userTestApp) ResourceClient() Client                          { return userTestClient{} }

// The personas: the caller (an administrator), a pilot linked through the directory
// (roles synced, in Acme), a clerk seated in the Anvil sector alone, and a marshal
// seated in every sector.
var (
	anvil   = accesstypes.Domain("anvil")
	bastion = accesstypes.Domain("bastion")

	global     = accesstypes.GlobalPolicyScope()
	everywhere = accesstypes.EveryDomainPolicyScope()
)

func newUserFixture(t *testing.T) *userFixture {
	t.Helper()
	ids := make([]ccc.UUID, 4)
	for i := range ids {
		id, err := ccc.NewUUID()
		if err != nil {
			t.Fatal(err)
		}
		ids[i] = id
	}
	signedIn := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	acme := "acme"
	accounts := &fakeAccounts{
		users: []UserAccount{
			{ID: ids[0], Username: "admin", HasPassword: true, CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), LastSignInAt: &signedIn},
			{ID: ids[1], Username: "pilot", CreatedAt: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)},
			{ID: ids[2], Username: "clerk", HasPassword: true, CreatedAt: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)},
			{ID: ids[3], Username: "marshal", HasPassword: true, CreatedAt: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)},
		},
		identities: map[ccc.UUID][]UserIdentity{
			ids[1]: {{ID: mustUUID(t), Method: "google", Connection: "google", Subject: "sub-1", Organization: &acme, LinkedAt: signedIn, LastUsedAt: signedIn}},
		},
	}
	seats := &fakeSeats{roles: map[accesstypes.PolicyScope][]accesstypes.Role{
		global:                                 {"Administrator", "Auditor"},
		everywhere:                             {"Marshal", "Pilot", "Clerk"},
		accesstypes.DomainPolicyScope(anvil):   {"Marshal", "Pilot", "Clerk"},
		accesstypes.DomainPolicyScope(bastion): {"Marshal", "Pilot", "Clerk"},
	}}
	seats.seat(global, "Administrator", "admin")
	seats.seat(everywhere, "Pilot", "pilot")
	seats.seat(everywhere, "Marshal", "marshal")
	seats.seat(accesstypes.DomainPolicyScope(anvil), "Clerk", "clerk")
	seats.seat(accesstypes.DomainPolicyScope(bastion), "Clerk", "marshal")

	perms := &scriptedPermissions{user: "admin"}
	b := NewCollectionBuilder()
	decl := testUserAuth{}.UserSurfaceDeclaration()
	if err := RegisterUserSurface(b, &decl, true); err != nil {
		t.Fatalf("RegisterUserSurface() error = %v", err)
	}
	collection := b.GeneratedCollection()
	surface := NewUserSurface[testUserAuth](accounts, seats)
	app := userTestApp{perms: perms, key: mustCursorKey(t, base64.StdEncoding.EncodeToString([]byte("users-test-cookie-key-32-bytes!!")))}

	n := surface.Names()
	r := chi.NewRouter()
	r.Get("/"+n.Global.UsersRoute, surface.Users(app, collection))
	r.Get("/"+n.Global.UsersRoute+"/{"+string(n.Global.UserParam)+"}", surface.User(app, collection))
	r.Patch("/"+n.Global.UsersRoute, surface.PatchUsers(app, collection))
	r.Get("/"+n.Global.SeatsRoute, surface.Seats(app, collection))
	r.Get("/"+n.Global.SeatsRoute+"/{"+string(n.Global.SeatUserParam)+"}/{"+string(n.Global.SeatRoleParam)+"}", surface.Seat(app, collection))
	r.Patch("/"+n.Global.SeatsRoute, surface.PatchSeats(app, collection))
	r.Get("/"+n.Global.RolesRoute, surface.Roles(app, collection))
	r.Post("/"+n.Global.SetPasswordRoute, surface.SetUserPassword(app, collection))
	r.Post("/"+n.Global.UnlinkIdentityRoute, surface.UnlinkUserIdentity(app, collection))
	tenant := "/sectors/{sectorID}/"
	r.Get(tenant+n.Tenant.UsersRoute, surface.TenantUsers(app, collection))
	r.Get(tenant+n.Tenant.UsersRoute+"/{"+string(n.Tenant.UserParam)+"}", surface.TenantUser(app, collection))
	r.Patch(tenant+n.Tenant.UsersRoute, surface.PatchTenantUsers(app, collection))
	r.Get(tenant+n.Tenant.SeatsRoute, surface.TenantSeats(app, collection))
	r.Patch(tenant+n.Tenant.SeatsRoute, surface.PatchTenantSeats(app, collection))
	r.Get(tenant+n.Tenant.RolesRoute, surface.TenantRoles(app, collection))
	r.Post(tenant+n.Tenant.SetPasswordRoute, surface.SetTenantUserPassword(app, collection))

	return &userFixture{accounts: accounts, seats: seats, perms: perms, surface: surface, router: r}
}

func mustUUID(t *testing.T) ccc.UUID {
	t.Helper()
	id, err := ccc.NewUUID()
	if err != nil {
		t.Fatal(err)
	}

	return id
}

// user finds a persona's account by username.
func (f *userFixture) user(t *testing.T, username string) *UserAccount {
	t.Helper()
	for i := range f.accounts.users {
		if f.accounts.users[i].Username == username {
			return &f.accounts.users[i]
		}
	}
	t.Fatalf("no user %q", username)

	return nil
}

// do runs one request through the router and decodes a JSON body.
func (f *userFixture) do(t *testing.T, method, target, body string) (code int, answer []byte) {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequestWithContext(t.Context(), method, target, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rr := httptest.NewRecorder()
	f.router.ServeHTTP(rr, req)

	return rr.Code, rr.Body.Bytes()
}

func decodeRows(t *testing.T, body []byte) []map[string]any {
	t.Helper()
	var rows []map[string]any
	if err := json.Unmarshal(body, &rows); err != nil {
		t.Fatalf("json.Unmarshal(%s) error = %v", body, err)
	}

	return rows
}

func usernames(rows []map[string]any) []string {
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		name, _ := row["username"].(string)
		names = append(names, name)
	}

	return names
}

// TestUserSurface_lists pins the lists of both partitions: who each one shows, how a
// resource condition narrows the rows and a field condition masks a cell, and what the
// subject bindings resolve to.
func TestUserSurface_lists(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		caller    string
		grant     func(p *scriptedPermissions)
		target    string
		wantCode  int
		wantNames []string
		check     func(t *testing.T, rows []map[string]any)
	}{
		{
			name: "the global partition lists everyone with their seats",
			grant: func(p *scriptedPermissions) {
				p.grant(accesstypes.List, "CrewUsers", "CrewUsers.username", "CrewUsers.roles", "CrewUsers.everywhereRoles", "CrewUsers.rolesSynced")
			},
			target:    "/crew-users",
			wantCode:  http.StatusOK,
			wantNames: []string{"admin", "clerk", "marshal", "pilot"},
			check: func(t *testing.T, rows []map[string]any) {
				t.Helper()
				if got := rows[0]["roles"]; !cmp.Equal(got, []any{"Administrator"}) {
					t.Errorf("admin roles = %v", got)
				}
				if got := rows[3]["everywhereRoles"]; !cmp.Equal(got, []any{"Pilot"}) {
					t.Errorf("pilot everywhereRoles = %v", got)
				}
				if got := rows[3]["rolesSynced"]; got != true {
					t.Errorf("pilot rolesSynced = %v, want true: the directory writes the roles", got)
				}
				if _, leaked := rows[0]["disabled"]; leaked {
					t.Errorf("disabled rendered without a grant on the field")
				}
			},
		},
		{
			name: "a resource condition narrows the rows: organization = 'acme'",
			grant: func(p *scriptedPermissions) {
				p.when(accesstypes.List, "CrewUsers", "organization = 'acme'").grant(accesstypes.List, "CrewUsers.username")
			},
			target:    "/crew-users",
			wantCode:  http.StatusOK,
			wantNames: []string{"pilot"},
		},
		{
			name: "a field condition masks the cell on the rows it refuses",
			grant: func(p *scriptedPermissions) {
				p.grant(accesstypes.List, "CrewUsers", "CrewUsers.username").when(accesstypes.List, "CrewUsers.createdAt", "hasPassword = true")
			},
			target:    "/crew-users",
			wantCode:  http.StatusOK,
			wantNames: []string{"admin", "clerk", "marshal", "pilot"},
			check: func(t *testing.T, rows []map[string]any) {
				t.Helper()
				if _, ok := rows[0]["createdAt"]; !ok {
					t.Errorf("admin's createdAt masked; the condition holds for a password user")
				}
				if _, ok := rows[3]["createdAt"]; ok {
					t.Errorf("pilot's createdAt rendered; the condition refuses a password-less user")
				}
			},
		},
		{
			name:   "subject.ownOrganization resolves to the caller's own organization",
			caller: "pilot",
			grant: func(p *scriptedPermissions) {
				p.when(accesstypes.List, "CrewUsers", "organization = subject.ownOrganization").grant(accesstypes.List, "CrewUsers.username")
			},
			target:    "/crew-users",
			wantCode:  http.StatusOK,
			wantNames: []string{"pilot"},
		},
		{
			name: "a caller with no organization sees nothing under the same condition",
			grant: func(p *scriptedPermissions) {
				p.when(accesstypes.List, "CrewUsers", "organization = subject.ownOrganization").grant(accesstypes.List, "CrewUsers.username")
			},
			target:    "/crew-users",
			wantCode:  http.StatusOK,
			wantNames: []string{},
		},
		{
			name: "a sort on a conditional field is refused",
			grant: func(p *scriptedPermissions) {
				p.grant(accesstypes.List, "CrewUsers").when(accesstypes.List, "CrewUsers.username", "disabled = false")
			},
			target:   "/crew-users?sort=username",
			wantCode: http.StatusForbidden,
		},
		{
			name:     "no grant on the resource is Forbidden",
			grant:    func(*scriptedPermissions) {},
			target:   "/crew-users",
			wantCode: http.StatusForbidden,
		},
		{
			name: "the tenant partition lists the users seated in the sector or everywhere",
			grant: func(p *scriptedPermissions) {
				p.grant(accesstypes.List, "CrewSectorUsers", "CrewSectorUsers.username", "CrewSectorUsers.roles", "CrewSectorUsers.everywhereRoles")
			},
			target:    "/sectors/anvil/crew-sector-users",
			wantCode:  http.StatusOK,
			wantNames: []string{"clerk", "marshal", "pilot"},
			check: func(t *testing.T, rows []map[string]any) {
				t.Helper()
				if got := rows[0]["roles"]; !cmp.Equal(got, []any{"Clerk"}) {
					t.Errorf("clerk's roles at anvil = %v", got)
				}
				if got := rows[1]["roles"]; !cmp.Equal(got, []any{}) {
					t.Errorf("marshal's roles at anvil = %v, want none: the seat is held everywhere", got)
				}
			},
		},
		{
			name: "the global seats list every seat with where it is held",
			grant: func(p *scriptedPermissions) {
				p.grant(accesstypes.List, "CrewSeats", "CrewSeats.username", "CrewSeats.everyDomain")
			},
			target:    "/crew-seats",
			wantCode:  http.StatusOK,
			wantNames: []string{"admin", "marshal", "pilot"},
			check: func(t *testing.T, rows []map[string]any) {
				t.Helper()
				if got := rows[0]["everyDomain"]; got != false {
					t.Errorf("the administrator seat everyDomain = %v", got)
				}
				if got := rows[1]["everyDomain"]; got != true {
					t.Errorf("the marshal seat everyDomain = %v", got)
				}
			},
		},
		{
			name: "the tenant seats show the sector's seats and the ones held everywhere",
			grant: func(p *scriptedPermissions) {
				p.grant(accesstypes.List, "CrewSectorSeats", "CrewSectorSeats.username", "CrewSectorSeats.everywhere")
			},
			target:    "/sectors/anvil/crew-sector-seats",
			wantCode:  http.StatusOK,
			wantNames: []string{"clerk", "marshal", "pilot"},
		},
		{
			name:     "the global roles list the global kind and the every-sector kind",
			grant:    func(p *scriptedPermissions) { p.grant(accesstypes.List, "CrewRoles", "CrewRoles.kind") },
			target:   "/crew-roles",
			wantCode: http.StatusOK,
			check: func(t *testing.T, rows []map[string]any) {
				t.Helper()
				got := make([]string, 0, len(rows))
				for _, row := range rows {
					got = append(got, fmt.Sprintf("%s:%s", row["name"], row["kind"]))
				}
				want := []string{"Administrator:global", "Auditor:global", "Clerk:domain", "Marshal:domain", "Pilot:domain"}
				if diff := cmp.Diff(want, got); diff != "" {
					t.Errorf("roles mismatch (-want +got):\n%s", diff)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newUserFixture(t)
			if tt.caller != "" {
				f.perms.user = accesstypes.User(tt.caller)
			}
			tt.grant(f.perms)

			code, body := f.do(t, http.MethodGet, tt.target, "")
			if code != tt.wantCode {
				t.Fatalf("status = %d, want %d: %s", code, tt.wantCode, body)
			}
			if code != http.StatusOK {
				return
			}
			rows := decodeRows(t, body)
			if tt.wantNames != nil {
				if diff := cmp.Diff(tt.wantNames, usernames(rows)); diff != "" {
					t.Errorf("rows mismatch (-want +got):\n%s", diff)
				}
			}
			if tt.check != nil {
				tt.check(t, rows)
			}
		})
	}
}

// TestUserSurface_read pins the read: the row by id, NotFound for a row the condition
// refuses, and the envelope's rules on one's own row.
func TestUserSurface_read(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		grant    func(p *scriptedPermissions)
		of       string
		query    string
		wantCode int
		check    func(t *testing.T, row map[string]any)
	}{
		{
			name:     "a user by id",
			grant:    func(p *scriptedPermissions) { p.grant(accesstypes.Read, "CrewUsers", "CrewUsers.username") },
			of:       "pilot",
			wantCode: http.StatusOK,
			check: func(t *testing.T, row map[string]any) {
				t.Helper()
				if row["username"] != "pilot" {
					t.Errorf("username = %v", row["username"])
				}
			},
		},
		{
			name: "a row the condition refuses does not exist for the caller",
			grant: func(p *scriptedPermissions) {
				p.when(accesstypes.Read, "CrewUsers", "organization = 'acme'").grant(accesstypes.Read, "CrewUsers.username")
			},
			of:       "clerk",
			wantCode: http.StatusNotFound,
		},
		{
			name: "the envelope on one's own row withholds the deactivation, the delete and the password reset",
			grant: func(p *scriptedPermissions) {
				p.grant(accesstypes.Read, "CrewUsers", "CrewUsers.username").
					grant(accesstypes.Update, "CrewUsers", "CrewUsers.username", "CrewUsers.disabled").
					grant(accesstypes.Delete, "CrewUsers").
					grant(accesstypes.Execute, "SetCrewUserPassword", "UnlinkCrewUserIdentity")
			},
			of:       "admin",
			query:    "?capabilities=Update,Delete,Execute",
			wantCode: http.StatusOK,
			check: func(t *testing.T, row map[string]any) {
				t.Helper()
				want := map[string]any{"Update": []any{"username"}, "Delete": false, "Execute": []any{"UnlinkCrewUserIdentity"}}
				if diff := cmp.Diff(want, row[CapabilitiesProperty]); diff != "" {
					t.Errorf("envelope mismatch (-want +got):\n%s", diff)
				}
			},
		},
		{
			name: "the envelope on another row answers from the grants and the row's condition",
			grant: func(p *scriptedPermissions) {
				p.grant(accesstypes.Read, "CrewUsers", "CrewUsers.username").
					grant(accesstypes.Update, "CrewUsers", "CrewUsers.username").when(accesstypes.Update, "CrewUsers.disabled", "hasPassword = true").
					when(accesstypes.Delete, "CrewUsers", "disabled = true").
					grant(accesstypes.Execute, "SetCrewUserPassword")
			},
			of:       "clerk",
			query:    "?capabilities=Update,Delete,Execute",
			wantCode: http.StatusOK,
			check: func(t *testing.T, row map[string]any) {
				t.Helper()
				want := map[string]any{"Update": []any{"username", "disabled"}, "Delete": false, "Execute": []any{"SetCrewUserPassword"}}
				if diff := cmp.Diff(want, row[CapabilitiesProperty]); diff != "" {
					t.Errorf("envelope mismatch (-want +got):\n%s", diff)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newUserFixture(t)
			tt.grant(f.perms)

			code, body := f.do(t, http.MethodGet, "/crew-users/"+f.user(t, tt.of).ID.String()+tt.query, "")
			if code != tt.wantCode {
				t.Fatalf("status = %d, want %d: %s", code, tt.wantCode, body)
			}
			if code != http.StatusOK {
				return
			}
			var row map[string]any
			if err := json.Unmarshal(body, &row); err != nil {
				t.Fatalf("json.Unmarshal() error = %v", err)
			}
			tt.check(t, row)
		})
	}
}

// TestUserSurface_userWrites pins the users patch: creation under the shape's password
// rule, the rules nobody grants away, the renamed user's seats, and the delete.
func TestUserSurface_userWrites(t *testing.T) {
	t.Parallel()

	fullGrant := func(p *scriptedPermissions) {
		p.grant(accesstypes.Create, "CrewUsers", "CrewUsers.username", "CrewUsers.password", "CrewUsers.disabled").
			grant(accesstypes.Update, "CrewUsers", "CrewUsers.username", "CrewUsers.disabled").
			grant(accesstypes.Delete, "CrewUsers").
			grant(accesstypes.Create, "CrewSectorUsers", "CrewSectorUsers.username", "CrewSectorUsers.password", "CrewSectorUsers.role", "CrewSectorSeats").
			grant(accesstypes.Update, "CrewSectorUsers", "CrewSectorUsers.disabled").
			grant(accesstypes.Delete, "CrewSectorUsers")
	}
	tests := []struct {
		name     string
		grant    func(p *scriptedPermissions)
		target   string
		body     func(f *userFixture) string
		headers  map[string]string
		wantCode int
		check    func(t *testing.T, f *userFixture)
	}{
		{
			name:   "a create with a password adds the user with the forced-change mark",
			grant:  fullGrant,
			target: "/crew-users",
			body: func(*userFixture) string {
				return `[{"op":"add","path":"/","value":{"username":"newbie","password":"temporary-1"}}]`
			},
			wantCode: http.StatusOK,
			check: func(t *testing.T, f *userFixture) {
				t.Helper()
				user := f.user(t, "newbie")
				if !user.HasPassword || !user.PasswordChangeRequired {
					t.Errorf("newbie = %+v, want a password with the forced-change mark", user)
				}
			},
		},
		{
			name:     "a create without a password is refused where the password is the only way in",
			grant:    fullGrant,
			target:   "/crew-users",
			body:     func(*userFixture) string { return `[{"op":"add","path":"/","value":{"username":"newbie"}}]` },
			wantCode: http.StatusBadRequest,
		},
		{
			name:   "a create of a name in use is a conflict",
			grant:  fullGrant,
			target: "/crew-users",
			body: func(*userFixture) string {
				return `[{"op":"add","path":"/","value":{"username":"Pilot","password":"x"}}]`
			},
			wantCode: http.StatusConflict,
		},
		{
			name: "a create's condition is judged over the proposed row",
			grant: func(p *scriptedPermissions) {
				p.when(accesstypes.Create, "CrewUsers", "new.disabled = true").grant(accesstypes.Create, "CrewUsers.username", "CrewUsers.password")
			},
			target: "/crew-users",
			body: func(*userFixture) string {
				return `[{"op":"add","path":"/","value":{"username":"newbie","password":"x"}}]`
			},
			wantCode: http.StatusForbidden,
		},
		{
			name: "a field without its Create grant is refused",
			grant: func(p *scriptedPermissions) {
				p.grant(accesstypes.Create, "CrewUsers", "CrewUsers.username", "CrewUsers.password")
			},
			target: "/crew-users",
			body: func(*userFixture) string {
				return `[{"op":"add","path":"/","value":{"username":"newbie","password":"x","disabled":true}}]`
			},
			wantCode: http.StatusForbidden,
		},
		{
			name:   "a dry run checks and adds nothing",
			grant:  fullGrant,
			target: "/crew-users",
			body: func(*userFixture) string {
				return `[{"op":"add","path":"/","value":{"username":"newbie","password":"x"}}]`
			},
			headers:  map[string]string{DryRunHeader: "true"},
			wantCode: http.StatusOK,
			check: func(t *testing.T, f *userFixture) {
				t.Helper()
				if len(f.accounts.users) != 4 {
					t.Errorf("%d users after the dry run, want 4", len(f.accounts.users))
				}
			},
		},
		{
			name:   "nobody deactivates themselves",
			grant:  fullGrant,
			target: "/crew-users",
			body: func(f *userFixture) string {
				return fmt.Sprintf(`[{"op":"patch","path":"/%s","value":{"disabled":true}}]`, f.user(t, "admin").ID)
			},
			wantCode: http.StatusForbidden,
		},
		{
			name:   "a renamed user's seats move with the name",
			grant:  fullGrant,
			target: "/crew-users",
			body: func(f *userFixture) string {
				return fmt.Sprintf(`[{"op":"patch","path":"/%s","value":{"username":"marshal2"}}]`, f.user(t, "marshal").ID)
			},
			wantCode: http.StatusOK,
			check: func(t *testing.T, f *userFixture) {
				t.Helper()
				if !slices.Contains(f.seats.held[everywhere]["Marshal"], "marshal2") || slices.Contains(f.seats.held[everywhere]["Marshal"], "marshal") {
					t.Errorf("the Marshal seat did not move: %v", f.seats.held[everywhere]["Marshal"])
				}
				if !slices.Contains(f.seats.held[accesstypes.DomainPolicyScope(bastion)]["Clerk"], "marshal2") {
					t.Errorf("the Clerk seat at bastion did not move: %v", f.seats.held[accesstypes.DomainPolicyScope(bastion)]["Clerk"])
				}
			},
		},
		{
			name:   "the password is not a field of the update",
			grant:  fullGrant,
			target: "/crew-users",
			body: func(f *userFixture) string {
				return fmt.Sprintf(`[{"op":"patch","path":"/%s","value":{"password":"x"}}]`, f.user(t, "clerk").ID)
			},
			wantCode: http.StatusBadRequest,
		},
		{
			name: "an update's condition is judged over the row and the change",
			grant: func(p *scriptedPermissions) {
				p.when(accesstypes.Update, "CrewUsers", "organization = 'acme'").grant(accesstypes.Update, "CrewUsers.disabled")
			},
			target: "/crew-users",
			body: func(f *userFixture) string {
				return fmt.Sprintf(`[{"op":"patch","path":"/%s","value":{"disabled":true}}]`, f.user(t, "clerk").ID)
			},
			wantCode: http.StatusForbidden,
		},
		{
			name:   "nobody deletes themselves",
			grant:  fullGrant,
			target: "/crew-users",
			body: func(f *userFixture) string {
				return fmt.Sprintf(`[{"op":"remove","path":"/%s"}]`, f.user(t, "admin").ID)
			},
			wantCode: http.StatusForbidden,
		},
		{
			name:   "a delete takes the seats with the account",
			grant:  fullGrant,
			target: "/crew-users",
			body: func(f *userFixture) string {
				return fmt.Sprintf(`[{"op":"remove","path":"/%s"}]`, f.user(t, "marshal").ID)
			},
			wantCode: http.StatusOK,
			check: func(t *testing.T, f *userFixture) {
				t.Helper()
				if len(f.accounts.users) != 3 {
					t.Errorf("%d users after the delete, want 3", len(f.accounts.users))
				}
				if slices.Contains(f.seats.held[everywhere]["Marshal"], "marshal") {
					t.Errorf("the deleted user still holds Marshal everywhere")
				}
			},
		},
		{
			name:   "a tenant create seats the new user in the sector",
			grant:  fullGrant,
			target: "/sectors/anvil/crew-sector-users",
			body: func(*userFixture) string {
				return `[{"op":"add","path":"/","value":{"username":"newbie","password":"x","role":"Pilot"}}]`
			},
			wantCode: http.StatusOK,
			check: func(t *testing.T, f *userFixture) {
				t.Helper()
				if !slices.Contains(f.seats.held[accesstypes.DomainPolicyScope(anvil)]["Pilot"], "newbie") {
					t.Errorf("newbie not seated as Pilot at anvil: %v", f.seats.held[accesstypes.DomainPolicyScope(anvil)])
				}
			},
		},
		{
			name: "a tenant create's seat is judged by the seats' condition",
			grant: func(p *scriptedPermissions) {
				fullGrant(p)
				p.when(accesstypes.Create, "CrewSectorSeats", "new.role IN ('Clerk')")
			},
			target: "/sectors/anvil/crew-sector-users",
			body: func(*userFixture) string {
				return `[{"op":"add","path":"/","value":{"username":"newbie","password":"x","role":"Pilot"}}]`
			},
			wantCode: http.StatusForbidden,
		},
		{
			name:   "a tenant create without a role is refused",
			grant:  fullGrant,
			target: "/sectors/anvil/crew-sector-users",
			body: func(*userFixture) string {
				return `[{"op":"add","path":"/","value":{"username":"newbie","password":"x"}}]`
			},
			wantCode: http.StatusBadRequest,
		},
		{
			name:   "a user outside the sector does not exist for the tenant partition",
			grant:  fullGrant,
			target: "/sectors/anvil/crew-sector-users",
			body: func(f *userFixture) string {
				return fmt.Sprintf(`[{"op":"patch","path":"/%s","value":{"disabled":true}}]`, f.user(t, "admin").ID)
			},
			wantCode: http.StatusNotFound,
		},
		{
			name:   "an account-level action on a user seated outside the sector is refused there",
			grant:  fullGrant,
			target: "/sectors/anvil/crew-sector-users",
			body: func(f *userFixture) string {
				return fmt.Sprintf(`[{"op":"patch","path":"/%s","value":{"disabled":true}}]`, f.user(t, "marshal").ID)
			},
			wantCode: http.StatusConflict,
		},
		{
			name:   "the sector's own user is deactivated from the sector",
			grant:  fullGrant,
			target: "/sectors/anvil/crew-sector-users",
			body: func(f *userFixture) string {
				return fmt.Sprintf(`[{"op":"patch","path":"/%s","value":{"disabled":true}}]`, f.user(t, "clerk").ID)
			},
			wantCode: http.StatusOK,
			check: func(t *testing.T, f *userFixture) {
				t.Helper()
				if !f.user(t, "clerk").Disabled {
					t.Errorf("clerk not deactivated")
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
			for name, value := range tt.headers {
				req.Header.Set(name, value)
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

// TestUserSurface_seatWrites pins the seats patch: the role's scope, the post-image
// condition a delegate's grant carries, and the rules.
func TestUserSurface_seatWrites(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		grant    func(p *scriptedPermissions)
		target   string
		body     func(f *userFixture) string
		wantCode int
		check    func(t *testing.T, f *userFixture)
	}{
		{
			name:   "a global seat of a domain-kind role is held everywhere",
			grant:  func(p *scriptedPermissions) { p.grant(accesstypes.Create, "CrewSeats") },
			target: "/crew-seats",
			body: func(f *userFixture) string {
				return fmt.Sprintf(`[{"op":"add","path":"/%s/Marshal","value":{}}]`, f.user(t, "clerk").ID)
			},
			wantCode: http.StatusOK,
			check: func(t *testing.T, f *userFixture) {
				t.Helper()
				if !slices.Contains(f.seats.held[everywhere]["Marshal"], "clerk") {
					t.Errorf("clerk not seated as Marshal everywhere")
				}
			},
		},
		{
			name:   "a global seat of a global-kind role is held globally",
			grant:  func(p *scriptedPermissions) { p.grant(accesstypes.Create, "CrewSeats") },
			target: "/crew-seats",
			body: func(f *userFixture) string {
				return fmt.Sprintf(`[{"op":"add","path":"/%s/Auditor","value":{}}]`, f.user(t, "clerk").ID)
			},
			wantCode: http.StatusOK,
			check: func(t *testing.T, f *userFixture) {
				t.Helper()
				if !slices.Contains(f.seats.held[global]["Auditor"], "clerk") {
					t.Errorf("clerk not seated as Auditor globally")
				}
			},
		},
		{
			name: "a delegate's grant limits the roles with the post-image",
			grant: func(p *scriptedPermissions) {
				p.when(accesstypes.Create, "CrewSeats", "new.role IN ('Pilot', 'Clerk')")
			},
			target: "/crew-seats",
			body: func(f *userFixture) string {
				return fmt.Sprintf(`[{"op":"add","path":"/%s/Marshal","value":{}}]`, f.user(t, "clerk").ID)
			},
			wantCode: http.StatusForbidden,
		},
		{
			name: "the same grant permits a listed role",
			grant: func(p *scriptedPermissions) {
				p.when(accesstypes.Create, "CrewSeats", "new.role IN ('Pilot', 'Clerk')")
			},
			target: "/crew-seats",
			body: func(f *userFixture) string {
				return fmt.Sprintf(`[{"op":"add","path":"/%s/Pilot","value":{}}]`, f.user(t, "clerk").ID)
			},
			wantCode: http.StatusOK,
		},
		{
			name:   "a delegate hands out only the roles they hold: new.role IN subject.ownRoles",
			grant:  func(p *scriptedPermissions) { p.when(accesstypes.Create, "CrewSeats", "new.role IN subject.ownRoles") },
			target: "/crew-seats",
			body: func(f *userFixture) string {
				return fmt.Sprintf(`[{"op":"add","path":"/%s/Marshal","value":{}}]`, f.user(t, "clerk").ID)
			},
			wantCode: http.StatusForbidden,
		},
		{
			name:   "the same grant permits a role the caller holds",
			grant:  func(p *scriptedPermissions) { p.when(accesstypes.Create, "CrewSeats", "new.role IN subject.ownRoles") },
			target: "/crew-seats",
			body: func(f *userFixture) string {
				return fmt.Sprintf(`[{"op":"add","path":"/%s/Administrator","value":{}}]`, f.user(t, "clerk").ID)
			},
			wantCode: http.StatusOK,
		},
		{
			name:   "a role that does not exist is refused",
			grant:  func(p *scriptedPermissions) { p.grant(accesstypes.Create, "CrewSeats") },
			target: "/crew-seats",
			body: func(f *userFixture) string {
				return fmt.Sprintf(`[{"op":"add","path":"/%s/Admiral","value":{}}]`, f.user(t, "clerk").ID)
			},
			wantCode: http.StatusBadRequest,
		},
		{
			name:   "a directory's user is not seated by hand",
			grant:  func(p *scriptedPermissions) { p.grant(accesstypes.Create, "CrewSeats") },
			target: "/crew-seats",
			body: func(f *userFixture) string {
				return fmt.Sprintf(`[{"op":"add","path":"/%s/Marshal","value":{}}]`, f.user(t, "pilot").ID)
			},
			wantCode: http.StatusConflict,
		},
		{
			name:   "nobody unseats themselves",
			grant:  func(p *scriptedPermissions) { p.grant(accesstypes.Delete, "CrewSeats") },
			target: "/crew-seats",
			body: func(f *userFixture) string {
				return fmt.Sprintf(`[{"op":"remove","path":"/%s/Administrator"}]`, f.user(t, "admin").ID)
			},
			wantCode: http.StatusForbidden,
		},
		{
			name:   "an unseat removes the seat where it is held",
			grant:  func(p *scriptedPermissions) { p.grant(accesstypes.Delete, "CrewSeats") },
			target: "/crew-seats",
			body: func(f *userFixture) string {
				return fmt.Sprintf(`[{"op":"remove","path":"/%s/Marshal"}]`, f.user(t, "marshal").ID)
			},
			wantCode: http.StatusOK,
			check: func(t *testing.T, f *userFixture) {
				t.Helper()
				if slices.Contains(f.seats.held[everywhere]["Marshal"], "marshal") {
					t.Errorf("marshal still seated")
				}
			},
		},
		{
			name:   "an unseat's condition is judged over the seat",
			grant:  func(p *scriptedPermissions) { p.when(accesstypes.Delete, "CrewSeats", "role IN ('Pilot')") },
			target: "/crew-seats",
			body: func(f *userFixture) string {
				return fmt.Sprintf(`[{"op":"remove","path":"/%s/Marshal"}]`, f.user(t, "marshal").ID)
			},
			wantCode: http.StatusForbidden,
		},
		{
			name:   "a tenant seat is held in the sector",
			grant:  func(p *scriptedPermissions) { p.grant(accesstypes.Create, "CrewSectorSeats") },
			target: "/sectors/anvil/crew-sector-seats",
			body: func(f *userFixture) string {
				return fmt.Sprintf(`[{"op":"add","path":"/%s/Pilot","value":{}}]`, f.user(t, "clerk").ID)
			},
			wantCode: http.StatusOK,
			check: func(t *testing.T, f *userFixture) {
				t.Helper()
				if !slices.Contains(f.seats.held[accesstypes.DomainPolicyScope(anvil)]["Pilot"], "clerk") {
					t.Errorf("clerk not seated as Pilot at anvil")
				}
			},
		},
		{
			name:   "a tenant seat for a user outside the sector is refused",
			grant:  func(p *scriptedPermissions) { p.grant(accesstypes.Create, "CrewSectorSeats") },
			target: "/sectors/anvil/crew-sector-seats",
			body: func(f *userFixture) string {
				return fmt.Sprintf(`[{"op":"add","path":"/%s/Pilot","value":{}}]`, f.user(t, "admin").ID)
			},
			wantCode: http.StatusBadRequest,
		},
		{
			name:   "a seat held everywhere is not removed from a sector",
			grant:  func(p *scriptedPermissions) { p.grant(accesstypes.Delete, "CrewSectorSeats") },
			target: "/sectors/anvil/crew-sector-seats",
			body: func(f *userFixture) string {
				return fmt.Sprintf(`[{"op":"remove","path":"/%s/Marshal"}]`, f.user(t, "marshal").ID)
			},
			wantCode: http.StatusConflict,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newUserFixture(t)
			tt.grant(f.perms)

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

// TestUserSurface_methods pins the password reset and the unlinking.
func TestUserSurface_methods(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		grant    func(p *scriptedPermissions)
		target   string
		body     func(f *userFixture) string
		wantCode int
		check    func(t *testing.T, f *userFixture)
	}{
		{
			name:   "a reset sets the temporary password",
			grant:  func(p *scriptedPermissions) { p.grant(accesstypes.Execute, "SetCrewUserPassword") },
			target: "/set-crew-user-password",
			body: func(f *userFixture) string {
				return fmt.Sprintf(`{"user":%q,"password":"temporary-2"}`, f.user(t, "clerk").ID)
			},
			wantCode: http.StatusOK,
			check: func(t *testing.T, f *userFixture) {
				t.Helper()
				if got := f.accounts.resets[f.user(t, "clerk").ID]; got != "temporary-2" {
					t.Errorf("reset password = %q", got)
				}
			},
		},
		{
			name:     "one's own password is changed, not reset",
			grant:    func(p *scriptedPermissions) { p.grant(accesstypes.Execute, "SetCrewUserPassword") },
			target:   "/set-crew-user-password",
			body:     func(f *userFixture) string { return fmt.Sprintf(`{"user":%q,"password":"x"}`, f.user(t, "admin").ID) },
			wantCode: http.StatusBadRequest,
		},
		{
			name: "the method's condition is judged over the target",
			grant: func(p *scriptedPermissions) {
				p.when(accesstypes.Execute, "SetCrewUserPassword", "organization = 'acme'")
			},
			target:   "/set-crew-user-password",
			body:     func(f *userFixture) string { return fmt.Sprintf(`{"user":%q,"password":"x"}`, f.user(t, "clerk").ID) },
			wantCode: http.StatusForbidden,
		},
		{
			name:     "no Execute is Forbidden",
			grant:    func(*scriptedPermissions) {},
			target:   "/set-crew-user-password",
			body:     func(f *userFixture) string { return fmt.Sprintf(`{"user":%q,"password":"x"}`, f.user(t, "clerk").ID) },
			wantCode: http.StatusForbidden,
		},
		{
			name:   "the only way in is never unlinked",
			grant:  func(p *scriptedPermissions) { p.grant(accesstypes.Execute, "UnlinkCrewUserIdentity") },
			target: "/unlink-crew-user-identity",
			body: func(f *userFixture) string {
				pilot := f.user(t, "pilot")

				return fmt.Sprintf(`{"user":%q,"identity":%q}`, pilot.ID, f.accounts.identities[pilot.ID][0].ID)
			},
			wantCode: http.StatusConflict,
		},
		{
			name:   "an identity is unlinked where another way in remains",
			grant:  func(p *scriptedPermissions) { p.grant(accesstypes.Execute, "UnlinkCrewUserIdentity") },
			target: "/unlink-crew-user-identity",
			body: func(f *userFixture) string {
				pilot := f.user(t, "pilot")
				pilot.HasPassword = true

				return fmt.Sprintf(`{"user":%q,"identity":%q}`, pilot.ID, f.accounts.identities[pilot.ID][0].ID)
			},
			wantCode: http.StatusOK,
			check: func(t *testing.T, f *userFixture) {
				t.Helper()
				if len(f.accounts.unlinked) != 1 {
					t.Errorf("unlinked %d identities, want 1", len(f.accounts.unlinked))
				}
			},
		},
		{
			name:     "a tenant reset on a user seated outside the sector is refused",
			grant:    func(p *scriptedPermissions) { p.grant(accesstypes.Execute, "SetCrewSectorUserPassword") },
			target:   "/sectors/anvil/set-crew-sector-user-password",
			body:     func(f *userFixture) string { return fmt.Sprintf(`{"user":%q,"password":"x"}`, f.user(t, "marshal").ID) },
			wantCode: http.StatusConflict,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newUserFixture(t)
			tt.grant(f.perms)

			code, body := f.do(t, http.MethodPost, tt.target, tt.body(f))
			if code != tt.wantCode {
				t.Fatalf("status = %d, want %d: %s", code, tt.wantCode, body)
			}
			if tt.check != nil {
				tt.check(t, f)
			}
		})
	}
}

// TestUserSurfaceDeclaration_Names pins the names every consumer spells alike.
func TestUserSurfaceDeclaration_Names(t *testing.T) {
	t.Parallel()

	decl := testUserAuth{}.UserSurfaceDeclaration()
	names := decl.Names()
	want := UserResourceNames{
		Global: UserPartitionNames{
			Users: "CrewUsers", Seats: "CrewSeats", Roles: "CrewRoles", SetPassword: "SetCrewUserPassword", UnlinkIdentity: "UnlinkCrewUserIdentity",
			UsersRoute: "crew-users", SeatsRoute: "crew-seats", RolesRoute: "crew-roles", SetPasswordRoute: "set-crew-user-password", UnlinkIdentityRoute: "unlink-crew-user-identity",
			UserParam: "crewUserID", SeatUserParam: "crewSeatUser", SeatRoleParam: "crewSeatRole", RoleParam: "crewRoleName",
		},
		Tenant: UserPartitionNames{
			Tenanted: true,
			Users:    "CrewSectorUsers", Seats: "CrewSectorSeats", Roles: "CrewSectorRoles", SetPassword: "SetCrewSectorUserPassword", UnlinkIdentity: "UnlinkCrewSectorUserIdentity",
			UsersRoute: "crew-sector-users", SeatsRoute: "crew-sector-seats", RolesRoute: "crew-sector-roles", SetPasswordRoute: "set-crew-sector-user-password", UnlinkIdentityRoute: "unlink-crew-sector-user-identity",
			UserParam: "crewSectorUserID", SeatUserParam: "crewSectorSeatUser", SeatRoleParam: "crewSectorSeatRole", RoleParam: "crewSectorRoleName",
		},
	}
	if diff := cmp.Diff(want, names); diff != "" {
		t.Errorf("names mismatch (-want +got):\n%s", diff)
	}
}

// TestRegisterUserSurface_collection pins that the registered surfaces pass the
// collection's own validation: the attributes and subject bindings of a resource share
// one namespace, the subject vocabulary is declared once, and a second Auth's surface
// registers beside the first without a name in common.
func TestRegisterUserSurface_collection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		decls []UserSurfaceDeclaration
	}{
		{
			name:  "one auth with a password and a directory, tenanted",
			decls: []UserSurfaceDeclaration{{Auth: "Crew", Tenant: "Sector", DomainParam: "sectorID", Shape: UserSurfaceShape{Password: true, Registrations: []UserRegistration{{Name: "google", Kind: "google", Label: "Google", SyncsRoles: true}}}}},
		},
		{
			name: "two auths, the second with a directory alone, no tenancy",
			decls: []UserSurfaceDeclaration{
				{Auth: "Crew", Shape: UserSurfaceShape{Password: true}},
				{Auth: "Members", Shape: UserSurfaceShape{Registrations: []UserRegistration{{Name: "directory", Kind: "google", Label: "Company directory", Invited: true}}}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			b := NewCollectionBuilder()
			for i := range tt.decls {
				if err := RegisterUserSurface(b, &tt.decls[i], i == 0); err != nil {
					t.Fatalf("RegisterUserSurface(%s) error = %v", tt.decls[i].Auth, err)
				}
			}
			if _, err := NewGeneratedCollection(b.Data()); err != nil {
				t.Fatalf("NewGeneratedCollection() error = %v", err)
			}
		})
	}
}
