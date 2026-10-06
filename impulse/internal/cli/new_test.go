package cli

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/go-playground/errors/v5"
	"github.com/google/go-cmp/cmp"
	"golang.org/x/mod/modfile"

	"github.com/cccteam/ccc/impulse/internal/check"
	"github.com/cccteam/ccc/impulse/internal/skeleton"
	transition_ "github.com/cccteam/ccc/impulse/internal/transition"
)

// fakeGo records the commands impulse new runs, each as its directory, its extra
// environment and its words, and fails them all with err when err is set.
type fakeGo struct {
	mu    sync.Mutex
	calls []string
	err   error
}

func (f *fakeGo) Run(_ context.Context, dir string, extraEnv []string, name string, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, strings.Join(append(append([]string{filepath.Base(dir)}, extraEnv...), append([]string{name}, args...)...), " "))
	if f.err != nil {
		return []byte("go: module lookup disabled by GOPROXY=off"), f.err
	}

	return nil, nil
}

// impulsePin reads go.mod's impulse require and whether it holds the impulse tool
// directive.
func impulsePin(t *testing.T, file string, data []byte) (version string, tool bool) {
	t.Helper()

	mod, err := modfile.Parse(file, data, nil)
	if err != nil {
		t.Fatalf("modfile.Parse(%s) error = %v", file, err)
	}
	for _, r := range mod.Require {
		if r.Mod.Path == check.ImpulseModule {
			version = r.Mod.Version
		}
	}
	for _, tl := range mod.Tool {
		if tl.Path == check.ImpulseModule {
			tool = true
		}
	}

	return version, tool
}

// templatePin is the impulse pin the base template's go.mod carries.
func templatePin(t *testing.T) string {
	t.Helper()

	sub, err := skeleton.FS(skeleton.Base)
	if err != nil {
		t.Fatalf("skeleton.FS() error = %v", err)
	}
	data, err := fs.ReadFile(sub, skeleton.ModFile)
	if err != nil {
		t.Fatalf("fs.ReadFile() error = %v", err)
	}
	version, _ := impulsePin(t, skeleton.ModFile, data)
	if version == "" {
		t.Fatalf("the %s template's go.mod requires no impulse", skeleton.Base)
	}

	return version
}

// TestNew runs impulse new over the builds an impulse can be: a release and an impulse
// installed from a commit pin the application's impulse tool at their own version and
// resolve it with go get -tool; a build from a checkout, or one reporting (devel), is
// refused before anything is written; with --dev-root the template's pin stands, whatever
// the build; and a pin go get cannot resolve stops before the first commit.
func TestNew(t *testing.T) {
	t.Parallel()

	const (
		release = "v0.4.0"
		commit  = "v0.0.0-20261004053859-89401d630235"
	)
	refusal := "install impulse from a release or a pushed commit (go install github.com/cccteam/ccc/impulse@<version|commit>) and run impulse new again; nothing was written"
	tests := []struct {
		name    string
		build   check.Build
		devRoot bool
		goErr   error
		// wantPin is go.mod's impulse require after the run; empty means the template's.
		wantPin   string
		wantCalls []string
		wantLine  string
		wantErr   string
	}{
		{
			name:      "a released impulse pins its own version",
			build:     check.Build{Version: release, FromModule: true},
			wantPin:   release,
			wantCalls: []string{"app GOWORK=off go get -tool github.com/cccteam/ccc/impulse@" + release},
			wantLine:  "Pinned the impulse tool in go.mod at " + release + ", the impulse that created the application, so CI's go tool impulse check runs the same code.",
		},
		{
			name:      "an impulse installed from a commit pins that commit's pseudo-version",
			build:     check.Build{Version: commit, FromModule: true},
			wantPin:   commit,
			wantCalls: []string{"app GOWORK=off go get -tool github.com/cccteam/ccc/impulse@" + commit},
			wantLine:  "Pinned the impulse tool in go.mod at " + commit + ", the impulse that created the application, so CI's go tool impulse check runs the same code.",
		},
		{
			name:    "a build from a checkout is refused, whatever version control stamped on it",
			build:   check.Build{Version: commit + "+dirty"},
			wantErr: "this impulse is a build from a checkout (" + commit + "+dirty), which names no commit the module proxy serves: " + refusal,
		},
		{
			name:    "a build that reports (devel) is refused",
			build:   check.Build{Version: "(devel)"},
			wantErr: "this impulse is a build from a checkout ((devel)), which names no commit the module proxy serves: " + refusal,
		},
		{
			name:    "with --dev-root a build from a checkout keeps the template's pin",
			build:   check.Build{Version: "(devel)"},
			devRoot: true,
		},
		{
			name:    "with --dev-root a release keeps the template's pin too",
			build:   check.Build{Version: release, FromModule: true},
			devRoot: true,
		},
		{
			name:      "a pin the module proxy cannot resolve stops before the first commit",
			build:     check.Build{Version: commit, FromModule: true},
			goErr:     errors.New("exit status 1"),
			wantCalls: []string{"app GOWORK=off go get -tool github.com/cccteam/ccc/impulse@" + commit},
			wantErr:   "go get -tool github.com/cccteam/ccc/impulse@" + commit + " could not resolve the impulse pin through the module proxy (exit status 1): go: module lookup disabled by GOPROXY=off.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := filepath.Join(t.TempDir(), "app")
			run := &fakeGo{err: tt.goErr}
			cmd := newNewWith(newDeps{build: func() check.Build { return tt.build }, run: run})
			args := []string{dir, "--module", "example.com/acme/ledger", "--auth", "staff", "--skip-git"}
			if tt.devRoot {
				args = append(args, "--dev-root", t.TempDir())
			}
			cmd.SetArgs(args)
			var out strings.Builder
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			err := cmd.Execute()
			if diff := cmp.Diff(tt.wantCalls, run.calls); diff != "" {
				t.Errorf("go commands mismatch (-want +got):\n%s", diff)
			}
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Execute() error = %v, want containing %q", err, tt.wantErr)
				}
				if tt.goErr == nil {
					if _, statErr := os.Stat(dir); !errors.Is(statErr, os.ErrNotExist) {
						t.Errorf("the refused run wrote %s (stat error %v); want nothing written", dir, statErr)
					}
				}

				return
			}
			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			file := filepath.Join(dir, "go.mod")
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			wantPin := tt.wantPin
			if wantPin == "" {
				wantPin = templatePin(t)
			}
			version, tool := impulsePin(t, file, data)
			if version != wantPin || !tool {
				t.Errorf("go.mod pins impulse at %q with the tool directive %v; want %q with it", version, tool, wantPin)
			}
			if tt.wantLine != "" && !strings.Contains(out.String(), tt.wantLine+"\n") {
				t.Errorf("output lacks the line %q:\n%s", tt.wantLine, out.String())
			}
		})
	}
}

func TestComposedOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		opts          composedOptions
		want          []transition
		wantErr       string
		wantDescribe  string
		wantReference string
	}{
		{name: "nothing composed", opts: composedOptions{tenantTable: "Tenants"}, wantReference: transition_.TenancyReferenceCandidate},
		{
			name: "tenancy alone", opts: composedOptions{tenancy: true, tenantTable: "Clients"},
			want:         []transition{transition_.Tenancy{Table: "Clients"}},
			wantDescribe: "tenancy (Clients)", wantReference: transition_.TenancyReferenceCandidate,
		},
		{
			name: "tenancy then outlets, in the order given", opts: composedOptions{tenancy: true, tenantTable: "Tenants", outlets: []string{"portal=portal/api", "kiosk=kiosk/api"}, apiOutlets: []string{"machines=machines"}},
			want: []transition{
				transition_.Tenancy{Table: "Tenants"},
				transition_.Outlet{Name: "portal", Prefix: "portal/api", Sessions: true},
				transition_.Outlet{Name: "kiosk", Prefix: "kiosk/api", Sessions: true},
				transition_.Outlet{Name: "machines", Prefix: "machines"},
			},
			wantDescribe:  "tenancy (Tenants), the session outlet portal=portal/api, the session outlet kiosk=kiosk/api, the API-key outlet machines=machines",
			wantReference: transition_.ReferenceCandidate,
		},
		{
			name: "sites promote the base and add the rest", opts: composedOptions{tenancy: true, tenantTable: "Tenants", sites: []string{"console", "portal", "kiosk"}},
			want: []transition{
				transition_.Tenancy{Table: "Tenants"},
				transition_.Site{Name: "portal", Existing: "console"},
				transition_.Site{Name: "kiosk"},
			},
			wantDescribe:  "tenancy (Tenants), the sites console, portal, kiosk (the base site becomes console)",
			wantReference: transition_.SitesReference,
		},
		{
			name: "a directory flavor for the first auth composes first, fresh", opts: composedOptions{authName: "staff", flavor: transition_.FlavorOIDCGoogle, authority: transition_.AuthorityDirectory, tenancy: true, tenantTable: "Tenants"},
			want: []transition{
				transition_.AuthFlavor{Name: "staff", Flavor: transition_.FlavorOIDCGoogle, Authority: transition_.AuthorityDirectory, Fresh: true},
				transition_.Tenancy{Table: "Tenants"},
			},
			wantDescribe:  "the oidc-google flavor for the staff auth, role membership the directory's, tenancy (Tenants)",
			wantReference: transition_.ReferenceCandidate,
		},
		{
			name: "the file store composes after the outlets and before the sites", opts: composedOptions{files: true, apiOutlets: []string{"machines=machines"}, sites: []string{"console", "portal"}},
			want: []transition{
				transition_.Outlet{Name: "machines", Prefix: "machines"},
				transition_.Files{},
				transition_.Site{Name: "portal", Existing: "console"},
			},
			wantDescribe:  "the API-key outlet machines=machines, the file store, the sites console, portal (the base site becomes console)",
			wantReference: transition_.SitesReference,
		},
		{name: "one site is no layout", opts: composedOptions{sites: []string{"console"}}, wantErr: "name at least two sites"},
		{name: "an outlet without a prefix", opts: composedOptions{outlets: []string{"portal"}}, wantErr: `--outlet "portal": name the outlet and its prefix`},
		{name: "an API outlet without a name", opts: composedOptions{apiOutlets: []string{"=machines"}}, wantErr: `--api-outlet "=machines"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.opts.transitions()
			switch {
			case tt.wantErr != "":
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("transitions() error = %v, want %q", err, tt.wantErr)
				}

				return
			case err != nil:
				t.Fatalf("transitions() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("transitions() mismatch (-want +got):\n%s", diff)
			}
			if d := tt.opts.describe(); d != tt.wantDescribe {
				t.Errorf("describe() = %q, want %q", d, tt.wantDescribe)
			}
			if r := tt.opts.reference(); r != tt.wantReference {
				t.Errorf("reference() = %q, want %q", r, tt.wantReference)
			}
		})
	}
}
