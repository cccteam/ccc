package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cccteam/ccc/bedrock/internal/deploy"
	"github.com/cccteam/ccc/bedrock/internal/org"
)

// orgTeardownStore is the lab organization's state bucket and records as org teardown
// reads them: 2-net routing the hosts or nothing, harbor's stacks in tst and prd (prd
// torn down already), beacon never applied.
func orgTeardownStore(t *testing.T, routed bool) *memoryStore {
	t.Helper()
	const bucket = "imp-boot-gbl-state-a1b2"
	state := func(app, env string) string {
		data, err := json.Marshal(map[string]any{"outputs": map[string]any{
			"net_hosts":     map[string]any{"value": map[string]string{app + "-" + env + ".impulseframework.dev": "projects/p/global/backendServices/imp-" + env + "-gbl-" + app + "-backend"}},
			"substitutions": map[string]any{"value": map[string]string{"_RECORDS_BUCKET": "imp-" + env + "-gbl-" + app + "-records", "_PROJECT": "imp-" + env + "-gbl-core", "_SERVICES": "uc1=x"}},
		}})
		if err != nil {
			t.Fatal(err)
		}

		return string(data)
	}
	hosts := map[string]string{}
	if routed {
		hosts["harbor-tst.impulseframework.dev"] = "projects/p/global/backendServices/imp-tst-gbl-harbor-backend"
		hosts["harbor.impulseframework.dev"] = "projects/p/global/backendServices/imp-prd-gbl-harbor-backend"
	}
	net, err := json.Marshal(map[string]any{"outputs": map[string]any{"hosts": map[string]any{"value": hosts}}})
	if err != nil {
		t.Fatal(err)
	}
	torn, err := json.Marshal(deploy.TeardownRecord{Kind: "teardown", StackID: "3c4d"})
	if err != nil {
		t.Fatal(err)
	}

	return &memoryStore{objects: map[string]string{
		"gs://" + bucket + "/2-net/default.tfstate":                       string(net),
		"gs://" + bucket + "/3-app/harbor/tst/default.tfstate":            state("harbor", "tst"),
		"gs://" + bucket + "/3-app/harbor/prd/default.tfstate":            state("harbor", "prd"),
		"gs://imp-tst-gbl-harbor-records/harbor/stacks/tst/stack.json":    `{"id": "1a2b"}`,
		"gs://imp-prd-gbl-harbor-records/harbor/stacks/prd/stack.json":    `{"id": "3c4d"}`,
		"gs://imp-prd-gbl-harbor-records/harbor/stacks/prd/teardown.json": string(torn),
	}}
}

