package generation

import (
	"context"
	"strings"
	"testing"
)

func TestApplicationName(t *testing.T) {
	testCases := []struct {
		name                    string
		opt                     ResourceOption
		expectedApplicationName string
		expectedReceiverName    string
	}{
		{
			name:                    "sets the application and receiver names",
			opt:                     ApplicationName("Server"),
			expectedApplicationName: "Server",
			expectedReceiverName:    "s",
		},
		{
			name:                    "uses the default application and receiver names",
			opt:                     nil,
			expectedApplicationName: "App",
			expectedReceiverName:    "a",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			opts := []ResourceOption{
				WithSpannerEmulatorVersion("1.5.55"),
			}
			if tc.opt != nil {
				opts = append(opts, tc.opt)
			}

			r, err := NewResourceGenerator(
				context.Background(),
				"testdata/resources.go",
				[]string{"file://generation/testdata/migrations"},
				[]string{},
				opts...,
			)
			if err != nil {
				t.Fatalf("NewResourceGenerator() error = %v, want no error", err)
			}

			rg, ok := r.(*resourceGenerator)
			if !ok {
				t.Fatalf("expected a *resourceGenerator, got %T", r)
			}
			if rg.applicationName != tc.expectedApplicationName {
				t.Errorf("expected application name %q, got %q", tc.expectedApplicationName, rg.applicationName)
			}
			if rg.receiverName != tc.expectedReceiverName {
				t.Errorf("expected receiver name %q, got %q", tc.expectedReceiverName, rg.receiverName)
			}
		})
	}
}

func TestWithRouterOutlet(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		outletName   string
		routePrefix  string
		wantErr      bool
		wantContains string
	}{
		{name: "valid lowerCamel name", outletName: "automation", routePrefix: "automation"},
		{name: "valid multi-word name", outletName: "partnerApi", routePrefix: "partner"},
		{name: "reserved default name", outletName: "default", routePrefix: "machine", wantErr: true, wantContains: "reserved"},
		{name: "PascalCase name", outletName: "Automation", routePrefix: "automation", wantErr: true, wantContains: "lowerCamelCase"},
		{name: "name with dash", outletName: "partner-api", routePrefix: "partner", wantErr: true, wantContains: "lowerCamelCase"},
		{name: "empty prefix", outletName: "automation", routePrefix: "", wantErr: true, wantContains: "non-empty route prefix"},
		{name: "prefix with braces", outletName: "automation", routePrefix: "a{b}", wantErr: true, wantContains: "must not contain"},
		{name: "prefix with leading slash", outletName: "automation", routePrefix: "/automation", wantErr: true, wantContains: "must not contain"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rg := &resourceGenerator{}
			opt, ok := WithRouterOutlet(tt.outletName, tt.routePrefix).(resourceOption)
			if !ok {
				t.Fatal("WithRouterOutlet must be a resourceOption")
			}
			err := opt(rg)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				if tt.wantContains != "" && !strings.Contains(err.Error(), tt.wantContains) {
					t.Fatalf("error %q does not contain %q", err, tt.wantContains)
				}

				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(rg.extraOutlets) != 1 || rg.extraOutlets[0].name != tt.outletName || rg.extraOutlets[0].prefix != tt.routePrefix {
				t.Fatalf("extraOutlets = %+v", rg.extraOutlets)
			}
		})
	}
}

