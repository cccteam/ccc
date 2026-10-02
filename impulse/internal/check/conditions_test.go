package check

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/impulse/app"
)

// conditionalRoles is a role file with two conditional grants and one unconditional.
const conditionalRoles = `{
  "roles": {
    "global": [
      {"name": "Auditor", "permissions": {"List": [{"resource": "Clients", "fields": ["name"], "condition": "trusted = true"}]}}
    ],
    "domain": [
      {"name": "Cadet", "permissions": {
        "List": [{"resource": "Missions", "fields": ["title"], "condition": "hazard IN (1, 2)"}],
        "Read": [{"resource": "Wings", "fields": ["name"]}]
      }}
    ]
  }
}
`

const (
	// bothProven names both conditional grants through the auth package's Roles(), one
	// with its condition in a constant of the file.
	bothProven = `package integration

import (
	"testing"

	"example.com/harbor/pkg/auth/staff"
)

const cadetRule = "hazard IN (1, 2)"

func TestConditions(t *testing.T) {
	provesGrant(t, staff.Roles(), "Auditor", "List", "Clients", "trusted = true")
	provesGrant(t, staff.Roles(), "Cadet", "List", "Missions", cadetRule)
}
`
	// oneProven names the global grant alone.
	oneProven = `package integration

import (
	"testing"

	"example.com/harbor/pkg/auth/staff"
)

func TestConditions(t *testing.T) {
	provesGrant(t, staff.Roles(), "Auditor", "List", "Clients", "trusted = true")
}
`
	// otherPackage names the grants through a package whose role file the engine does not
	// take.
	otherPackage = `package integration

import (
	"testing"

	"example.com/harbor/pkg/auth/crew"
)

func TestConditions(t *testing.T) {
	provesGrant(t, crew.Roles(), "Auditor", "List", "Clients", "trusted = true")
	provesGrant(t, crew.Roles(), "Cadet", "List", "Missions", "hazard IN (1, 2)")
}
`
	// unreadable passes the condition from a table row, which the check cannot read.
	unreadable = `package integration

import (
	"testing"

	"example.com/harbor/pkg/auth/staff"
)

func TestConditions(t *testing.T) {
	provesGrant(t, staff.Roles(), "Auditor", "List", "Clients", "trusted = true")
	for _, tt := range []struct{ condition string }{{"hazard IN (1, 2)"}} {
		provesGrant(t, staff.Roles(), "Cadet", "List", "Missions", tt.condition)
	}
}
`
	// stale names the Cadet grant under a condition the file no longer carries.
	stale = `package integration

import (
	"testing"

	"example.com/harbor/pkg/auth/staff"
)

func TestConditions(t *testing.T) {
	provesGrant(t, staff.Roles(), "Auditor", "List", "Clients", "trusted = true")
	provesGrant(t, staff.Roles(), "Cadet", "List", "Missions", "hazard IN (1, 2, 3)")
}
`
)

