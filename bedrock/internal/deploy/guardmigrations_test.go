package deploy

import (
	"strings"
	"testing"

	"github.com/cccteam/ccc/bedrock/internal/github"
	"github.com/cccteam/ccc/bedrock/internal/github/githubtest"
)

// migrationsRepo is the repository the guard reads: the pull request's commit c9
// branched from c3, whose tree holds 000001; master has moved on to c4, which added
// 000002.
func migrationsRepo() *githubtest.Repo {
	return &githubtest.Repo{
		Refs:      map[string]github.Object{"refs/heads/master": {Type: "commit", SHA: "c4"}},
		Ancestry:  map[string][]string{"c4": {"c4", "c3"}, "c3": {"c3"}, "c9": {"c9", "c3"}},
		MergeBase: map[string]string{"c4 c9": "c3"},
		Trees:     map[string]string{"c3": "t3", "c4": "t4"},
		Files: map[string]string{
			"t3:schema/migrations/000001_Init.up.sql":   "create a",
			"t3:schema/devseed/000001_Seed.up.sql":      "insert a",
			"t4:schema/migrations/000001_Init.up.sql":   "create a",
			"t4:schema/migrations/000002_Master.up.sql": "create b",
			"t4:schema/devseed/000001_Seed.up.sql":      "insert a",
		},
	}
}

