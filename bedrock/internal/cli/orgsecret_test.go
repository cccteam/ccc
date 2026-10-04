package cli

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cccteam/ccc/bedrock/internal/secret"
)

const (
	infraKey     = "imp-boot-gbl-github-infrastructure-key"
	tstDeployer  = "imp-tst-gbl-github-deployer-key"
	stgDeployer  = "imp-stg-gbl-github-deployer-key"
	tstKeyPinned = `tst = "projects/imp-tst-gbl-core-1a2b/secrets/imp-tst-gbl-github-deployer-key/versions/1"`
)

// orgSecrets are the organization's containers: the infrastructure key at three
// versions, the third disabled, and the deployer key in tst at one and in stg at two;
// prd's container has not been made yet.
func orgSecrets() labSecrets {
	return labSecrets{containers: map[string][]string{
		infraKey:    {secret.Enabled, secret.Enabled, "DISABLED"},
		tstDeployer: {secret.Enabled},
		stgDeployer: {secret.Enabled, secret.Enabled},
	}}
}

// orgSecretRepo is an infrastructure repository rendered from the fixture organization's
// placement, marked a repository so it is found from a working directory inside it.
func orgSecretRepo(t *testing.T) string {
	t.Helper()

	dir := orgRepo(t)
	if code, out := runOrg(t, "org", "new", dir); code != 0 {
		t.Fatalf("org new: %d %s", code, out)
	}
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}

	return dir
}

