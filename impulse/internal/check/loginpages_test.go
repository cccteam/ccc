package check

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/impulse/app"
)

// The outlet declarations the login page tests compose a flat program from: the console
// on the default outlet bound to the staff auth, at the root or under /console, with the
// password method alone or with a Google directory method beside it, a portal outlet
// bound to the members auth under /portal, a machines outlet bound to the members auth
// serving no browser application, and a console declaring no auth, as a hand-written
// router's does.
const (
	staffAtRoot          = `generation.GenerateRoutes("pkg/router", "api", generation.Auth("example.com/harbor/pkg/auth/staff", generation.PasswordMethod()), generation.WebApp("/")),`
	staffAtConsole       = `generation.GenerateRoutes("pkg/router", "console/api", generation.Auth("example.com/harbor/pkg/auth/staff", generation.PasswordMethod()), generation.WebApp("/console")),`
	staffGoogleAtRoot    = `generation.GenerateRoutes("pkg/router", "api", generation.Auth("example.com/harbor/pkg/auth/staff", generation.PasswordMethod(), generation.GoogleMethod("google")), generation.WebApp("/")),`
	staffGoogleAtConsole = `generation.GenerateRoutes("pkg/router", "console/api", generation.Auth("example.com/harbor/pkg/auth/staff", generation.PasswordMethod(), generation.GoogleMethod("google")), generation.WebApp("/console")),`
	membersAtPortal      = `generation.WithRouterOutlet("portal", "portal/api", generation.Auth("example.com/harbor/pkg/auth/members", generation.AzureMethod("directory")), generation.WebApp("/portal")),`
	membersMachines      = `generation.WithRouterOutlet("machines", "machines", generation.Auth("example.com/harbor/pkg/auth/members", generation.AzureMethod("directory"))),`
	consoleUndeclared    = `generation.GenerateRoutes("pkg/router", "api", generation.WebApp("/")),`
)

// loginProgram renders a flat generator program under the generated router with the
// outlet declarations.
func loginProgram(outlets ...string) string {
	return program("pkg/resources", append([]string{`generation.GenerateHandlers("app"),`, `generation.GenerateRouter(),`}, outlets...)...)
}

// dataLevel renders the data level: the environment struct and the settings
// constructions, each a method on it building one auth's Settings literal. The first
// construction's literal is on line 13 and its first field after the cookie key on line
// 15; a construction takes 5 lines plus its fields, and a blank line separates two.
func dataLevel(constructions ...string) string {
	return `package config

import (
	"example.com/harbor/pkg/auth/members"
	"example.com/harbor/pkg/auth/staff"
)

type dataConfig struct {
	PublicURL string
}

` + strings.Join(constructions, "\n\n") + "\n"
}

// settingsFunc renders one auth's settings construction on the environment struct, with
// the fields after the cookie key.
func settingsFunc(auth string, fields ...string) string {
	lines := append([]string{`CookieKey: "key",`}, fields...)

	return "func (env *dataConfig) " + auth + "Settings() *" + auth + ".Settings {\n\treturn &" + auth + ".Settings{\n\t\t" + strings.Join(lines, "\n\t\t") + "\n\t}\n}"
}

