package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const orgFixture = "../org/testdata/imp"

// orgRepo copies the fixture organization's placement into a temporary directory.
func orgRepo(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	data, err := os.ReadFile(filepath.Join(orgFixture, "placement.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "placement.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	return dir
}

func TestOrgCommands(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		steps    func(t *testing.T, dir string) (code int, out string)
		wantCode int
		wantOut  []string
	}{
		{
			name: "new renders the layers and prints the hand steps",
			steps: func(t *testing.T, dir string) (int, string) {
				t.Helper()

				return run(t, "org", "new", dir)
			},
			wantOut: []string{"Rendered the imp foundation", "53 owned file(s)", "seeded .gitignore, 0-bootstrap/terraform.tfvars", "By hand, before the first apply", "Seed, as bedrock@impulseframework.com", "imp-boot-gbl-tofu"},
		},
		{
			name: "check is clean after new",
			steps: func(t *testing.T, dir string) (int, string) {
				t.Helper()
				if code, out := run(t, "org", "new", dir); code != 0 {
					t.Fatalf("org new: %d %s", code, out)
				}

				return run(t, "org", "check", "--dir", dir)
			},
			wantOut: []string{"53 owned file(s) match the placement"},
		},
		{
			name: "check reports a hand edit",
			steps: func(t *testing.T, dir string) (int, string) {
				t.Helper()
				if code, out := run(t, "org", "new", dir); code != 0 {
					t.Fatalf("org new: %d %s", code, out)
				}
				path := filepath.Join(dir, "2-shr", "registry.tf")
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, append([]byte("# edited by hand\n"), data...), 0o600); err != nil {
					t.Fatal(err)
				}

				return run(t, "org", "check", "--dir", dir)
			},
			wantCode: 1,
			wantOut:  []string{"2-shr/registry.tf: line 1 differs", "1 of 53 owned file(s) differ"},
		},
		{
			name: "render keeps the seeded values",
			steps: func(t *testing.T, dir string) (int, string) {
				t.Helper()
				if code, out := run(t, "org", "new", dir); code != 0 {
					t.Fatalf("org new: %d %s", code, out)
				}
				values := filepath.Join(dir, "2-spn", "terraform.tfvars")
				if err := os.WriteFile(values, []byte("processing_units = 200\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				code, out := run(t, "org", "render", "--dir", dir)
				data, err := os.ReadFile(values)
				if err != nil {
					t.Fatal(err)
				}
				if string(data) != "processing_units = 200\n" {
					t.Errorf("render rewrote the seeded values: %q", data)
				}

				return code, out
			},
			wantOut: []string{"53 owned file(s) written"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := orgRepo(t)
			code, out := tt.steps(t, dir)
			if code != tt.wantCode {
				t.Errorf("exit code = %d, want %d:\n%s", code, tt.wantCode, out)
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out, want) {
					t.Errorf("output lacks %q:\n%s", want, out)
				}
			}
		})
	}
}

func TestOrgNewRefusal(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "placement.json"), []byte(`{"prefix": "impulse"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	d := deps{domains: noCloudDomains, secrets: noSecretManager, projects: noProjects, cwd: dir, interactive: never}
	if _, err := execute(d, "", "org", "new", dir); err == nil || !strings.Contains(err.Error(), "prefix") {
		t.Errorf("org new error = %v, want a refusal naming the prefix", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "1-org")); !os.IsNotExist(err) {
		t.Errorf("org new wrote layers from a refused placement: %v", err)
	}
}
