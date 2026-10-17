package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cccteam/ccc/bedrock/internal/deploy"
	"github.com/cccteam/ccc/bedrock/internal/github"
	"github.com/cccteam/ccc/bedrock/internal/github/githubtest"
)

// statusStore is the state bucket with harbor's stack applied in every environment,
// each state naming its records bucket, and each records bucket holding the stack's id
// and the live record of the release the environment runs.
func statusStore(t *testing.T, live map[string]string) *memoryStore {
	t.Helper()
	objects := map[string]string{}
	for env, release := range live {
		state, err := json.Marshal(map[string]any{"outputs": map[string]any{
			"substitutions": map[string]any{"value": map[string]string{"_RECORDS_BUCKET": "imp-" + env + "-gbl-harbor-records", "_PROJECT": "imp-" + env + "-gbl-core"}},
		}})
		if err != nil {
			t.Fatal(err)
		}
		objects["gs://imp-boot-gbl-state-REPLACEME/3-app/harbor/"+env+"/default.tfstate"] = string(state)
		objects["gs://imp-"+env+"-gbl-harbor-records/harbor/stacks/"+env+"/stack.json"] = `{"id": "1a2b"}`
		if release != "" {
			objects["gs://imp-"+env+"-gbl-harbor-records/harbor/"+env+"/1a2b/releases/"+release+"/b1.json"] = `{"app": "harbor", "env": "` + env + `", "version": "` + release + `", "status": "live", "timestamp": "2026-10-15T20:00:00Z", "build": "b1"}`
		}
	}

	return &memoryStore{objects: objects}
}

func TestPatchStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		live map[string]string
		// window is written into the placement as maintenanceWindow when not empty.
		window  string
		prepare func(repo *githubtest.Repo)
		wantOut []string
		wantErr string
	}{
		{
			name:   "the same release live everywhere, the line at its newest release, nothing owed: ready",
			live:   map[string]string{"tst": "v0.1.4", "stg": "v0.1.4", "prd": "v0.1.4"},
			window: "Tuesdays 02:00-04:00 UTC",
			prepare: func(repo *githubtest.Repo) {
				repo.Refs["refs/heads/patch/0.1.x"] = github.Object{Type: "commit", SHA: "c4"}
			},
			wantOut: []string{
				"Maintenance window: Tuesdays 02:00-04:00 UTC (placement.json maintenanceWindow; the team's to act on, bedrock acts on it nowhere).",
				"tst: v0.1.4 live since 2026-10-15T20:00:00Z (build b1).",
				"prd: v0.1.4 live since 2026-10-15T20:00:00Z (build b1).",
				"Line: patch/0.1.x at c4, which is its newest release v0.1.4; nothing pending.",
				"Merge-backs: none owed; master carries every release.",
				"Ready for a patch release: yes.",
			},
		},
		{
			name: "no line yet, and no window declared",
			live: map[string]string{"tst": "v0.1.4", "stg": "v0.1.4", "prd": "v0.1.4"},
			wantOut: []string{
				"Maintenance window: none declared (placement.json maintenanceWindow).",
				"Line: patch/0.1.x does not exist yet; bedrock patch start v0.1.4 makes it at v0.1.4's commit.",
				"Ready for a patch release: yes.",
			},
		},
		{
			name: "staging behind, a patch release pending on the line and its release not merged back: not ready, each reason named",
			live: map[string]string{"tst": "v0.1.5", "stg": "v0.1.4", "prd": "v0.1.4"},
			prepare: func(repo *githubtest.Repo) {
				repo.Refs["refs/heads/patch/0.1.x"] = github.Object{Type: "commit", SHA: "c6"}
				repo.Refs["refs/tags/v0.1.5"] = github.Object{Type: "commit", SHA: "c5"}
				repo.Ancestry["c6"] = []string{"c6", "c5", "c4"}
				repo.PullRequests = []github.PullRequest{{Number: 9, State: "open", HTMLURL: "https://github.com/impulseframework/harbor/pull/9", Head: github.PullRequestRef{Ref: "merge-back/v0.1.5"}}}
			},
			wantOut: []string{
				"stg: v0.1.4 live since",
				"Line: patch/0.1.x at c6, past its newest release v0.1.5 (c5): a patch release is pending (a release pull request in flight, or changes no release carries yet).",
				"Merge-back owed: v0.1.5 (patch/0.1.x) has pull request #9 from merge-back/v0.1.5 open, not merged: https://github.com/impulseframework/harbor/pull/9",
				"Ready for a patch release: no; the same release is not live in every environment, a patch release is pending on patch/0.1.x, v0.1.5 is not merged back.",
			},
		},
		{
			name: "production runs nothing live: no line to read; a merge-back owed is still named",
			live: map[string]string{"tst": "v0.1.5", "stg": "", "prd": ""},
			prepare: func(repo *githubtest.Repo) {
				repo.Refs["refs/tags/v0.1.5"] = github.Object{Type: "commit", SHA: "c5"}
			},
			wantOut: []string{
				"stg: nothing live.",
				"Production (prd) runs nothing live, so there is no line to read yet.",
				"Merge-back owed: v0.1.5 (patch/0.1.x) has no pull request from merge-back/v0.1.5; bedrock patch merge v0.1.5 opens it.",
				"Ready for a patch release: no; the same release is not live in every environment, v0.1.5 is not merged back.",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server, repo := labGitHub(t)
			if tt.prepare != nil {
				tt.prepare(repo)
			}
			dir := harborRepo(t, "git@github.com:impulseframework/harbor.git")
			file := filepath.Join(dir, "infrastructure", placementFile)
			data, err := os.ReadFile(placement)
			if err != nil {
				t.Fatal(err)
			}
			if tt.window != "" {
				var p map[string]any
				if err := json.Unmarshal(data, &p); err != nil {
					t.Fatal(err)
				}
				p["maintenanceWindow"] = tt.window
				if data, err = json.Marshal(p); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(file, data, 0o600); err != nil {
				t.Fatal(err)
			}
			store := statusStore(t, tt.live)
			d := deps{
				domains: noCloudDomains, secrets: noSecretManager, projects: noProjects, cwd: dir, interactive: never,
				github: func(context.Context) (*github.Client, error) {
					return server.Client(), nil
				},
				deploy: &deploy.Clients{Storage: store.open},
			}
			out, err := execute(d, "", "patch", "status", "--placement", file)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Execute() error = %v, wantErr %q; output:\n%s", err, tt.wantErr, out)
				}

				return
			}
			if err != nil {
				t.Fatalf("Execute() error = %v; output:\n%s", err, out)
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out, want) {
					t.Errorf("output lacks %q; printed:\n%s", want, out)
				}
			}
		})
	}
}

// TestPatchAlias: bedrock hotfix still runs the patch command group.
func TestPatchAlias(t *testing.T) {
	t.Parallel()

	code, out := run(t, "hotfix", "--help")
	if code != 0 || !strings.Contains(out, "patch is the new name of bedrock hotfix, which still runs it") || !strings.Contains(out, "status") {
		t.Errorf("bedrock hotfix --help exit = %d, output:\n%s", code, out)
	}
}