func TestLoginPages(t *testing.T) {
	t.Parallel()

	const (
		dataFile  = "pkg/config/data.go"
		staffFile = "pkg/auth/staff/staff.go"
	)
	// layout lays out the program and the data level over the auth packages: the staff
	// auth, and the members auth when the program binds it.
	layout := func(program, data string) map[string]string {
		files := map[string]string{staffFile: webAuth("staff", true)}
		if strings.Contains(program, "pkg/auth/members") {
			files["pkg/auth/members/members.go"] = webAuth("members", true)
		}
		files["cmd/generate/main.go"] = program
		files[dataFile] = data

		return files
	}

	tests := []struct {
		name        string
		files       map[string]string
		wantStatus  Status
		wantSummary string
		wantDetails []string
	}{
		{
			name:        "a console at the root names /login",
			files:       layout(loginProgram(staffAtRoot), dataLevel(settingsFunc("staff", `MailLoginURL: env.PublicURL + "/login",`, `LoginURL: "/login",`))),
			wantStatus:  Pass,
			wantSummary: "1 auth(s) return a refused or waiting sign-in to their outlet's login page",
			wantDetails: []string{
				"staff: pkg/config/data.go:13 returns a refused or waiting sign-in to /login and mails its links to /login, the login page under the default outlet's mount path /",
			},
		},
		{
			name: "a console under /console and a portal under /portal each name their page",
			files: layout(loginProgram(staffAtConsole, membersAtPortal), dataLevel(
				settingsFunc("staff", `MailLoginURL: env.PublicURL + "/console/login",`, `LoginURL: "/console/login",`),
				settingsFunc("members", `MailLoginURL: env.PublicURL + "/portal/login",`, `LoginURL: "/portal/login",`),
			)),
			wantStatus:  Pass,
			wantSummary: "2 auth(s) return a refused or waiting sign-in to their outlet's login page",
			wantDetails: []string{
				"members: pkg/config/data.go:21 returns a refused or waiting sign-in to /portal/login and mails its links to /portal/login, the login page under the portal outlet's mount path /portal",
				"staff: pkg/config/data.go:13 returns a refused or waiting sign-in to /console/login and mails its links to /console/login, the login page under the default outlet's mount path /console",
			},
		},
		{
			name:        "a console moved under /console kept /login",
			files:       layout(loginProgram(staffAtConsole, membersAtPortal), dataLevel(settingsFunc("staff", `MailLoginURL: env.PublicURL + "/console/login",`, `LoginURL: "/login",`), settingsFunc("members", `LoginURL: "/portal/login",`))),
			wantStatus:  Fail,
			wantSummary: "1 login page problem(s)",
			wantDetails: []string{
				`pkg/config/data.go:16: the staff auth's LoginURL is "/login", but the login page under the default outlet's mount path /console is /console/login, so a refused or waiting sign-in returns to a page nothing serves; set LoginURL to "/console/login" in pkg/config/data.go`,
			},
		},
		{
			name:        "mailed links open another page",
			files:       layout(loginProgram(staffAtConsole, membersAtPortal), dataLevel(settingsFunc("staff", `MailLoginURL: env.PublicURL + "/login",`, `LoginURL: "/console/login",`), settingsFunc("members", `LoginURL: "/portal/login",`))),
			wantStatus:  Fail,
			wantSummary: "1 login page problem(s)",
			wantDetails: []string{
				`pkg/config/data.go:15: the staff auth's MailLoginURL ends in "/login", but the login page under the default outlet's mount path /console is /console/login, so every mailed link opens a page nothing serves; end MailLoginURL in "/console/login" in pkg/config/data.go`,
			},
		},
		{
			name:        "a directory method's construction naming no LoginURL takes the library's default, the root's page",
			files:       layout(loginProgram(staffGoogleAtRoot), dataLevel(settingsFunc("staff", `MailLoginURL: env.PublicURL + "/login",`))),
			wantStatus:  Pass,
			wantSummary: "1 auth(s) return a refused or waiting sign-in to their outlet's login page",
			wantDetails: []string{
				"staff: pkg/config/data.go:13 returns a refused or waiting sign-in to /login (the library's default) and mails its links to /login, the login page under the default outlet's mount path /",
			},
		},
		{
			name:        "a directory method's construction naming no LoginURL under a mount path sends sign-ins to the root's page",
			files:       layout(loginProgram(staffGoogleAtConsole, membersAtPortal), dataLevel(settingsFunc("staff", `MailLoginURL: env.PublicURL + "/console/login",`), settingsFunc("members", `LoginURL: "/portal/login",`))),
			wantStatus:  Fail,
			wantSummary: "1 login page problem(s)",
			wantDetails: []string{
				"pkg/config/data.go:13: the staff auth's settings name no LoginURL, so a refused or waiting sign-in returns to the library's default, /login, but the login page under the default outlet's mount path /console is /console/login; set LoginURL to \"/console/login\" in pkg/config/data.go",
			},
		},
		{
			name:        "a password-only construction under a mount path names no LoginURL, since nothing is sent back",
			files:       layout(loginProgram(staffAtConsole), dataLevel(settingsFunc("staff", `MailLoginURL: env.PublicURL + "/console/login",`))),
			wantStatus:  Pass,
			wantSummary: "1 auth(s) return a refused or waiting sign-in to their outlet's login page",
			wantDetails: []string{
				"staff: pkg/config/data.go:13 mails its links to /console/login, the login page under the default outlet's mount path /console; its password method sends no sign-in back to a login page",
			},
		},
		{
			name:        "a password-only construction naming no page",
			files:       layout(loginProgram(staffAtConsole), dataLevel(settingsFunc("staff"))),
			wantStatus:  Pass,
			wantSummary: "1 auth(s) return a refused or waiting sign-in to their outlet's login page",
			wantDetails: []string{
				"staff: pkg/config/data.go:13 names no login page, and its password method sends no sign-in back to one",
			},
		},
		{
			name:        "an auth bound to no browser outlet is passed over",
			files:       layout(loginProgram(staffAtRoot, membersMachines), dataLevel(settingsFunc("staff", `LoginURL: "/login",`), settingsFunc("members", `LoginURL: "/portal/login",`))),
			wantStatus:  Pass,
			wantSummary: "1 auth(s) return a refused or waiting sign-in to their outlet's login page",
			wantDetails: []string{
				"members: bound to no browser outlet, so its login page is not held",
				"staff: pkg/config/data.go:13 returns a refused or waiting sign-in to /login, the login page under the default outlet's mount path /",
			},
		},
		{
			name:        "a Mail literal names the mailed links' page",
			files:       layout(loginProgram(staffAtConsole), dataLevel(settingsFunc("staff", `LoginURL: "/console/login",`, `Mail: auth.Mail{Sender: sender, LoginURL: env.PublicURL + "/console/login"},`))),
			wantStatus:  Pass,
			wantSummary: "1 auth(s) return a refused or waiting sign-in to their outlet's login page",
			wantDetails: []string{
				"staff: pkg/config/data.go:13 returns a refused or waiting sign-in to /console/login and mails its links to /console/login, the login page under the default outlet's mount path /console",
			},
		},
		{
			name:        "a Mail literal naming another page",
			files:       layout(loginProgram(staffAtConsole), dataLevel(settingsFunc("staff", `LoginURL: "/console/login",`, `Mail: auth.Mail{Sender: sender, LoginURL: "https://example.com/login"},`))),
			wantStatus:  Fail,
			wantSummary: "1 login page problem(s)",
			wantDetails: []string{
				`pkg/config/data.go:16: the staff auth's Mail.LoginURL ends in "/login", but the login page under the default outlet's mount path /console is /console/login, so every mailed link opens a page nothing serves; end Mail.LoginURL in "/console/login" in pkg/config/data.go`,
			},
		},
		{
			name:        "a LoginURL that is not a string literal cannot be read",
			files:       layout(loginProgram(staffAtConsole), dataLevel(settingsFunc("staff", `LoginURL: env.LoginPage,`))),
			wantStatus:  Fail,
			wantSummary: "1 login page problem(s)",
			wantDetails: []string{
				`pkg/config/data.go:15: the staff auth's LoginURL is not a string literal, so the page a refused or waiting sign-in returns to cannot be read; set LoginURL to "/console/login" in pkg/config/data.go`,
			},
		},
		{
			name:        "an auth whose settings no file builds warns",
			files:       layout(loginProgram(staffAtConsole), dataLevel()),
			wantStatus:  Warn,
			wantSummary: "1 auth(s) whose settings no file the check reads builds",
			wantDetails: []string{
				"staff: no file outside tests builds staff.Settings, so the page a refused or waiting sign-in returns to is not read; build the settings on the data level naming /console/login (default)",
			},
		},
		{
			name:        "no outlet serves a browser application",
			files:       layout(loginProgram(`generation.GenerateRoutes("pkg/router", "api", generation.Auth("example.com/harbor/pkg/auth/staff", generation.PasswordMethod())),`), dataLevel(settingsFunc("staff", `LoginURL: "/login",`))),
			wantStatus:  Skip,
			wantSummary: "no outlet serves a browser application",
		},
		{
			name:        "a hand-written router declares no auth",
			files:       layout(loginProgram(consoleUndeclared), dataLevel(settingsFunc("staff", `LoginURL: "/login",`))),
			wantStatus:  Skip,
			wantSummary: "no browser outlet declares its auth (a hand-written router), so no login page is bound",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			write := func(rel, content string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Dir(filepath.Join(root, rel)), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, rel), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			write("go.mod", "module example.com/harbor\n\ngo 1.26.6\n")
			for rel, content := range tt.files {
				write(rel, content)
			}

			a, err := app.Discover(root)
			if err != nil {
				t.Fatalf("app.Discover() error = %v", err)
			}
			got := loginPages{}.Run(context.Background(), &Env{App: a})
			if got.Status != tt.wantStatus {
				t.Errorf("Status = %s, want %s (%s)", got.Status, tt.wantStatus, got.Summary)
			}
			if got.Summary != tt.wantSummary {
				t.Errorf("Summary = %q, want %q", got.Summary, tt.wantSummary)
			}
			if diff := cmp.Diff(tt.wantDetails, got.Details); diff != "" {
				t.Errorf("Details mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
