package generation

import (
	"go/format"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// tenantCreationFixtureGenerator builds a generator around one tenantfixture record,
// resolved through the real extraction so its fields carry their nullability, with the
// outlets and methods a case declares.
func tenantCreationFixtureGenerator(t *testing.T, record string, mutate func(r *resourceGenerator)) *resourceGenerator {
	t.Helper()

	structs := fixtureStructs(loadFixture(t, "tenantfixture"))
	c := &client{tableMap: tenantFixtureTables()}
	resources, err := c.structsToResources(tenantFixtureStructs(t, structs, record))
	if err != nil {
		t.Fatalf("structsToResources() error = %v", err)
	}
	c.resources = resources
	r := &resourceGenerator{client: c, routePrefix: "api", applicationName: "App", receiverName: "a"}
	if mutate != nil {
		mutate(r)
	}

	return r
}

// openAuth is an outlet auth with a registration admitting open sign-up.
func openAuth(importPath string) *outletAuth {
	return &outletAuth{importPath: importPath, methods: []AuthMethod{PasswordMethod(), GoogleMethod("sign-up").OpenSignUp()}}
}

// closedAuth is an outlet auth whose registrations admit invited users alone.
func closedAuth(importPath string) *outletAuth {
	return &outletAuth{importPath: importPath, methods: []AuthMethod{PasswordMethod(), GoogleMethod("google").InvitedUsers()}}
}

// Test_tenantCreation_shapes pins what each shape allows: the administrator's create
// path under the default and both, the self-service route under self-service and both.
func Test_tenantCreation_shapes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                     string
		shape                    tenantCreation
		wantAdministratorCreates bool
		wantSelfService          bool
		wantString               string
	}{
		{name: "unset is the administrator alone", shape: "", wantAdministratorCreates: true, wantString: "administrator"},
		{name: "administrator", shape: tenantCreationAdministrator, wantAdministratorCreates: true, wantString: "administrator"},
		{name: "self-service", shape: tenantCreationSelfService, wantSelfService: true, wantString: "self-service"},
		{name: "both", shape: tenantCreationBoth, wantAdministratorCreates: true, wantSelfService: true, wantString: "both"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.shape.AdministratorCreates(); got != tt.wantAdministratorCreates {
				t.Errorf("AdministratorCreates() = %v, want %v", got, tt.wantAdministratorCreates)
			}
			if got := tt.shape.SelfService(); got != tt.wantSelfService {
				t.Errorf("SelfService() = %v, want %v", got, tt.wantSelfService)
			}
			if got := tt.shape.String(); got != tt.wantString {
				t.Errorf("String() = %q, want %q", got, tt.wantString)
			}
		})
	}
}

