package generation

import (
	"fmt"
	"go/format"
	"strings"
	"testing"

	"github.com/cccteam/ccc/resource"
	"github.com/google/go-cmp/cmp"
)

// The user-management surfaces' fixture: the crew auth on the default outlet (password
// and a role-syncing Google registration), the members auth on the portal outlet (an
// invited-users directory registration), the default outlet managing members, and an
// API-key outlet serving no sessions. Tenanted, the Ship record is the tenant.
const (
	crewAuthPath    = "example.com/acme/beacon/pkg/auth/crew"
	membersAuthPath = "example.com/acme/beacon/pkg/auth/members"
	fleetDirectory  = "Fleet directory"
)

func userFixtureGenerator(t *testing.T, tenanted bool) *resourceGenerator {
	t.Helper()

	crew := &outletAuth{importPath: crewAuthPath, methods: []AuthMethod{PasswordMethod(), GoogleMethod("google").Labeled(fleetDirectory)}}
	members := &outletAuth{importPath: membersAuthPath, methods: []AuthMethod{AzureMethod("directory").Labeled("Company directory").InvitedUsers()}}
	r := &resourceGenerator{
		client:             &client{},
		genRoutes:          true,
		routePrefix:        "api",
		domainRouteSegment: "ships",
		domainRouteParam:   "shipID",
		defaultOutlet:      routerOutlet{auth: crew, manages: []string{membersAuthPath}},
		extraOutlets: []routerOutlet{
			{name: "droids", prefix: "droids", apiKey: true},
			{name: "portal", prefix: "portal/api", servesSessions: true, auth: members},
		},
	}
	if tenanted {
		_, structs := featureFixtureClient(t)
		r.resources = []*resourceInfo{fixtureResource(t, structs, "Ship", func(res *resourceInfo) { res.IsTenant = true })}
	}

	return r
}

// Test_userSurfaces pins the derivation: one surface per auth package, named after the
// package, served on its own outlet and on every outlet managing it; the shape from the
// methods; the tenant partition from the tenant record; the refusals of a Manages that
// names no auth or the outlet's own, and of two auth packages sharing a name.
func Test_userSurfaces(t *testing.T) {
	t.Parallel()

	type want struct {
		auth, typeName, accessor string
		outlets                  []string
		password                 bool
		registrations            []resource.UserRegistration
		tenant, param            string
	}
	tests := []struct {
		name      string
		generator func(t *testing.T) *resourceGenerator
		want      []want
		wantErr   string
	}{
		{
			name: "no routes: no surfaces",
			generator: func(t *testing.T) *resourceGenerator {
				t.Helper()
				r := userFixtureGenerator(t, false)
				r.genRoutes = false

				return r
			},
		},
		{
			name:      "two auths, the default outlet managing members, no tenancy",
			generator: func(t *testing.T) *resourceGenerator { t.Helper(); return userFixtureGenerator(t, false) },
			want: []want{
				{auth: "Crew", typeName: "CrewAuth", accessor: "CrewUserSurface", outlets: []string{"default"}, password: true, registrations: []resource.UserRegistration{{Name: "google", Kind: "google", Label: fleetDirectory, SyncsRoles: false}}},
				{auth: "Members", typeName: "MembersAuth", accessor: "MembersUserSurface", outlets: []string{"portal", "default"}, registrations: []resource.UserRegistration{{Name: "directory", Kind: "azure", Label: "Company directory", Invited: true}}},
			},
		},
		{
			name:      "tenancy gives every surface the tenant partition under the route's parameter",
			generator: func(t *testing.T) *resourceGenerator { t.Helper(); return userFixtureGenerator(t, true) },
			want: []want{
				{auth: "Crew", typeName: "CrewAuth", accessor: "CrewUserSurface", outlets: []string{"default"}, password: true, registrations: []resource.UserRegistration{{Name: "google", Kind: "google", Label: fleetDirectory, SyncsRoles: false}}, tenant: "Ship", param: "shipID"},
				{auth: "Members", typeName: "MembersAuth", accessor: "MembersUserSurface", outlets: []string{"portal", "default"}, registrations: []resource.UserRegistration{{Name: "directory", Kind: "azure", Label: "Company directory", Invited: true}}, tenant: "Ship", param: "shipID"},
			},
		},
		{
			name: "Manages naming no declared auth is refused",
			generator: func(t *testing.T) *resourceGenerator {
				t.Helper()
				r := userFixtureGenerator(t, false)
				r.extraOutlets[1].auth = nil

				return r
			},
			wantErr: `outlet "default": Manages("example.com/acme/beacon/pkg/auth/members") names no declared auth`,
		},
		{
			name: "Manages naming the outlet's own auth is refused",
			generator: func(t *testing.T) *resourceGenerator {
				t.Helper()
				r := userFixtureGenerator(t, false)
				r.defaultOutlet.manages = []string{crewAuthPath}

				return r
			},
			wantErr: `outlet "default": Manages("example.com/acme/beacon/pkg/auth/crew") names the outlet's own auth`,
		},
		{
			name: "two auth packages sharing a name are refused",
			generator: func(t *testing.T) *resourceGenerator {
				t.Helper()
				r := userFixtureGenerator(t, false)
				r.defaultOutlet.manages = nil
				r.extraOutlets[1].auth = &outletAuth{importPath: "example.com/acme/other/crew", methods: []AuthMethod{PasswordMethod()}}

				return r
			},
			wantErr: "both name their surface Crew",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			surfaces, err := tt.generator(t).userSurfaces()
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("userSurfaces() error = %v, want one containing %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("userSurfaces() error = %v", err)
			}
			var got []want
			for _, s := range surfaces {
				got = append(got, want{
					auth: s.decl.Auth, typeName: s.typeName, accessor: s.accessor, outlets: s.outlets,
					password: s.decl.Shape.Password, registrations: s.decl.Shape.Registrations,
					tenant: s.decl.Tenant, param: string(s.decl.DomainParam),
				})
				if s.names.Tenant.Tenanted != (s.decl.Tenant != "") {
					t.Errorf("%s: names.Tenant.Tenanted = %v with Tenant %q", s.decl.Auth, s.names.Tenant.Tenanted, s.decl.Tenant)
				}
			}
			if diff := cmp.Diff(tt.want, got, cmp.AllowUnexported(want{})); diff != "" {
				t.Errorf("userSurfaces() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// Test_rejectReservedUserNames pins the refusal of an application registration taking a
// surface's name.
func Test_rejectReservedUserNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		manual  []ManualRegistration
		wantErr string
	}{
		{name: "nothing clashes", manual: []ManualRegistration{{Resource: "Ships"}}},
		{name: "a manual registration taking a global surface resource", manual: []ManualRegistration{{Resource: "CrewRoles"}}, wantErr: `manual registration of CrewRoles takes the name of a resource the Crew auth's user-management surface serves (Auth("example.com/acme/beacon/pkg/auth/crew")); rename it`},
		{name: "a manual registration taking a tenant surface method", manual: []ManualRegistration{{Resource: "SetMembersShipUserPassword"}}, wantErr: "the Members auth's user-management surface"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := userFixtureGenerator(t, true)
			r.manualRegistrations = tt.manual
			surfaces, err := r.userSurfaces()
			if err != nil {
				t.Fatalf("userSurfaces() error = %v", err)
			}
			err = r.rejectReservedUserNames(surfaces)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("rejectReservedUserNames() error = %v, want nil", err)
				}

				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("rejectReservedUserNames() error = %v, want one containing %q", err, tt.wantErr)
			}
		})
	}
}

