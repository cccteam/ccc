package app

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

const editProgram = `package main

import "github.com/cccteam/ccc/resource/generation"

func run() error {
	g, err := generation.NewResourceGenerator(nil, "pkg/resources", []string{"file://schema/migrations"},
		generation.GenerateHandlers("app"),
		// Routes under /api.
		generation.GenerateRoutes("pkg/router", "api"),
		generation.GenerateTypescript("web/console/src/app/core/service",
			generation.GenerateEnums(),
		),
	)
	_ = g

	return err
}
`

func TestInsertOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		src     string
		after   string
		options []string
		want    string
		wantErr string
	}{
		{
			name:    "after an option in the middle, keeping the comment that follows",
			src:     editProgram,
			after:   "GenerateHandlers",
			options: []string{`generation.WithRPC("pkg/rpc")`},
			want: strings.Replace(editProgram, "\t\tgeneration.GenerateHandlers(\"app\"),\n",
				"\t\tgeneration.GenerateHandlers(\"app\"),\n\t\tgeneration.WithRPC(\"pkg/rpc\"),\n", 1),
		},
		{
			name:  "after the last option, two at once",
			src:   editProgram,
			after: "",
			options: []string{
				`generation.WithRouterOutlet("portal", "portal/api", generation.ServesSessions())`,
				`generation.WithRouterOutlet("machines", "machines")`,
			},
			want: strings.Replace(editProgram, "\t\t\tgeneration.GenerateEnums(),\n\t\t),\n",
				"\t\t\tgeneration.GenerateEnums(),\n\t\t),\n\t\tgeneration.WithRouterOutlet(\"portal\", \"portal/api\", generation.ServesSessions()),\n\t\tgeneration.WithRouterOutlet(\"machines\", \"machines\"),\n", 1),
		},
		{
			name: "an aliased import is honored",
			src: `package main

import gen "github.com/cccteam/ccc/resource/generation"

func run() {
	gen.NewResourceGenerator(nil, "pkg/resources", nil, gen.GenerateHandlers("app"))
}
`,
			after:   "GenerateHandlers",
			options: []string{`generation.GenerateRoutes("pkg/router", "api")`},
			want: `package main

import gen "github.com/cccteam/ccc/resource/generation"

func run() {
	gen.NewResourceGenerator(nil, "pkg/resources", nil, gen.GenerateHandlers("app"),
		gen.GenerateRoutes("pkg/router", "api"))
}
`,
		},
		{
			name:    "no anchor",
			src:     editProgram,
			after:   "WithRPC",
			options: []string{`generation.WithRouterOutlet("portal", "portal/api")`},
			wantErr: "no WithRPC option to insert after",
		},
		{
			name:    "no generator program",
			src:     "package main\n\nfunc main() {}\n",
			after:   "",
			options: []string{`generation.WithRPC("pkg/rpc")`},
			wantErr: "does not import github.com/cccteam/ccc/resource/generation",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := InsertOptions("main.go", []byte(tt.src), tt.after, tt.options)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("InsertOptions() error = %v, want %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("InsertOptions() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, string(got)); diff != "" {
				t.Errorf("InsertOptions() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestOptionText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, option, first string
		want                string
	}{
		{
			name: "the default client target", option: "GenerateTypescript", first: "web/console/src/app/core/service",
			want: "generation.GenerateTypescript(\"web/console/src/app/core/service\",\n\t\t\tgeneration.GenerateEnums(),\n\t\t)",
		},
		{name: "a target the program lacks", option: "GenerateTypescript", first: "web/portal/src/app/core/service"},
		{name: "an option without a string first argument", option: "GenerateEnums", first: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := OptionText("main.go", []byte(editProgram), tt.option, tt.first)
			if err != nil {
				t.Fatalf("OptionText() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("OptionText() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRemoveOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, option, first string
		want                string
		wantN               int
	}{
		{
			name: "an option with the comment that introduces it", option: "GenerateRoutes", first: "pkg/router",
			want:  strings.Replace(editProgram, "\t\t// Routes under /api.\n\t\tgeneration.GenerateRoutes(\"pkg/router\", \"api\"),\n", "", 1),
			wantN: 1,
		},
		{
			name: "the last option, spanning lines", option: "GenerateTypescript", first: "web/console/src/app/core/service",
			want:  strings.Replace(editProgram, "\t\tgeneration.GenerateTypescript(\"web/console/src/app/core/service\",\n\t\t\tgeneration.GenerateEnums(),\n\t\t),\n", "", 1),
			wantN: 1,
		},
		{name: "an option the program lacks", option: "WithRouterOutlet", first: "portal", want: editProgram},
		{name: "the same option under another first argument", option: "GenerateRoutes", first: "pkg/other", want: editProgram},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, n, err := RemoveOptions("main.go", []byte(editProgram), tt.option, tt.first)
			if err != nil {
				t.Fatalf("RemoveOptions() error = %v", err)
			}
			if n != tt.wantN {
				t.Errorf("RemoveOptions() n = %d, want %d", n, tt.wantN)
			}
			if diff := cmp.Diff(tt.want, string(got)); diff != "" {
				t.Errorf("RemoveOptions() diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAddAuthMethod(t *testing.T) {
	t.Parallel()

	src := `package main

import gen "github.com/cccteam/ccc/resource/generation"

func run() {
	_, _ = gen.NewResourceGenerator(nil, "pkg/resources", nil,
		gen.GenerateRouter(),
		gen.GenerateRoutes("pkg/router", "api", gen.Auth("example.com/acme/pkg/auth/staff", gen.PasswordMethod()), gen.WebApp("/")),
		gen.WithRouterOutlet("portal", "portal/api", gen.Auth("example.com/acme/pkg/auth/members", gen.GoogleMethod("directory").Labeled("Company directory"))),
		gen.WithRouterOutlet("kiosk", "kiosk", gen.Auth("example.com/acme/pkg/auth/staff", gen.PasswordMethod())),
	)
}
`
	tests := []struct {
		name        string
		importPath  string
		method      string
		declaration string
		wantCount   int
		wantContain []string
	}{
		{
			name: "every outlet bound to the package", importPath: "example.com/acme/pkg/auth/staff", method: "entra", declaration: `generation.AzureMethod("entra")`, wantCount: 2,
			wantContain: []string{`gen.Auth("example.com/acme/pkg/auth/staff", gen.PasswordMethod(), gen.AzureMethod("entra")), gen.WebApp("/")`, `gen.WithRouterOutlet("kiosk", "kiosk", gen.Auth("example.com/acme/pkg/auth/staff", gen.PasswordMethod(), gen.AzureMethod("entra")))`, `gen.Auth("example.com/acme/pkg/auth/members", gen.GoogleMethod("directory").Labeled("Company directory"))`},
		},
		{
			name: "a method the outlet declares already", importPath: "example.com/acme/pkg/auth/members", method: "directory", declaration: `generation.GoogleMethod("directory")`, wantCount: 0,
			wantContain: []string{`gen.GoogleMethod("directory").Labeled("Company directory"))`},
		},
		{
			name: "a package no outlet binds to", importPath: "example.com/acme/pkg/auth/partners", method: "entra", declaration: `generation.AzureMethod("entra")`, wantCount: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			out, n, err := AddAuthMethod("main.go", []byte(src), tt.importPath, tt.method, tt.declaration)
			if err != nil {
				t.Fatalf("AddAuthMethod() error = %v", err)
			}
			if n != tt.wantCount {
				t.Errorf("edited = %d, want %d", n, tt.wantCount)
			}
			for _, want := range tt.wantContain {
				if !strings.Contains(string(out), want) {
					t.Errorf("output lacks %q:\n%s", want, out)
				}
			}
		})
	}
}

func TestBindOutletAuth(t *testing.T) {
	t.Parallel()

	const src = `package main

import gen "github.com/cccteam/ccc/resource/generation"

func run() {
	_, _ = gen.NewResourceGenerator(nil, "pkg/resources", nil,
		gen.GenerateRouter(),
		gen.GenerateRoutes("pkg/router", "console/api",
			gen.Auth("example.com/acme/pkg/auth/staff", gen.PasswordMethod()),
			gen.WebApp("/console"),
		),
		gen.WithRouterOutlet("portal", "portal/api", gen.Auth("example.com/acme/pkg/auth/staff", gen.PasswordMethod()), gen.WebApp("/portal")),
		gen.WithRouterOutlet("kiosk", "kiosk/api",
			gen.WebApp("/kiosk"),
		),
		gen.WithRouterOutlet("machines", "machines", gen.APIKey()),
		gen.WithRouterOutlet("bare", "bare"),
	)
}
`
	const declaration = `generation.Auth("example.com/acme/pkg/auth/members", generation.GoogleMethod("google"))`
	tests := []struct {
		name         string
		outlet       string
		wantReplaced bool
		wantContain  string
		wantErr      string
	}{
		{
			name: "an outlet declaring an auth on one line", outlet: "portal", wantReplaced: true,
			wantContain: `gen.WithRouterOutlet("portal", "portal/api", gen.Auth("example.com/acme/pkg/auth/members", gen.GoogleMethod("google")), gen.WebApp("/portal")),`,
		},
		{
			name: "the default outlet, its options one per line", outlet: DefaultOutletName, wantReplaced: true,
			wantContain: "\t\tgen.GenerateRoutes(\"pkg/router\", \"console/api\",\n\t\t\tgen.Auth(\"example.com/acme/pkg/auth/members\", gen.GoogleMethod(\"google\")),\n\t\t\tgen.WebApp(\"/console\"),\n\t\t),",
		},
		{
			name: "an outlet declaring no auth, its options one per line", outlet: "kiosk",
			wantContain: "\t\tgen.WithRouterOutlet(\"kiosk\", \"kiosk/api\",\n\t\t\tgen.Auth(\"example.com/acme/pkg/auth/members\", gen.GoogleMethod(\"google\")),\n\t\t\tgen.WebApp(\"/kiosk\"),\n\t\t),",
		},
		{
			name: "an outlet declaring nothing", outlet: "bare",
			wantContain: `gen.WithRouterOutlet("bare", "bare", gen.Auth("example.com/acme/pkg/auth/members", gen.GoogleMethod("google"))),`,
		},
		{name: "an outlet the program does not declare", outlet: "shop", wantErr: "declares no shop outlet"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			out, replaced, err := BindOutletAuth("main.go", []byte(src), tt.outlet, declaration)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("BindOutletAuth() error = %v, want %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("BindOutletAuth() error = %v", err)
			}
			if replaced != tt.wantReplaced {
				t.Errorf("replaced = %t, want %t", replaced, tt.wantReplaced)
			}
			if !strings.Contains(string(out), tt.wantContain) {
				t.Errorf("output lacks %q:\n%s", tt.wantContain, out)
			}
			if strings.Count(string(out), "pkg/auth/members") != 1 {
				t.Errorf("the members auth is declared %d times, want once:\n%s", strings.Count(string(out), "pkg/auth/members"), out)
			}
		})
	}
}

func TestMoveDefaultOutlet(t *testing.T) {
	t.Parallel()

	const rooted = `package main

import gen "github.com/cccteam/ccc/resource/generation"

func run() {
	_, _ = gen.NewResourceGenerator(nil, "pkg/resources", nil,
		gen.GenerateRouter(),
		gen.GenerateRoutes("pkg/router", "api",
			gen.Auth("example.com/acme/pkg/auth/staff", gen.PasswordMethod()),
			gen.WebApp("/"),
		),
		gen.WithRouterOutlet("portal", "portal/api", gen.Auth("example.com/acme/pkg/auth/members", gen.GoogleMethod("directory")), gen.WebApp("/portal")),
	)
}
`
	const handWritten = `package main

import "github.com/cccteam/ccc/resource/generation"

func run() {
	_, _ = generation.NewResourceGenerator(nil, "pkg/resources", nil,
		generation.GenerateRoutes("pkg/router", "api"),
	)
}
`
	const noRoutes = `package main

import "github.com/cccteam/ccc/resource/generation"

func run() {
	_, _ = generation.NewResourceGenerator(nil, "pkg/resources", nil,
		generation.GenerateHandlers("app"),
	)
}
`
	tests := []struct {
		name        string
		src         string
		wantErr     string
		wantContain []string
		wantAbsent  []string
	}{
		{
			name: "the prefix and the mount path move together",
			src:  rooted,
			wantContain: []string{
				"\t\tgen.GenerateRoutes(\"pkg/router\", \"console/api\",\n\t\t\tgen.Auth(\"example.com/acme/pkg/auth/staff\", gen.PasswordMethod()),\n\t\t\tgen.WebApp(\"/console\"),\n\t\t),\n",
				`gen.WithRouterOutlet("portal", "portal/api", gen.Auth("example.com/acme/pkg/auth/members", gen.GoogleMethod("directory")), gen.WebApp("/portal"))`,
			},
			wantAbsent: []string{`"api",`, `gen.WebApp("/")`},
		},
		{
			name:        "without a WebApp only the prefix moves",
			src:         handWritten,
			wantContain: []string{`generation.GenerateRoutes("pkg/router", "console/api"),`},
			wantAbsent:  []string{"WebApp"},
		},
		{
			name:    "no GenerateRoutes to move",
			src:     noRoutes,
			wantErr: "main.go: no GenerateRoutes with a prefix to move",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			out, err := MoveDefaultOutlet("main.go", []byte(tt.src), "console/api", "/console")
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("MoveDefaultOutlet() error = %v, want %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("MoveDefaultOutlet() error = %v", err)
			}
			for _, want := range tt.wantContain {
				if !strings.Contains(string(out), want) {
					t.Errorf("output lacks %q:\n%s", want, out)
				}
			}
			for _, absent := range tt.wantAbsent {
				if strings.Contains(string(out), absent) {
					t.Errorf("output has %q:\n%s", absent, out)
				}
			}
		})
	}
}

func TestChainMethodFlag(t *testing.T) {
	t.Parallel()

	src := `package main

import gen "github.com/cccteam/ccc/resource/generation"

func run() {
	_, _ = gen.NewResourceGenerator(nil, "pkg/resources", nil,
		gen.GenerateRouter(),
		gen.GenerateRoutes("pkg/router", "api", gen.Auth("example.com/acme/pkg/auth/staff", gen.PasswordMethod(), gen.GoogleMethod("google")), gen.WebApp("/")),
		gen.WithRouterOutlet("portal", "portal/api", gen.Auth("example.com/acme/pkg/auth/members", gen.GoogleMethod("directory").Labeled("Company directory").InvitedUsers(), gen.AzureMethod("entra").SyncsRoles())),
		gen.WithRouterOutlet("kiosk", "kiosk", gen.Auth("example.com/acme/pkg/auth/staff", gen.PasswordMethod(), gen.GoogleMethod("google"))),
	)
}
`
	tests := []struct {
		name        string
		importPath  string
		method      string
		flag        string
		wantCount   int
		wantContain []string
		wantErr     string
	}{
		{
			name: "every outlet bound to the package, after what the declaration chains", importPath: "example.com/acme/pkg/auth/staff", method: "google", flag: "SyncsRoles", wantCount: 2,
			wantContain: []string{`gen.Auth("example.com/acme/pkg/auth/staff", gen.PasswordMethod(), gen.GoogleMethod("google").SyncsRoles()), gen.WebApp("/")`, `gen.WithRouterOutlet("kiosk", "kiosk", gen.Auth("example.com/acme/pkg/auth/staff", gen.PasswordMethod(), gen.GoogleMethod("google").SyncsRoles()))`, `gen.GoogleMethod("directory").Labeled("Company directory").InvitedUsers(), gen.AzureMethod("entra").SyncsRoles()`},
		},
		{
			name: "a label and a flag already chained", importPath: "example.com/acme/pkg/auth/members", method: "directory", flag: "SyncsRoles", wantCount: 1,
			wantContain: []string{`gen.GoogleMethod("directory").Labeled("Company directory").InvitedUsers().SyncsRoles(), gen.AzureMethod("entra").SyncsRoles()`, `gen.GoogleMethod("google")), gen.WebApp("/")`},
		},
		{
			name: "a declaration chaining the flag already", importPath: "example.com/acme/pkg/auth/members", method: "entra", flag: "SyncsRoles", wantCount: 0,
			wantContain: []string{`gen.AzureMethod("entra").SyncsRoles())`},
		},
		{
			name: "the password method", importPath: "example.com/acme/pkg/auth/staff", method: "password", flag: "UsernameIsEmail", wantCount: 2,
			wantContain: []string{`gen.Auth("example.com/acme/pkg/auth/staff", gen.PasswordMethod().UsernameIsEmail(), gen.GoogleMethod("google")), gen.WebApp("/")`},
		},
		{
			name: "a package no outlet binds to", importPath: "example.com/acme/pkg/auth/partners", method: "google", flag: "SyncsRoles", wantCount: 0,
		},
		{
			name: "a method the outlet does not declare", importPath: "example.com/acme/pkg/auth/members", method: "google", flag: "SyncsRoles", wantCount: 0,
		},
		{
			name: "a word that is no flag", importPath: "example.com/acme/pkg/auth/staff", method: "google", flag: "Labeled",
			wantErr: `"Labeled" is not a flag a method declaration chains`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			out, n, err := ChainMethodFlag("main.go", []byte(src), tt.importPath, tt.method, tt.flag)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ChainMethodFlag() error = %v, want %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("ChainMethodFlag() error = %v", err)
			}
			if n != tt.wantCount {
				t.Errorf("edited = %d, want %d", n, tt.wantCount)
			}
			for _, want := range tt.wantContain {
				if !strings.Contains(string(out), want) {
					t.Errorf("output lacks %q:\n%s", want, out)
				}
			}
			if n == 0 && string(out) != src {
				t.Errorf("nothing to chain, but the source changed:\n%s", out)
			}
		})
	}
}
