package cli

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cccteam/ccc/bedrock/internal/org"
	"github.com/cccteam/ccc/bedrock/internal/secret"
)

const (
	infraKey    = "imp-boot-gbl-github-infrastructure-key"
	deployerKey = "imp-boot-gbl-github-deployer-key"
)

// orgSecrets are the organization's containers, both in the boot project: the
// infrastructure key at three versions, the third disabled, and the deployer key at two.
func orgSecrets() labSecrets {
	return labSecrets{containers: map[string][]string{
		infraKey:    {secret.Enabled, secret.Enabled, "DISABLED"},
		deployerKey: {secret.Enabled, secret.Enabled},
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
// organization secret's value in the container 0-bootstrap made in the boot project, and
// secret pin writes the version in placement.json, where the triggers' runs read it, and
// renders (the deployer key's version reaches 2-env's outputs). An unknown secret, an
// application's arguments and an environment are refused.
func TestOrgSecret(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// args are the command line after "secret"; --dir <root> is added unless cwd
		// names a directory under the root to run from. edit changes the placement
		// first. value is standard input.
		args       []string
		cwd        string
		edit       func(p *org.Placement)
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
			name:  "the deployer key is added to the boot project's container too, found from the working directory",
			args:  []string{"add", "github-deployer-key"},
			cwd:   "2-env",
			value: "deployer-pem",
			wantOut: []string{
				"Added version 3 of imp-boot-gbl-github-deployer-key in project imp-boot-gbl-core-a1b2: the deployer GitHub App's private key.\n",
				"Pin it: bedrock secret pin github-deployer-key 3\n",
			},
			wantAdded: map[string]string{deployerKey: "deployer-pem"},
		},
		{
			name:    "a container the layer has not made is refused, not created",
			args:    []string{"add", "github-deployer-key", "--container", "imp-boot-gbl-github-deployer-key-2"},
			value:   "deployer-pem",
			wantErr: "project imp-boot-gbl-core-a1b2 has no container imp-boot-gbl-github-deployer-key-2: the apply of 0-bootstrap creates it; apply the layer, then add the value",
		},
		{
			name:    "an empty value is refused",
			args:    []string{"add", "github-infrastructure-key"},
			wantErr: "the value of github-infrastructure-key is empty: nothing to add",
		},
		{
			name:    "the deployer key takes no environment either",
			args:    []string{"add", "github-deployer-key", "stg"},
			value:   "deployer-pem",
			wantErr: "github-deployer-key lives in the boot project and takes no environment: bedrock secret add github-deployer-key (given github-deployer-key stg)",
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
			wantErr: "this is the organization's infrastructure repository (placement.json names organization 123456789012): secret add takes an organization secret here, github-infrastructure-key or github-deployer-key; an application's secret ([env] [VARIABLE]) is added from the application's repository",
		},
		{
			name:    "an application's layer flag is refused",
			args:    []string{"add", "github-deployer-key", "--app", "quill"},
			value:   "pem",
			wantErr: "--app and --layer name an application's layer: an organization secret takes neither",
		},
		{
			name: "the infrastructure key's version is pinned in placement.json, where the triggers' runs read it",
			args: []string{"pin", "github-infrastructure-key", "2"},
			wantOut: []string{
				"Pinned github-infrastructure-key to version 2: githubInfrastructureKeyVersion in ",
				"Secret Manager confirms version 2 of imp-boot-gbl-github-infrastructure-key in project imp-boot-gbl-core-a1b2 is enabled.\n",
				"Next: commit placement.json and the files org render rewrote (",
				"from its merge on, the triggers' runs mint 1-org's GitHub token from version 2.\n",
			},
			wantAbsent: []string{"githubInfrastructureAppId"},
			wantFiles:  map[string][]string{"placement.json": {`"githubInfrastructureKeyVersion": "2"`}},
		},
		{
			name:    "the infrastructure key's version pinned already changes nothing",
			args:    []string{"pin", "github-infrastructure-key", "1"},
			wantOut: []string{"github-infrastructure-key is already pinned to version 1 (githubInfrastructureKeyVersion in "},
		},
		{
			name:    "the infrastructure app not recorded is said",
			args:    []string{"pin", "github-infrastructure-key", "2"},
			edit:    func(p *org.Placement) { p.GithubInfrastructureAppID = "" },
			wantOut: []string{"placement.json records no githubInfrastructureAppId: record the infrastructure app's App ID there, from the app's settings page, and run bedrock org render; until both are recorded a run of the triggers that plans or applies 1-org stops.\n"},
		},
		{
			name:    "a disabled version is refused",
			args:    []string{"pin", "github-infrastructure-key", "3"},
			wantErr: "version 3 of imp-boot-gbl-github-infrastructure-key in project imp-boot-gbl-core-a1b2 is DISABLED, not ENABLED: the triggers' runs could not read it; choose an enabled version",
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
			name: "the deployer key's version is pinned in placement.json and rendered into 2-env's outputs, from the working directory",
			args: []string{"pin", "github-deployer-key", "2"},
			cwd:  "2-env",
			wantOut: []string{
				"Pinned github-deployer-key to version 2: githubDeployerKeyVersion in ",
				"Secret Manager confirms version 2 of imp-boot-gbl-github-deployer-key in project imp-boot-gbl-core-a1b2 is enabled.\n",
				"Next: commit placement.json and the files org render rewrote (",
				"2-env's outputs among them) and open the pull request; from its merge on, the plan trigger's run posts the layers' plans as the deployer app from version 2, and each environment's apply of 2-env carries the version to the applications' pipelines through its output github_deployer_key_secret_version.\n",
			},
			wantAbsent: []string{"githubDeployerAppId"},
			wantFiles: map[string][]string{
				"placement.json":  {`"githubDeployerKeyVersion": "2"`},
				"2-env/locals.tf": {`github_deployer_key_secret_version = "projects/imp-boot-gbl-core-a1b2/secrets/imp-boot-gbl-github-deployer-key/versions/2"`},
			},
		},
		{
			name:    "the deployer app not recorded is said",
			args:    []string{"pin", "github-deployer-key", "2"},
			edit:    func(p *org.Placement) { p.GithubDeployerAppID = "" },
			wantOut: []string{"placement.json records no githubDeployerAppId: record the deployer app's App ID there, from the app's settings page, and run bedrock org render; until both are recorded the plans are posted by nobody and the pipelines talk back through nothing.\n"},
		},
		{
			name:      "the deployer key's version pinned already changes nothing",
			args:      []string{"pin", "github-deployer-key", "1"},
			wantOut:   []string{"github-deployer-key is already pinned to version 1 (githubDeployerKeyVersion in "},
			wantFiles: map[string][]string{"placement.json": {`"githubDeployerKeyVersion": "1"`}},
		},
		{
			name:    "the deployer key's version left out is refused with the container's versions",
			args:    []string{"pin", "github-deployer-key"},
			wantErr: "no version given and no terminal to ask on: pass one of 2 (ENABLED), 1 (ENABLED)",
		},
		{
			name:    "the deployer key takes its version alone",
			args:    []string{"pin", "github-deployer-key", "tst", "1"},
			wantErr: "github-deployer-key lives in the boot project and takes no environment: bedrock secret pin github-deployer-key <version> (given github-deployer-key tst 1)",
		},
		{
			name:    "an unknown organization secret is refused on pin",
			args:    []string{"pin", "github-release-key", "1"},
			wantErr: `"github-release-key" is not an organization secret: one of github-infrastructure-key, github-deployer-key`,
		},
		{
			name:    "an application's secret is refused on pin",
			args:    []string{"pin", "tst", "APP_COOKIE_KEY", "3"},
			wantErr: "secret pin takes an organization secret here, github-infrastructure-key or github-deployer-key; an application's secret ([env] [VARIABLE]) is pinned from the application's repository",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := orgSecretRepo(t)
			if tt.edit != nil {
				editPlacement(t, dir, tt.edit)
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
// organization's secrets and, for pin, the container's versions with their states.
func TestOrgSecretCompletion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want []string
	}{
		{name: "the secrets", args: []string{"pin", ""}, want: []string{"github-infrastructure-key", "github-deployer-key"}},
		{name: "the infrastructure key's versions", args: []string{"pin", "github-infrastructure-key", ""}, want: []string{"3\tDISABLED", "2\tENABLED", "1\tENABLED"}},
		{name: "the deployer key's versions", args: []string{"pin", "github-deployer-key", ""}, want: []string{"2\tENABLED", "1\tENABLED"}},
		{name: "nothing after the infrastructure key on add", args: []string{"add", "github-infrastructure-key", ""}},
		{name: "nothing after the deployer key on add", args: []string{"add", "github-deployer-key", ""}},
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