// Test_validateTenantCreation pins the rules a self-service record is held to: a
// session outlet with an Auth, one auth across the record's outlets, a registration
// with the open sign-up rule on it, and the handler's name free; an administrator-only
// record is held to none of them.
func Test_validateTenantCreation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		record  string
		mutate  func(r *resourceGenerator)
		wantErr string
	}{
		{
			name:   "an administrator-only record needs no auth",
			record: "Sector",
		},
		{
			name:   "a self-service record on the default outlet with open sign-up",
			record: "Station",
			mutate: func(r *resourceGenerator) { r.defaultOutlet.auth = openAuth("example.com/app/pkg/auth/staff") },
		},
		{
			name:   "a both record on an extra session outlet with open sign-up",
			record: "Colony",
			mutate: func(r *resourceGenerator) {
				r.resources[0].OutletNames = []string{"members"}
				r.extraOutlets = []routerOutlet{{name: "members", prefix: "members", servesSessions: true, auth: openAuth("example.com/app/pkg/auth/members")}}
			},
		},
		{
			name:    "a self-service record on an API-key outlet alone is refused",
			record:  "Station",
			mutate:  func(r *resourceGenerator) { r.defaultOutlet.apiKey = true },
			wantErr: "struct Station: @tenant(creation: self-service) on a record no session-serving outlet serves; the create-your-station route is reached by a signed-in person, so declare the record on an outlet with a session Auth",
		},
		{
			name:    "a self-service record on an outlet without an Auth is refused",
			record:  "Station",
			wantErr: `struct Station: @tenant(creation: self-service), but outlet "default" declares no Auth; the create-your-station route seats the person in the outlet's auth, so declare it (Auth(<package>, ...)) with a registration admitting open sign-up (GoogleMethod(name).OpenSignUp() or AzureMethod(name).OpenSignUp()), or keep the administrator shape`,
		},
		{
			name:   "a self-service record served by outlets bound to different auths is refused",
			record: "Station",
			mutate: func(r *resourceGenerator) {
				r.resources[0].OutletNames = []string{defaultOutletName, "members"}
				r.defaultOutlet.auth = openAuth("example.com/app/pkg/auth/staff")
				r.extraOutlets = []routerOutlet{{name: "members", prefix: "members", servesSessions: true, auth: openAuth("example.com/app/pkg/auth/members")}}
			},
			wantErr: `struct Station: @tenant(creation: self-service) on a record served by outlets bound to different auths ("example.com/app/pkg/auth/staff" and "example.com/app/pkg/auth/members"); the create-your-station route seats the person in one auth, so serve the record on the outlets of one (@outlet), or keep the administrator shape`,
		},
		{
			name:    "a self-service record without an open sign-up registration is refused",
			record:  "Station",
			mutate:  func(r *resourceGenerator) { r.defaultOutlet.auth = closedAuth("example.com/app/pkg/auth/staff") },
			wantErr: "struct Station: @tenant(creation: self-service) needs a registration with the open sign-up rule on the staff auth, since a stranger signs in before creating a station and only open sign-up admits one; declare it on the outlet's Auth (GoogleMethod(name).OpenSignUp() or AzureMethod(name).OpenSignUp(), the registration's Accounts being signin.OpenSignUp), or keep the administrator shape",
		},
		{
			name:   "a method named as the handler is refused",
			record: "Station",
			mutate: func(r *resourceGenerator) {
				r.defaultOutlet.auth = openAuth("example.com/app/pkg/auth/staff")
				structs := fixtureStructs(loadFixture(t, "tenantfixture"))
				r.rpcMethods = []*rpcMethodInfo{{Struct: structs["CreateStation"]}}
			},
			wantErr: "struct Station: @tenant(creation: self-service) generates the handler CreateStation, the name of the RPC method CreateStation; rename the method",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := tenantCreationFixtureGenerator(t, tt.record, tt.mutate)
			err := r.validateTenantCreation()
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("validateTenantCreation() error = %v, want it to contain %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("validateTenantCreation() error = %v", err)
			}
		})
	}
}

