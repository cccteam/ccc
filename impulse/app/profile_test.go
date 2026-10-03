package app

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestProfile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		dir         string
		wantLayout  Layout
		wantSites   []Site
		wantShared  []string
		wantOutlets []string
	}{
		{
			name:       "flat",
			dir:        "testdata/flat",
			wantLayout: LayoutFlat,
			wantSites: []Site{{
				Name: "lighthouse", Dir: ".", GeneratedRouter: true,
				Default: Outlet{Name: DefaultOutletName, Prefix: "api", ServesSessions: true, Auth: &OutletAuth{ImportPath: "example.com/lighthouse/pkg/config", Flavor: "Password"}, WebApp: "/", Pos: "cmd/generate/resourcegenerator/main.go:25"},
				Outlets: []Outlet{{Name: "portal", Prefix: "portal", ServesSessions: true, Auth: &OutletAuth{ImportPath: "example.com/lighthouse/pkg/config", Flavor: "Password"}, WebApp: "/portal", Pos: "cmd/generate/resourcegenerator/main.go:26"}},
			}},
			wantOutlets: []string{"portal"},
		},
		{
			name:       "sites",
			dir:        "testdata/sites",
			wantLayout: LayoutSites,
			wantSites: []Site{
				{Name: "pilots", Dir: "apps/pilots", Default: Outlet{Name: DefaultOutletName, Prefix: "api", ServesSessions: true, Pos: "cmd/generate/resourcegenerator_pilots/main.go:16"}},
				{Name: "tugs", Dir: "apps/tugs", Default: Outlet{Name: DefaultOutletName, Prefix: "api", ServesSessions: true, Pos: "cmd/generate/resourcegenerator_tugs/main.go:18"}},
			},
			wantShared: []string{"cmd/generate/resourcegenerator_shared/main.go"},
		},
		{
			name:       "bad program reads as shared",
			dir:        "testdata/badprogram",
			wantLayout: LayoutFlat,
			wantShared: []string{"cmd/generate/main.go"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a, err := Discover(tt.dir)
			if err != nil {
				t.Fatalf("Discover() error = %v", err)
			}
			p := a.Profile()
			if p.Layout != tt.wantLayout {
				t.Errorf("Layout = %s, want %s", p.Layout, tt.wantLayout)
			}
			if diff := cmp.Diff(tt.wantSites, p.Sites, cmpopts.EquateEmpty(), cmpopts.IgnoreFields(Site{}, "Generator")); diff != "" {
				t.Errorf("Sites mismatch (-want +got):\n%s", diff)
			}
			for i, s := range p.Sites {
				if s.Generator == nil || s.Generator.HandlersDir() == "" {
					t.Errorf("Sites[%d].Generator emits no handlers", i)
				}
			}
			if diff := cmp.Diff(tt.wantShared, files(p.Shared), cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("Shared mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantOutlets, p.OutletNames(), cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("OutletNames() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSiteDir(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		resources string
		handlers  string
		routes    string
		want      string
	}{
		{name: "flat", resources: "pkg/resources", handlers: "app", routes: "pkg/router", want: "."},
		{name: "under apps", resources: "apps/console/pkg/resources", handlers: "apps/console/app", routes: "apps/console/pkg/router", want: "apps/console"},
		{name: "no routes", resources: "apps/console/pkg/resources", handlers: "apps/console/app", want: "apps/console"},
		{name: "mixed sites", resources: "apps/console/pkg/resources", handlers: "apps/portal/app", routes: "apps/console/pkg/router", want: "."},
		{name: "one package outside apps", resources: "apps/console/pkg/resources", handlers: "app", routes: "apps/console/pkg/router", want: "."},
		{name: "apps itself is not a site", resources: "apps/resources", handlers: "apps/app", routes: "apps/router", want: "."},
		{name: "other prefix", resources: "sites/console/pkg/resources", handlers: "sites/console/app", routes: "sites/console/pkg/router", want: "."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			g := &Generator{ResourcePackageDir: tt.resources, Options: []Call{
				{Name: "GenerateHandlers", Args: []Arg{{Kind: ArgString, Str: tt.handlers}}},
			}}
			if tt.routes != "" {
				g.Options = append(g.Options, Call{Name: "GenerateRoutes", Args: []Arg{{Kind: ArgString, Str: tt.routes}, {Kind: ArgString, Str: "api"}}})
			}
			if got := siteDir(g); got != tt.want {
				t.Errorf("siteDir() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTenancyScan(t *testing.T) {
	t.Parallel()

	a, err := Discover("testdata/flat")
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	wantResources := []DomainResource{{File: "pkg/resources/beacons.go", Line: 8, Name: "Beacon"}}
	if diff := cmp.Diff(wantResources, a.DomainResources); diff != "" {
		t.Errorf("DomainResources mismatch (-want +got):\n%s", diff)
	}
	if len(a.DefaultRoles) != 0 {
		t.Errorf("DefaultRoles = %v, want none", a.DefaultRoles)
	}
	if len(a.PolicyChecks) != 0 {
		t.Errorf("PolicyChecks = %v, want none", a.PolicyChecks)
	}
}

// TestReadOutletOptions pins what an outlet declaration's options set on the outlet: the
// web app's mount, the oldest answered release in either of its forms, and nothing from
// an argument the reader could not interpret.
func TestReadOutletOptions(t *testing.T) {
	t.Parallel()

	call := func(name string, args ...Arg) Arg {
		return Arg{Kind: ArgCall, Call: &Call{Name: name, Args: args}}
	}
	tests := []struct {
		name string
		args []Arg
		want Outlet
	}{
		{
			name: "a web app and a release",
			args: []Arg{call(optWebApp, Arg{Kind: ArgString, Str: "/console"}), call(optOldestAnswered, Arg{Kind: ArgString, Str: "1.5.0"})},
			want: Outlet{WebApp: "/console", OldestAnswered: "1.5.0"},
		},
		{
			name: "this release",
			args: []Arg{call(optOldestAnswered, Arg{Kind: ArgIdent, Str: ThisReleaseIdent})},
			want: Outlet{OldestAnswered: ThisReleaseIdent},
		},
		{
			name: "a release the reader could not interpret",
			args: []Arg{call(optOldestAnswered, Arg{Kind: ArgOther, Text: "release"})},
			want: Outlet{},
		},
		{
			name: "an API key outlet declares no release",
			args: []Arg{call(optAPIKey)},
			want: Outlet{APIKey: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var got Outlet
			readOutletOptions(&got, tt.args)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("readOutletOptions() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
