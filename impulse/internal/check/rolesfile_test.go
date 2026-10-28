package check

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/impulse/app"
)

func TestRolesFileAccepted(t *testing.T) {
	t.Parallel()

	// The role files under testdata/rolesfile: one the engine accepts, one naming a role
	// outside the grammar, and one whose role carries an empty description.
	fixture := func(name string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join("testdata", "rolesfile", name))
		if err != nil {
			t.Fatal(err)
		}

		return string(data)
	}
	withRoles := func(roles string) map[string]string {
		return map[string]string{
			"pkg/auth/staff/staff.go":   staffAuth,
			"pkg/auth/staff/roles.json": roles,
			"pkg/config/data.go":        staffConfig,
			"app/app.go":                staffApp,
		}
	}

	tests := []struct {
		name        string
		files       map[string]string
		wantStatus  Status
		wantSummary string
		wantDetails []string
	}{
		{
			name: "a file the engine accepts", files: withRoles(fixture("good.json")),
			wantStatus: Pass, wantSummary: "1 role file(s) parse the way the permission engine reads them: pkg/auth/staff/roles.json",
		},
		{
			name: "an empty file, as add auth writes it", files: withRoles(rolesJSON),
			wantStatus: Pass, wantSummary: "1 role file(s) parse the way the permission engine reads them: pkg/auth/staff/roles.json",
		},
		{
			name: "a role named with an underscore", files: withRoles(fixture("underscore.json")),
			wantStatus: Fail, wantSummary: "1 role file(s) the permission engine would refuse at start",
			wantDetails: []string{"pkg/auth/staff/roles.json: role Administrator_Global: a role name is letters and digits starting with a capital letter, such as Administrator or TenantAdministrator; the scope is never part of the name"},
		},
		{
			name: "a role with an empty description", files: withRoles(fixture("undescribed.json")),
			wantStatus: Fail, wantSummary: "1 role file(s) the permission engine would refuse at start",
			wantDetails: []string{"pkg/auth/staff/roles.json: role Auditor: a role carries a description, one or two plain sentences saying what the role is for"},
		},
		{
			name: "a file that is not JSON", files: withRoles("{\"roles\": {\"global\": [}\n"),
			wantStatus: Fail, wantSummary: "1 role file(s) the permission engine would refuse at start",
			wantDetails: []string{"pkg/auth/staff/roles.json: the role file does not parse: invalid character '}' looking for beginning of value"},
		},
		{
			name: "the role file is missing", files: withRoles(""),
			wantStatus: Fail, wantSummary: "1 role file(s) the permission engine would refuse at start",
			wantDetails: []string{"pkg/auth/staff/roles.json does not exist; the staff auth embeds its role file from there"},
		},
		{
			name: "no auth package", files: map[string]string{"pkg/config/doc.go": "package config\n"},
			wantStatus: Skip, wantSummary: "no auth package (pkg/auth/<name>) holds a role file",
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
				if content == "" {
					continue
				}
				write(rel, content)
			}
			a, err := app.Discover(root)
			if err != nil {
				t.Fatalf("app.Discover() error = %v", err)
			}
			got := rolesFileAccepted{}.Run(context.Background(), &Env{App: a})
			want := Result{Name: "roles-file", Status: tt.wantStatus, Summary: tt.wantSummary, Details: tt.wantDetails}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("Run() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
