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

const (
	rolesFile = "schema/" + "roles/staff.json"
	cadetRule = "hazard IN (1, 2)"
)

func TestConditions(t *testing.T) {
	provesGrant(t, staff.RolesPath, "Cadet", "List", "Missions", cadetRule)
	provesGrant(t, crewauth.RolesPath, "Pilot", "Read", "Ships", "hangarZone != 'quarantine'")
	provesGrant(t, rolesFile, "Auditor", "List", "Clients", "trusted = true")
	provesGrant(t, "./schema/roles/staff.json", "Auditor", "Read", "Clients", "trusted = true")
	provesGrant(t, staff.RolesPath, "Cadet", "List", "Missions")
	provesGrant(t, rolesPath, "Cadet", "List", "Missions", cadetRule)
	provesGrant(t, staff.RolesPath, role, "List", "Missions", cadetRule)
	provesGrant(t, staff.RolesPath, "Cadet", "List", "Missions", tt.condition)
	other.provesGrant(t, staff.RolesPath, "Cadet", "List", "Missions", cadetRule)
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
				{File: "test/integration/conditions_test.go", Line: 16, RolesPackage: "example.com/harbor/pkg/auth/staff", Role: "Cadet", Permission: "List", Resource: "Missions", Condition: "hazard IN (1, 2)"},
				{File: "test/integration/conditions_test.go", Line: 17, RolesPackage: "example.com/harbor/pkg/auth/crew", Role: "Pilot", Permission: "Read", Resource: "Ships", Condition: "hangarZone != 'quarantine'"},
				{File: "test/integration/conditions_test.go", Line: 18, RolesPath: "schema/roles/staff.json", Role: "Auditor", Permission: "List", Resource: "Clients", Condition: "trusted = true"},
				{File: "test/integration/conditions_test.go", Line: 19, RolesPath: "schema/roles/staff.json", Role: "Auditor", Permission: "Read", Resource: "Clients", Condition: "trusted = true"},
				{File: "test/integration/conditions_test.go", Line: 20, Problem: "takes 6 arguments, found 5"},
				{File: "test/integration/conditions_test.go", Line: 21, Problem: "argument rolesPath is neither a literal, a constant of the file, nor an imported package's RolesPath"},
				{File: "test/integration/conditions_test.go", Line: 22, RolesPackage: "example.com/harbor/pkg/auth/staff", Problem: "argument role is not a literal or a constant of the file"},
				{File: "test/integration/conditions_test.go", Line: 23, RolesPackage: "example.com/harbor/pkg/auth/staff", Role: "Cadet", Permission: "List", Resource: "Missions", Problem: "argument condition is not a literal or a constant of the file"},
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
