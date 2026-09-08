package check

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/mod/modfile"

	"github.com/cccteam/ccc/impulse/internal/app"
)

// TestStaticChecksOnFixtures runs every check that reads files against the fixture
// applications. Each case pins the status, the summary, and the detail lines.
func TestStaticChecksOnFixtures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		fixture     string
		check       Check
		wantStatus  Status
		wantSummary string
		wantDetails []string
	}{
		{
			name: "generator-program single site", fixture: "singlesite", check: generatorProgram{},
			wantStatus: Pass, wantSummary: "1 generator program(s) read completely",
		},
		{
			name: "generator-program bad program", fixture: "badprogram", check: generatorProgram{},
			wantStatus: Fail, wantSummary: "6 generator program problem(s)",
			wantDetails: []string{
				"cmd/generate/main.go:18: GenerateHandlers argument handlersDir is not a literal",
				"cmd/generate/main.go:19: GenerateRoutes takes 2 argument(s), found 1",
				"cmd/generate/main.go:20: unknown option generation.WithFrobnicator: this impulse release does not know it",
				"cmd/generate/main.go:21: GenerateEnums is a TSOption, but a ResourceOption is expected here",
				`cmd/generate/main.go:22: WithConsolidatedHandlers argument "yes" should be a bool literal`,
				"cmd/generate/main.go:23: WithRPC is a ResourceOption, but a TSOption is expected here",
			},
		},
		{
			name: "options single site", fixture: "singlesite", check: options{},
			wantStatus: Pass, wantSummary: "flat layout, 1 site(s); not tenanted; outlets portal (sessions)",
			wantDetails: []string{
				"lighthouse (cmd/generate/resourcegenerator/main.go): resources pkg/resources, handlers app, routes pkg/router under /api, tests test/authz, rpc pkg/rpc, typescript web/console/src/app/core/service, typescript web/portal/src/app/core/service (outlet portal)",
			},
		},
		{
			name: "options multi site", fixture: "multisite", check: options{},
			wantStatus: Fail, wantSummary: "2 option set problem(s)",
			wantDetails: []string{
				"cmd/generate/resourcegenerator_tugs/main.go: GenerateRoutes names apps/tugs/pkg/router, which is not a directory in the tree",
				"cmd/generate/resourcegenerator_tugs/main.go: no //go:generate directive runs this program, so go generate ./... never regenerates it",
			},
		},
		{
			name: "options bad program", fixture: "badprogram", check: options{},
			wantStatus: Skip, wantSummary: "a generator program could not be read completely (see generator-program)",
		},
		{
			name: "tenancy-wired untenanted with a tenant-scoped struct", fixture: "singlesite", check: tenancyWired{},
			wantStatus: Fail, wantSummary: "1 tenancy wiring problem(s) in an untenanted application",
			wantDetails: []string{
				"pkg/resources/beacons.go:8: Beacon is @permissionScope(domain), but no WithDomainRoute names the tenant segment; it is served under the default /domain/{domain}/ pair",
			},
		},
		{
			name: "tenancy-wired untenanted multi site", fixture: "multisite", check: tenancyWired{},
			wantStatus: Pass, wantSummary: "not tenanted: no tenant-scoped resources, roles provisioned globally",
		},
		{
			name: "tenancy-wired no site", fixture: "badprogram", check: tenancyWired{},
			wantStatus: Skip, wantSummary: "no site generator",
		},
		{
			name: "outlet-wired portal never mounted", fixture: "singlesite", check: outletWired{},
			wantStatus: Fail, wantSummary: "1 outlet wiring problem(s)",
			wantDetails: []string{
				"cmd/generate/resourcegenerator/main.go: no file in pkg/router calls generatedPortalRoutes; the portal outlet's routes are not mounted",
			},
		},
		{
			name: "outlet-wired no router files", fixture: "multisite", check: outletWired{},
			wantStatus: Fail, wantSummary: "2 outlet wiring problem(s)",
			wantDetails: []string{
				"cmd/generate/resourcegenerator_pilots/main.go: no file in apps/pilots/pkg/router calls generatedRoutes; the default outlet's routes are not mounted",
				"cmd/generate/resourcegenerator_tugs/main.go: no file in apps/tugs/pkg/router calls generatedRoutes; the default outlet's routes are not mounted",
			},
		},
		{
			name: "outlet-wired no site", fixture: "badprogram", check: outletWired{},
			wantStatus: Skip, wantSummary: "no site generator",
		},
		{
			name: "sites-wired flat", fixture: "singlesite", check: sitesWired{},
			wantStatus: Skip, wantSummary: "flat layout: one site",
		},
		{
			name: "sites-wired nothing serves the sites", fixture: "multisite", check: sitesWired{},
			wantStatus: Fail, wantSummary: "4 site wiring problem(s)",
			wantDetails: []string{
				"site pilots has no main package in apps/pilots; nothing serves it",
				"no process file runs site pilots (expected a process running go run ./apps/pilots)",
				"site tugs has no main package in apps/tugs; nothing serves it",
				"no process file runs site tugs (expected a process running go run ./apps/tugs)",
			},
		},
		{
			name: "sites-wired no site", fixture: "badprogram", check: sitesWired{},
			wantStatus: Skip, wantSummary: "no site generator",
		},
		{
			name: "auth-wired tables missing", fixture: "singlesite", check: authWired{},
			wantStatus: Fail, wantSummary: "2 auth wiring problem(s)",
			wantDetails: []string{
				"pkg/config/session.go:11: password auth reads table LighthouseSessions, which no migration creates (the session library's schema is under schema/spanner/migrations)",
				"pkg/config/session.go:11: password auth reads table SessionUsers, which no migration creates (the session library's schema is under schema/spanner/migrations)",
			},
		},
		{
			name: "auth-wired no authenticator", fixture: "multisite", check: authWired{},
			wantStatus: Fail, wantSummary: "no session authenticator is constructed outside tests (session.NewPasswordAuth, NewOIDCAzure, NewOIDCGoogle, or NewPreauth)",
		},
		{
			name: "auth-wired no site", fixture: "badprogram", check: authWired{},
			wantStatus: Skip, wantSummary: "no site generator",
		},
		{
			name: "emulator-version single site", fixture: "singlesite", check: emulatorVersion{},
			wantStatus: Pass, wantSummary: "3 reference(s) agree on 1.5.56",
		},
		{
			name: "emulator-version multi site", fixture: "multisite", check: emulatorVersion{},
			wantStatus: Fail, wantSummary: "2 different emulator versions in use",
			wantDetails: []string{
				"1.5.43     process-compose.yaml:3",
				"1.5.44     cmd/generate/resourcegenerator_pilots/main.go",
				"1.5.44     cmd/generate/resourcegenerator_shared/main.go",
				"1.5.44     cmd/generate/resourcegenerator_tugs/main.go",
			},
		},
		{
			name: "emulator-version none", fixture: "badprogram", check: emulatorVersion{},
			wantStatus: Skip, wantSummary: "no Spanner emulator version is named anywhere",
		},
		{
			name: "prettier-ignore single site", fixture: "singlesite", check: prettierIgnore{},
			wantStatus: Fail, wantSummary: "1 TypeScript target(s) not excluded from prettier (--fix adds the entries)",
			wantDetails: []string{
				"web/portal/.prettierignore does not cover src/app/core/service/zz_gen_*.ts (add: src/app/core/service/zz_gen_*.ts)",
			},
		},
		{
			name: "prettier-ignore target outside a browser app", fixture: "badprogram", check: prettierIgnore{},
			wantStatus: Fail, wantSummary: "1 TypeScript target(s) not excluded from prettier (--fix adds the entries)",
			wantDetails: []string{
				"cmd/generate/main.go:23: TypeScript target gui/src/app/core/service is not inside a browser app (no angular.json above it)",
			},
		},
		{
			name: "eslint-ignore single site", fixture: "singlesite", check: eslintIgnore{},
			wantStatus: Pass, wantSummary: "1 browser app(s) ignore the generated TypeScript",
			wantDetails: []string{
				"web/portal: no eslint configuration, nothing to check",
			},
		},
		{
			name: "eslint-ignore multi site", fixture: "multisite", check: eslintIgnore{},
			wantStatus: Fail, wantSummary: "2 TypeScript target(s) not ignored by eslint",
			wantDetails: []string{
				"apps/pilots/gui/eslint.config.js does not ignore src/app/core/service/zz_gen_*.ts (add ignores: ['**/zz_gen_*.ts'])",
				"apps/pilots/gui/eslint.config.js does not ignore src/app/core/service/shared-resources/zz_gen_*.ts (add ignores: ['**/zz_gen_*.ts'])",
				"apps/tugs/gui: no eslint configuration, nothing to check",
			},
		},
		{
			name: "eslint-ignore no browser app", fixture: "badprogram", check: eslintIgnore{},
			wantStatus: Skip, wantSummary: "no GenerateTypescript target inside a browser app",
		},
		{
			name: "package-manager mixed lockfiles", fixture: "singlesite", check: packageManager{},
			wantStatus: Fail, wantSummary: "browser apps are locked by 2 different package managers",
			wantDetails: []string{
				"bun: web/console",
				"npm: web/portal",
			},
		},
		{
			name: "package-manager foreign invocations", fixture: "multisite", check: packageManager{},
			wantStatus: Fail, wantSummary: "2 place(s) disagree with bun, the package manager the lockfiles name",
			wantDetails: []string{
				"process-compose.yaml:5 runs npm",
				`apps/pilots/gui/package.json script "lint" runs npm`,
			},
		},
		{
			name: "package-manager no browser apps", fixture: "badprogram", check: packageManager{},
			wantStatus: Skip, wantSummary: "no browser apps",
		},
		{
			name: "rpc-execute single site", fixture: "singlesite", check: rpcExecute{},
			wantStatus: Fail, wantSummary: "1 RPC finding(s) (regenerate and read the generator output)",
			wantDetails: []string{
				"app/zz_gen_relight_beacon.go: no Execute call; the handler decodes and returns without running the method",
			},
		},
		{
			name: "rpc-execute no rpc", fixture: "multisite", check: rpcExecute{},
			wantStatus: Skip, wantSummary: "no generated RPC handlers",
		},
		{
			name: "multi-site single generator", fixture: "singlesite", check: multiSite{},
			wantStatus: Skip, wantSummary: "single generator",
		},
		{
			name: "multi-site disagreements", fixture: "multisite", check: multiSite{},
			wantStatus: Fail, wantSummary: "2 multi-site disagreement(s)",
			wantDetails: []string{
				"cmd/generate/resourcegenerator_tugs/main.go reads migrations [file://apps/tugs/schema/migrations] but cmd/generate/resourcegenerator_pilots/main.go reads [file://schema/migrations]",
				"cmd/generate/resourcegenerator_shared/main.go emits no TypeScript into apps/tugs/gui (site generator cmd/generate/resourcegenerator_tugs/main.go writes there)",
			},
		},
		{
			name: "env-template single site", fixture: "singlesite", check: envTemplate{},
			wantStatus: Fail, wantSummary: "1 variable(s) missing from .envrc.template (--fix adds them)",
			wantDetails: []string{
				"pkg/config/config.go:23: LIGHTHOUSE_BEACON_API_KEY (required) is not in .envrc.template",
			},
		},
		{
			name: "env-template no tags", fixture: "multisite", check: envTemplate{},
			wantStatus: Skip, wantSummary: "no env tags declared",
		},
		{
			name: "pins released", fixture: "singlesite", check: pins{},
			wantStatus: Pass, wantSummary: "3 framework pin(s) are released versions",
		},
		{
			name: "pins unreleased", fixture: "multisite", check: pins{},
			wantStatus: Warn, wantSummary: "2 framework pin(s) point at unreleased code",
			wantDetails: []string{
				"github.com/cccteam/session is pinned to pseudo-version v0.11.2-0.20260903182144-ffa51dacf20e",
				"github.com/cccteam/ccc/resource is replaced by local path ../resource",
			},
		},
		{
			name: "pins none", fixture: "badprogram", check: pins{},
			wantStatus: Skip, wantSummary: "go.mod requires no github.com/cccteam/* module",
		},
		{
			name: "paging offset in Go", fixture: "badprogram", check: paging{},
			wantStatus: Warn, wantSummary: "1 offset use(s) to move to cursors",
			wantDetails: []string{
				"pkg/legacy/list.go:13: positions a list by offset; pages are positioned by the cursor in the Link header",
			},
		},
		{
			name: "paging offset in the browser", fixture: "singlesite", check: paging{},
			wantStatus: Warn, wantSummary: "1 offset use(s) to move to cursors",
			wantDetails: []string{
				"web/console/src/app/legacy.ts:4: sends an offset parameter; follow the Link header (page() in @cccteam/resource) instead",
			},
		},
		{
			name: "paging clean", fixture: "multisite", check: paging{},
			wantStatus: Pass, wantSummary: "no list is positioned by offset",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := tt.check.Run(context.Background(), &Env{App: fixture(t, tt.fixture)})
			want := Result{Name: tt.check.Name(), Status: tt.wantStatus, Summary: tt.wantSummary, Details: tt.wantDetails}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("Run() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestRPCExecuteSignatures(t *testing.T) {
	t.Parallel()

	const program = `package main

import (
	"context"

	"github.com/cccteam/ccc/resource/generation"
)

func main() {
	_, _ = generation.NewResourceGenerator(context.Background(), "pkg/resources", []string{"file://schema"}, []string{"example.com/rpcforms/pkg/resources"},
		generation.GenerateHandlers("app"),
		generation.WithRPC("pkg/rpc"),
	)
}
`
	const handler = "// Code generated by resourcegeneration. DO NOT EDIT.\n// Source: pkg/rpc\n\npackage app\n\nfunc (a *App) PingBeacon() { p.Execute(ctx, txn, a.RPCClient()) }\n"

	tests := []struct {
		name  string
		files map[string]string
		want  Result
	}{
		{
			name: "both forms and both result shapes pass",
			files: map[string]string{
				"pkg/rpc/ping_beacon.go": "package rpc\n\n// @rpc\ntype PingBeacon struct{}\n\nfunc (p *PingBeacon) Execute(ctx context.Context, txn resource.ReadWriteTransaction, client *Client) error { return nil }\n",
				"pkg/rpc/relight.go":     "package rpc\n\ntype (\n\t// @rpc\n\tRelight struct{}\n)\n\nfunc (p Relight) Execute(ctx context.Context, client resource.Client, rpcClient *Client) (Report, error) { return Report{}, nil }\n",
			},
			want: Result{Name: rpcExecute{}.Name(), Status: Pass, Summary: "2 RPC method(s) declare a recognized Execute and 1 generated handler(s) call it"},
		},
		{
			name: "a struct without Execute and one of another shape fail",
			files: map[string]string{
				"pkg/rpc/ping_beacon.go": "package rpc\n\n// @rpc\ntype PingBeacon struct{}\n\nfunc (p *PingBeacon) Execute(ctx context.Context, client *Client) error { return nil }\n",
				"pkg/rpc/relight.go":     "package rpc\n\n// @rpc\ntype Relight struct{}\n",
			},
			want: Result{Name: rpcExecute{}.Name(), Status: Fail, Summary: "2 RPC finding(s) (regenerate and read the generator output)", Details: []string{
				"pkg/rpc/ping_beacon.go:6: PingBeacon.Execute(context.Context, *Client) (error) is neither form the generator classifies: Execute(ctx context.Context, txn resource.ReadWriteTransaction, client *Client) error or Execute(ctx context.Context, client resource.Client, rpcClient *Client) error",
				"pkg/rpc/relight.go:4: Relight declares @rpc but no Execute method; the generator refuses it",
			}},
		},
		{
			name: "a leftover TxnRunner interface warns with the cleanup",
			files: map[string]string{
				"pkg/rpc/ping_beacon.go": "package rpc\n\n// @rpc\ntype PingBeacon struct{}\n\nfunc (p *PingBeacon) Execute(ctx context.Context, txn resource.ReadWriteTransaction, client *Client) error { return nil }\n",
				"pkg/rpc/rpc_iface.go":   "package rpc\n\n// TxnRunner is Execute-only.\ntype TxnRunner interface {\n\tExecute(ctx context.Context, txn resource.ReadWriteTransaction, client *Client) error\n}\n",
			},
			want: Result{Name: rpcExecute{}.Name(), Status: Warn, Summary: "1 RPC method(s) declare a recognized Execute and 1 generated handler(s) call it; an interface the generator no longer consults remains", Details: []string{
				"pkg/rpc/rpc_iface.go:4: interface TxnRunner is no longer consulted; the generator classifies RPC methods by the Execute signature, so delete it",
			}},
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
			write("go.mod", "module example.com/rpcforms\n\ngo 1.26.6\n")
			write("cmd/generate/main.go", program)
			write("app/zz_gen_ping_beacon.go", handler)
			for rel, content := range tt.files {
				write(rel, content)
			}

			a, err := app.Discover(root)
			if err != nil {
				t.Fatalf("app.Discover() error = %v", err)
			}
			got := rpcExecute{}.Run(context.Background(), &Env{App: a})
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Run() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestRPCExecuteHeaderless(t *testing.T) {
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
	write("go.mod", "module example.com/headerless\n\ngo 1.26.6\n")
	write("cmd/generate/main.go", `package main

import (
	"context"

	"github.com/cccteam/ccc/resource/generation"
)

func main() {
	_, _ = generation.NewResourceGenerator(context.Background(), "pkg/resources", []string{"file://schema"}, []string{"example.com/headerless/pkg/resources"},
		generation.GenerateHandlers("app"),
		generation.WithRPC("pkg/rpc"),
	)
}
`)
	write("app/zz_gen_handwritten.go", "package app\n\nfunc (a *App) Nothing() {}\n")

	a, err := app.Discover(root)
	if err != nil {
		t.Fatalf("app.Discover() error = %v", err)
	}
	got := rpcExecute{}.Run(context.Background(), &Env{App: a})
	want := Result{
		Name:    rpcExecute{}.Name(),
		Status:  Warn,
		Summary: "cannot identify RPC handlers: 1 zz_gen file(s) in the handlers directory carry no Source header, so the resource generator did not write them",
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Run() mismatch (-want +got):\n%s", diff)
	}
}

func TestFixes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		check       Check
		wantSummary string
		wantFile    string
		wantContent string
	}{
		{
			name:        "prettier-ignore creates the file",
			check:       prettierIgnore{},
			wantSummary: "1 .prettierignore file(s) updated",
			wantFile:    "web/portal/.prettierignore",
			wantContent: "# Generated by the resource generator; reformatting it breaks generate idempotence.\nsrc/app/core/service/zz_gen_*.ts\n",
		},
		{
			name:        "env-template appends the variables",
			check:       envTemplate{},
			wantSummary: "1 variable(s) added to .envrc.template",
			wantFile:    ".envrc.template",
			wantContent: "# Lighthouse local development\nexport LIGHTHOUSE_PROJECT_ID=\nexport LIGHTHOUSE_SPANNER_DATABASE=\n# export LIGHTHOUSE_COOKIE_KEY=\n# Added by impulse check --fix: set these for local development.\nexport LIGHTHOUSE_BEACON_API_KEY=\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a := fixtureCopy(t, "singlesite")
			got := tt.check.Run(context.Background(), &Env{App: a, Fix: true})
			if got.Status != Pass || got.Summary != tt.wantSummary {
				t.Fatalf("Run() with Fix = %s %q, want PASS %q\n%s", got.Status, got.Summary, tt.wantSummary, strings.Join(got.Details, "\n"))
			}
			data, err := os.ReadFile(filepath.Join(a.Root, tt.wantFile))
			if err != nil {
				t.Fatalf("read fixed file: %v", err)
			}
			if diff := cmp.Diff(tt.wantContent, string(data)); diff != "" {
				t.Errorf("fixed file mismatch (-want +got):\n%s", diff)
			}

			// The fix is complete: the check passes on the next run.
			again := tt.check.Run(context.Background(), &Env{App: a})
			if again.Status != Pass {
				t.Errorf("second Run() = %s %q, want PASS", again.Status, again.Summary)
			}
		})
	}
}

func TestIgnored(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		patterns []string
		rel      string
		want     bool
	}{
		{name: "anchored glob", patterns: []string{"src/app/core/service/zz_gen_*.ts"}, rel: "src/app/core/service/zz_gen_api.ts", want: true},
		{name: "anchored glob wrong dir", patterns: []string{"src/app/core/service/zz_gen_*.ts"}, rel: "src/app/other/zz_gen_api.ts", want: false},
		{name: "leading slash", patterns: []string{"/src/app/core/service/zz_gen_*.ts"}, rel: "src/app/core/service/zz_gen_api.ts", want: true},
		{name: "basename anywhere", patterns: []string{"zz_gen_*.ts"}, rel: "src/app/core/service/zz_gen_api.ts", want: true},
		{name: "directory pattern", patterns: []string{"src/app/core/service"}, rel: "src/app/core/service/zz_gen_api.ts", want: true},
		{name: "directory with trailing slash", patterns: []string{"src/app/core/service/"}, rel: "src/app/core/service/zz_gen_api.ts", want: true},
		{name: "directory name anywhere", patterns: []string{"service"}, rel: "src/app/core/service/zz_gen_api.ts", want: true},
		{name: "directory-only pattern does not match a file", patterns: []string{"zz_gen_api.ts/"}, rel: "src/zz_gen_api.ts", want: false},
		{name: "double star", patterns: []string{"src/**/zz_gen_*.ts"}, rel: "src/app/core/service/zz_gen_api.ts", want: true},
		{name: "double star zero dirs", patterns: []string{"src/**/zz_gen_*.ts"}, rel: "src/zz_gen_api.ts", want: true},
		{name: "trailing double star", patterns: []string{"src/app/**"}, rel: "src/app/core/service/zz_gen_api.ts", want: true},
		{name: "negation wins later", patterns: []string{"src/**", "!src/app/core/service/zz_gen_*.ts"}, rel: "src/app/core/service/zz_gen_api.ts", want: false},
		{name: "re-ignored after negation", patterns: []string{"*.ts", "!zz_gen_*.ts", "src/app/core/service/"}, rel: "src/app/core/service/zz_gen_api.ts", want: true},
		{name: "no patterns", patterns: nil, rel: "src/zz_gen_api.ts", want: false},
		{name: "unrelated", patterns: []string{"dist", "node_modules/"}, rel: "src/zz_gen_api.ts", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := ignored(tt.patterns, tt.rel); got != tt.want {
				t.Errorf("ignored(%v, %q) = %v, want %v", tt.patterns, tt.rel, got, tt.want)
			}
		})
	}
}