func Test_validateOutletConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		genRoutes    bool
		routePrefix  string
		extraOutlets []routerOutlet
		wantErr      bool
		wantContains string
	}{
		{name: "no extra outlets needs nothing", genRoutes: false},
		{
			name:         "requires GenerateRoutes",
			extraOutlets: []routerOutlet{{name: "automation", prefix: "automation"}},
			wantErr:      true, wantContains: "requires GenerateRoutes",
		},
		{
			name: "distinct prefixes pass", genRoutes: true, routePrefix: "api",
			extraOutlets: []routerOutlet{{name: "automation", prefix: "automation"}, {name: "partnerApi", prefix: "partner"}},
		},
		{
			name: "duplicate name rejected", genRoutes: true, routePrefix: "api",
			extraOutlets: []routerOutlet{{name: "automation", prefix: "automation"}, {name: "automation", prefix: "machine"}},
			wantErr:      true, wantContains: "redeclares outlet",
		},
		{
			name: "case-insensitive duplicate name rejected", genRoutes: true, routePrefix: "api",
			extraOutlets: []routerOutlet{{name: "partnerApi", prefix: "partner"}, {name: "partnerapi", prefix: "machine"}},
			wantErr:      true, wantContains: "redeclares outlet",
		},
		{
			name: "prefix equal to default rejected", genRoutes: true, routePrefix: "api",
			extraOutlets: []routerOutlet{{name: "automation", prefix: "api"}},
			wantErr:      true, wantContains: "must not equal or nest",
		},
		{
			name: "prefix nested under default rejected", genRoutes: true, routePrefix: "api",
			extraOutlets: []routerOutlet{{name: "automation", prefix: "api/v2"}},
			wantErr:      true, wantContains: "must not equal or nest",
		},
		{
			name: "default nested under prefix rejected", genRoutes: true, routePrefix: "machine/api",
			extraOutlets: []routerOutlet{{name: "automation", prefix: "machine"}},
			wantErr:      true, wantContains: "must not equal or nest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rg := &resourceGenerator{genRoutes: tt.genRoutes, routePrefix: tt.routePrefix, extraOutlets: tt.extraOutlets}
			err := rg.validateOutletConfig()
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				if !strings.Contains(err.Error(), tt.wantContains) {
					t.Fatalf("error %q does not contain %q", err, tt.wantContains)
				}

				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestServesSessions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		options []OutletOption
		want    bool
	}{
		{name: "without the option the outlet serves no sessions", options: nil, want: false},
		{name: "the option marks the outlet session-serving", options: []OutletOption{ServesSessions()}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rg := &resourceGenerator{}
			opt, ok := WithRouterOutlet("portal", "portal", tt.options...).(resourceOption)
			if !ok {
				t.Fatal("WithRouterOutlet must be a resourceOption")
			}
			if err := opt(rg); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := rg.extraOutlets[0].servesSessions; got != tt.want {
				t.Errorf("servesSessions = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_validateTypescriptOutletTargets(t *testing.T) {
	t.Parallel()

	outlets := []routerOutlet{
		{name: "portal", prefix: "portal", servesSessions: true},
		{name: "automation", prefix: "automation"},
	}

	tests := []struct {
		name         string
		options      []TSOption
		wantErr      bool
		wantContains string
	}{
		{name: "a target without ForOutlet serves the default outlet"},
		{name: "naming the default outlet explicitly passes", options: []TSOption{ForOutlet("default")}},
		{name: "a session-serving outlet passes", options: []TSOption{ForOutlet("portal")}},
		{
			name: "a session-less outlet is rejected", options: []TSOption{ForOutlet("automation")},
			wantErr: true, wantContains: "does not serve browser sessions",
		},
		{
			name: "an undeclared outlet is rejected", options: []TSOption{ForOutlet("bogus")},
			wantErr: true, wantContains: "undeclared outlet",
		},
		{
			name: "an empty outlet name is rejected", options: []TSOption{ForOutlet("")},
			wantErr: true, wantContains: "requires an outlet name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rg := &resourceGenerator{
				routePrefix:       "api",
				extraOutlets:      outlets,
				typescriptTargets: []typescriptTarget{{destination: "ui/src", options: tt.options}},
			}
			err := rg.validateTypescriptOutletTargets()
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				if !strings.Contains(err.Error(), tt.wantContains) {
					t.Fatalf("error %q does not contain %q", err, tt.wantContains)
				}

				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func Test_outletMembership_OnOutlet(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		outlets []string
		query   string
		want    bool
	}{
		{name: "unannotated is on default", outlets: nil, query: "default", want: true},
		{name: "unannotated is not on extras", outlets: nil, query: "automation", want: false},
		{name: "naming an outlet replaces the default", outlets: []string{"automation"}, query: "default", want: false},
		{name: "named outlet matches", outlets: []string{"automation"}, query: "automation", want: true},
		{name: "default kept when named", outlets: []string{"default", "automation"}, query: "default", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := &outletMembership{OutletNames: tt.outlets}
			if got := m.OnOutlet(tt.query); got != tt.want {
				t.Errorf("OnOutlet(%q) = %v, want %v", tt.query, got, tt.want)
			}
		})
	}
}
