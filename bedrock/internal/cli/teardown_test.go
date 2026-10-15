package cli

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/deploy"
)

// teardownTofu is tofu in the teardown tests: it records every command and answers the
// state list and the plan of a stack holding one service, one trigger and one database.
type teardownTofu struct {
	ran []string
}

const (
	teardownStateList = "google_cloud_run_v2_service.app[\"uc1\"]\ngoogle_cloudbuild_trigger.version[0]\ngoogle_spanner_database.quill[0]\n"
	teardownPlan      = `{"resource_changes": [{"address": "google_cloud_run_v2_service.app[\"uc1\"]", "mode": "managed", "change": {"actions": ["delete"]}}, {"address": "google_cloudbuild_trigger.version[0]", "mode": "managed", "change": {"actions": ["delete"]}}]}`
	teardownState     = `{"values": {"root_module": {"resources": [{"address": "google_cloud_run_v2_service.app[\"uc1\"]", "mode": "managed"}, {"address": "google_cloudbuild_trigger.version[0]", "mode": "managed", "depends_on": ["google_cloud_run_v2_service.app"]}, {"address": "google_spanner_database.quill[0]", "mode": "managed"}]}}}`
	destroyPlan       = `{"resource_changes": [{"address": "google_cloud_run_v2_service.app[\"uc1\"]", "mode": "managed", "change": {"actions": ["delete"]}}, {"address": "google_cloudbuild_trigger.version[0]", "mode": "managed", "change": {"actions": ["delete"]}}, {"address": "google_spanner_database.quill[0]", "mode": "managed", "change": {"actions": ["delete"]}}]}`
)

func (f *teardownTofu) Run(_ context.Context, c deploy.Command, _ io.Writer) error {
	f.ran = append(f.ran, c.String())

	return nil
}

func (f *teardownTofu) Output(_ context.Context, c deploy.Command, _ io.Writer) ([]byte, error) {
	f.ran = append(f.ran, c.String())
	switch {
	case strings.HasPrefix(c.String(), "tofu state list"):
		return []byte(teardownStateList), nil
	case c.String() == "tofu show -json":
		return []byte(teardownState), nil
	case strings.HasPrefix(c.String(), "tofu output"):
		return []byte(`{"_SERVICES": "uc1=quill-pr4"}`), nil
	case strings.Contains(strings.Join(f.ran, "\n"), "-var destroy=true"):
		return []byte(destroyPlan), nil
	default:
		return []byte(teardownPlan), nil
	}
}

// teardownRepo is an application repository in the application layout, quill, with its
// stack under infrastructure/ and the lab placement beside it.
func teardownRepo(t *testing.T) string {
	t.Helper()
	dir := copyRepo(t, fixtureAppRepo)
	data, err := os.ReadFile(placement)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "infrastructure", placementFile), data, 0o600); err != nil {
		t.Fatal(err)
	}

	return dir
}

// teardownStore is the state bucket with quill applied in tst and prd, 2-net routing
// nothing of it, and the records buckets with each stack's id.
func teardownStore(t *testing.T) *memoryStore {
	t.Helper()
	state := func(env string) string {
		data, err := json.Marshal(map[string]any{"outputs": map[string]any{
			"net_hosts":     map[string]any{"value": map[string]string{"quill-" + env + ".impulseframework.dev": "projects/p/global/backendServices/imp-" + env + "-gbl-quill-backend"}},
			"substitutions": map[string]any{"value": map[string]string{"_RECORDS_BUCKET": "imp-" + env + "-gbl-quill-records", "_PROJECT": "imp-" + env + "-gbl-core", "_SERVICES": "uc1=x"}},
		}})
		if err != nil {
			t.Fatal(err)
		}

		return string(data)
	}

	return &memoryStore{objects: map[string]string{
		"gs://imp-boot-gbl-state-REPLACEME/3-app/quill/tst/default.tfstate":     state("tst"),
		"gs://imp-boot-gbl-state-REPLACEME/3-app/quill/prd/default.tfstate":     state("prd"),
		"gs://imp-boot-gbl-state-REPLACEME/3-app/quill/tst/pr4/default.tfstate": "{}",
		"gs://imp-boot-gbl-state-REPLACEME/2-net/default.tfstate":               `{"outputs": {"hosts": {"value": {"other.impulseframework.dev": "projects/p/global/backendServices/other"}}}}`,
		"gs://imp-tst-gbl-quill-records/quill/stacks/tst/stack.json":            `{"id": "1a2b"}`,
		"gs://imp-prd-gbl-quill-records/quill/stacks/prd/stack.json":            `{"id": "3c4d"}`,
	}}
}