// withApplicationLayers gives the organization repository its applications' stacks
// under 3-app, each with the lab placement naming the organization's state bucket.
func withApplicationLayers(t *testing.T, dir string) {
	t.Helper()
	data, err := os.ReadFile(placement)
	if err != nil {
		t.Fatal(err)
	}
	for _, app := range []string{"harbor", "beacon"} {
		layer := filepath.Join(dir, "3-app", app)
		if err := os.MkdirAll(layer, 0o750); err != nil {
			t.Fatal(err)
		}
		text := strings.Replace(string(data), "imp-boot-gbl-state-REPLACEME", "imp-boot-gbl-state-a1b2", 1)
		if err := os.WriteFile(filepath.Join(layer, placementFile), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(layer, "terraform.tfvars"), []byte("state_bucket = \"imp-boot-gbl-state-a1b2\"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestOrgTeardown(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		tornDown    bool
		routed      bool
		layers      bool
		interactive func() bool
		in          string
		args        []string
		wantErr     string
		wantOut     []string
		wantRan     []string
		wantJournal []string
		wantFlag    bool
	}{
		{
			name:        "the first run refuses without a terminal and without --yes",
			interactive: never,
			wantErr:     "bedrock org teardown asks for imp typed back at the terminal, and this run has none: --yes stands in for it",
			wantOut:     []string{"bedrock org teardown takes the imp organization's applications out of service.", "tears every registered application (harbor, beacon) down to what it keeps"},
		},
		{
			name:        "the first run flips the placement, renders 2-net torn down and writes the journal",
			interactive: always,
			in:          "imp\n",
			wantOut:     []string{"Type imp to go on; anything else stops:", "Recorded teardown in", "2-net's applications.auto.tfvars now routes no host and sets serving = false.", "Next: commit placement.json, JOURNAL.md and the rendered files, open the pull request and merge it"},
			wantJournal: []string{"bedrock org teardown: the hosts and the load balancer's listeners leave 2-net on this pull request (teardown in placement.json)"},
			wantFlag:    true,
		},
		{
			name:        "the second run refuses while 2-net still routes",
			tornDown:    true,
			routed:      true,
			interactive: never,
			args:        []string{"--yes"},
			wantErr:     "2-net still routes 2 host(s) (harbor-tst.impulseframework.dev, harbor.impulseframework.dev): the pull request that takes them out is not applied yet; merge it, let the apply trigger apply 2-net, then run bedrock org teardown again",
		},
		{
			name:        "the second run accounts for applications in their own repositories",
			tornDown:    true,
			interactive: never,
			args:        []string{"--yes"},
			wantOut:     []string{"harbor tst: stands.", "harbor stg: no stack.", "harbor prd: torn down.", "From harbor's checkout: bedrock teardown --all", "beacon tst: no stack.", "1 environment(s) stand: run the command above from each application's checkout, then bedrock org teardown again."},
		},
		{
			name:        "the second run tears the applications under 3-app down, with the prefix typed",
			tornDown:    true,
			layers:      true,
			interactive: always,
			in:          "imp\n",
			wantOut:     []string{"harbor:\ntst: 2 resource(s) destroyed, 1 kept.", `protection lifted first: google_cloud_run_v2_service.app["uc1"]`, "beacon: nothing stands.", "Type imp to go on; anything else stops:", "Tore harbor's stack in tst down: 2 resource(s) destroyed, 1 kept", "The imp organization is torn down: its applications (harbor, beacon) keep their data, identities and records, and nothing serves"},
			wantRan: []string{
				"tofu init -input=false -no-color -reconfigure -backend-config=prefix=3-app/harbor/tst", "tofu state list",
				"tofu init -input=false -no-color -reconfigure -backend-config=prefix=3-app/harbor/prd", "tofu state list",
				"tofu plan -destroy -input=false -no-color -out=DATA/tst/teardown.tfplan -var environment=tst -var database_generation=1 -var teardown=true -exclude=google_spanner_database.quill[0]",
				"tofu show -json DATA/tst/teardown.tfplan", "tofu apply -input=false -no-color DATA/tst/teardown.tfplan",
				`tofu apply -input=false -no-color -auto-approve -var environment=prd -var database_generation=1 -var teardown=true -target=google_cloud_run_v2_service.app["uc1"]`,
				"tofu plan -destroy -input=false -no-color -out=DATA/prd/teardown.tfplan -var environment=prd -var database_generation=1 -var teardown=true -exclude=google_spanner_database.quill[0]",
				"tofu show -json DATA/prd/teardown.tfplan", "tofu apply -input=false -no-color DATA/prd/teardown.tfplan",
			},
			wantJournal: []string{"bedrock org teardown: every application torn down to what it keeps (harbor, beacon); 2-net routes nothing."},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := orgRepo(t)
			if tt.tornDown {
				editPlacement(t, dir, func(p *org.Placement) { p.Teardown = true })
			}
			if tt.layers {
				withApplicationLayers(t, dir)
			}
			store := orgTeardownStore(t, tt.routed)
			tofu := &teardownTofu{}
			d := orgDeps(dir)
			d.interactive = tt.interactive
			d.deploy = &deploy.Clients{Storage: store.open, Exec: tofu}
			out, err := execute(d, tt.in, append([]string{"org", "teardown", "--dir", dir}, tt.args...)...)
			switch {
			case tt.wantErr == "" && err != nil:
				t.Fatalf("org teardown = %v, want no error; printed:\n%s", err, out)
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Fatalf("org teardown = %v, want an error containing %q; printed:\n%s", err, tt.wantErr, out)
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out, want) {
					t.Errorf("output lacks %q; printed:\n%s", want, out)
				}
			}
			if tt.wantRan != nil {
				got := make([]string, 0, len(tofu.ran))
				for _, line := range tofu.ran {
					got = append(got, dataDirRE.ReplaceAllString(line, "DATA"))
				}
				if strings.Join(got, "\n") != strings.Join(tt.wantRan, "\n") {
					t.Errorf("ran:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(tt.wantRan, "\n"))
				}
			}
			journal, _ := os.ReadFile(filepath.Join(dir, "JOURNAL.md"))
			for _, want := range tt.wantJournal {
				if !strings.Contains(string(journal), want) {
					t.Errorf("the journal lacks %q:\n%s", want, journal)
				}
			}
			if tt.wantJournal == nil && len(journal) > 0 {
				t.Errorf("the journal was written:\n%s", journal)
			}
			p, err := org.ReadPlacement(filepath.Join(dir, placementFile))
			if err != nil {
				t.Fatal(err)
			}
			if tt.wantFlag && !p.Teardown {
				t.Error("the placement does not say teardown")
			}
			if tt.wantFlag {
				tfvars, err := os.ReadFile(filepath.Join(dir, "2-net", "applications.auto.tfvars"))
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(tfvars), "serving = false") {
					t.Errorf("2-net was not rendered torn down:\n%s", tfvars)
				}
			}
		})
	}
}