// routeLine spells a route for comparison: method, path, handler, the dispatch test's
// URL and, domain-scoped, a marker.
func routeLine(route *generatedRoute) string {
	line := fmt.Sprintf("%s %s %s -> %s", route.Method, route.Path, route.HandlerFunc, route.TestURL)
	if route.DomainScoped {
		line += " (domain)"
	}

	return line
}

// Test_userRoutes pins a surface's routes under a prefix: each partition's users, seats
// and roles (list, read with the read parameters, patch where written), then its
// methods; the tenant partition under the domain pair, every route of it domain-scoped.
func Test_userRoutes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		tenanted bool
		surface  string
		prefix   string
		want     []string
	}{
		{
			name: "the crew surface, no tenancy, on the default prefix", surface: crewAuthPath, prefix: "api",
			want: []string{
				"GET /api/crew-users CrewUsers -> /api/crew-users",
				"GET /api/crew-users/{crewUserID} CrewUser -> /api/crew-users/testCrewUserID",
				"PATCH /api/crew-users PatchCrewUsers -> /api/crew-users",
				"GET /api/crew-seats CrewSeats -> /api/crew-seats",
				"GET /api/crew-seats/{crewSeatUser}/{crewSeatRole} CrewSeat -> /api/crew-seats/testCrewSeatUser/testCrewSeatRole",
				"PATCH /api/crew-seats PatchCrewSeats -> /api/crew-seats",
				"GET /api/crew-roles CrewRoles -> /api/crew-roles",
				"GET /api/crew-roles/{crewRoleName} CrewRole -> /api/crew-roles/testCrewRoleName",
				"GET /api/crew-invitations CrewInvitations -> /api/crew-invitations",
				"GET /api/crew-invitations/{crewInvitationID} CrewInvitation -> /api/crew-invitations/testCrewInvitationID",
				"PATCH /api/crew-invitations PatchCrewInvitations -> /api/crew-invitations",
				"POST /api/set-crew-user-password SetCrewUserPassword -> /api/set-crew-user-password",
				"POST /api/unlink-crew-user-identity UnlinkCrewUserIdentity -> /api/unlink-crew-user-identity",
			},
		},
		{
			name: "the members surface, tenanted, on the portal prefix: no password method, the tenant partition under the domain pair", tenanted: true, surface: membersAuthPath, prefix: "portal/api",
			want: []string{
				"GET /portal/api/members-users MembersUsers -> /portal/api/members-users",
				"GET /portal/api/members-users/{membersUserID} MembersUser -> /portal/api/members-users/testMembersUserID",
				"PATCH /portal/api/members-users PatchMembersUsers -> /portal/api/members-users",
				"GET /portal/api/members-seats MembersSeats -> /portal/api/members-seats",
				"GET /portal/api/members-seats/{membersSeatUser}/{membersSeatRole} MembersSeat -> /portal/api/members-seats/testMembersSeatUser/testMembersSeatRole",
				"PATCH /portal/api/members-seats PatchMembersSeats -> /portal/api/members-seats",
				"GET /portal/api/members-roles MembersRoles -> /portal/api/members-roles",
				"GET /portal/api/members-roles/{membersRoleName} MembersRole -> /portal/api/members-roles/testMembersRoleName",
				"GET /portal/api/members-invitations MembersInvitations -> /portal/api/members-invitations",
				"GET /portal/api/members-invitations/{membersInvitationID} MembersInvitation -> /portal/api/members-invitations/testMembersInvitationID",
				"PATCH /portal/api/members-invitations PatchMembersInvitations -> /portal/api/members-invitations",
				"POST /portal/api/unlink-members-user-identity UnlinkMembersUserIdentity -> /portal/api/unlink-members-user-identity",
				"GET /portal/api/ships/{shipID}/members-ship-users MembersShipUsers -> /portal/api/ships/testDomain/members-ship-users (domain)",
				"GET /portal/api/ships/{shipID}/members-ship-users/{membersShipUserID} MembersShipUser -> /portal/api/ships/testDomain/members-ship-users/testMembersShipUserID (domain)",
				"PATCH /portal/api/ships/{shipID}/members-ship-users PatchMembersShipUsers -> /portal/api/ships/testDomain/members-ship-users (domain)",
				"GET /portal/api/ships/{shipID}/members-ship-seats MembersShipSeats -> /portal/api/ships/testDomain/members-ship-seats (domain)",
				"GET /portal/api/ships/{shipID}/members-ship-seats/{membersShipSeatUser}/{membersShipSeatRole} MembersShipSeat -> /portal/api/ships/testDomain/members-ship-seats/testMembersShipSeatUser/testMembersShipSeatRole (domain)",
				"PATCH /portal/api/ships/{shipID}/members-ship-seats PatchMembersShipSeats -> /portal/api/ships/testDomain/members-ship-seats (domain)",
				"GET /portal/api/ships/{shipID}/members-ship-roles MembersShipRoles -> /portal/api/ships/testDomain/members-ship-roles (domain)",
				"GET /portal/api/ships/{shipID}/members-ship-roles/{membersShipRoleName} MembersShipRole -> /portal/api/ships/testDomain/members-ship-roles/testMembersShipRoleName (domain)",
				"GET /portal/api/ships/{shipID}/members-ship-invitations MembersShipInvitations -> /portal/api/ships/testDomain/members-ship-invitations (domain)",
				"GET /portal/api/ships/{shipID}/members-ship-invitations/{membersShipInvitationID} MembersShipInvitation -> /portal/api/ships/testDomain/members-ship-invitations/testMembersShipInvitationID (domain)",
				"PATCH /portal/api/ships/{shipID}/members-ship-invitations PatchMembersShipInvitations -> /portal/api/ships/testDomain/members-ship-invitations (domain)",
				"POST /portal/api/ships/{shipID}/unlink-members-ship-user-identity UnlinkMembersShipUserIdentity -> /portal/api/ships/testDomain/unlink-members-ship-user-identity (domain)",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := userFixtureGenerator(t, tt.tenanted)
			surfaces, err := r.userSurfaces()
			if err != nil {
				t.Fatalf("userSurfaces() error = %v", err)
			}
			var surface *userSurface
			for _, s := range surfaces {
				if s.importPath == tt.surface {
					surface = s
				}
			}
			if surface == nil {
				t.Fatalf("no surface for %s", tt.surface)
			}
			var got []string
			for _, ur := range r.userRoutes(surface, tt.prefix) {
				got = append(got, routeLine(ur.route))
				if ur.route.DomainScoped {
					if len(ur.route.TestParams) == 0 || ur.route.TestParams[0] != (routeTestParam{Key: "shipID", Value: domainTestValue}) {
						t.Errorf("%s: domain route's test params = %+v, want the domain parameter first", ur.route.Path, ur.route.TestParams)
					}
				}
				wantGroup := ur.route.HandlerFunc
				switch ur.route.HandlerType {
				case ReadHandler:
					wantGroup += "s"
				case PatchHandler:
					wantGroup = strings.TrimPrefix(wantGroup, "Patch")
				default:
				}
				if ur.group != wantGroup {
					t.Errorf("%s %s: filed under %q, want %q", ur.route.Method, ur.route.Path, ur.group, wantGroup)
				}
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("userRoutes() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// Test_userRouteParams pins the parameter names the dispatch test's stubs record: each
// partition's four read parameters, surface by surface.
func Test_userRouteParams(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		tenanted bool
		want     []string
	}{
		{name: "no tenancy: the global partitions' parameters", want: []string{"crewUserID", "crewSeatUser", "crewSeatRole", "crewRoleName", "crewInvitationID", "membersUserID", "membersSeatUser", "membersSeatRole", "membersRoleName", "membersInvitationID"}},
		{name: "tenancy adds the tenant partitions'", tenanted: true, want: []string{
			"crewUserID", "crewSeatUser", "crewSeatRole", "crewRoleName", "crewInvitationID", "crewShipUserID", "crewShipSeatUser", "crewShipSeatRole", "crewShipRoleName", "crewShipInvitationID",
			"membersUserID", "membersSeatUser", "membersSeatRole", "membersRoleName", "membersInvitationID", "membersShipUserID", "membersShipSeatUser", "membersShipSeatRole", "membersShipRoleName", "membersShipInvitationID",
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := userFixtureGenerator(t, tt.tenanted).userRouteParams()
			if err != nil {
				t.Fatalf("userRouteParams() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("userRouteParams() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// Test_accumulateUserRoutes pins the mounting: each surface's routes join the RoutesMap
// of every outlet serving it under the resource or method they belong to, the managed
// members surface on the portal and the default outlet, nothing on the API-key outlet,
// and every mounted route joins the dispatch test.
func Test_accumulateUserRoutes(t *testing.T) {
	t.Parallel()

	r := userFixtureGenerator(t, false)
	outlets := r.allOutlets()
	outletRoutes := make([]*outletRouteData, len(outlets))
	for i, outlet := range outlets {
		outletRoutes[i] = &outletRouteData{Name: outlet.name, RoutesMap: make(map[string][]*generatedRoute)}
	}
	testRoutes, err := r.accumulateUserRoutes(outlets, outletRoutes)
	if err != nil {
		t.Fatalf("accumulateUserRoutes() error = %v", err)
	}

	groups := func(data *outletRouteData) map[string]int {
		counts := make(map[string]int, len(data.RoutesMap))
		for group, routes := range data.RoutesMap {
			counts[group] = len(routes)
		}

		return counts
	}
	wantDefault := map[string]int{
		"CrewUsers": 3, "CrewSeats": 3, "CrewRoles": 2, "CrewInvitations": 3, "SetCrewUserPassword": 1, "UnlinkCrewUserIdentity": 1,
		"MembersUsers": 3, "MembersSeats": 3, "MembersRoles": 2, "MembersInvitations": 3, "UnlinkMembersUserIdentity": 1,
	}
	if diff := cmp.Diff(wantDefault, groups(outletRoutes[0])); diff != "" {
		t.Errorf("default outlet's groups mismatch (-want +got):\n%s", diff)
	}
	if got := groups(outletRoutes[1]); len(got) != 0 {
		t.Errorf("droids outlet's groups = %v, want none", got)
	}
	wantPortal := map[string]int{"MembersUsers": 3, "MembersSeats": 3, "MembersRoles": 2, "MembersInvitations": 3, "UnlinkMembersUserIdentity": 1}
	if diff := cmp.Diff(wantPortal, groups(outletRoutes[2])); diff != "" {
		t.Errorf("portal outlet's groups mismatch (-want +got):\n%s", diff)
	}
	if len(testRoutes) != 25+12 {
		t.Errorf("dispatch test routes = %d, want 37 (the default outlet's 25 and the portal's 12)", len(testRoutes))
	}
	for _, route := range outletRoutes[2].RoutesMap["MembersUsers"] {
		if !strings.HasPrefix(route.Path, "/portal/api/members-users") {
			t.Errorf("portal route %s is not under the portal prefix", route.Path)
		}
	}
}

// Test_userNegativeTests pins the isolation cases: an outlet gets a 404 case, per test
// method, for every route of a surface it does not serve, addressed under its own
// prefix; the outlet serving every surface gets none.
func Test_userNegativeTests(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		outlet int
		want   []negativeRouterTest
	}{
		{
			name:   "the API-key outlet 404s every surface's routes under its prefix",
			outlet: 1,
			want: []negativeRouterTest{
				{Method: "http.MethodGet", URL: "/droids/crew-users"},
				{Method: "http.MethodPost", URL: "/droids/crew-users"},
				{Method: "http.MethodGet", URL: "/droids/crew-users/testCrewUserID"},
				{Method: "http.MethodPost", URL: "/droids/crew-users/testCrewUserID"},
				{Method: "http.MethodPatch", URL: "/droids/crew-users"},
				{Method: "http.MethodGet", URL: "/droids/crew-seats"},
				{Method: "http.MethodPost", URL: "/droids/crew-seats"},
				{Method: "http.MethodGet", URL: "/droids/crew-seats/testCrewSeatUser/testCrewSeatRole"},
				{Method: "http.MethodPost", URL: "/droids/crew-seats/testCrewSeatUser/testCrewSeatRole"},
				{Method: "http.MethodPatch", URL: "/droids/crew-seats"},
				{Method: "http.MethodGet", URL: "/droids/crew-roles"},
				{Method: "http.MethodPost", URL: "/droids/crew-roles"},
				{Method: "http.MethodGet", URL: "/droids/crew-roles/testCrewRoleName"},
				{Method: "http.MethodPost", URL: "/droids/crew-roles/testCrewRoleName"},
				{Method: "http.MethodGet", URL: "/droids/crew-invitations"},
				{Method: "http.MethodPost", URL: "/droids/crew-invitations"},
				{Method: "http.MethodGet", URL: "/droids/crew-invitations/testCrewInvitationID"},
				{Method: "http.MethodPost", URL: "/droids/crew-invitations/testCrewInvitationID"},
				{Method: "http.MethodPatch", URL: "/droids/crew-invitations"},
				{Method: "http.MethodPost", URL: "/droids/set-crew-user-password"},
				{Method: "http.MethodPost", URL: "/droids/unlink-crew-user-identity"},
				{Method: "http.MethodGet", URL: "/droids/members-users"},
				{Method: "http.MethodPost", URL: "/droids/members-users"},
				{Method: "http.MethodGet", URL: "/droids/members-users/testMembersUserID"},
				{Method: "http.MethodPost", URL: "/droids/members-users/testMembersUserID"},
				{Method: "http.MethodPatch", URL: "/droids/members-users"},
				{Method: "http.MethodGet", URL: "/droids/members-seats"},
				{Method: "http.MethodPost", URL: "/droids/members-seats"},
				{Method: "http.MethodGet", URL: "/droids/members-seats/testMembersSeatUser/testMembersSeatRole"},
				{Method: "http.MethodPost", URL: "/droids/members-seats/testMembersSeatUser/testMembersSeatRole"},
				{Method: "http.MethodPatch", URL: "/droids/members-seats"},
				{Method: "http.MethodGet", URL: "/droids/members-roles"},
				{Method: "http.MethodPost", URL: "/droids/members-roles"},
				{Method: "http.MethodGet", URL: "/droids/members-roles/testMembersRoleName"},
				{Method: "http.MethodPost", URL: "/droids/members-roles/testMembersRoleName"},
				{Method: "http.MethodGet", URL: "/droids/members-invitations"},
				{Method: "http.MethodPost", URL: "/droids/members-invitations"},
				{Method: "http.MethodGet", URL: "/droids/members-invitations/testMembersInvitationID"},
				{Method: "http.MethodPost", URL: "/droids/members-invitations/testMembersInvitationID"},
				{Method: "http.MethodPatch", URL: "/droids/members-invitations"},
				{Method: "http.MethodPost", URL: "/droids/unlink-members-user-identity"},
			},
		},
		{
			name:   "the portal 404s the crew surface it does not serve",
			outlet: 2,
			want: []negativeRouterTest{
				{Method: "http.MethodGet", URL: "/portal/api/crew-users"},
				{Method: "http.MethodPost", URL: "/portal/api/crew-users"},
				{Method: "http.MethodGet", URL: "/portal/api/crew-users/testCrewUserID"},
				{Method: "http.MethodPost", URL: "/portal/api/crew-users/testCrewUserID"},
				{Method: "http.MethodPatch", URL: "/portal/api/crew-users"},
				{Method: "http.MethodGet", URL: "/portal/api/crew-seats"},
				{Method: "http.MethodPost", URL: "/portal/api/crew-seats"},
				{Method: "http.MethodGet", URL: "/portal/api/crew-seats/testCrewSeatUser/testCrewSeatRole"},
				{Method: "http.MethodPost", URL: "/portal/api/crew-seats/testCrewSeatUser/testCrewSeatRole"},
				{Method: "http.MethodPatch", URL: "/portal/api/crew-seats"},
				{Method: "http.MethodGet", URL: "/portal/api/crew-roles"},
				{Method: "http.MethodPost", URL: "/portal/api/crew-roles"},
				{Method: "http.MethodGet", URL: "/portal/api/crew-roles/testCrewRoleName"},
				{Method: "http.MethodPost", URL: "/portal/api/crew-roles/testCrewRoleName"},
				{Method: "http.MethodGet", URL: "/portal/api/crew-invitations"},
				{Method: "http.MethodPost", URL: "/portal/api/crew-invitations"},
				{Method: "http.MethodGet", URL: "/portal/api/crew-invitations/testCrewInvitationID"},
				{Method: "http.MethodPost", URL: "/portal/api/crew-invitations/testCrewInvitationID"},
				{Method: "http.MethodPatch", URL: "/portal/api/crew-invitations"},
				{Method: "http.MethodPost", URL: "/portal/api/set-crew-user-password"},
				{Method: "http.MethodPost", URL: "/portal/api/unlink-crew-user-identity"},
			},
		},
		{name: "the default outlet serves both surfaces and gets no case", outlet: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := userFixtureGenerator(t, false)
			outlet := r.allOutlets()[tt.outlet]
			got, err := r.userNegativeTests(outlet)
			if err != nil {
				t.Fatalf("userNegativeTests() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("userNegativeTests() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// Test_userAuthzCases pins the matrix's cases: the lists and reads as granted-and-denied
// query pairs with the placeholder id or key in the read's URL, the patches denied-only
// with a remove on the placeholder row, each method denied-only with its dry run, the
// tenant partition's cases keeping the dispatch test's domain, and a non-default outlet
// naming itself.
func Test_userAuthzCases(t *testing.T) {
	t.Parallel()

	dryRun := []authzHeader{{Name: resource.DryRunHeader, Value: jsonTrueLiteral}}
	r := userFixtureGenerator(t, true)
	got, err := r.userAuthzCases()
	if err != nil {
		t.Fatalf("userAuthzCases() error = %v", err)
	}
	byName := make(map[string][]authzCase)
	for _, c := range got {
		byName[c.Name] = append(byName[c.Name], c)
	}

	tests := []struct {
		name string
		want []authzCase
	}{
		{name: "CrewUsers", want: []authzCase{{Name: "CrewUsers", Method: "http.MethodGet", URL: "/api/crew-users", Permission: "List"}}},
		{name: "CrewUser", want: []authzCase{{Name: "CrewUser", Method: "http.MethodGet", URL: "/api/crew-users/" + userAuthzID, Permission: "Read"}}},
		{name: "PatchCrewUsers", want: []authzCase{{Name: "PatchCrewUsers", Method: "http.MethodPatch", URL: "/api/crew-users", Body: `[{"op":"remove","path":"/` + userAuthzID + `"}]`, DeniedOnly: true}}},
		{name: "CrewSeat", want: []authzCase{{Name: "CrewSeat", Method: "http.MethodGet", URL: "/api/crew-seats/" + userAuthzID + "/" + userAuthzRole, Permission: "Read"}}},
		{name: "PatchCrewSeats", want: []authzCase{{Name: "PatchCrewSeats", Method: "http.MethodPatch", URL: "/api/crew-seats", Body: `[{"op":"remove","path":"/` + userAuthzID + "/" + userAuthzRole + `"}]`, DeniedOnly: true}}},
		{name: "CrewRole", want: []authzCase{{Name: "CrewRole", Method: "http.MethodGet", URL: "/api/crew-roles/" + userAuthzRole, Permission: "Read"}}},
		{name: "SetCrewUserPassword", want: []authzCase{{Name: "SetCrewUserPassword", Method: "http.MethodPost", URL: "/api/set-crew-user-password", Body: "{}", DeniedOnly: true}}},
		{name: "SetCrewUserPassword dry run", want: []authzCase{{Name: "SetCrewUserPassword dry run", Method: "http.MethodPost", URL: "/api/set-crew-user-password", Body: "{}", Headers: dryRun, DeniedOnly: true}}},
		{name: "CrewShipUser", want: []authzCase{{Name: "CrewShipUser", Method: "http.MethodGet", URL: "/api/ships/testDomain/crew-ship-users/" + userAuthzID, Permission: "Read"}}},
		{name: "CrewShipSeat", want: []authzCase{{Name: "CrewShipSeat", Method: "http.MethodGet", URL: "/api/ships/testDomain/crew-ship-seats/" + userAuthzID + "/" + userAuthzRole, Permission: "Read"}}},
		{name: "UnlinkCrewShipUserIdentity", want: []authzCase{{Name: "UnlinkCrewShipUserIdentity", Method: "http.MethodPost", URL: "/api/ships/testDomain/unlink-crew-ship-user-identity", Body: "{}", DeniedOnly: true}}},
		{name: "MembersUsers (portal)", want: []authzCase{{Name: "MembersUsers (portal)", Method: "http.MethodGet", URL: "/portal/api/members-users", Permission: "List"}}},
		{name: "MembersUsers", want: []authzCase{{Name: "MembersUsers", Method: "http.MethodGet", URL: "/api/members-users", Permission: "List"}}},
		{name: "SetMembersUserPassword"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(tt.want, byName[tt.name]); diff != "" {
				t.Errorf("cases named %q mismatch (-want +got):\n%s", tt.name, diff)
			}
		})
	}
	// Every case is one of the shapes above: 2 surfaces × (crew: 13 + 13 routes, members: 12 + 12 routes), the
	// methods doubled by their dry runs, the members surface on two outlets.
	crew, members := 26+4, 24+2
	if want := crew + 2*members; len(got) != want {
		t.Errorf("cases = %d, want %d", len(got), want)
	}
}

// Test_usersTemplate pins the handler file: the compile-time assertion that the
// application supplies each surface, and one delegating handler per route, the tenant
// partition's going to the Tenant methods; a password-only auth has no unlink handler,
// a directory-only auth no set-password handler.
func Test_usersTemplate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		surface         func(t *testing.T) *userSurface
		wantContains    []string
		wantNotContains []string
	}{
		{
			name: "the crew surface, tenanted: both partitions' handlers and both methods",
			surface: func(t *testing.T) *userSurface {
				t.Helper()

				return fixtureSurface(t, true, crewAuthPath)
			},
			wantContains: []string{
				"var _ interface {\n\tCrewUserSurface() *resource.UserSurface[router.CrewAuth]\n} = (*App)(nil)",
				"// CrewUsers serves the users of the global partition: List on CrewUsers.\nfunc (a *App) CrewUsers() http.HandlerFunc {\n\treturn a.CrewUserSurface().Users(a, router.Collection())\n}",
				"func (a *App) CrewUser() http.HandlerFunc {\n\treturn a.CrewUserSurface().User(a, router.Collection())\n}",
				"func (a *App) PatchCrewUsers() http.HandlerFunc {\n\treturn a.CrewUserSurface().PatchUsers(a, router.Collection())\n}",
				"func (a *App) CrewSeat() http.HandlerFunc {\n\treturn a.CrewUserSurface().Seat(a, router.Collection())\n}",
				"func (a *App) PatchCrewSeats() http.HandlerFunc {\n\treturn a.CrewUserSurface().PatchSeats(a, router.Collection())\n}",
				"func (a *App) CrewRoles() http.HandlerFunc {\n\treturn a.CrewUserSurface().Roles(a, router.Collection())\n}",
				"func (a *App) SetCrewUserPassword() http.HandlerFunc {\n\treturn a.CrewUserSurface().SetUserPassword(a, router.Collection())\n}",
				"func (a *App) UnlinkCrewUserIdentity() http.HandlerFunc {\n\treturn a.CrewUserSurface().UnlinkUserIdentity(a, router.Collection())\n}",
				"// CrewShipUsers serves the users of the route's ship: List on CrewShipUsers.\nfunc (a *App) CrewShipUsers() http.HandlerFunc {\n\treturn a.CrewUserSurface().TenantUsers(a, router.Collection())\n}",
				"func (a *App) CrewShipUser() http.HandlerFunc {\n\treturn a.CrewUserSurface().TenantUser(a, router.Collection())\n}",
				"func (a *App) PatchCrewShipUsers() http.HandlerFunc {\n\treturn a.CrewUserSurface().PatchTenantUsers(a, router.Collection())\n}",
				"func (a *App) PatchCrewShipSeats() http.HandlerFunc {\n\treturn a.CrewUserSurface().PatchTenantSeats(a, router.Collection())\n}",
				"func (a *App) CrewShipRole() http.HandlerFunc {\n\treturn a.CrewUserSurface().TenantRole(a, router.Collection())\n}",
				"func (a *App) SetCrewShipUserPassword() http.HandlerFunc {\n\treturn a.CrewUserSurface().SetTenantUserPassword(a, router.Collection())\n}",
				"func (a *App) UnlinkCrewShipUserIdentity() http.HandlerFunc {\n\treturn a.CrewUserSurface().UnlinkTenantUserIdentity(a, router.Collection())\n}",
			},
		},
		{
			name: "the members surface, no tenancy: no password method, the unlink alone",
			surface: func(t *testing.T) *userSurface {
				t.Helper()

				return fixtureSurface(t, false, membersAuthPath)
			},
			wantContains: []string{
				"MembersUserSurface() *resource.UserSurface[router.MembersAuth]",
				"func (a *App) UnlinkMembersUserIdentity() http.HandlerFunc {\n\treturn a.MembersUserSurface().UnlinkUserIdentity(a, router.Collection())\n}",
			},
			wantNotContains: []string{"SetMembersUserPassword", "Tenant", "MembersShip"},
		},
		{
			name: "a password-only auth: no unlink handler",
			surface: func(t *testing.T) *userSurface {
				t.Helper()
				r := userFixtureGenerator(t, false)
				r.defaultOutlet.auth.methods = []AuthMethod{PasswordMethod()}
				r.defaultOutlet.manages = nil

				return surfaceOf(t, r, crewAuthPath)
			},
			wantContains:    []string{"func (a *App) SetCrewUserPassword() http.HandlerFunc {"},
			wantNotContains: []string{"UnlinkCrewUserIdentity"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := tt.surface(t)
			data := &usersData{
				Source: "pkg/resources", Package: "app", LocalPackageImports: `"example.com/acme/beacon/pkg/router"`, ApplicationName: "App", ReceiverName: "a", RouterPackage: "router",
				Surfaces: []*usersSurfaceData{{Auth: s.decl.Auth, ImportPath: s.importPath, TypeName: s.typeName, Accessor: s.accessor, Handlers: usersHandlers(s)}},
			}
			out, err := (&client{}).generateTemplateOutput("usersTemplate", usersTemplate, data)
			if err != nil {
				t.Fatalf("generateTemplateOutput() error = %v", err)
			}
			if _, err := format.Source(out); err != nil {
				t.Fatalf("the rendered file is not Go: %v\n%s", err, out)
			}
			assertContains(t, "usersTemplate", string(out), tt.wantContains, tt.wantNotContains)
		})
	}
}

// Test_userSurfacesTemplate pins the router package's file: the marker type carrying the
// derived declaration and the constructor over the two stores.
func Test_userSurfacesTemplate(t *testing.T) {
	t.Parallel()

	s := fixtureSurface(t, true, crewAuthPath)
	data := &userSurfacesData{Source: "pkg/resources", Package: "router", Surfaces: []*userSurfaceFileData{{
		Auth: s.decl.Auth, ImportPath: s.importPath, PackageName: "crew", TypeName: s.typeName, Accessor: s.accessor, Constructor: "New" + s.accessor,
		Tenant: s.decl.Tenant, DomainParam: string(s.decl.DomainParam), Password: s.decl.Shape.Password, Registrations: s.decl.Shape.Registrations,
		OutletList: outletList([]string{"default", "portal"}), ResourceList: "CrewUsers, CrewSeats",
	}}}
	out, err := (&client{}).generateTemplateOutput("userSurfacesTemplate", userSurfacesTemplate, data)
	if err != nil {
		t.Fatalf("generateTemplateOutput() error = %v", err)
	}
	if _, err := format.Source(out); err != nil {
		t.Fatalf("the rendered file is not Go: %v\n%s", err, out)
	}
	assertContains(t, "userSurfacesTemplate", string(out), []string{
		"// CrewAuth marks the crew auth (example.com/acme/beacon/pkg/auth/crew) for the user-management\n// surface the library serves on the default and portal outlets: CrewUsers, CrewSeats.",
		"type CrewAuth struct{}",
		"func (CrewAuth) UserSurfaceDeclaration() resource.UserSurfaceDeclaration {\n\treturn resource.UserSurfaceDeclaration{\n\t\tAuth:        \"Crew\",\n\t\tTenant:      \"Ship\",\n\t\tDomainParam: \"shipID\",",
		"Password:        true,\n\t\t\tUsernameIsEmail: false,\n\t\t\tOpenSignUp:      false,",
		"{Name: \"google\", Kind: \"google\", Label: \"Fleet directory\", Invited: false, SyncsRoles: false},",
		"func NewCrewUserSurface(accounts resource.UserAccounts, seats resource.UserSeats, opts ...resource.UserSurfaceOption) *resource.UserSurface[CrewAuth] {\n\treturn resource.NewUserSurface[CrewAuth](accounts, seats, opts...)\n}",
		"resource.WithTenantDirectories",
	}, nil)
	if got, want := outletList([]string{"default"}), "default outlet"; got != want {
		t.Errorf("outletList(one) = %q, want %q", got, want)
	}
	if got, want := outletList([]string{"default", "portal", "kiosk"}), "default, portal and kiosk outlets"; got != want {
		t.Errorf("outletList(three) = %q, want %q", got, want)
	}
}

// Test_typescriptMetadata_users pins the metadata files: the identity shape, each surface
// resource's interface, map entry (the tenant partition's route under the domain pair,
// the writes it refuses marked) and scope, and each method's body shapes and map entry.
func Test_typescriptMetadata_users(t *testing.T) {
	t.Parallel()

	s := fixtureSurface(t, true, crewAuthPath)
	gen := &typescriptGenerator{client: &client{}}
	resources, err := gen.generateTemplateOutput(typescriptResourcesTemplate, typescriptResourcesTemplate, tsResourcesData{
		File: gen, GenPrefix: "zz_gen", DomainRoutePrefix: "ships/{shipID}", UserResources: tsUserResources(s),
	})
	if err != nil {
		t.Fatalf("generateTemplateOutput(resources) error = %v", err)
	}
	assertContains(t, "typescriptResourcesTemplate", string(resources), []string{
		"export interface UserIdentity {\n  id: string;\n  method: string;\n  connection: string;\n  subject: string;\n  organization: string | null;\n  email: string | null;\n  linkedAt: Date;\n  lastUsedAt: Date;\n}",
		"/** A user of the Crew auth in the global partition: the account, its sign-in means and its seats. */\nexport interface CrewUsers {\n  id: string;\n  username?: string;\n  disabled?: boolean;\n  hasPassword?: boolean;\n  passwordChangeRequired?: boolean;\n  createdAt?: Date;\n  lastSignInAt?: Date | null;\n  organization?: string | null;\n  email?: string | null;\n  emailVerifiedAt?: Date | null;\n  identities?: UserIdentity[];\n  roles?: string[];\n  everywhereRoles?: string[];\n  rolesSynced?: boolean;\n}",
		"/** A standing invitation into the global partition of the Crew auth: an address invited to hold roles, until it is accepted, revoked or expires. */\nexport interface CrewInvitations {\n  id: string;\n  email?: string;\n  roles?: string[];\n  invitedBy?: string | null;\n  createdAt?: Date;\n  expiresAt?: Date;\n}",
		"[Resources.CrewInvitations]: {\n    route: 'crew-invitations',\n    updateDisabled: true,",
		"[Resources.CrewShipInvitations]: PermissionScopes.domain,",
		"export interface CrewSeats {\n  user: string;\n  role: string;\n  username?: string;\n  everyDomain?: boolean;\n}",
		"export interface CrewShipSeats {\n  user: string;\n  role: string;\n  username?: string;\n  everywhere?: boolean;\n}",
		"export interface CrewRoles {\n  name: string;\n  kind?: 'global' | 'domain';\n}",
		"export interface CrewShipRoles {\n  name: string;\n}",
		"[Resources.CrewUsers]: {\n    route: 'crew-users',\n    fields: [\n      { fieldName: 'id', primaryKey: { ordinalPosition: 1 }, displayType: 'uuid', required: true, isIndex: true },\n      { fieldName: 'username', displayType: 'string', required: true, isIndex: false },",
		"{ fieldName: 'lastSignInAt', displayType: 'date', required: false, isIndex: false },",
		"[Resources.CrewSeats]: {\n    route: 'crew-seats',\n    updateDisabled: true,\n    fields: [\n      { fieldName: 'user', primaryKey: { ordinalPosition: 1 }, displayType: 'uuid', required: true, isIndex: true },\n      { fieldName: 'role', primaryKey: { ordinalPosition: 2 }, displayType: 'string', required: true, isIndex: true },",
		"[Resources.CrewRoles]: {\n    route: 'crew-roles',\n    createDisabled: true,\n    updateDisabled: true,\n    deleteDisabled: true,",
		"[Resources.CrewShipUsers]: {\n    route: 'ships/{shipID}/crew-ship-users',",
		"[Resources.CrewUsers]: PermissionScopes.global,",
		"[Resources.CrewShipUsers]: PermissionScopes.domain,",
		"[Resources.CrewShipRoles]: PermissionScopes.domain,",
	}, nil)

	methods, err := gen.generateTemplateOutput(typescriptMethodsTemplate, typescriptMethodsTemplate, tsMethodsData{File: gen, GenPrefix: "zz_gen", UserMethods: tsUserMethods(s)})
	if err != nil {
		t.Fatalf("generateTemplateOutput(methods) error = %v", err)
	}
	assertContains(t, "typescriptMethodsTemplate", string(methods), []string{
		"export interface SetCrewUserPasswordConfig {\n  user: string | FieldPointer;\n  password: string | FieldPointer;\n}\nexport interface SetCrewUserPassword {\n  user: string;\n  password: string;\n}",
		"export interface UnlinkCrewUserIdentity {\n  user: string;\n  identity: string;\n}",
		"export interface SetCrewShipUserPassword {\n  user: string;\n  password: string;\n}",
		"[Methods.SetCrewUserPassword]: {\n    route: 'set-crew-user-password',\n    fields: [\n      { fieldName: 'user', displayType: 'uuid' },\n      { fieldName: 'password', displayType: 'string' },\n    ],\n  },",
		"[Methods.UnlinkCrewShipUserIdentity]: {\n    route: 'unlink-crew-ship-user-identity',\n    fields: [\n      { fieldName: 'user', displayType: 'uuid' },\n      { fieldName: 'identity', displayType: 'uuid' },\n    ],\n  },",
	}, nil)
}

// Test_apiClientData_users pins the descriptor per outlet: the default outlet carries the
// crew surface and the managed members surface, the portal the members surface alone,
// an outlet serving neither nothing; each resource with its keys, operations, patchable
// fields and order, each method with its scope, the handles and key tuples.
func Test_apiClientData_users(t *testing.T) {
	t.Parallel()

	r := userFixtureGenerator(t, true)
	surfaces, err := r.userSurfaces()
	if err != nil {
		t.Fatalf("userSurfaces() error = %v", err)
	}

	tests := []struct {
		name            string
		outlet          string
		wantContains    []string
		wantNotContains []string
	}{
		{
			name: "the default outlet: crew and the managed members surface",
			wantContains: []string{
				"[Resources.CrewUsers]: {\n      resource: Resources.CrewUsers,\n      property: 'crewUsers',\n      route: 'crew-users',\n      scope: 'global',\n      consolidated: false,\n      keys: ['id'],\n      operations: ['list', 'read', 'create', 'patch', 'remove'],\n      page: { default: 50 },\n      order: [{ field: 'username', direction: 'asc' }],\n      patchable: ['username', 'disabled', 'email'],\n    },",
				"[Resources.CrewSeats]: {\n      resource: Resources.CrewSeats,\n      property: 'crewSeats',\n      route: 'crew-seats',\n      scope: 'global',\n      consolidated: false,\n      keys: ['user', 'role'],\n      operations: ['list', 'read', 'create', 'remove'],\n      page: { default: 50 },\n      order: [{ field: 'username', direction: 'asc' }, { field: 'role', direction: 'asc' }],\n    },",
				"[Resources.CrewRoles]: {\n      resource: Resources.CrewRoles,\n      property: 'crewRoles',\n      route: 'crew-roles',\n      scope: 'global',\n      consolidated: false,\n      keys: ['name'],\n      operations: ['list', 'read'],\n      page: { default: 50 },\n      order: [{ field: 'name', direction: 'asc' }],\n    },",
				"[Resources.CrewShipUsers]: {\n      resource: Resources.CrewShipUsers,\n      property: 'crewShipUsers',\n      route: 'crew-ship-users',\n      scope: 'domain',",
				"[Resources.MembersUsers]: {\n      resource: Resources.MembersUsers,\n      property: 'membersUsers',\n      route: 'members-users',\n      scope: 'global',\n      consolidated: false,\n      keys: ['id'],\n      operations: ['list', 'read', 'create', 'patch', 'remove'],",
				"[Methods.SetCrewUserPassword]: { method: Methods.SetCrewUserPassword, property: 'setCrewUserPassword', route: 'set-crew-user-password', scope: 'global' },",
				"[Methods.UnlinkCrewShipUserIdentity]: { method: Methods.UnlinkCrewShipUserIdentity, property: 'unlinkCrewShipUserIdentity', route: 'unlink-crew-ship-user-identity', scope: 'domain' },",
				"[Methods.UnlinkMembersUserIdentity]: { method: Methods.UnlinkMembersUserIdentity, property: 'unlinkMembersUserIdentity', route: 'unlink-members-user-identity', scope: 'global' },",
				"export interface CrewUsersCreate {\n  username: string;\n  password?: string;\n  disabled?: boolean;\n  email?: string;\n}",
				"export interface CrewShipUsersCreate {\n  username: string;\n  password?: string;\n  disabled?: boolean;\n  email?: string;\n  role: string;\n}",
				"export interface MembersUsersCreate {\n  username: string;\n  password?: string;\n  disabled?: boolean;\n  email?: string;\n}",
				"export interface CrewUsersPatch {\n  username?: string;\n  disabled?: boolean;\n  email?: string;\n}",
				"export interface CrewInvitationsCreate {\n  email: string;\n  roles: string[];\n}",
				"crewInvitations: ResourceHandle<CrewInvitations, CrewInvitationsKey, 'list' | 'read' | 'create' | 'remove', CrewInvitationsCreate>;",
				"export type CrewUsersKey = [id: string];",
				"export type CrewSeatsKey = [user: string, role: string];",
				"export type CrewRolesKey = [name: string];",
				"crewUsers: ResourceHandle<CrewUsers, CrewUsersKey, 'list' | 'read' | 'create' | 'patch' | 'remove', CrewUsersCreate, CrewUsersPatch>;",
				"crewSeats: ResourceHandle<CrewSeats, CrewSeatsKey, 'list' | 'read' | 'create' | 'remove', CrewSeatsCreate>;",
				"crewRoles: ResourceHandle<CrewRoles, CrewRolesKey, 'list' | 'read'>;",
				"crewShipUsers: ResourceHandle<CrewShipUsers, CrewShipUsersKey, 'list' | 'read' | 'create' | 'patch' | 'remove', CrewShipUsersCreate, CrewShipUsersPatch>;",
				"setCrewUserPassword: MethodHandle<SetCrewUserPassword>;",
				"  userSurfaces: [\n    {\n      auth: 'Crew',\n      label: 'Crew',\n      password: true,\n      usernameIsEmail: false,\n      openSignUp: false,\n      registrations: [{ name: 'google', kind: 'google', label: 'Fleet directory', invited: false, syncsRoles: false }],\n      partitions: [\n        { scope: 'global', users: Resources.CrewUsers, seats: Resources.CrewSeats, roles: Resources.CrewRoles, invitations: Resources.CrewInvitations, setPassword: Methods.SetCrewUserPassword, unlinkIdentity: Methods.UnlinkCrewUserIdentity },\n        { scope: 'domain', users: Resources.CrewShipUsers, seats: Resources.CrewShipSeats, roles: Resources.CrewShipRoles, invitations: Resources.CrewShipInvitations, setPassword: Methods.SetCrewShipUserPassword, unlinkIdentity: Methods.UnlinkCrewShipUserIdentity },\n      ],\n    },\n    {\n      auth: 'Members',\n      label: 'Members',\n      password: false,\n      usernameIsEmail: false,\n      openSignUp: false,\n      registrations: [{ name: 'directory', kind: 'azure', label: 'Company directory', invited: true, syncsRoles: false }],\n      partitions: [\n        { scope: 'global', users: Resources.MembersUsers, seats: Resources.MembersSeats, roles: Resources.MembersRoles, invitations: Resources.MembersInvitations, unlinkIdentity: Methods.UnlinkMembersUserIdentity },\n        { scope: 'domain', users: Resources.MembersShipUsers, seats: Resources.MembersShipSeats, roles: Resources.MembersShipRoles, invitations: Resources.MembersShipInvitations, unlinkIdentity: Methods.UnlinkMembersShipUserIdentity },\n      ],\n    },\n  ],\n});",
			},
			wantNotContains: []string{"SetMembersUserPassword"},
		},
		{
			name:            "the portal: the members surface alone",
			outlet:          "portal",
			wantContains:    []string{"property: 'membersUsers',", "property: 'membersShipSeats',", "[Methods.UnlinkMembersShipUserIdentity]:", "  userSurfaces: [\n    {\n      auth: 'Members',"},
			wantNotContains: []string{"CrewUsers", "SetCrewUserPassword", "SetMembersUserPassword", "auth: 'Crew'"},
		},
		{
			name:            "an outlet serving no surface",
			outlet:          "droids",
			wantNotContains: []string{"CrewUsers", "MembersUsers", "UserIdentity", "userSurfaces"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gen := &typescriptGenerator{client: &client{}, userSurfaces: surfaces, outletName: tt.outlet, domainRouteSegment: "ships", domainRouteParam: "shipID"}
			out, err := gen.generateTemplateOutput("typescriptAPITemplate", typescriptAPITemplate, gen.apiClientData())
			if err != nil {
				t.Fatalf("generateTemplateOutput() error = %v", err)
			}
			assertContains(t, "typescriptAPITemplate", string(out), tt.wantContains, tt.wantNotContains)
		})
	}
}

// tokenFixtureGenerator is a generator whose default outlet signs the crew in and whose
// crew auth issues the tokens of two machine outlets, droids and probes, beside a portal
// signing the members in and managing the crew surface, and a kiosk signing the crew in
// a second time.
func tokenFixtureGenerator() *resourceGenerator {
	crew := &outletAuth{importPath: crewAuthPath, methods: []AuthMethod{PasswordMethod()}}
	members := &outletAuth{importPath: membersAuthPath, methods: []AuthMethod{AzureMethod("directory")}}

	return &resourceGenerator{
		client:        &client{},
		genRoutes:     true,
		routePrefix:   "console/api",
		defaultOutlet: routerOutlet{auth: crew},
		extraOutlets: []routerOutlet{
			{name: "droids", prefix: "droids", apiKey: true, tokenAuth: crewAuthPath},
			{name: "portal", prefix: "portal/api", servesSessions: true, auth: members, manages: []string{crewAuthPath}},
			{name: "probes", prefix: "probes/v1", apiKey: true, tokenAuth: crewAuthPath},
			{name: "kiosk", prefix: "kiosk/api", servesSessions: true, auth: crew},
		},
	}
}

// Test_tokenIssuers pins the derivation: the first session outlet declaring the auth a
// machine outlet's Tokens names carries the block, listing every machine outlet of that
// auth in declaration order with its mount path; a second session outlet declaring the
// same auth, an outlet managing it, and an outlet whose auth issues none get nothing;
// nothing where the run generates no routes or where no session outlet declares the auth.
func Test_tokenIssuers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		generator func() *resourceGenerator
		want      map[string][]*tsAPIMachineOutlet
	}{
		{
			name: "no routes: nothing",
			generator: func() *resourceGenerator {
				r := tokenFixtureGenerator()
				r.genRoutes = false

				return r
			},
		},
		{
			name:      "the default outlet issues for both machine outlets, in declaration order",
			generator: tokenFixtureGenerator,
			want:      map[string][]*tsAPIMachineOutlet{defaultOutletName: {{Name: "droids", Prefix: "/droids"}, {Name: "probes", Prefix: "/probes/v1"}}},
		},
		{
			name: "no session outlet declares the auth: nothing",
			generator: func() *resourceGenerator {
				r := tokenFixtureGenerator()
				r.extraOutlets[0].tokenAuth = "example.com/acme/beacon/pkg/auth/robots"
				r.extraOutlets[2].tokenAuth = "example.com/acme/beacon/pkg/auth/robots"

				return r
			},
			want: map[string][]*tsAPIMachineOutlet{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := tt.generator().tokenIssuers()
			if tt.want == nil {
				if got != nil {
					t.Fatalf("tokenIssuers() = %v, want nil", got)
				}

				return
			}
			outlets := make(map[string][]*tsAPIMachineOutlet, len(got))
			for name, block := range got {
				outlets[name] = block.Outlets
				want := &tsAPITokens{Route: "user/tokens", RevokeRoute: "user/tokens/revoke", DeviceRoute: "user/device", ApproveRoute: "user/device/approve", DenyRoute: "user/device/deny", Outlets: block.Outlets}
				if diff := cmp.Diff(want, block); diff != "" {
					t.Errorf("tokenIssuers()[%s] mismatch (-want +got):\n%s", name, diff)
				}
			}
			if diff := cmp.Diff(tt.want, outlets); diff != "" {
				t.Errorf("tokenIssuers() outlets mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// Test_apiClientData_tokens pins the descriptor's tokens block: on the client of the
// session outlet whose Auth issues tokens, the routes in the form of userDomainsRoute and
// every machine outlet of the auth with its mount path; absent from the client of an
// outlet that only manages the auth, of one whose auth issues none, and of a second outlet
// declaring the issuing auth.
func Test_apiClientData_tokens(t *testing.T) {
	t.Parallel()

	issuers := tokenFixtureGenerator().tokenIssuers()
	const block = "  tokens: {\n    route: 'user/tokens',\n    revokeRoute: 'user/tokens/revoke',\n    deviceRoute: 'user/device',\n    approveRoute: 'user/device/approve',\n    denyRoute: 'user/device/deny',\n    outlets: [{ name: 'droids', prefix: '/droids' }, { name: 'probes', prefix: '/probes/v1' }],\n  },\n});"

	tests := []struct {
		name            string
		outlet          string
		wantContains    []string
		wantNotContains []string
	}{
		{name: "the default outlet, whose crew auth issues for droids and probes", wantContains: []string{"  userDomainsRoute: 'user-domains',", block}},
		{name: "the portal, which manages the crew surface and whose members auth issues none", outlet: "portal", wantNotContains: []string{"tokens:"}},
		{name: "the kiosk, a second outlet declaring the crew auth", outlet: "kiosk", wantNotContains: []string{"tokens:"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gen := &typescriptGenerator{client: &client{}, tokenIssuers: issuers, outletName: tt.outlet}
			out, err := gen.generateTemplateOutput("typescriptAPITemplate", typescriptAPITemplate, gen.apiClientData())
			if err != nil {
				t.Fatalf("generateTemplateOutput() error = %v", err)
			}
			assertContains(t, "typescriptAPITemplate", string(out), tt.wantContains, tt.wantNotContains)
		})
	}
}

// Test_AuthMethod_directoryDeclarations pins InvitedUsers and SyncsRoles: spelled in the
// method's String, refused on the password method.
func Test_AuthMethod_directoryDeclarations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		methods    []AuthMethod
		wantString string
		wantErr    string
	}{
		{name: "a registration declaring both", methods: []AuthMethod{AzureMethod("directory").InvitedUsers().SyncsRoles()}, wantString: `AzureMethod("directory").InvitedUsers().SyncsRoles()`},
		{name: "a registration declaring neither", methods: []AuthMethod{GoogleMethod("google")}, wantString: `GoogleMethod("google")`},
		{name: "a registration admitting open sign-up", methods: []AuthMethod{GoogleMethod("sign-up").OpenSignUp()}, wantString: `GoogleMethod("sign-up").OpenSignUp()`},
		{name: "open sign-up beside role synchronization", methods: []AuthMethod{AzureMethod("anyone").SyncsRoles().OpenSignUp()}, wantString: `AzureMethod("anyone").SyncsRoles().OpenSignUp()`},
		{name: "InvitedUsers on the password method", methods: []AuthMethod{PasswordMethod().InvitedUsers()}, wantErr: "declares InvitedUsers or SyncsRoles on the password method"},
		{name: "SyncsRoles on the password method", methods: []AuthMethod{PasswordMethod().SyncsRoles()}, wantErr: "declares InvitedUsers or SyncsRoles on the password method"},
		{name: "the password method offering sign-up", methods: []AuthMethod{PasswordMethod().OpenSignUp()}, wantString: "PasswordMethod().OpenSignUp()"},
		{name: "the password method whose username is the address", methods: []AuthMethod{PasswordMethod().UsernameIsEmail().OpenSignUp()}, wantString: "PasswordMethod().UsernameIsEmail().OpenSignUp()"},
		{name: "UsernameIsEmail on a registration", methods: []AuthMethod{GoogleMethod("google").UsernameIsEmail()}, wantErr: "declares UsernameIsEmail on a registration: it describes the password method's username"},
		{name: "InvitedUsers beside OpenSignUp", methods: []AuthMethod{GoogleMethod("google").InvitedUsers().OpenSignUp()}, wantErr: "declares both InvitedUsers and OpenSignUp: a registration admits invited users only or anyone, not both"},
		{name: "SyncsRoles beside the password method", methods: []AuthMethod{PasswordMethod(), GoogleMethod("google").SyncsRoles()}, wantErr: "declares SyncsRoles beside 1 other method(s): a registration whose directory writes the roles stands alone on its Auth"},
		{name: "SyncsRoles beside another registration", methods: []AuthMethod{AzureMethod("directory").SyncsRoles(), GoogleMethod("google")}, wantErr: "declares SyncsRoles beside 1 other method(s)"},
		{name: "two registrations declaring SyncsRoles", methods: []AuthMethod{AzureMethod("directory").SyncsRoles(), GoogleMethod("google").SyncsRoles()}, wantErr: "stands alone on its Auth"},
		{name: "SyncsRoles alone", methods: []AuthMethod{AzureMethod("directory").SyncsRoles()}, wantString: `AzureMethod("directory").SyncsRoles()`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := validateAuthMethods(crewAuthPath, tt.methods)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("validateAuthMethods() error = %v, want one containing %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("validateAuthMethods() error = %v", err)
			}
			if got := tt.methods[0].String(); got != tt.wantString {
				t.Errorf("String() = %q, want %q", got, tt.wantString)
			}
		})
	}
}

// Test_Manages pins the option: an import path, declared once.
func Test_Manages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		paths   []string
		want    []string
		wantErr string
	}{
		{name: "two managed auths", paths: []string{membersAuthPath, "example.com/acme/beacon/pkg/auth/vendors"}, want: []string{membersAuthPath, "example.com/acme/beacon/pkg/auth/vendors"}},
		{name: "an empty path", paths: []string{""}, wantErr: `Manages("") requires an import path`},
		{name: "a path with a slash at the end", paths: []string{membersAuthPath + "/"}, wantErr: "requires an import path"},
		{name: "the same auth twice", paths: []string{membersAuthPath, membersAuthPath}, wantErr: "is declared twice on the outlet"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var outlet routerOutlet
			var err error
			for _, p := range tt.paths {
				if err = Manages(p).applyToOutlet(&outlet); err != nil {
					break
				}
			}
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Manages() error = %v, want one containing %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("Manages() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, outlet.manages); diff != "" {
				t.Errorf("manages mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// fixtureSurface derives the fixture's surfaces and returns the one of the auth.
func fixtureSurface(t *testing.T, tenanted bool, importPath string) *userSurface {
	t.Helper()

	return surfaceOf(t, userFixtureGenerator(t, tenanted), importPath)
}

// surfaceOf derives the generator's surfaces and returns the one of the auth.
func surfaceOf(t *testing.T, r *resourceGenerator, importPath string) *userSurface {
	t.Helper()

	surfaces, err := r.userSurfaces()
	if err != nil {
		t.Fatalf("userSurfaces() error = %v", err)
	}
	for _, s := range surfaces {
		if s.importPath == importPath {
			return s
		}
	}
	t.Fatalf("no surface for %s among %d", importPath, len(surfaces))

	return nil
}

func Test_authLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		auth string
		want string
	}{
		{name: "one word", auth: "Crew", want: "Crew"},
		{name: "two words", auth: "FleetStaff", want: "Fleet staff"},
		{name: "an initialism stays together", auth: "HQStaff", want: "HQ staff"},
		{name: "a trailing initialism stays together", auth: "StaffHQ", want: "Staff HQ"},
		{name: "empty", auth: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := authLabel(tt.auth); got != tt.want {
				t.Errorf("authLabel(%q) = %q, want %q", tt.auth, got, tt.want)
			}
		})
	}
}
