package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/org"
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

// orgDeps are the org commands' seams in tests: no cloud client answers, and the owner
// report runs without credentials.
func orgDeps(dir string) deps {
	return deps{domains: noCloudDomains, secrets: noSecretManager, projects: noProjects, policies: noPolicies, cwd: dir, interactive: never}
}

// noPolicies refuses to open an IAM policy reader, as a run without Google credentials.
func noPolicies(context.Context) (org.PolicyReader, error) {
	return nil, errors.New("no Google credentials in this test")
}

// runOrg runs an org command over the test seams and returns its exit code and output.
func runOrg(t *testing.T, args ...string) (code int, out string) {
	t.Helper()

	out, err := execute(orgDeps(""), "", args...)
	if err != nil {
		var exit exitError
		if !asExit(err, &exit) {
			t.Fatalf("Execute(%v) error = %v", args, err)
		}
		code = exit.code
	}

	return code, out
}

// authorizeConnection sets the two connection values in 2-env's placement, as the
// browser step's reader does before the first application is registered.
func authorizeConnection(t *testing.T, dir string) {
	t.Helper()

	path := filepath.Join(dir, "2-env", "terraform.tfvars")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	values := "github_app_installation_id        = 12345678\ngithub_oauth_token_secret_version = \"projects/imp-tst-gbl-core-1a2b/locations/us-central1/secrets/github-github-oauthtoken-abcdef/versions/1\"\n"
	if err := os.WriteFile(path, append(data, []byte(values)...), 0o600); err != nil {
		t.Fatal(err)
	}
}

// fakePolicyReader answers every project's IAM policy from one map; a project it lacks
// has no bindings.
type fakePolicyReader struct {
	byProject map[string][]org.Binding
}

func (f *fakePolicyReader) ProjectPolicy(_ context.Context, project string) ([]org.Binding, error) {
	return f.byProject[project], nil
}

func (*fakePolicyReader) Close() error {
	return nil
}

// TestOrgCheckOwners: org check lists each person holding roles/owner on an environment
// project, with the project, and says what it does when it has no credentials; neither
// fails the check.
func TestOrgCheckOwners(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		policies org.PolicyReaderFunc
		wantOut  []string
	}{
		{
			name: "a person on roles/owner is reported with the project; a service account is not",
			policies: func(context.Context) (org.PolicyReader, error) {
				return &fakePolicyReader{byProject: map[string][]org.Binding{
					"imp-tst-gbl-core-1a2b": {{Role: "roles/owner", Members: []string{"user:seed@imp.example", "serviceAccount:imp-tst-gbl-tofu@imp-tst-gbl-core-1a2b.iam.gserviceaccount.com"}}},
				}}, nil
			},
			wantOut: []string{
				"tst (imp-tst-gbl-core-1a2b): user:seed@imp.example holds roles/owner, the creator's grant from the first apply of 1-org by hand; it is temporary, removed once the layers workflow applies the layers (1-org/README.md, Applying).",
				"owned file(s) match the placement",
			},
		},
		{
			name: "nobody on roles/owner",
			policies: func(context.Context) (org.PolicyReader, error) {
				return &fakePolicyReader{}, nil
			},
			wantOut: []string{"No person holds roles/owner on an environment project.", "owned file(s) match the placement"},
		},
		{
			name:     "without credentials the check says what it would do",
			policies: noPolicies,
			wantOut: []string{
				"Owners not checked (no Google credentials in this test): org check lists each person (a user: member) holding roles/owner on an environment project, the creator's temporary grant, when it runs with Google credentials that read the projects' IAM policies (gcloud auth application-default login).",
				"owned file(s) match the placement",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := orgRepo(t)
			if code, out := runOrg(t, "org", "new", dir); code != 0 {
				t.Fatalf("org new: %d %s", code, out)
			}
			d := orgDeps(dir)
			d.policies = tt.policies
			out, err := execute(d, "", "org", "check", "--dir", dir)
			if err != nil {
				t.Fatalf("Execute() error = %v; output:\n%s", err, out)
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out, want) {
					t.Errorf("output lacks %q:\n%s", want, out)
				}
			}
		})
	}
}