func TestTeardownAndDestroy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		args        []string
		interactive func() bool
		in          string
		wantErr     string
		wantOut     []string
		wantRan     []string
		wantRecords []string
		// noAccount leaves the credentials' account unreadable, so the record names the
		// computer's user at its host.
		noAccount bool
		wantBy    string
	}{
		{
			name:        "refuses without an environment",
			args:        []string{"teardown"},
			interactive: never,
			wantErr:     "name the environment (tst, stg, prd), or --all for every one",
		},
		{
			name:        "refuses an environment the placement lacks",
			args:        []string{"destroy", "qa"},
			interactive: never,
			wantErr:     `"qa" is not one of the environments (tst, stg, prd)`,
		},
		{
			name:        "refuses an environment with --all",
			args:        []string{"teardown", "tst", "--all"},
			interactive: never,
			wantErr:     "--all takes every environment: name none",
		},
		{
			name:        "refuses without a terminal and without --yes, after the warning",
			args:        []string{"teardown", "tst"},
			interactive: never,
			wantErr:     "bedrock teardown asks for tst typed back at the terminal, and this run has none: --yes stands in for it",
			wantOut:     []string{"bedrock teardown takes quill's stack in tst down to what it keeps", "tst: 2 resource(s) destroyed, 1 kept.", "goes: 1 google_cloud_run_v2_service, 1 google_cloudbuild_trigger", "stays: 1 google_spanner_database", "Nothing here is undone by bedrock."},
			wantRan:     []string{"tofu init -input=false -no-color -reconfigure -backend-config=prefix=3-app/quill/tst", "tofu state list", "tofu show -json"},
		},
		{
			name:        "stops when something else than the environment is typed",
			args:        []string{"destroy", "prd"},
			interactive: always,
			in:          "tst\n",
			wantOut:     []string{"bedrock destroy destroys quill's stack whole in prd", "prd: 3 resource(s) destroyed, 0 kept.", `protection lifted first: google_cloud_run_v2_service.app["uc1"], google_spanner_database.quill[0]`, "Type prd to go on; anything else stops:", "Stopped: nothing was destroyed."},
			wantRan:     []string{"tofu init -input=false -no-color -reconfigure -backend-config=prefix=3-app/quill/prd", "tofu state list"},
		},
		{
			name:        "tears tst down when the environment is typed",
			args:        []string{"teardown", "tst"},
			interactive: always,
			in:          "tst\n",
			wantOut:     []string{"Type tst to go on; anything else stops:", "The plan destroys the 2 resource(s) the teardown names and keeps 1, as meant; applying it.", "Tore quill's stack in tst down: 2 resource(s) destroyed, 1 kept"},
			wantRan: []string{
				"tofu init -input=false -no-color -reconfigure -backend-config=prefix=3-app/quill/tst", "tofu state list", "tofu show -json",
				"tofu plan -destroy -input=false -no-color -out=DATA/tst/teardown.tfplan -var environment=tst -var database_generation=1 -var teardown=true -exclude=google_spanner_database.quill[0]",
				"tofu show -json DATA/tst/teardown.tfplan", "tofu apply -input=false -no-color DATA/tst/teardown.tfplan",
			},
			wantRecords: []string{"gs://imp-tst-gbl-quill-records/quill/stacks/tst/teardown.json"},
			wantBy:      `"by": "jo@lab.example"`,
		},
		{
			name:        "names the computer's user at its host when the credentials' account cannot be read",
			args:        []string{"teardown", "tst", "--yes"},
			interactive: never,
			noAccount:   true,
			wantOut:     []string{"Tore quill's stack in tst down: 2 resource(s) destroyed, 1 kept"},
			wantRan: []string{
				"tofu init -input=false -no-color -reconfigure -backend-config=prefix=3-app/quill/tst", "tofu state list", "tofu show -json",
				"tofu plan -destroy -input=false -no-color -out=DATA/tst/teardown.tfplan -var environment=tst -var database_generation=1 -var teardown=true -exclude=google_spanner_database.quill[0]",
				"tofu show -json DATA/tst/teardown.tfplan", "tofu apply -input=false -no-color DATA/tst/teardown.tfplan",
			},
			wantRecords: []string{"gs://imp-tst-gbl-quill-records/quill/stacks/tst/teardown.json"},
			wantBy:      `"by": "` + computerUser() + `@`,
		},
		{
			name:        "destroys every environment and the pull-request stacks with --all --yes, the unapplied one skipped",
			args:        []string{"destroy", "--all", "--yes"},
			interactive: never,
			wantOut:     []string{"No stack in stg", "Pull-request stacks under tst, destroyed whole with their builds' jobs: 4", "Destroyed quill's stack in tst: 3 resource(s).", "Destroyed quill's stack in prd: 3 resource(s).", "=== Pull request 4: stack at 3-app/quill/tst/pr4 ==="},
			wantRan: []string{
				"tofu init -input=false -no-color -reconfigure -backend-config=prefix=3-app/quill/tst", "tofu state list",
				"tofu init -input=false -no-color -reconfigure -backend-config=prefix=3-app/quill/prd", "tofu state list",
				"tofu plan -destroy -input=false -no-color -out=DATA/tst/teardown.tfplan -var environment=tst -var database_generation=1 -var destroy=true",
				"tofu show -json DATA/tst/teardown.tfplan", "tofu apply -input=false -no-color DATA/tst/teardown.tfplan",
				`tofu apply -input=false -no-color -auto-approve -var environment=prd -var database_generation=1 -var destroy=true -target=google_cloud_run_v2_service.app["uc1"] -target=google_spanner_database.quill[0]`,
				"tofu plan -destroy -input=false -no-color -out=DATA/prd/teardown.tfplan -var environment=prd -var database_generation=1 -var destroy=true",
				"tofu show -json DATA/prd/teardown.tfplan", "tofu apply -input=false -no-color DATA/prd/teardown.tfplan",
				"tofu init -input=false -no-color -reconfigure -backend-config=prefix=3-app/quill/tst/pr4",
				"tofu output -json substitutions",
				"tofu destroy -auto-approve -input=false -no-color -var environment=tst -var pull_request=4",
			},
			wantRecords: []string{"gs://imp-tst-gbl-quill-records/quill/stacks/tst/teardown.json", "gs://imp-prd-gbl-quill-records/quill/stacks/prd/teardown.json"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := teardownRepo(t)
			store := teardownStore(t)
			tofu := &teardownTofu{}
			principal := func(context.Context) (string, error) { return "jo@lab.example", nil }
			if tt.noAccount {
				principal = func(context.Context) (string, error) { return "", errors.New("no credentials") }
			}
			d := deps{domains: noCloudDomains, secrets: noSecretManager, projects: noProjects, cwd: repo, interactive: tt.interactive, deploy: &deploy.Clients{Storage: store.open, Exec: tofu}, principal: principal}
			out, err := execute(d, tt.in, tt.args...)
			switch {
			case tt.wantErr == "" && err != nil:
				t.Fatalf("%v = %v, want no error; printed:\n%s", tt.args, err, out)
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Fatalf("%v = %v, want an error containing %q", tt.args, err, tt.wantErr)
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out, want) {
					t.Errorf("output lacks %q; printed:\n%s", want, out)
				}
			}
			got := make([]string, 0, len(tofu.ran))
			for _, line := range tofu.ran {
				got = append(got, dataDirRE.ReplaceAllString(line, "DATA"))
			}
			if strings.Join(got, "\n") != strings.Join(tt.wantRan, "\n") {
				t.Errorf("ran:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(tt.wantRan, "\n"))
			}
			for _, key := range tt.wantRecords {
				if !strings.Contains(store.objects[key], `"by": "`) {
					t.Errorf("no record at %s; objects: %v", key, store.objects)
				}
				if tt.wantBy != "" && !strings.Contains(store.objects[key], tt.wantBy) {
					t.Errorf("record at %s = %s, want it to contain %s", key, store.objects[key], tt.wantBy)
				}
			}
		})
	}
}

// dataDirRE matches the run's data directory in a command line, which the test cannot
// know.
var dataDirRE = regexp.MustCompile(`/[^ ]*bedrock-[a-z-]*(teardown|destroy)-[^ /]*`)

// computerUser is the name the record falls back to: the process's user.
func computerUser() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}

	return nobody
}