// Test_tenantCreationFields pins the create-your-tenant request's shape: the key first
// and required, then every column the wire may write, required where the column is,
// and no server-owned column.
// Test_tenantSeatData pins what the record's create path seats its creator with: the
// accessors over the auth of the session outlets serving the record; nothing for a
// record created through its route alone or served by no session auth; a refusal
// where the record's outlets bind to different auths.
func Test_tenantSeatData(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		creation tenantCreation
		mutate   func(r *resourceGenerator)
		want     *tenantSeatData
		wantErr  string
	}{
		{
			name:   "the default outlet's auth seats the creator",
			mutate: func(r *resourceGenerator) { r.defaultOutlet.auth = openAuth("example.com/app/pkg/auth/staff") },
			want:   &tenantSeatData{Record: "Station", RecordLower: "station", KeyType: "string", Auth: "Staff", AuthType: "StaffAuth", SurfaceAccessor: "StaffUserSurface", RolesAccessor: "StationAdministratorRoles"},
		},
		{
			name:     "the both shape seats the creator of an administrator's create too",
			creation: tenantCreationBoth,
			mutate:   func(r *resourceGenerator) { r.defaultOutlet.auth = openAuth("example.com/app/pkg/auth/staff") },
			want:     &tenantSeatData{Record: "Station", RecordLower: "station", KeyType: "string", Auth: "Staff", AuthType: "StaffAuth", SurfaceAccessor: "StaffUserSurface", RolesAccessor: "StationAdministratorRoles"},
		},
		{
			name:     "a self-service record has no create path to seat through",
			creation: tenantCreationSelfService,
			mutate:   func(r *resourceGenerator) { r.defaultOutlet.auth = openAuth("example.com/app/pkg/auth/staff") },
		},
		{
			name: "no session auth, no seat",
		},
		{
			name: "outlets bound to different auths are refused",
			mutate: func(r *resourceGenerator) {
				r.resources[0].OutletNames = []string{defaultOutletName, "members"}
				r.defaultOutlet.auth = openAuth("example.com/app/pkg/auth/staff")
				r.extraOutlets = []routerOutlet{{name: "members", prefix: "members", servesSessions: true, auth: openAuth("example.com/app/pkg/auth/members")}}
			},
			wantErr: `struct Station: the record is served by outlets bound to different auths ("example.com/app/pkg/auth/staff" and "example.com/app/pkg/auth/members"); its create seats the creator as the station's first administrator through one auth's user-management surface, so serve the record on the outlets of one auth (@outlet)`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := tenantCreationFixtureGenerator(t, "Station", func(r *resourceGenerator) {
				r.resources[0].TenantCreation = tt.creation
				if tt.mutate != nil {
					tt.mutate(r)
				}
			})
			got, err := r.tenantSeatData(r.resources[0])
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("tenantSeatData() error = %v, want it to contain %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("tenantSeatData() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("tenantSeatData() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_tenantCreationFields(t *testing.T) {
	t.Parallel()

	r := tenantCreationFixtureGenerator(t, "Station", nil)
	key, fields, setters := tenantCreationFields(r.resources[0])

	wantFields := []*tenantCreationField{
		{Name: "ID", Type: "string", Wire: "id", Tags: `json:"id" validate:"required"`},
		{Name: "Name", Type: "string", Wire: "name", Tags: `json:"name" validate:"required"`},
		{Name: "Motto", Type: "*string", Wire: "motto", Tags: `json:"motto"`},
		{Name: "Opened", Type: "civil.Date", Wire: "opened", Tags: `json:"opened" validate:"required"`},
	}
	if diff := cmp.Diff(wantFields, fields); diff != "" {
		t.Errorf("tenantCreationFields() fields mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(wantFields[0], key); diff != "" {
		t.Errorf("tenantCreationFields() key mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"Name", "Motto", "Opened"}, setters); diff != "" {
		t.Errorf("tenantCreationFields() setters mismatch (-want +got):\n%s", diff)
	}
}

// Test_tenantCreationData_typeImports pins the payload's import scope: the packages the
// record's field types name, so the handler file resolves a column type from another
// package (civil.Date) the way the handlers files do.
func Test_tenantCreationData_typeImports(t *testing.T) {
	t.Parallel()

	r := tenantCreationFixtureGenerator(t, "Station", func(r *resourceGenerator) {
		r.defaultOutlet.auth = openAuth("example.com/app/pkg/auth/staff")
	})
	outlets := r.allOutlets()
	data := r.tenantCreationData(r.resources[0], outlets[0])
	imports := data.typeImports()
	paths := make([]string, 0, len(imports))
	for _, imp := range imports {
		paths = append(paths, imp.path)
	}
	if !slices.Contains(paths, "cloud.google.com/go/civil") {
		t.Errorf("typeImports() = %v, want cloud.google.com/go/civil among them", paths)
	}
	if data.RoutePath != "/api/create-station" || data.Auth != "Staff" || data.SurfaceAccessor != "StaffUserSurface" || data.RolesAccessor != "StationAdministratorRoles" {
		t.Errorf("tenantCreationData() = route %q, auth %q, surface %q, roles %q", data.RoutePath, data.Auth, data.SurfaceAccessor, data.RolesAccessor)
	}
}

// Test_tenantCreationTemplate pins the generated handler: the contract it asserts on
// the application, the gate on a session holding a tenant, the row in its own
// transaction, the seat after the commit with the row removed when the seat fails, and
// the roster and signal before the answer.
func Test_tenantCreationTemplate(t *testing.T) {
	t.Parallel()

	r := tenantCreationFixtureGenerator(t, "Station", nil)
	key, fields, setters := tenantCreationFields(r.resources[0])
	out, err := r.generateTemplateOutput("tenantCreationTemplate", tenantCreationTemplate, &tenantCreationData{
		Source:              "resources",
		Package:             "app",
		LocalPackageImports: `"example.com/app/pkg/resources"` + "\n\t" + `"example.com/app/pkg/router"`,
		ApplicationName:     "App",
		ReceiverName:        "a",
		ResourcePackage:     "resources",
		RouterPackage:       "router",
		Record:              "Station",
		RecordLower:         "station",
		Shape:               "self-service",
		HandlerFunc:         "CreateStation",
		RoutePath:           "/api/create-station",
		Auth:                "Staff",
		AuthType:            "StaffAuth",
		SurfaceAccessor:     "StaffUserSurface",
		RolesAccessor:       "StationAdministratorRoles",
		Key:                 key,
		Fields:              fields,
		Setters:             setters,
		Refusal:             tenantCreationRefusal("Station"),
	})
	if err != nil {
		t.Fatalf("generateTemplateOutput() error = %v", err)
	}
	if _, err := format.Source(out); err != nil {
		t.Fatalf("the rendered file does not parse: %v\n%s", err, out)
	}

	assertContains(t, "tenantCreationTemplate", string(out), []string{
		"StationAdministratorRoles() []accesstypes.Role",
		"StaffUserSurface() *resource.UserSurface[router.StaffAuth]",
		"} = (*App)(nil)",
		"func (a *App) CreateStation() http.HandlerFunc {",
		"ID string `json:\"id\" validate:\"required\"`",
		"Name string `json:\"name\" validate:\"required\"`",
		"Motto *string `json:\"motto\"`",
		"Opened civil.Date `json:\"opened\" validate:\"required\"`",
		"ID string `json:\"id\"`",
		"decoder := resource.MustNewBodyDecoder[request](a.Validator())",
		"roles := a.StationAdministratorRoles()",
		"seats := a.StaffUserSurface().SeatStore()",
		"held, err := userPermissions.Domains(ctx)",
		`httpio.NewForbiddenMessage("the session already holds a station; a person creates one station and becomes its administrator")`,
		"patch := resources.NewStationCreatePatch(req.ID)",
		"patch.SetName(req.Name)",
		"patch.SetMotto(req.Motto)",
		"patch.SetOpened(req.Opened)",
		"resource.SeatFirstAdministrator(ctx, seats, userPermissions, domain, roles)",
		"if err := resources.NewStationDeletePatch(req.ID).Buffer(ctx, txn, eventSource); err != nil {",
		"if err := a.TenantRoster().Reload(ctx); err != nil {",
		"a.LiveService().Signal(ctx, resource.KindTenants)",
		"return httpio.NewEncoder(w).Ok(response{ID: req.ID})",
	}, []string{"Derived", "SetID", "SetDerived", "sessioninfo", "EstablishedBy"})

	// The gate before the body, the commit before the roster, the roster before the
	// seat, the seat before the signal, the signal before the answer.
	s := string(out)
	gate := strings.Index(s, "if len(held) > 0 {")
	decode := strings.Index(s, "req, err := decoder.Decode(r)")
	commit := strings.Index(s, "}); err != nil {")
	roster := strings.Index(s, "a.TenantRoster().Reload(ctx)")
	seat := strings.Index(s, "resource.SeatFirstAdministrator(")
	signal := strings.Index(s, "Signal(ctx, resource.KindTenants)")
	answer := strings.Index(s, "return httpio.NewEncoder(w).Ok(")
	if gate >= decode || decode >= commit || commit >= roster || roster >= seat || seat >= signal || signal >= answer {
		t.Errorf("order: gate %d, decode %d, commit %d, roster %d, seat %d, signal %d, answer %d; want ascending:\n%s", gate, decode, commit, roster, seat, signal, answer, out)
	}
}

// Test_accumulateTenantCreationRoute pins the route's registration: a POST under the
// prefix of every session outlet the record is on, none on an API-key outlet, and none
// at all for an administrator-only record.
func Test_accumulateTenantCreationRoute(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		record     string
		mutate     func(r *resourceGenerator)
		wantRoutes map[string]*generatedRoute
	}{
		{
			name:       "an administrator-only record registers no route",
			record:     "Sector",
			mutate:     func(r *resourceGenerator) { r.defaultOutlet.auth = openAuth("example.com/app/pkg/auth/staff") },
			wantRoutes: map[string]*generatedRoute{},
		},
		{
			name:   "a self-service record on every session outlet it is on",
			record: "Station",
			mutate: func(r *resourceGenerator) {
				r.resources[0].OutletNames = []string{defaultOutletName, "members", "machines"}
				r.defaultOutlet.auth = openAuth("example.com/app/pkg/auth/staff")
				r.extraOutlets = []routerOutlet{
					{name: "members", prefix: "members", servesSessions: true, auth: openAuth("example.com/app/pkg/auth/staff")},
					{name: "machines", prefix: "machines", apiKey: true},
				}
			},
			wantRoutes: map[string]*generatedRoute{
				defaultOutletName: {Method: "POST", Path: "/api/create-station", HandlerFunc: "CreateStation", TestURL: "/api/create-station"},
				"members":         {Method: "POST", Path: "/members/create-station", HandlerFunc: "CreateStation", TestURL: "/members/create-station"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := tenantCreationFixtureGenerator(t, tt.record, tt.mutate)
			outlets := r.allOutlets()
			outletRoutes := make([]*outletRouteData, len(outlets))
			for i, outlet := range outlets {
				outletRoutes[i] = &outletRouteData{Name: outlet.name, RoutesMap: map[string][]*generatedRoute{}}
			}
			testRoutes := r.accumulateTenantCreationRoute(outlets, outletRoutes)

			gotRoutes := map[string]*generatedRoute{}
			for _, outlet := range outletRoutes {
				routes := outlet.RoutesMap["CreateStation"]
				if len(routes) == 0 {
					continue
				}
				if len(routes) != 1 {
					t.Errorf("outlet %s registers %d routes, want 1", outlet.Name, len(routes))
				}
				gotRoutes[outlet.Name] = routes[0]
			}
			if diff := cmp.Diff(tt.wantRoutes, gotRoutes, cmp.AllowUnexported(generatedRoute{})); diff != "" {
				t.Errorf("registered routes mismatch (-want +got):\n%s", diff)
			}
			if got, want := len(testRoutes), len(tt.wantRoutes); got != want {
				t.Errorf("dispatch test routes = %d, want %d", got, want)
			}
		})
	}
}

// Test_tenantCreationAuthzCases pins the matrix's cases for the route: one no-grant
// case per session outlet the record is on, named for the outlet off the default.
func Test_tenantCreationAuthzCases(t *testing.T) {
	t.Parallel()

	r := tenantCreationFixtureGenerator(t, "Station", func(r *resourceGenerator) {
		r.resources[0].OutletNames = []string{defaultOutletName, "members"}
		r.defaultOutlet.auth = openAuth("example.com/app/pkg/auth/staff")
		r.extraOutlets = []routerOutlet{{name: "members", prefix: "members", servesSessions: true, auth: openAuth("example.com/app/pkg/auth/staff")}}
	})
	want := []authzCase{
		{Name: "CreateStation", Method: "http.MethodPost", URL: "/api/create-station", Body: "{}", NoGrant: true},
		{Name: "CreateStation (members)", Method: "http.MethodPost", URL: "/members/create-station", Body: "{}", NoGrant: true},
	}
	if diff := cmp.Diff(want, r.tenantCreationAuthzCases(), cmp.AllowUnexported(authzCase{})); diff != "" {
		t.Errorf("tenantCreationAuthzCases() mismatch (-want +got):\n%s", diff)
	}
	if cases := tenantCreationFixtureGenerator(t, "Sector", nil).tenantCreationAuthzCases(); cases != nil {
		t.Errorf("an administrator-only record has cases %v, want none", cases)
	}
}

// Test_authzTestTemplate_noGrant pins the no-grant case's two halves: a caller holding
// a grant is refused, a caller holding none reaches the body's validation.
func Test_authzTestTemplate_noGrant(t *testing.T) {
	t.Parallel()

	out, err := (&client{}).generateTemplateOutput("authzTestTemplate", authzTestTemplate, &authzTestData{
		Package: "authz",
		Cases:   []authzCase{{Name: "CreateStation", Method: "http.MethodPost", URL: "/api/create-station", Body: "{}", NoGrant: true}},
	})
	if err != nil {
		t.Fatalf("generateTemplateOutput() error = %v", err)
	}
	assertContains(t, "authzTestTemplate", string(out),
		[]string{
			"name:         \"CreateStation refused to a caller holding a tenant\",",
			"grants:       grants{accesstypes.List: true},",
			"wantStatuses: []int{http.StatusForbidden},",
			"name:         \"CreateStation admits a caller holding no grant\",",
			"wantStatuses: []int{http.StatusBadRequest},",
		},
		[]string{"CreateStation denied", "CreateStation granted", "CreateStation open"})
}

// Test_apiClientData_tenantCreation pins the client's block: the route constant and
// the request's interface on an outlet serving the route, nothing elsewhere.
func Test_apiClientData_tenantCreation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		record          string
		outlet          string
		wantContains    []string
		wantNotContains []string
	}{
		{
			name:   "a both record on the client's outlet",
			record: "Colony",
			wantContains: []string{
				"The create-your-colony route (@tenant(creation: both)): POST create-colony under the API",
				"export const createColonyRoute = 'create-colony';",
				"export interface CreateColony {\n  id: string;\n  name: string;\n}",
			},
		},
		{
			name:   "a self-service record's nullable column is optional and takes null",
			record: "Station",
			wantContains: []string{
				"export const createStationRoute = 'create-station';",
				"export interface CreateStation {\n  id: string;\n  name: string;\n  motto?: string | null;\n  opened: string;\n}",
			},
			wantNotContains: []string{"derived"},
		},
		{
			name:            "an administrator-only record has no block",
			record:          "Sector",
			wantNotContains: []string{"createSectorRoute", "CreateSector"},
		},
		{
			name:            "a client for an outlet the record is off has no block",
			record:          "Station",
			outlet:          "members",
			wantNotContains: []string{"createStationRoute", "CreateStation"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := tenantCreationFixtureGenerator(t, tt.record, nil)
			for _, f := range r.resources[0].Fields {
				f.typescriptType = "string"
			}
			gen := &typescriptGenerator{client: r.client, outletName: tt.outlet}
			out, err := gen.generateTemplateOutput("typescriptAPITemplate", typescriptAPITemplate, gen.apiClientData())
			if err != nil {
				t.Fatalf("generateTemplateOutput() error = %v", err)
			}
			assertContains(t, "typescriptAPITemplate", string(out), tt.wantContains, tt.wantNotContains)
		})
	}
}