// TestOrgSecret: in the organization's infrastructure repository, secret add stores an
// organization secret's value in the container its layer made, and secret pin writes the
// version where the layers read it: the infrastructure key in placement.json and, through
// org render, the layers workflow; the deployer key per environment in 2-env's placement.
// An unknown secret, an application's arguments and a key past what it takes are refused.
func TestOrgSecret(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// args are the command line after "secret"; --dir <root> is added unless cwd
		// names a directory under the root to run from. tfvars is appended to 2-env's
		// placement first. value is standard input.
		args       []string
		cwd        string
		tfvars     string
		value      string
		wantErr    string
		wantOut    []string
		wantAbsent []string
		// wantAdded is the value the container named received; wantFiles maps a file
		// under the root to what it must hold afterwards.
		wantAdded map[string]string
		wantFiles map[string][]string
	}{
		{
			name:  "the infrastructure key is added to the boot project's container",
			args:  []string{"add", "github-infrastructure-key"},
			value: "infrastructure key\n",
			wantOut: []string{
				"Added version 4 of imp-boot-gbl-github-infrastructure-key in project imp-boot-gbl-core-a1b2: the infrastructure GitHub App's private key.\n",
				"Pin it: bedrock secret pin github-infrastructure-key 4\n",
			},
			wantAdded: map[string]string{infraKey: "infrastructure key\n"},
		},
		{
			name:  "the deployer key is added to the environment's container, found from the working directory",
			args:  []string{"add", "github-deployer-key", "stg"},
			cwd:   "2-env",
			value: "deployer-pem",
			wantOut: []string{
				"Added version 3 of imp-stg-gbl-github-deployer-key in project imp-stg-gbl-core-3c4d: the deployer GitHub App's private key in stg.\n",
				"Pin it: bedrock secret pin github-deployer-key stg 3\n",
			},
			wantAdded: map[string]string{stgDeployer: "deployer-pem"},
		},
		{
			name:    "a container the layer has not made is refused, not created",
			args:    []string{"add", "github-deployer-key", "prd"},
			value:   "deployer-pem",
			wantErr: "project imp-prd-gbl-core-5e6f has no container imp-prd-gbl-github-deployer-key: the apply of 2-env in prd creates it; apply the layer, then add the value",
		},
		{
			name:    "an empty value is refused",
			args:    []string{"add", "github-infrastructure-key"},
			wantErr: "the value of github-infrastructure-key is empty: nothing to add",
		},
		{
			name:    "the deployer key's environment left out is refused with the choices",
			args:    []string{"add", "github-deployer-key"},
			value:   "deployer-pem",
			wantErr: "no environment given and no terminal to ask on: pass one of tst, stg, prd",
		},
		{
			name:    "the secret left out is refused with the choices",
			args:    []string{"add"},
			value:   "pem",
			wantErr: "no organization secret given and no terminal to ask on: pass one of github-infrastructure-key, github-deployer-key",
		},
		{
			name:    "the infrastructure key takes no environment",
			args:    []string{"add", "github-infrastructure-key", "tst"},
			value:   "pem",
			wantErr: "github-infrastructure-key lives in the boot project and takes no environment: bedrock secret add github-infrastructure-key (given github-infrastructure-key tst)",
		},
		{
			name:    "an unknown organization secret is refused on add",
			args:    []string{"add", "github-release-key"},
			value:   "pem",
			wantErr: `"github-release-key" is not an organization secret: one of github-infrastructure-key, github-deployer-key`,
		},
		{
			name:    "an application's secret is refused on add",
			args:    []string{"add", "tst", "APP_MAIL_API_KEY"},
			value:   "k-1",
			wantErr: "this is the organization's infrastructure repository (placement.json names organization 123456789012): secret add takes an organization secret here, github-infrastructure-key or github-deployer-key with an environment; an application's secret ([env] [VARIABLE]) is added from the application's repository",
		},
		{
			name:    "an application's layer flag is refused",
			args:    []string{"add", "github-deployer-key", "tst", "--app", "quill"},
			value:   "pem",
			wantErr: "--app and --layer name an application's layer: an organization secret takes neither",
		},
		{
			name: "the infrastructure key's version is pinned in placement.json and rendered into the workflow",
			args: []string{"pin", "github-infrastructure-key", "2"},
			wantOut: []string{
				"Pinned github-infrastructure-key to version 2: githubInfrastructureKeyVersion in ",
				"Secret Manager confirms version 2 of imp-boot-gbl-github-infrastructure-key in project imp-boot-gbl-core-a1b2 is enabled.\n",
				"Next: commit placement.json and the files org render rewrote (",
				"from its merge on, the layers workflow mints 1-org's GitHub token from version 2.\n",
			},
			wantAbsent: []string{"githubInfrastructureAppId"},
			wantFiles: map[string][]string{
				"placement.json":               {`"githubInfrastructureKeyVersion": "2"`},
				".github/workflows/layers.yml": {"KEY_VERSION: 2\n"},
			},
		},
		{
			name:      "the infrastructure key's version pinned already changes nothing",
			args:      []string{"pin", "github-infrastructure-key", "1"},
			wantOut:   []string{"github-infrastructure-key is already pinned to version 1 (githubInfrastructureKeyVersion in "},
			wantFiles: map[string][]string{".github/workflows/layers.yml": {"KEY_VERSION: 1\n"}},
		},
		{
			name:    "a disabled version is refused",
			args:    []string{"pin", "github-infrastructure-key", "3"},
			wantErr: "version 3 of imp-boot-gbl-github-infrastructure-key in project imp-boot-gbl-core-a1b2 is DISABLED, not ENABLED: the layers could not read it; choose an enabled version",
		},
		{
			name:    "latest is refused",
			args:    []string{"pin", "github-infrastructure-key", "latest"},
			wantErr: `version "latest": an organization secret is pinned by its number, never latest`,
		},
		{
			name:    "the infrastructure key takes its version alone",
			args:    []string{"pin", "github-infrastructure-key", "tst", "2"},
			wantErr: "github-infrastructure-key lives in the boot project and takes no environment: bedrock secret pin github-infrastructure-key <version> (given github-infrastructure-key tst 2)",
		},
		{
			name: "the deployer key's version is pinned in 2-env's placement by resource name, from the working directory",
			args: []string{"pin", "github-deployer-key", "tst", "1"},
			cwd:  "2-env",
			wantOut: []string{
				"Pinned github-deployer-key in tst to version 1: github_deployer_key_secret_versions.tst in ",
				"projects/imp-tst-gbl-core-1a2b/secrets/imp-tst-gbl-github-deployer-key/versions/1.\n",
				"Secret Manager confirms version 1 of imp-tst-gbl-github-deployer-key in project imp-tst-gbl-core-1a2b is enabled.\n",
				"2-env/terraform.tfvars leaves github_deployer_app_id unset: set it to the deployer app's App ID, from the app's settings page; until then the pipeline talks back through nothing.\n",
				"Next: commit 2-env/terraform.tfvars and open the pull request; the layers workflow's plan of 2-env shows the change to tst's output github_deployer_key_secret_version",
			},
			wantFiles: map[string][]string{"2-env/terraform.tfvars": {"github_deployer_key_secret_versions = {\n  " + tstKeyPinned + "\n}\n"}},
		},
		{
			name:       "a second environment's pin joins the first, beside the app's id",
			args:       []string{"pin", "github-deployer-key", "stg", "2"},
			tfvars:     "github_deployer_app_id = 5080800\ngithub_deployer_key_secret_versions = {\n  " + tstKeyPinned + "\n}\n",
			wantOut:    []string{"Pinned github-deployer-key in stg to version 2: github_deployer_key_secret_versions.stg in "},
			wantAbsent: []string{"leaves github_deployer_app_id unset"},
			wantFiles: map[string][]string{"2-env/terraform.tfvars": {
				tstKeyPinned + "\n  stg = \"projects/imp-stg-gbl-core-3c4d/secrets/imp-stg-gbl-github-deployer-key/versions/2\"\n}\n",
			}},
		},
		{
			name:      "the deployer key's version pinned already changes nothing",
			args:      []string{"pin", "github-deployer-key", "tst", "1"},
			tfvars:    "github_deployer_key_secret_versions = { " + tstKeyPinned + " }\n",
			wantOut:   []string{"github-deployer-key in tst is already pinned to version 1 (github_deployer_key_secret_versions.tst in "},
			wantFiles: map[string][]string{"2-env/terraform.tfvars": {"github_deployer_key_secret_versions = { " + tstKeyPinned + " }\n"}},
		},
		{
			name:    "the deployer key's version left out is refused with the container's versions",
			args:    []string{"pin", "github-deployer-key", "stg"},
			wantErr: "no version given and no terminal to ask on: pass one of 2 (ENABLED), 1 (ENABLED)",
		},
		{
			name:    "an unknown organization secret is refused on pin",
			args:    []string{"pin", "github-release-key", "1"},
			wantErr: `"github-release-key" is not an organization secret: one of github-infrastructure-key, github-deployer-key`,
		},
		{
			name:    "an application's secret is refused on pin",
			args:    []string{"pin", "tst", "APP_COOKIE_KEY", "3"},
			wantErr: "secret pin takes an organization secret here, github-infrastructure-key or github-deployer-key with an environment; an application's secret ([env] [VARIABLE]) is pinned from the application's repository",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := orgSecretRepo(t)
			if tt.tfvars != "" {
				file := filepath.Join(dir, "2-env", "terraform.tfvars")
				data, err := os.ReadFile(file)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(file, append(data, []byte(tt.tfvars)...), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			secrets := orgSecrets()
			secrets.created, secrets.added = map[string]map[string]string{}, map[string]string{}
			d := deps{domains: noCloudDomains, secrets: secrets.open, projects: noProjects, cwd: dir, interactive: never}
			args := append([]string{secretCommand}, tt.args...)
			if tt.cwd != "" {
				d.cwd = filepath.Join(dir, tt.cwd)
			} else {
				args = append(args, "--dir", dir)
			}
			out, err := execute(d, tt.value, args...)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Execute() error = %v, wantErr %q; output:\n%s", err, tt.wantErr, out)
				}
				if len(secrets.added) != 0 || len(secrets.created) != 0 {
					t.Errorf("Execute() added %v and created %v, want nothing", secrets.added, secrets.created)
				}

				return
			}
			if err != nil {
				t.Fatalf("Execute() error = %v; output:\n%s", err, out)
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out, want) {
					t.Errorf("output lacks %q:\n%s", want, out)
				}
			}
			for _, absent := range tt.wantAbsent {
				if strings.Contains(out, absent) {
					t.Errorf("output shows %q:\n%s", absent, out)
				}
			}
			if len(secrets.created) != 0 {
				t.Errorf("created %v, want nothing created", secrets.created)
			}
			for container, want := range tt.wantAdded {
				if got := secrets.added[container]; got != want {
					t.Errorf("%s received %q, want %q", container, got, want)
				}
			}
			if tt.wantAdded == nil && len(secrets.added) != 0 {
				t.Errorf("added %v, want nothing added", secrets.added)
			}
			for file, wants := range tt.wantFiles {
				data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(file)))
				if err != nil {
					t.Fatal(err)
				}
				for _, want := range wants {
					if !strings.Contains(string(data), want) {
						t.Errorf("%s lacks %q:\n%s", file, want, data)
					}
				}
			}
		})
	}
}