// TestOrgRegisterNeedsAuthorization: the first application is registered after the Cloud
// Build GitHub App's browser authorization, so register refuses while 2-env's placement
// leaves either connection value unset, and changes nothing.
func TestOrgRegisterNeedsAuthorization(t *testing.T) {
	t.Parallel()

	dir := orgRepo(t)
	if code, out := runOrg(t, "org", "new", dir); code != 0 {
		t.Fatalf("org new: %d %s", code, out)
	}
	_, err := execute(orgDeps(dir), "", "org", "register", "quill", "--dir", dir)
	want := `2-env/terraform.tfvars leaves github_app_installation_id and github_oauth_token_secret_version unset: the Cloud Build GitHub App's browser authorization comes before the first application (2-env/README.md, "The GitHub authorization, before the first application"); set both and register again`
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("Execute() error = %v, want %q", err, want)
	}
	placement, err := os.ReadFile(filepath.Join(dir, "placement.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(placement), `"quill"`) {
		t.Errorf("placement.json changed on a refusal:\n%s", placement)
	}
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

				return runOrg(t, "org", "new", dir)
			},
			wantOut: []string{
				"Rendered the imp foundation", "68 owned file(s)", "seeded .gitignore, 0-bootstrap/terraform.tfvars",
				"By hand, before the layers workflow can run", "0. In the Workspace Admin console: one team group per environment, named in\n     placement.json (teamGroups",
				"Seed, as seed@imp.example", "imp-boot-gbl-tofu",
				"projects.boot, projectNumbers.boot", "5. Apply 1-org, with GITHUB_TOKEN", "a creator's grants, temporary, removed by hand once the",
				"its machine account (imp-machine, an owner of the organization, named in\n     placement.json as githubMachineAccount)",
				"6. In the tst project's Cloud Build console, signed in to GitHub as the organization's\n     machine account (imp-machine): the Cloud Build GitHub App's authorization", "bedrock org register refuses the first application until both are set.",
				"the layers workflow (.github/workflows/layers.yml) applies every layer",
			},
		},
		{
			name: "check is clean after new, the workflow among the owned files",
			steps: func(t *testing.T, dir string) (int, string) {
				t.Helper()
				if code, out := runOrg(t, "org", "new", dir); code != 0 {
					t.Fatalf("org new: %d %s", code, out)
				}
				if _, err := os.Stat(filepath.Join(dir, ".github", "workflows", "layers.yml")); err != nil {
					t.Errorf("org new wrote no workflow: %v", err)
				}

				return runOrg(t, "org", "check", "--dir", dir)
			},
			wantOut: []string{"68 owned file(s) match the placement"},
		},
		{
			name: "register adds an application, renders the values and prints the sequence",
			steps: func(t *testing.T, dir string) (int, string) {
				t.Helper()
				if code, out := runOrg(t, "org", "new", dir); code != 0 {
					t.Fatalf("org new: %d %s", code, out)
				}
				authorizeConnection(t, dir)
				code, out := runOrg(t, "org", "register", "quill", "--dir", dir)
				placement, err := os.ReadFile(filepath.Join(dir, "placement.json"))
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(placement), `"quill"`) {
					t.Errorf("placement.json lacks quill:\n%s", placement)
				}
				hosts, err := os.ReadFile(filepath.Join(dir, "2-net", "applications.auto.tfvars"))
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(hosts), `"quill-tst.apps.imp.example"       = "projects/imp-tst-gbl-core-1a2b/global/backendServices/imp-tst-gbl-quill-backend"`) {
					t.Errorf("2-net/applications.auto.tfvars lacks quill's host:\n%s", hosts)
				}
				if code, out := runOrg(t, "org", "check", "--dir", dir); code != 0 {
					t.Errorf("org check after register: %d %s", code, out)
				}

				return code, out
			},
			wantOut: []string{
				"Registered quill in", "1-org/public-invokers.auto.tfvars among them", "Register through the layers workflow, as pull requests into master",
				"1. placement.json, 1-org/applications.auto.tfvars and 2-env/applications.auto.tfvars", "2. Run workflow (the Actions tab) with 2-env",
				"3. 1-org/public-invokers.auto.tfvars, 2-shr/applications.auto.tfvars and", "5. 2-net/applications.auto.tfvars: the hostnames",
			},
		},
		{
			name: "render says what the workflow still lacks",
			steps: func(t *testing.T, dir string) (int, string) {
				t.Helper()
				if code, out := runOrg(t, "org", "new", dir); code != 0 {
					t.Fatalf("org new: %d %s", code, out)
				}
				path := filepath.Join(dir, "placement.json")
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				edited := strings.Replace(string(data), `"projectNumbers": {"boot": "100000000001"},`, "", 1)
				edited = strings.Replace(edited, `    "boot": "imp-boot-gbl-core-a1b2",`, "", 1)
				if err := os.WriteFile(path, []byte(edited), 0o600); err != nil {
					t.Fatal(err)
				}

				return runOrg(t, "org", "render", "--dir", dir)
			},
			wantOut: []string{"68 owned file(s) written", "The layers workflow cannot run every layer yet: record projectNumbers.boot, projects.boot in placement.json and run bedrock org render."},
		},
		{
			name: "check reports a hand edit",
			steps: func(t *testing.T, dir string) (int, string) {
				t.Helper()
				if code, out := runOrg(t, "org", "new", dir); code != 0 {
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

				return runOrg(t, "org", "check", "--dir", dir)
			},
			wantCode: 1,
			wantOut:  []string{"2-shr/registry.tf: line 1 differs", "1 of 68 owned file(s) differ"},
		},
		{
			name: "check reports a hand edit to the workflow",
			steps: func(t *testing.T, dir string) (int, string) {
				t.Helper()
				if code, out := runOrg(t, "org", "new", dir); code != 0 {
					t.Fatalf("org new: %d %s", code, out)
				}
				path := filepath.Join(dir, ".github", "workflows", "layers.yml")
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				edited := strings.Replace(string(data), "      fail-fast: true\n", "      fail-fast: false\n", 1)
				if err := os.WriteFile(path, []byte(edited), 0o600); err != nil {
					t.Fatal(err)
				}

				return runOrg(t, "org", "check", "--dir", dir)
			},
			wantCode: 1,
			wantOut:  []string{".github/workflows/layers.yml: line ", "  want:       fail-fast: true", "  got:        fail-fast: false", "1 of 68 owned file(s) differ"},
		},
		{
			name: "render keeps the seeded values",
			steps: func(t *testing.T, dir string) (int, string) {
				t.Helper()
				if code, out := runOrg(t, "org", "new", dir); code != 0 {
					t.Fatalf("org new: %d %s", code, out)
				}
				values := filepath.Join(dir, "2-spn", "terraform.tfvars")
				if err := os.WriteFile(values, []byte("processing_units = 200\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				code, out := runOrg(t, "org", "render", "--dir", dir)
				data, err := os.ReadFile(values)
				if err != nil {
					t.Fatal(err)
				}
				if string(data) != "processing_units = 200\n" {
					t.Errorf("render rewrote the seeded values: %q", data)
				}

				return code, out
			},
			wantOut: []string{"68 owned file(s) written"},
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

func TestOrgRegisterRefusal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		app     string
		wantErr string
	}{
		{name: "an application registered already", app: "harbor", wantErr: `application "harbor" is registered already`},
		{name: "a code of the wrong shape", app: "Harbor", wantErr: `application "Harbor" is not 1 to 6 lowercase alphanumeric characters`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := orgRepo(t)
			d := deps{domains: noCloudDomains, secrets: noSecretManager, projects: noProjects, cwd: dir, interactive: never}
			if _, err := execute(d, "", "org", "register", tt.app, "--dir", dir); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Execute() error = %v, wantErr %q", err, tt.wantErr)
			}
			placement, err := os.ReadFile(filepath.Join(dir, "placement.json"))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Count(string(placement), `"harbor"`) != 1 || strings.Contains(string(placement), "Harbor") {
				t.Errorf("placement.json changed on a refusal:\n%s", placement)
			}
		})
	}
}
