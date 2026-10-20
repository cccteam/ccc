package transition

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/impulse/app"
	"github.com/cccteam/ccc/impulse/internal/authpkg"
)

func TestMethodCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		method Method
		want   string
	}{
		{name: "password", method: Method{Auth: "staff", Spec: authpkg.Method{Kind: app.MethodPassword}}, want: "impulse add method staff password"},
		{name: "a directory method with its shape", method: Method{Auth: "members", Spec: authpkg.Method{Kind: app.MethodGoogle, Name: "workspace", Authority: AuthorityApplication, Slot: authpkg.SlotListed, Accounts: authpkg.AccountsInvited}}, want: "impulse add method members google workspace --authority application --slot listed --accounts invited"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.method.Command(); got != tt.want {
				t.Errorf("Command() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMethodValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		method  Method
		wantErr string
	}{
		{name: "a Google method on the staff auth", method: Method{Auth: "staff", Spec: authpkg.Method{Kind: app.MethodGoogle, Name: "workspace", Authority: AuthorityApplication}}},
		{name: "no such auth", method: Method{Auth: "partners", Spec: authpkg.Method{Kind: app.MethodPassword}}, wantErr: "no auth package pkg/auth/partners"},
		{name: "the registration exists", method: Method{Auth: "staff", Spec: authpkg.Method{Kind: app.MethodPassword}}, wantErr: "the staff auth registers password already"},
		{name: "a directory method without an authority", method: Method{Auth: "staff", Spec: authpkg.Method{Kind: app.MethodAzure, Name: "directory"}}, wantErr: "the directory method needs --authority"},
		{name: "a slot the kind lacks", method: Method{Auth: "staff", Spec: authpkg.Method{Kind: app.MethodAzure, Name: "directory", Authority: AuthorityApplication, Slot: "galaxy"}}, wantErr: "galaxy"},
		{name: "the directory authority with any organization", method: Method{Auth: "staff", Spec: authpkg.Method{Kind: app.MethodAzure, Name: "directory", Authority: AuthorityDirectory, Slot: authpkg.SlotAny}}, wantErr: "directory"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			files := authFiles(t)
			files["cmd/generate/resourcegenerator/main.go"] = beaconRouterProgram
			err := tt.method.Validate(beacon(t, files))
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v", err)
				}

				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Validate() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestMethodApply(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		method      Method
		extra       map[string]string
		wantDid     []string
		wantSkipped []string
		// wantNotSkipped are words no skipped line carries: an obligation the shape does
		// not have.
		wantNotSkipped []string
		check          func(t *testing.T, a *app.App)
		// tidiedGoMod is the go.mod the tidy script leaves; empty leaves go.mod as it is.
		tidiedGoMod string
		tidyErr     error
		generateErr error
		wantErr     string
	}{
		{
			name:   "a Google method under the application authority",
			method: Method{Auth: "staff", Spec: authpkg.Method{Kind: app.MethodGoogle, Name: "workspace", Authority: AuthorityApplication}},
			wantDid: []string{
				"pkg/auth/staff/workspace.go: the workspace method (google, one Workspace, members of the directory, role membership the application's): its registration constant, its settings, its handlers (WorkspaceLogin and the rest), and registerWorkspace, which registers it on the Auth",
				"pkg/auth/staff/staff.go: Handlers holds the workspace method (workspace *google.Method), New registers it (registerWorkspace), and Settings carries its WorkspaceSettings and the LoginURL, and the package doc lists it",
				`cmd/generate/resourcegenerator/main.go: generation.GoogleMethod("workspace") declared on 1 outlet Auth option(s) bound to the staff auth, so the generated router mounts user/workspace/login and the callback and the methods route offers it`,
				"Procfile: 2 go run command(s) build with -tags skipAuth, so the staff auth's directory is simulated in development",
				"ran go generate ./..., so the router mounts the method's routes and the login page offers it",
			},
			wantSkipped: []string{
				"pkg/config/data.go: no environment struct (env := &T{}) to add the workspace method's variables to; read APP_STAFF_WORKSPACE_HOSTED_DOMAIN, APP_STAFF_WORKSPACE_CLIENT_ID, APP_STAFF_WORKSPACE_CLIENT_SECRET, APP_STAFF_WORKSPACE_REDIRECT_URL and pass them in staff.Settings.Workspace",
				"no environment template (.envrc.template, .env.template, .env.example) to add the workspace method's variables to (APP_STAFF_WORKSPACE_HOSTED_DOMAIN, APP_STAFF_WORKSPACE_CLIENT_ID, APP_STAFF_WORKSPACE_CLIENT_SECRET, APP_STAFF_WORKSPACE_REDIRECT_URL)",
				`test/integration/harness_test.go and cmd/deployment/migrate/main_test.go: the workspace registration validates its organization slot at registration whether or not the directory is simulated, so give the staff auth's construction in the harness Workspace: staff.WorkspaceSettings{HostedDomain: "example.com", RedirectURL: <the served callback>} and set APP_STAFF_WORKSPACE_HOSTED_DOMAIN=example.com where the migrate run's environment is`,
			},
			wantNotSkipped: []string{"GroupPrefix", "GROUP_PREFIX", "role groups' prefix"},
			check: func(t *testing.T, a *app.App) {
				t.Helper()
				file := read(t, a, "pkg/auth/staff/workspace.go")
				for _, want := range []string{"package staff", `const WorkspaceRegistration = "workspace"`, "type WorkspaceSettings struct", "func (h *Handlers) WorkspaceLogin() http.HandlerFunc", "func (h *Handlers) WorkspaceCallback() http.HandlerFunc", "func registerWorkspace(authSession *Session, accessClient *access.Client, settings *Settings) *google.Method", "google.Workspace(WorkspaceRegistration, settings.Workspace.HostedDomain)", "signin.MembersOfDirectory", "google.DisableRoleSync()"} {
					if !strings.Contains(file, want) {
						t.Errorf("workspace.go lacks %q:\n%s", want, file)
					}
				}
				main := read(t, a, "pkg/auth/staff/staff.go")
				for _, want := range []string{"\tpassword  *session.PasswordMethod\n\tworkspace *google.Method\n", "\thandlers.password = registerPassword(authSession)\n\thandlers.workspace = registerWorkspace(authSession, accessClient, settings)\n\n\treturn &Auth{db: db, access: accessClient, handlers: handlers}, nil", "\tWorkspace WorkspaceSettings\n", "\tLoginURL string\n", "\t\"github.com/cccteam/session/google\"\n"} {
					if !strings.Contains(main, want) {
						t.Errorf("staff.go lacks %q:\n%s", want, main)
					}
				}
				if program := read(t, a, "cmd/generate/resourcegenerator/main.go"); !strings.Contains(program, `generation.Auth("example.com/acme/beacon/pkg/auth/staff", generation.PasswordMethod(), generation.GoogleMethod("workspace"))`) {
					t.Errorf("program = %s", program)
				}
			},
		},
		{
			name:   "a Google method under the directory authority names the role groups' prefix the registration validates",
			method: Method{Auth: "staff", Spec: authpkg.Method{Kind: app.MethodGoogle, Name: "workspace", Authority: AuthorityDirectory}},
			wantDid: []string{
				"pkg/auth/staff/workspace.go: the workspace method (google, one Workspace, members of the directory, role membership the directory's): its registration constant, its settings, its handlers (WorkspaceLogin and the rest), and registerWorkspace, which registers it on the Auth",
			},
			wantSkipped: []string{
				"pkg/config/data.go: no environment struct (env := &T{}) to add the workspace method's variables to; read APP_STAFF_WORKSPACE_HOSTED_DOMAIN, APP_STAFF_WORKSPACE_CLIENT_ID, APP_STAFF_WORKSPACE_CLIENT_SECRET, APP_STAFF_WORKSPACE_REDIRECT_URL, APP_STAFF_WORKSPACE_GROUP_PREFIX, APP_STAFF_WORKSPACE_GROUP_LOOKUP and pass them in staff.Settings.Workspace",
				`test/integration/harness_test.go and cmd/deployment/migrate/main_test.go: the workspace registration validates its organization slot and its role groups' prefix at registration whether or not the directory is simulated, so give the staff auth's construction in the harness Workspace: staff.WorkspaceSettings{HostedDomain: "example.com", RedirectURL: <the served callback>, GroupPrefix: "staff-"} and set APP_STAFF_WORKSPACE_HOSTED_DOMAIN=example.com and APP_STAFF_WORKSPACE_GROUP_PREFIX=staff- where the migrate run's environment is`,
			},
			wantNotSkipped: []string{"GroupLookup", "GROUP_LOOKUP=", "RedirectURL: <the served callback>}"},
			check: func(t *testing.T, a *app.App) {
				t.Helper()
				file := read(t, a, "pkg/auth/staff/workspace.go")
				for _, want := range []string{"google.Workspace(WorkspaceRegistration, settings.Workspace.HostedDomain)", "google.RoleSync(accessClient.UserManager(), settings.Workspace.GroupPrefix, lookup)", "google.ParseGroupLookup(settings.Workspace.GroupLookup)"} {
					if !strings.Contains(file, want) {
						t.Errorf("workspace.go lacks %q:\n%s", want, file)
					}
				}
			},
		},
		{
			name:   "a Google method admitting invited users only declares .InvitedUsers() and writes the rule to its file",
			method: Method{Auth: "staff", Spec: authpkg.Method{Kind: app.MethodGoogle, Name: "workspace", Authority: AuthorityApplication, Accounts: authpkg.AccountsInvited}},
			wantDid: []string{
				"pkg/auth/staff/workspace.go: the workspace method (google, one Workspace, invited users only, role membership the application's): its registration constant, its settings, its handlers (WorkspaceLogin and the rest), and registerWorkspace, which registers it on the Auth",
				`cmd/generate/resourcegenerator/main.go: generation.GoogleMethod("workspace").InvitedUsers() declared on 1 outlet Auth option(s) bound to the staff auth, so the generated router mounts user/workspace/login and the callback and the methods route offers it`,
			},
			check: func(t *testing.T, a *app.App) {
				t.Helper()
				file := read(t, a, "pkg/auth/staff/workspace.go")
				if !strings.Contains(file, "signin.InvitedUsersOnly") || strings.Contains(file, "signin.MembersOfDirectory") {
					t.Errorf("workspace.go's account rule:\n%s", file)
				}
				if program := read(t, a, "cmd/generate/resourcegenerator/main.go"); !strings.Contains(program, `generation.Auth("example.com/acme/beacon/pkg/auth/staff", generation.PasswordMethod(), generation.GoogleMethod("workspace").InvitedUsers())`) {
					t.Errorf("program = %s", program)
				}
				meaning := (&Method{Auth: "staff", Spec: authpkg.Method{Kind: app.MethodGoogle, Name: "workspace", Authority: AuthorityApplication, Accounts: authpkg.AccountsInvited}}).Meaning()
				if !strings.Contains(meaning, "grant Create on `StaffUsers` to the role that invites in the role file") {
					t.Errorf("Meaning() lacks the Create grant:\n%s", meaning)
				}
			},
		},
		{
			name:   "an Entra method under the directory authority, read from the environment struct",
			method: Method{Auth: "staff", Spec: authpkg.Method{Kind: app.MethodAzure, Name: "directory", Authority: AuthorityDirectory}},
			extra:  map[string]string{"pkg/config/data.go": authConfigEnv},
			wantDid: []string{
				"pkg/auth/staff/directory.go: the directory method (azure, one tenant, members of the directory, role membership the directory's): its registration constant, its settings, its handlers (DirectoryLogin and the rest), and registerDirectory, which registers it on the Auth",
				"pkg/config/data.go: dataConfig reads the staff auth's directory registration from APP_STAFF_DIRECTORY_TENANT_ID, APP_STAFF_DIRECTORY_CLIENT_ID, APP_STAFF_DIRECTORY_CLIENT_SECRET, APP_STAFF_DIRECTORY_REDIRECT_URL, and the staff.Settings literal in NewDataConfiguration (an application composed before the staffSettings method) passes them",
				`cmd/generate/resourcegenerator/main.go: generation.AzureMethod("directory") declared on 1 outlet Auth option(s) bound to the staff auth, so the generated router mounts user/directory/login, the callback and the front-channel logout and the methods route offers it`,
			},
			wantSkipped: []string{
				"pkg/config/data.go: LoginURL is set to /login when the construction had none; make it the login page of the surface bound to the staff auth",
				`test/integration/harness_test.go and cmd/deployment/migrate/main_test.go: the directory registration validates its organization slot at registration whether or not the directory is simulated, so give the staff auth's construction in the harness Directory: staff.DirectorySettings{TenantID: "00000000-0000-0000-0000-000000000000", RedirectURL: <the served callback>} and set APP_STAFF_DIRECTORY_TENANT_ID=00000000-0000-0000-0000-000000000000 where the migrate run's environment is`,
			},
			wantNotSkipped: []string{"GroupPrefix", "GROUP_PREFIX", "role groups' prefix"},
			check: func(t *testing.T, a *app.App) {
				t.Helper()
				file := read(t, a, "pkg/auth/staff/directory.go")
				for _, want := range []string{"azure.Tenant(settings.Directory.TenantID)", "signin.RoleSync(accessClient.UserManager())", "func (h *Handlers) DirectoryFrontChannelLogout() http.HandlerFunc"} {
					if !strings.Contains(file, want) {
						t.Errorf("directory.go lacks %q:\n%s", want, file)
					}
				}
				data := read(t, a, "pkg/config/data.go")
				for _, want := range []string{
					"\tStaffDirectoryTenantID     string `env:\"APP_STAFF_DIRECTORY_TENANT_ID\"`\n",
					"\tStaffDirectoryClientSecret string `env:\"APP_STAFF_DIRECTORY_CLIENT_SECRET\" secret:\"true\"`\n",
					"\t\tDirectory: staff.DirectorySettings{\n\t\t\tTenantID:     env.StaffDirectoryTenantID,\n\t\t\tClientID:     env.StaffDirectoryClientID,\n\t\t\tClientSecret: env.StaffDirectoryClientSecret,\n\t\t\tRedirectURL:  env.StaffDirectoryRedirectURL,\n\t\t},\n",
					"\t\tLoginURL:       \"/login\",\n",
				} {
					if !strings.Contains(data, want) {
						t.Errorf("data.go lacks %q:\n%s", want, data)
					}
				}
			},
		},
		{
			name:   "an Entra method's settings join the staff auth's settings function",
			method: Method{Auth: "staff", Spec: authpkg.Method{Kind: app.MethodAzure, Name: "directory", Authority: AuthorityApplication}},
			extra:  map[string]string{"pkg/config/data.go": authConfigSettings},
			wantDid: []string{
				"pkg/config/data.go: dataConfig reads the staff auth's directory registration from APP_STAFF_DIRECTORY_TENANT_ID, APP_STAFF_DIRECTORY_CLIENT_ID, APP_STAFF_DIRECTORY_CLIENT_SECRET, APP_STAFF_DIRECTORY_REDIRECT_URL, and dataConfig.staffSettings passes them",
			},
			wantSkipped: []string{
				"pkg/config/data.go: LoginURL is set to /login when the construction had none; make it the login page of the surface bound to the staff auth",
			},
			check: func(t *testing.T, a *app.App) {
				t.Helper()
				data := read(t, a, "pkg/config/data.go")
				for _, want := range []string{
					// The constructor is as it was: the settings come from the function.
					"\tstaffAuth, err := staff.New(ctx, spannerClient, env.staffSettings(cookieKey))\n",
					"\tStaffDirectoryTenantID     string `env:\"APP_STAFF_DIRECTORY_TENANT_ID\"`\n",
					"func (env *dataConfig) staffSettings(cookieKey string) *staff.Settings {\n\treturn &staff.Settings{\n\t\tCookieKey:      cookieKey,\n\t\tSessionTimeout: env.SessionTimeout,\n\t\tLoginURL:       \"/login\",\n\t\tDirectory: staff.DirectorySettings{\n\t\t\tTenantID:     env.StaffDirectoryTenantID,\n\t\t\tClientID:     env.StaffDirectoryClientID,\n\t\t\tClientSecret: env.StaffDirectoryClientSecret,\n\t\t\tRedirectURL:  env.StaffDirectoryRedirectURL,\n\t\t},\n\t}\n}\n",
				} {
					if !strings.Contains(data, want) {
						t.Errorf("data.go lacks %q:\n%s", want, data)
					}
				}
			},
		},
		{
			name:   "a second registration joins the function under the first",
			method: Method{Auth: "staff", Spec: authpkg.Method{Kind: app.MethodGoogle, Name: "workspace", Authority: AuthorityApplication}},
			extra: map[string]string{"pkg/config/data.go": strings.Replace(authConfigSettings,
				"\treturn &staff.Settings{CookieKey: cookieKey, SessionTimeout: env.SessionTimeout}\n",
				"\treturn &staff.Settings{\n\t\tCookieKey:      cookieKey,\n\t\tSessionTimeout: env.SessionTimeout,\n\t\tLoginURL:       \"/console/login\",\n\t\tDirectory: staff.DirectorySettings{\n\t\t\tTenantID: env.StaffDirectoryTenantID,\n\t\t},\n\t}\n", 1)},
			wantDid: []string{
				"pkg/config/data.go: dataConfig reads the staff auth's workspace registration from APP_STAFF_WORKSPACE_HOSTED_DOMAIN, APP_STAFF_WORKSPACE_CLIENT_ID, APP_STAFF_WORKSPACE_CLIENT_SECRET, APP_STAFF_WORKSPACE_REDIRECT_URL, and dataConfig.staffSettings passes them",
			},
			check: func(t *testing.T, a *app.App) {
				t.Helper()
				data := read(t, a, "pkg/config/data.go")
				// The login page the literal had is kept, and the new registration follows the first.
				if want := "\t\tLoginURL:       \"/console/login\",\n\t\tDirectory: staff.DirectorySettings{\n\t\t\tTenantID: env.StaffDirectoryTenantID,\n\t\t},\n\t\tWorkspace: staff.WorkspaceSettings{\n\t\t\tHostedDomain: env.StaffWorkspaceHostedDomain,\n\t\t\tClientID:     env.StaffWorkspaceClientID,\n\t\t\tClientSecret: env.StaffWorkspaceClientSecret,\n\t\t\tRedirectURL:  env.StaffWorkspaceRedirectURL,\n\t\t},\n\t}\n}\n"; !strings.Contains(data, want) {
					t.Errorf("data.go lacks %q:\n%s", want, data)
				}
			},
		},
		{
			name:   "a level with neither the function nor the literal reads the variables and says what to pass",
			method: Method{Auth: "staff", Spec: authpkg.Method{Kind: app.MethodAzure, Name: "directory", Authority: AuthorityApplication}},
			extra:  map[string]string{"pkg/config/data.go": strings.Replace(authConfigEnv, "&staff.Settings{CookieKey: cookieKey, SessionTimeout: env.SessionTimeout}", "settings(env, cookieKey)", 1)},
			wantDid: []string{
				"pkg/config/data.go: dataConfig reads the staff auth's directory registration from APP_STAFF_DIRECTORY_TENANT_ID, APP_STAFF_DIRECTORY_CLIENT_ID, APP_STAFF_DIRECTORY_CLIENT_SECRET, APP_STAFF_DIRECTORY_REDIRECT_URL",
			},
			wantSkipped: []string{
				"pkg/config/data.go: dataConfig has no staffSettings method and NewDataConfiguration builds no staff.Settings{...} literal, so the directory method's settings are read into dataConfig but not passed; set staff.Settings.Directory from them",
			},
			wantNotSkipped: []string{"LoginURL is set to /login"},
		},
		{
			name:        "a directory method on a module that never required the OIDC library",
			method:      Method{Auth: "staff", Spec: authpkg.Method{Kind: app.MethodGoogle, Name: "workspace", Authority: AuthorityApplication}},
			tidiedGoMod: beaconGoMod + "\nrequire github.com/coreos/go-oidc/v3 v3.21.0 // indirect\n",
			wantDid: []string{
				"go.mod: requires github.com/coreos/go-oidc/v3 v3.21.0, the OIDC library the session library's directory methods sign in through (go mod tidy)",
				"ran go generate ./..., so the router mounts the method's routes and the login page offers it",
			},
		},
		{
			name:    "tidy fails",
			method:  Method{Auth: "staff", Spec: authpkg.Method{Kind: app.MethodGoogle, Name: "workspace", Authority: AuthorityApplication}},
			tidyErr: errors.New("exit status 1"),
			wantErr: "go mod tidy failed, so go.mod does not require what the change imports; fix the cause and run it, then go generate ./...:\ngo: tidy: boom",
		},
		{
			name:        "generation fails",
			method:      Method{Auth: "staff", Spec: authpkg.Method{Kind: app.MethodGoogle, Name: "workspace", Authority: AuthorityApplication}},
			generateErr: errors.New("exit status 1"),
			wantErr:     "go generate ./... failed; fix the cause and run it:\ngeneration: boom",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			files := authFiles(t)
			files["cmd/generate/resourcegenerator/main.go"] = beaconRouterProgram
			for rel, content := range tt.extra {
				files[rel] = content
			}
			a := beacon(t, files)
			exec := &fakeExec{err: tt.generateErr}
			switch {
			case tt.tidyErr != nil:
				exec.tidy = func() error {
					return tt.tidyErr
				}
			case tt.tidiedGoMod != "":
				exec.tidy = tidied(a, tt.tidiedGoMod)
			}
			ch, err := tt.method.Apply(t.Context(), a, exec)
			wantCalls := regenerated
			if tt.tidyErr != nil {
				// A failed tidy refuses before the regeneration.
				wantCalls = regenerated[:1]
			}
			if diff := cmp.Diff(wantCalls, exec.calls); diff != "" {
				t.Errorf("exec calls mismatch (-want +got):\n%s", diff)
			}
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Apply() error = %v, want %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("Apply() error = %v", err)
			}
			for _, want := range tt.wantDid {
				if !containsLine(ch.Did, want) {
					t.Errorf("Did lacks %q; got:\n%s", want, strings.Join(ch.Did, "\n"))
				}
			}
			for _, want := range tt.wantSkipped {
				if !containsLine(ch.Skipped, want) {
					t.Errorf("Skipped lacks %q; got:\n%s", want, strings.Join(ch.Skipped, "\n"))
				}
			}
			for _, unwanted := range tt.wantNotSkipped {
				if skipped := strings.Join(ch.Skipped, "\n"); strings.Contains(skipped, unwanted) {
					t.Errorf("Skipped carries %q; got:\n%s", unwanted, skipped)
				}
			}
			if tt.tidiedGoMod == "" {
				noGoModLine(t, ch.Did)
			}
			if tt.check != nil {
				tt.check(t, a)
			}
		})
	}
}

func containsLine(lines []string, want string) bool {
	for _, l := range lines {
		if l == want {
			return true
		}
	}

	return false
}
