package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestGrantProofs(t *testing.T) {
	t.Parallel()

	const cases = `package integration

import (
	"testing"

	"example.com/harbor/pkg/auth/staff"
	crewauth "example.com/harbor/pkg/auth/crew"
)

const cadetRule = "hazard IN (1, 2)"

func TestConditions(t *testing.T) {
	provesGrant(t, staff.Roles(), "Cadet", "List", "Missions", cadetRule)
	provesGrant(t, crewauth.Roles(), "Pilot", "Read", "Ships", "hangarZone != 'quarantine'")
	provesGrant(t, staff.Roles(), "Cadet", "List", "Missions")
	provesGrant(t, roles, "Cadet", "List", "Missions", cadetRule)
	provesGrant(t, staff.RolesPath, "Cadet", "List", "Missions", cadetRule)
	provesGrant(t, other.Roles(), "Cadet", "List", "Missions", cadetRule)
	provesGrant(t, staff.Roles(), role, "List", "Missions", cadetRule)
	provesGrant(t, staff.Roles(), "Cadet", "List", "Missions", tt.condition)
	other.provesGrant(t, staff.Roles(), "Cadet", "List", "Missions", cadetRule)
}
`
	tests := []struct {
		name  string
		files map[string]string
		want  []GrantProof
	}{
		{
			name:  "every argument shape",
			files: map[string]string{"test/integration/conditions_test.go": cases},
			want: []GrantProof{
				{File: "test/integration/conditions_test.go", Line: 13, RolesPackage: "example.com/harbor/pkg/auth/staff", Role: "Cadet", Permission: "List", Resource: "Missions", Condition: "hazard IN (1, 2)"},
				{File: "test/integration/conditions_test.go", Line: 14, RolesPackage: "example.com/harbor/pkg/auth/crew", Role: "Pilot", Permission: "Read", Resource: "Ships", Condition: "hangarZone != 'quarantine'"},
				{File: "test/integration/conditions_test.go", Line: 15, Problem: "takes 6 arguments, found 5"},
				{File: "test/integration/conditions_test.go", Line: 16, Problem: "argument roles is not an imported auth package's Roles()"},
				{File: "test/integration/conditions_test.go", Line: 17, Problem: "argument roles is not an imported auth package's Roles()"},
				{File: "test/integration/conditions_test.go", Line: 18, Problem: "argument roles is not an imported auth package's Roles()"},
				{File: "test/integration/conditions_test.go", Line: 19, RolesPackage: "example.com/harbor/pkg/auth/staff", Problem: "argument role is not a literal or a constant of the file"},
				{File: "test/integration/conditions_test.go", Line: 20, RolesPackage: "example.com/harbor/pkg/auth/staff", Role: "Cadet", Permission: "List", Resource: "Missions", Problem: "argument condition is not a literal or a constant of the file"},
			},
		},
		{
			name:  "a non-test file is not read",
			files: map[string]string{"test/integration/conditions.go": cases},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			files := map[string]string{"go.mod": "module example.com/harbor\n\ngo 1.26\n"}
			for rel, content := range tt.files {
				files[rel] = content
			}
			for rel, content := range files {
				abs := filepath.Join(root, filepath.FromSlash(rel))
				if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(abs, []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			a, err := Discover(root)
			if err != nil {
				t.Fatalf("Discover() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, a.GrantProofs); diff != "" {
				t.Errorf("GrantProofs mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