func TestConditionsProven(t *testing.T) {
	t.Parallel()

	wired := map[string]string{
		"pkg/auth/staff/staff.go":   staffAuth,
		"pkg/auth/staff/roles.json": conditionalRoles,
		"pkg/config/data.go":        staffConfig,
		"pkg/deploy/deploy.go":      staffDeploy,
		"cmd/bootstrap/main.go":     staffBootstrap,
		"app/app.go":                staffApp,
	}
	with := func(extra map[string]string) map[string]string {
		files := map[string]string{}
		for k, v := range wired {
			files[k] = v
		}
		for k, v := range extra {
			files[k] = v
		}

		return files
	}
	without := func(files map[string]string, keys ...string) map[string]string {
		for _, k := range keys {
			delete(files, k)
		}

		return files
	}
	const (
		unprovenAuditor = `pkg/auth/staff/roles.json: Auditor List Clients under "trusted = true" is proven by no test case; write one over seeded rows and name the grant in it: provesGrant(t, staff.Roles(), "Auditor", "List", "Clients", "trusted = true")`
		unprovenCadet   = `pkg/auth/staff/roles.json: Cadet List Missions under "hazard IN (1, 2)" is proven by no test case; write one over seeded rows and name the grant in it: provesGrant(t, staff.Roles(), "Cadet", "List", "Missions", "hazard IN (1, 2)")`
	)

	tests := []struct {
		name        string
		files       map[string]string
		wantStatus  Status
		wantSummary string
		wantDetails []string
	}{
		{
			name: "every conditional grant has a naming case", files: with(map[string]string{"test/integration/conditions_test.go": bothProven}),
			wantStatus: Pass, wantSummary: "2 conditional grant(s) across 1 role file(s), each proven by a test case",
			wantDetails: []string{"pkg/auth/staff/roles.json: 2 conditional grant(s) proven"},
		},
		{
			name: "the cases name a package whose role file the engine does not take", files: with(map[string]string{"test/integration/conditions_test.go": otherPackage}),
			wantStatus: Fail, wantSummary: "2 conditional grant(s) proven by no test case; 2 provesGrant call(s) or role file(s) the check cannot read",
			wantDetails: []string{
				"test/integration/conditions_test.go:10: provesGrant names the Roles() of example.com/harbor/pkg/auth/crew, which is not an auth package whose role file reaches the permission engine",
				"test/integration/conditions_test.go:11: provesGrant names the Roles() of example.com/harbor/pkg/auth/crew, which is not an auth package whose role file reaches the permission engine",
				unprovenAuditor,
				unprovenCadet,
			},
		},
		{
			name: "a conditional grant no case names", files: with(map[string]string{"test/integration/conditions_test.go": oneProven}),
			wantStatus: Fail, wantSummary: "1 conditional grant(s) proven by no test case",
			wantDetails: []string{unprovenCadet},
		},
		{
			name: "no case at all", files: with(nil),
			wantStatus: Fail, wantSummary: "2 conditional grant(s) proven by no test case",
			wantDetails: []string{
				unprovenAuditor,
				unprovenCadet,
			},
		},
		{
			name: "a call the check cannot read", files: with(map[string]string{"test/integration/conditions_test.go": unreadable}),
			wantStatus: Fail, wantSummary: "1 conditional grant(s) proven by no test case; 1 provesGrant call(s) or role file(s) the check cannot read",
			wantDetails: []string{
				"test/integration/conditions_test.go:12: provesGrant argument condition is not a literal or a constant of the file, so the check cannot read which grant the case proves",
				unprovenCadet,
			},
		},
		{
			name: "a call naming a grant the file does not carry", files: with(map[string]string{"test/integration/conditions_test.go": stale}),
			wantStatus: Fail, wantSummary: "1 conditional grant(s) proven by no test case; 1 provesGrant call(s) or role file(s) the check cannot read",
			wantDetails: []string{
				unprovenCadet,
				`test/integration/conditions_test.go:11: provesGrant names a grant pkg/auth/staff/roles.json does not carry (Cadet List Missions under "hazard IN (1, 2, 3)"); the case fails at test time until the call matches the file`,
			},
		},
		{
			name: "no conditional grant", files: with(map[string]string{"pkg/auth/staff/roles.json": rolesJSON}),
			wantStatus: Pass, wantSummary: "0 conditional grant(s) across 1 role file(s), each proven by a test case",
			wantDetails: []string{"pkg/auth/staff/roles.json: 0 conditional grants"},
		},
		{
			name: "the role file is missing", files: without(with(nil), "pkg/auth/staff/roles.json"),
			wantStatus: Fail, wantSummary: "0 conditional grant(s) proven by no test case; 1 provesGrant call(s) or role file(s) the check cannot read",
			wantDetails: []string{"pkg/auth/staff/roles.json: cannot be read: no such file or directory"},
		},
		{
			name: "the role file never reaches the engine", files: with(map[string]string{"pkg/auth/staff/staff.go": staffAuthUnhanded}),
			wantStatus: Skip, wantSummary: "no role file reaches access.WithDefaultRoles (see auths-wired)",
		},
		{
			name: "no auth package", files: map[string]string{"pkg/config/session.go": "package config\n"},
			wantStatus: Skip, wantSummary: "no auth package",
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
			got := conditionsProven{}.Run(context.Background(), &Env{App: a})
			want := Result{Name: "conditions-proven", Status: tt.wantStatus, Summary: tt.wantSummary, Details: tt.wantDetails}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("Run() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