func TestPinsFromGoMod(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		gomod      string
		wantStatus Status
		wantDetail string
	}{
		{
			name:       "released",
			gomod:      "module x\n\nrequire github.com/cccteam/httpio v0.7.17\n",
			wantStatus: Pass,
		},
		{
			name:       "pseudo-version",
			gomod:      "module x\n\nrequire github.com/cccteam/httpio v0.7.18-0.20260901000000-0123456789ab\n",
			wantStatus: Warn,
			wantDetail: "github.com/cccteam/httpio is pinned to pseudo-version v0.7.18-0.20260901000000-0123456789ab",
		},
		{
			name:       "versioned replace",
			gomod:      "module x\n\nrequire github.com/cccteam/httpio v0.7.17\n\nreplace github.com/cccteam/httpio => github.com/someone/httpio v0.7.17\n",
			wantStatus: Warn,
			wantDetail: "github.com/cccteam/httpio is replaced by github.com/someone/httpio v0.7.17",
		},
		{
			name:       "non-framework replace is ignored",
			gomod:      "module x\n\nrequire github.com/cccteam/httpio v0.7.17\n\nreplace github.com/golang-migrate/migrate/v4 => github.com/other/migrate/v4 v4.19.2\n",
			wantStatus: Pass,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mod, err := modfile.Parse("go.mod", []byte(tt.gomod), nil)
			if err != nil {
				t.Fatalf("modfile.Parse() error = %v", err)
			}
			got := pins{}.Run(context.Background(), &Env{App: &app.App{GoMod: mod}})
			if got.Status != tt.wantStatus {
				t.Errorf("Status = %s, want %s (%s)", got.Status, tt.wantStatus, got.Summary)
			}
			if tt.wantDetail != "" && (len(got.Details) != 1 || got.Details[0] != tt.wantDetail) {
				t.Errorf("Details = %v, want [%q]", got.Details, tt.wantDetail)
			}
		})
	}
}