func TestGuardMigrations(t *testing.T) {
	t.Parallel()

	const env = "export GITHUB_TOKEN=\"test-token\"\nexport SKIP_DEPLOY=\"\"\nexport DOWN=\"\"\n"
	tagSubs := map[string]string{migrationsSub: "schema/migrations", tagSub: "v1.2.3", defaultBranchSub: "master", commitSub: "c9", repoFullNameSub: "acme/quill"}
	prSubs := func() map[string]string {
		return map[string]string{migrationsSub: "schema/migrations", prNumberSub: "7", defaultBranchSub: "master", commitSub: "c9", repoFullNameSub: "acme/quill"}
	}
	tests := []struct {
		name  string
		env   string
		subs  map[string]string
		files map[string]string
		// deployer registers the deployer app's key, so a refusal is posted as the app.
		deployer    bool
		wantOut     []string
		wantErr     string
		wantComment string
		wantCalls   []string
	}{
		{
			name:    "a torn-down environment has nothing to guard",
			env:     "export SKIP_DEPLOY=\"true\"\n",
			subs:    tagSubs,
			wantOut: []string{tornDown},
		},
		{
			name:    "a teardown has nothing to check",
			env:     "export DOWN=\"true\"\n",
			subs:    tagSubs,
			wantOut: []string{"Teardown: nothing to check."},
		},
		{
			name:    "an application without a schema has nothing to check",
			env:     env,
			subs:    map[string]string{tagSub: "v1.2.3"},
			wantOut: []string{"No schema migrations: nothing to check."},
		},
		{
			name:  "a tag build's sound sequence passes",
			env:   env,
			subs:  tagSubs,
			files: map[string]string{"schema/migrations/000001_Init.up.sql": "create a", "schema/migrations/000002_Next.up.sql": "create b", "schema/migrations/000002_Next.down.sql": "drop b", "schema/migrations/README.md": "not the rule's"},
			wantOut: []string{
				"schema/migrations: 2 migration(s), 000001 to 000002.",
				"Guard passed: the migrations form one sequence.",
			},
		},
		{
			name:    "a tag build's gap is refused",
			env:     env,
			subs:    tagSubs,
			files:   map[string]string{"schema/migrations/000001_Init.up.sql": "create a", "schema/migrations/000003_Later.up.sql": "create c", "schema/migrations/notes.sql": "select 1"},
			wantOut: []string{"schema/migrations/notes.sql: not a migration file name", "schema/migrations: gap: no migration 000002 between 000001 and 000003"},
			wantErr: "Build REJECTED: the migrations are not one sequence",
		},
		{
			name:  "a pull request's own migration after the default branch's passes, read together with it",
			env:   env,
			subs:  prSubs(),
			files: map[string]string{"schema/migrations/000001_Init.up.sql": "create a", "schema/migrations/000003_Mine.up.sql": "create c", "schema/devseed/000001_Seed.up.sql": "insert a"},
			wantOut: []string{
				"schema/migrations: 3 migration(s), 000001 to 000003, read together with master.",
				"schema/devseed: 1 migration(s), 000001 to 000001, read together with master.",
				"Guard passed: the migrations form one sequence, unchanged against master.",
			},
		},
		{
			name:        "an index the default branch took is refused and posted with the repository's token",
			env:         env,
			subs:        prSubs(),
			files:       map[string]string{"schema/migrations/000001_Init.up.sql": "create a", "schema/migrations/000002_Mine.up.sql": "create c"},
			wantOut:     []string{"schema/migrations/000002_Mine.up.sql: index 000002 has 2 up files", "schema/migrations/000002_Master.up.sql (on master, not in this pull request): index 000002 has 2 up files", "bedrock migration renumber on the branch", "Posted on pull request 7."},
			wantErr:     "Build REJECTED",
			wantComment: "`bedrock migration renumber` on the branch",
		},
		{
			name:        "a modified committed migration is refused, posted as the deployer app",
			env:         env,
			subs:        deployerSubs(prSubs()),
			files:       map[string]string{"schema/migrations/000001_Init.up.sql": "create a, edited", "schema/migrations/000003_Mine.up.sql": "create c"},
			deployer:    true,
			wantOut:     []string{"schema/migrations/000001_Init.up.sql: modified; a committed migration never changes, a new one follows it"},
			wantErr:     "Build REJECTED",
			wantComment: "000001_Init.up.sql: modified",
			wantCalls:   []string{"GET /repos/acme/quill/installation", "POST /app/installations/77/access_tokens"},
		},
		{
			name:        "a removed committed migration and a changed seed are refused, the seed's fix recreating the database",
			env:         env,
			subs:        prSubs(),
			files:       map[string]string{"schema/migrations/000003_Mine.up.sql": "create c", "schema/devseed/000001_Seed.up.sql": "insert b"},
			wantOut:     []string{"schema/migrations/000001_Init.up.sql: removed or renamed", "schema/devseed/000001_Seed.up.sql: modified; a committed migration never changes, a new one follows it, and /gcbrun reload-db recreates"},
			wantErr:     "Build REJECTED",
			wantComment: "removed or renamed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			files := map[string]string{EnvironmentFile: tt.env, BuildFile: buildFor(t, tt.subs)}
			for name, content := range tt.files {
				files[name] = content
			}
			w := workspaceFiles(t, files)
			repo := migrationsRepo()
			srv, gh := githubStandIn(t, repo)
			clients := &Clients{GitHub: gh, Secrets: (&fakeSecrets{}).open}
			if tt.deployer {
				clients.Secrets = deployerSecrets(t).open
			}
			var out strings.Builder
			err := GuardMigrations(t.Context(), clients, w, &out)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("GuardMigrations() error = %v, want %q\n%s", err, tt.wantErr, out.String())
				}
			} else if err != nil {
				t.Fatalf("GuardMigrations() error = %v\n%s", err, out.String())
			}
			containsAll(t, out.String(), tt.wantOut...)
			comments := repo.Comments[7]
			if tt.wantComment == "" && len(comments) > 0 {
				t.Errorf("posted %q, want nothing", comments[0].Body)
			}
			if tt.wantComment != "" && (len(comments) != 1 || !strings.Contains(comments[0].Body, tt.wantComment)) {
				t.Errorf("comments = %+v, want one holding %q", comments, tt.wantComment)
			}
			containsAll(t, strings.Join(srv.Calls, "\n"), tt.wantCalls...)
		})
	}
}
