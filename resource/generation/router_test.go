package generation

import (
	"testing"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func Test_readRouteTestParams(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		resourceName string
		pkNames      []string
		want         []routeTestParam
	}{
		{
			name:         "single primary key",
			resourceName: "Widget",
			pkNames:      []string{"ID"},
			want: []routeTestParam{
				{Key: "widgetID", Value: "testWidgetID"},
			},
		},
		{
			name:         "compound primary key",
			resourceName: "WidgetOrder",
			pkNames:      []string{"WidgetID", "OrderID"},
			want: []routeTestParam{
				{Key: "widgetOrderWidgetID", Value: "testWidgetOrderWidgetID"},
				{Key: "widgetOrderOrderID", Value: "testWidgetOrderOrderID"},
			},
		},
		{
			name:         "no primary keys",
			resourceName: "Widget",
			pkNames:      nil,
			want:         []routeTestParam{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := readRouteTestParams(tt.resourceName, tt.pkNames)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("readRouteTestParams() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// Test_negativeTestsForOutlet_sessionRoutes pins the permission-route half of the
// isolation cases: a session-less outlet's prefix must 404 for the permission routes,
// while a session-serving outlet contributes none.
func Test_negativeTestsForOutlet_sessionRoutes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		outlet routerOutlet
		want   []negativeRouterTest
	}{
		{
			name:   "session-less outlet gets 404 cases for the permission routes, the live routes and the feature flag routes",
			outlet: routerOutlet{name: "automation", prefix: "automation"},
			want: []negativeRouterTest{
				{Method: "http.MethodGet", URL: "/automation/permission-digest"},
				{Method: "http.MethodGet", URL: "/automation/user-domains"},
				{Method: "http.MethodPost", URL: "/automation/live/renew"},
				{Method: "http.MethodPost", URL: "/automation/live/unsubscribe"},
				{Method: "http.MethodGet", URL: "/automation/live/token"},
				{Method: "http.MethodGet", URL: "/automation/feature-flags"},
				{Method: "http.MethodPost", URL: "/automation/feature-flags"},
				{Method: "http.MethodGet", URL: "/automation/feature-flags/testFeatureFlagName"},
				{Method: "http.MethodPost", URL: "/automation/feature-flags/testFeatureFlagName"},
				{Method: "http.MethodPost", URL: "/automation/set-feature"},
			},
		},
		{
			name:   "session-serving outlet contributes no permission-route, live-route or feature-route cases",
			outlet: routerOutlet{name: "portal", prefix: "portal", servesSessions: true},
			want:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rg := &resourceGenerator{client: &client{}}
			got, err := rg.negativeTestsForOutlet(&tt.outlet)
			if err != nil {
				t.Fatalf("negativeTestsForOutlet() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("negativeTestsForOutlet() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// Test_resourceGenerator_validateAnnotatedOutlets_manualRegistrations pins that a
// manual registration's outlets are checked against the declared outlets like a
// struct's @outlet, so a typo fails generation instead of silently dropping the
// registration from every filtered target.
func Test_resourceGenerator_validateAnnotatedOutlets_manualRegistrations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		registrations []ManualRegistration
		wantErr       bool
	}{
		{name: "no outlets is the default outlet", registrations: []ManualRegistration{{Permission: accesstypes.Execute, Resource: "ViewAsUser"}}},
		{name: "declared outlets pass", registrations: []ManualRegistration{{Permission: accesstypes.Execute, Resource: "ViewAsUser", Outlets: []string{"default", "portal"}}}},
		{name: "an undeclared outlet fails", registrations: []ManualRegistration{{Permission: accesstypes.Execute, Resource: "ViewAsUser", Outlets: []string{"protal"}}}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := &resourceGenerator{
				client:              &client{},
				extraOutlets:        []routerOutlet{{name: "portal", prefix: "portal"}},
				manualRegistrations: tt.registrations,
			}
			if err := r.validateAnnotatedOutlets(); (err != nil) != tt.wantErr {
				t.Fatalf("validateAnnotatedOutlets() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// Test_singleKeyRouteTestParam pins that a single-key read route names its parameter
// after the key field: a resource keyed by Code reads {resourceCode}, so the generated
// route test and the handler's route constant agree.
func Test_singleKeyRouteTestParam(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		resourceName string
		pkName       string
		want         routeTestParam
	}{
		{name: "keyed by ID", resourceName: "Widget", pkName: "ID", want: routeTestParam{Key: "widgetID", Value: "testWidgetID"}},
		{name: "keyed by Code", resourceName: "Country", pkName: "Code", want: routeTestParam{Key: "countryCode", Value: "testCountryCode"}},
		{name: "keyed by a multi-word field", resourceName: "Ship", pkName: "RegistryNumber", want: routeTestParam{Key: "shipRegistryNumber", Value: "testShipRegistryNumber"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := singleKeyRouteTestParam(tt.resourceName, tt.pkName); got != tt.want {
				t.Errorf("singleKeyRouteTestParam() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// Test_authBindings pins what each outlet's routes bind for the live pages: a declared
// auth binds its package's Name, imported once however many outlets share it; a session
// outlet without a declared auth, as when the application writes its own router, takes
// the name as a parameter of its routes function and of NewTestRouter; an outlet
// without sessions and an API-key outlet bind nothing.
func Test_authBindings(t *testing.T) {
	t.Parallel()

	crew := &outletAuth{importPath: "example.com/app/pkg/auth/crew", flavor: Password}
	members := &outletAuth{importPath: "example.com/app/pkg/auth/members", flavor: OIDCGoogle}

	type binding struct {
		name, testParam string
		param           bool
	}
	tests := []struct {
		name           string
		outlets        []routerOutlet
		wantBindings   []binding
		wantImports    []string
		wantTestParams []string
		wantTestArgs   []string
	}{
		{
			name: "declared auths bind their packages' Name, each imported once",
			outlets: []routerOutlet{
				{name: "default", prefix: "api", servesSessions: true, auth: members},
				{name: "kiosk", prefix: "kiosk/api", servesSessions: true, auth: crew},
				{name: "portal", prefix: "portal/api", servesSessions: true, auth: members},
				{name: "droids", prefix: "droids", apiKey: true},
			},
			wantBindings: []binding{{name: "members.Name"}, {name: "crew.Name"}, {name: "members.Name"}, {}},
			wantImports:  []string{"example.com/app/pkg/auth/crew", "example.com/app/pkg/auth/members"},
		},
		{
			name: "under the application's own router each session outlet takes its auth's name",
			outlets: []routerOutlet{
				{name: "default", prefix: "api", servesSessions: true},
				{name: "field-portal", prefix: "portal/api", servesSessions: true, declaredSessions: true},
				{name: "automation", prefix: "automation"},
			},
			wantBindings: []binding{
				{name: "auth", param: true, testParam: "auth"},
				{name: "auth", param: true, testParam: "fieldPortalAuth"},
				{},
			},
			wantTestParams: []string{"auth", "fieldPortalAuth"},
			wantTestArgs:   []string{`"default"`, `"field-portal"`},
		},
		{
			name:         "an API-key default outlet binds nothing though the default serves sessions",
			outlets:      []routerOutlet{{name: "default", prefix: "api", servesSessions: true, apiKey: true}},
			wantBindings: []binding{{}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			routes := make([]*outletRouteData, len(tt.outlets))
			got := make([]binding, len(tt.outlets))
			for i := range tt.outlets {
				routes[i] = &outletRouteData{Name: tt.outlets[i].name}
				routes[i].AuthName, routes[i].AuthParam, routes[i].TestRouterParam = authBinding(&tt.outlets[i])
				got[i] = binding{name: routes[i].AuthName, param: routes[i].AuthParam, testParam: routes[i].TestRouterParam}
			}
			if diff := cmp.Diff(tt.wantBindings, got, cmp.AllowUnexported(binding{})); diff != "" {
				t.Errorf("bindings mismatch (-want +got):\n%s", diff)
			}

			imports, testParams, testArgs := authBindings(tt.outlets, routes)
			if diff := cmp.Diff(tt.wantImports, imports, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("imports mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantTestParams, testParams, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("NewTestRouter parameters mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantTestArgs, testArgs, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("test router arguments mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
