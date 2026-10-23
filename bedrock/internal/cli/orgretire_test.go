package cli

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cccteam/ccc/bedrock/internal/deploy"
	"github.com/cccteam/ccc/bedrock/internal/github"
	"github.com/cccteam/ccc/bedrock/internal/github/githubtest"
	"github.com/cccteam/ccc/bedrock/internal/org"
)

// retireTofu is tofu in org retire's tests: it records every command and answers 1-org's
// state listing with what the organization holds of two applications.
type retireTofu struct {
	ran   []string
	state []string
}

func (t *retireTofu) Run(_ context.Context, c deploy.Command, _ io.Writer) error {
	t.ran = append(t.ran, c.String())

	return nil
}

func (t *retireTofu) Output(_ context.Context, c deploy.Command, _ io.Writer) ([]byte, error) {
	t.ran = append(t.ran, c.String())
	if len(c.Args) >= 2 && c.Args[0] == "state" && c.Args[1] == "list" {
		return []byte(strings.Join(t.state, "\n") + "\n"), nil
	}

	return nil, nil
}

// retireStore is the state bucket as org retire reads it: 2-net routing the hosts named,
// and harbor's stack standing in the environments named (an applied state with outputs),
// nothing of beacon.
func retireStore(t *testing.T, routed, standing []string) *memoryStore {
	t.Helper()
	const bucket = "imp-boot-gbl-state-a1b2"
	hosts := map[string]string{}
	for _, host := range routed {
		hosts[host] = "projects/p/global/backendServices/imp-tst-gbl-harbor-backend"
	}
	net, err := json.Marshal(map[string]any{"outputs": map[string]any{"hosts": map[string]any{"value": hosts}}})
	if err != nil {
		t.Fatal(err)
	}
	objects := map[string]string{"gs://" + bucket + "/2-net/default.tfstate": string(net)}
	for _, env := range standing {
		state, err := json.Marshal(map[string]any{"outputs": map[string]any{
			"substitutions": map[string]any{"value": map[string]string{"_RECORDS_BUCKET": "imp-" + env + "-gbl-harbor-records"}},
		}})
		if err != nil {
			t.Fatal(err)
		}
		objects["gs://"+bucket+"/3-app/harbor/"+env+"/default.tfstate"] = string(state)
	}

	return &memoryStore{objects: objects}
}