// TestOrgSecretInApplication: in an application's repository, an organization secret's
// name is refused where the environment goes, and nothing is looked up.
func TestOrgSecretInApplication(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		fixture string
		args    []string
		wantErr string
	}{
		{
			name:    "add in the organization layout without an organization placement",
			fixture: fixtureFlat,
			args:    []string{"add", "github-deployer-key", "tst"},
			wantErr: "github-deployer-key is an organization secret, added and pinned from the organization's infrastructure repository (the one whose placement.json names the organization, organizationId); in an application's repository secret add takes [env] [VARIABLE]",
		},
		{
			name:    "pin in an application repository",
			fixture: fixtureAppRepo,
			args:    []string{"pin", "github-infrastructure-key", "2"},
			wantErr: "github-infrastructure-key is an organization secret, added and pinned from the organization's infrastructure repository (the one whose placement.json names the organization, organizationId); in an application's repository secret pin takes [env] [VARIABLE]",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := copyRepo(t, tt.fixture)
			d := deps{domains: noCloudDomains, secrets: noSecretManager, projects: noProjects, cwd: dir, interactive: never}
			out, err := execute(d, "pem", append([]string{secretCommand}, tt.args...)...)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Execute() error = %v, wantErr %q; output:\n%s", err, tt.wantErr, out)
			}
		})
	}
}

