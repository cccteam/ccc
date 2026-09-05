package handoff

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/mod/modfile"

	"github.com/cccteam/ccc/impulse/internal/app"
	"github.com/cccteam/ccc/impulse/internal/check"
)

// beacon is a minimal application for the brief: a root and a module path.
func beacon(t *testing.T) *app.App {
	t.Helper()

	mod, err := modfile.Parse("go.mod", []byte("module example.com/acme/beacon\n\ngo 1.26\n"), nil)
	if err != nil {
		t.Fatalf("modfile.Parse() error = %v", err)
	}

	return &app.App{Root: "/w/beacon", GoMod: mod}
}

const preamble = "# Impulse handoff: example.com/acme/beacon\n\n" +
	"You are working in an Impulse application (Go services built on the cccteam libraries, with ccc/resource generating the handlers, routes, and browser clients from annotated resource structs). The application root is `/w/beacon`, and it is your working directory.\n\n"

const rulesTail = "5. Do not stage or commit. Leave your work in the working tree; the pull request is the review.\n" +
	"6. Keep the tests table-driven and the suite passing: `go test ./...`.\n" +
	"7. Stop when the check is clean. In your final message, say what you changed and what a reviewer should look at.\n"

func TestBriefWrite(t *testing.T) {
	t.Parallel()

	optionsPass := check.Result{Name: "options", Status: check.Pass, Summary: `flat layout, 1 site(s); tenanted on "tenants"; no outlets`, Details: []string{"beacon (cmd/generate/main.go): resources pkg/resources, handlers app"}}
	tenancy := check.Result{Name: "tenancy-wired", Status: check.Fail, Summary: "2 tenancy wiring problem(s)", Details: []string{
		`WithDomainRoute("tenants"): no migration creates a table named like it (the tenant-record table)`,
		"no struct is annotated @permissionScope(domain): every resource is global, and the tenant segment serves nothing",
	}}
	guard := Snapshot{Programs: map[string][]string{"cmd/generate/main.go": nil}, Configs: map[string]string{GolangciConfig: "a", "web/eslint.config.js": "b"}}

	tests := []struct {
		name  string
		brief Brief
		want  string
	}{
		{
			name:  "nothing changed by the tool, options in force",
			brief: Brief{Results: []check.Result{optionsPass, tenancy, {Name: "pins", Status: check.Warn, Summary: "1 pin"}}, Guard: guard},
			want: preamble +
				"## What changed\n\nNothing was changed by the tool. `impulse check` found the obligations below in the tree as it is.\n\n" +
				"## The option set in force\n\nflat layout, 1 site(s); tenanted on \"tenants\"; no outlets\n- beacon (cmd/generate/main.go): resources pkg/resources, handlers app\n\n" +
				"This is read from the generator programs, never from a record. It must read the same when you are done.\n\n" +
				"## The failing checks\n\nThis is the output of `impulse check`, failing checks only. Each line under a check is one obligation.\n\n```\n" +
				"FAIL  tenancy-wired  2 tenancy wiring problem(s)\n" +
				"      WithDomainRoute(\"tenants\"): no migration creates a table named like it (the tenant-record table)\n" +
				"      no struct is annotated @permissionScope(domain): every resource is global, and the tenant segment serves nothing\n" +
				"```\n\n" +
				"## Rules\n\n" +
				"1. Run `impulse check` from the application root until it reports no FAIL. The regen check runs go generate, which needs the Spanner emulator through podman or docker; if neither is available, run `impulse check --skip-generate` and say so in your final message.\n" +
				"2. Do not edit generated files (`zz_gen_*`). Change the source they are generated from and run `go generate ./...`.\n" +
				"3. Do not edit the generator program(s): `cmd/generate/main.go`. Do not add or remove generator options; the option set was recorded and is compared when you finish.\n" +
				"4. Do not edit the lint configuration: `.golangci.yml`, `web/eslint.config.js`. Fix the findings in the code.\n" +
				rulesTail +
				"\n## Done when\n\n`impulse check` reports no FAIL, and its options line still reads:\n\n    flat layout, 1 site(s); tenanted on \"tenants\"; no outlets\n",
		},
		{
			name: "a transition with meaning and a reference, options failing",
			brief: Brief{
				Change:    "impulse add tenancy added WithDomainRoute(\"tenants\") to the generator program and the Tenants migration.\n",
				Meaning:   "Tenancy scopes resources to a tenant record.",
				Reference: "/w/tenanted",
				Results:   []check.Result{{Name: "options", Status: check.Fail, Summary: "1 option set problem(s)", Details: []string{"x"}}},
				Guard:     Snapshot{},
			},
			want: preamble +
				"## What changed\n\nimpulse add tenancy added WithDomainRoute(\"tenants\") to the generator program and the Tenants migration.\n\n" +
				"## What it means\n\nTenancy scopes resources to a tenant record.\n\n" +
				"## The failing checks\n\nThis is the output of `impulse check`, failing checks only. Each line under a check is one obligation.\n\n```\n" +
				"FAIL  options  1 option set problem(s)\n      x\n```\n\n" +
				"## Reference\n\nThe application at `/w/tenanted` has these options wired and its check clean. Read it for the shape of the wiring. Do not copy its resources, names, or data into this application.\n\n" +
				"## Rules\n\n" +
				"1. Run `impulse check` from the application root until it reports no FAIL. The regen check runs go generate, which needs the Spanner emulator through podman or docker; if neither is available, run `impulse check --skip-generate` and say so in your final message.\n" +
				"2. Do not edit generated files (`zz_gen_*`). Change the source they are generated from and run `go generate ./...`.\n" +
				"3. Do not stage or commit. Leave your work in the working tree; the pull request is the review.\n" +
				"4. Keep the tests table-driven and the suite passing: `go test ./...`.\n" +
				"5. Stop when the check is clean. In your final message, say what you changed and what a reviewer should look at.\n" +
				"\n## Done when\n\n`impulse check` reports no FAIL.\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tt.brief.App = beacon(t)
			if diff := cmp.Diff(tt.want, tt.brief.String()); diff != "" {
				t.Errorf("Brief.String() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