func TestOrgRetire(t *testing.T) {
	t.Parallel()

	state := []string{
		`github_repository.app["beacon"]`,
		`github_repository.app["harbor"]`,
		`github_repository_ruleset.release_tags["harbor"]`,
		`github_repository_ruleset.branch["harbor:default"]`,
		`github_repository_environment.operations["harbor-tst"]`,
		`google_service_account.apply["harbor"]`,
	}
	tests := []struct {
		name         string
		outOfService map[string][]string
		teardown     bool
		routed       []string
		standing     []string
		args         []string
		wantErr      string
		wantOut      []string
		wantRan      []string
		wantArchived bool
		wantRetired  bool
	}{
		{
			name:    "an application the placement does not register is refused",
			args:    []string{"quill", "--yes"},
			wantErr: "quill is not registered (applications in placement.json): nothing to retire",
		},
		{
			name:         "an application still in service somewhere is refused, naming the environments",
			outOfService: map[string][]string{"harbor": {"tst"}},
			args:         []string{"harbor", "--yes"},
			wantErr:      "harbor is in service in stg, prd (outOfService in placement.json): run bedrock org teardown harbor first",
		},
		{
			name:         "an application 2-net still routes is refused: the teardown's pull request is not applied",
			outOfService: map[string][]string{"harbor": {"tst", "stg", "prd"}},
			routed:       []string{"harbor-tst.apps.imp.example"},
			args:         []string{"harbor", "--yes"},
			wantErr:      "2-net still routes harbor's harbor-tst.apps.imp.example: the pull request bedrock org teardown harbor left is not applied yet",
		},
		{
			name:         "an application whose stack stands is refused: the destroy comes first",
			outOfService: map[string][]string{"harbor": {"tst", "stg", "prd"}},
			standing:     []string{"tst", "prd"},
			args:         []string{"harbor", "--yes"},
			wantErr:      "harbor's stack stands in tst, prd: run bedrock destroy --all from its checkout first, which takes the final backups and the data down, then retire",
		},
		{
			name:         "the run refuses without a terminal and without --yes, after the checks",
			outOfService: map[string][]string{"harbor": {"tst", "stg", "prd"}},
			args:         []string{"harbor"},
			wantErr:      "bedrock org retire harbor asks for harbor typed back at the terminal, and this run has none: --yes stands in for it",
			wantOut:      []string{"harbor: out of service everywhere, 2-net routes nothing to it, and no stack stands in tst, stg, prd.", "bedrock org retire harbor takes harbor out of the imp organization. Its repository imp-example/harbor is archived on GitHub"},
		},
		{
			name:         "the run archives the repository, takes it and its settings out of 1-org's state, removes the entry and renders",
			outOfService: map[string][]string{"harbor": {"tst", "stg", "prd"}},
			args:         []string{"harbor", "--yes"},
			wantOut: []string{
				"Archived imp-example/harbor: read-only on GitHub, with its code, pull requests and releases.",
				`Took github_repository.app["harbor"], github_repository_ruleset.release_tags["harbor"], github_repository_ruleset.branch["harbor:default"], github_repository_environment.operations["harbor-tst"] out of 1-org's state: the repository and its settings stay on GitHub as they are.`,
				"Removed harbor from",
				"the layers' applications.auto.tfvars name harbor no more.",
				"Next: commit placement.json, JOURNAL.md and the rendered files, open the pull request and merge it; the plan run shows harbor's identities, grants and registry entries destroyed and nothing on GitHub",
			},
			wantRan: []string{
				"tofu init -input=false -no-color -reconfigure",
				"tofu state list",
				`tofu state rm github_repository.app["harbor"]`,
				`tofu state rm github_repository_ruleset.release_tags["harbor"]`,
				`tofu state rm github_repository_ruleset.branch["harbor:default"]`,
				`tofu state rm github_repository_environment.operations["harbor-tst"]`,
			},
			wantArchived: true,
			wantRetired:  true,
		},
		{
			name:         "a torn-down organization needs no out-of-service entry",
			teardown:     true,
			args:         []string{"harbor", "--yes"},
			wantOut:      []string{"Archived imp-example/harbor"},
			wantArchived: true,
			wantRetired:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := orgRepo(t)
			editPlacement(t, dir, func(p *org.Placement) {
				p.OutOfService = tt.outOfService
				p.Teardown = tt.teardown
			})
			server := githubtest.New(t)
			server.Repos["imp-example/harbor"] = &githubtest.Repo{}
			tofu := &retireTofu{state: state}
			d := orgDeps(dir)
			d.deploy = &deploy.Clients{Storage: retireStore(t, tt.routed, tt.standing).open, Exec: tofu}
			d.github = func(context.Context) (*github.Client, error) { return server.Client(), nil }
			d.githubToken = func(context.Context) (string, error) { return "test-token", nil }
			out, err := execute(d, "", append([]string{"org", "retire", "--dir", dir}, tt.args...)...)
			switch {
			case tt.wantErr == "" && err != nil:
				t.Fatalf("org retire = %v, want no error; printed:\n%s", err, out)
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Fatalf("org retire = %v, want an error containing %q; printed:\n%s", err, tt.wantErr, out)
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out, want) {
					t.Errorf("output lacks %q; printed:\n%s", want, out)
				}
			}
			if tt.wantRan != nil && strings.Join(tofu.ran, "\n") != strings.Join(tt.wantRan, "\n") {
				t.Errorf("ran:\n%s\nwant:\n%s", strings.Join(tofu.ran, "\n"), strings.Join(tt.wantRan, "\n"))
			}
			if server.Repos["imp-example/harbor"].Archived != tt.wantArchived {
				t.Errorf("archived = %v, want %v", server.Repos["imp-example/harbor"].Archived, tt.wantArchived)
			}
			p, err := org.ReadPlacement(filepath.Join(dir, placementFile))
			if err != nil {
				t.Fatal(err)
			}
			if p.Registered("harbor") == tt.wantRetired {
				t.Errorf("harbor registered = %v, want retired = %v", p.Registered("harbor"), tt.wantRetired)
			}
			journal, _ := os.ReadFile(filepath.Join(dir, "JOURNAL.md"))
			if tt.wantRetired {
				if !strings.Contains(string(journal), "bedrock org retire harbor: imp-example/harbor archived and out of 1-org's state with its settings; harbor leaves placement.json on this pull request") {
					t.Errorf("the journal lacks the retire line:\n%s", journal)
				}
				tfvars, err := os.ReadFile(filepath.Join(dir, "1-org", "applications.auto.tfvars"))
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(tfvars), "harbor") {
					t.Errorf("1-org still names harbor:\n%s", tfvars)
				}
			} else if len(journal) > 0 {
				t.Errorf("the journal was written:\n%s", journal)
			}
		})
	}
}

func TestRetireNotice(t *testing.T) {
	t.Parallel()

	p := &org.Placement{Applications: []org.Application{{Code: "harbor", Repository: "harbor"}, {Code: "beacon", Repository: "beacon"}}, OutOfService: map[string][]string{"harbor": {"tst", "stg", "prd"}, "beacon": {"tst"}}}
	got := retireNotice(p)
	want := "harbor is out of service in every environment (outOfService in placement.json): once bedrock destroy --all has taken its stacks down from its checkout, bedrock org retire harbor archives its repository, takes it out of 1-org's state and removes the entry; until then the entry stays, since 1-org never deletes a repository (prevent_destroy).\n"
	if got != want {
		t.Errorf("retireNotice() =\n%s\nwant:\n%s", got, want)
	}
	if retireNotice(&org.Placement{}) != "" {
		t.Error("an organization with no application out of service got a notice")
	}
}