// TestOrgSecretCompletion: in the infrastructure repository the arguments complete to the
// organization's secrets, the deployer key's environments and, for pin, the container's
// versions with their states.
func TestOrgSecretCompletion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want []string
	}{
		{name: "the secrets", args: []string{"pin", ""}, want: []string{"github-infrastructure-key", "github-deployer-key"}},
		{name: "the deployer key's environments", args: []string{"add", "github-deployer-key", ""}, want: []string{"tst", "stg", "prd"}},
		{name: "the infrastructure key's versions", args: []string{"pin", "github-infrastructure-key", ""}, want: []string{"3\tDISABLED", "2\tENABLED", "1\tENABLED"}},
		{name: "the deployer key's versions in the environment", args: []string{"pin", "github-deployer-key", "stg", ""}, want: []string{"2\tENABLED", "1\tENABLED"}},
		{name: "nothing after the infrastructure key on add", args: []string{"add", "github-infrastructure-key", ""}},
		{name: "nothing after an unknown secret", args: []string{"pin", "github-release-key", ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := orgSecretRepo(t)
			d := deps{domains: noCloudDomains, secrets: orgSecrets().open, projects: noProjects, cwd: dir, interactive: never}
			args := append([]string{"__complete", secretCommand, tt.args[0], "--dir", dir}, tt.args[1:]...)
			out, err := execute(d, "", args...)
			if err != nil {
				t.Fatalf("Execute() error = %v; output:\n%s", err, out)
			}
			lines := strings.Split(out, "\n")
			end := slices.Index(lines, ":4")
			if end < 0 {
				t.Fatalf("no :4 directive (no file completion) in the output:\n%s", out)
			}
			got := lines[:end]
			if len(got) == 0 {
				got = nil
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("completions = %q, want %q", got, tt.want)
			}
		})
	}
}
