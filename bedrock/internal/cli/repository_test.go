package cli

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cccteam/ccc/bedrock/internal/github"
	"github.com/cccteam/ccc/bedrock/internal/github/githubtest"
)

// harborRepo is a copy of the harbor fixture marked as a repository whose origin is
// impulseframework/harbor on GitHub (no origin when remote is empty).
func harborRepo(t *testing.T, remote string) string {
	t.Helper()

	dir := copyRepo(t, fixtureApp)
	config := "[core]\n\tbare = false\n"
	if remote != "" {
		config += "[remote \"origin\"]\n\turl = " + remote + "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "config"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	return dir
}

// labGitHub is the stand-in GitHub with harbor at v0.1.4 (c4) on master, plus what the
// test adds.
func labGitHub(t *testing.T) (*githubtest.Server, *githubtest.Repo) {
	t.Helper()

	server := githubtest.New(t)
	repo := server.AddRepo("impulseframework", "harbor", &githubtest.Repo{
		Refs:     map[string]github.Object{"refs/heads/master": {Type: "commit", SHA: "c4"}, "refs/tags/v0.1.4": {Type: "commit", SHA: "c4"}},
		Ancestry: map[string][]string{"c4": {"c4"}, "c5": {"c5", "c4"}},
		Trees:    map[string]string{"c4": "tree4"},
		Files:    map[string]string{"tree4:.release-please-manifest.json": "{\n  \".\": \"0.1.4\"\n}\n"},
	})

	return server, repo
}

func TestRestoreAndHotfix(t *testing.T) {
	t.Parallel()

	placement := filepath.Join(fixtureApp, "..", "placement.json")
	tests := []struct {
		name string
		// remote is the origin URL written into the copy; empty for none.
		remote string
		// prepare adjusts the stand-in before the run.
		prepare func(server *githubtest.Server, repo *githubtest.Repo)
		args    []string
		// in is what the person types: a rollback asks for the environment's name.
		in      string
		wantOut []string
		// wantDispatch is the workflow_dispatch event the run leaves: file, ref and inputs.
		wantDispatch string
		wantErr      string
	}{
		{
			name:   "restore dispatches the operations workflow for a wired environment",
			remote: "git@github.com:impulseframework/harbor.git",
			args:   []string{"restore", "tst", "v0.1.4", "--placement", placement},
			wantOut: []string{
				"Asked, as octocat, for tst to be restored to v0.1.4: the operations workflow of impulseframework/harbor runs it (https://github.com/impulseframework/harbor/actions/workflows/operations.yml). The run replaces tst's database (empty), deploys v0.1.4, and its record names you.",
			},
			wantDispatch: "operations.yml master action=restore environment=tst release=v0.1.4",
		},
		{
			name:   "restore with no release in an environment restored from production's backup leaves the release to the workflow, which reads production's live release",
			remote: "git@github.com:impulseframework/harbor.git",
			args:   []string{"restore", "stg", "--placement", placement},
			wantOut: []string{
				"Asked, as octocat, for stg to be restored to production's live release: the operations workflow of impulseframework/harbor runs it (https://github.com/impulseframework/harbor/actions/workflows/operations.yml). Its job reads the release from production's deployment records and says which; the run replaces stg's database (production-backup), deploys that release, and its record names you.",
			},
			wantDispatch: "operations.yml master action=restore environment=stg release=",
		},
		{
			name:   "restore with a release named in an environment restored from production's backup says the workflow tells whether it is production's",
			remote: "git@github.com:impulseframework/harbor.git",
			args:   []string{"restore", "stg", "v0.1.4", "--placement", placement},
			wantOut: []string{
				"Asked, as octocat, for stg to be restored to v0.1.4: the operations workflow of impulseframework/harbor runs it (https://github.com/impulseframework/harbor/actions/workflows/operations.yml). Its job says whether v0.1.4 is production's live release, the one a restore from production's backup returns to when none is named; the run replaces stg's database (production-backup), deploys v0.1.4, and its record names you.",
			},
			wantDispatch: "operations.yml master action=restore environment=stg release=v0.1.4",
		},
		{
			name:    "restore with no release in the first environment is refused: an empty database has no production state to return to",
			remote:  "git@github.com:impulseframework/harbor.git",
			args:    []string{"restore", "tst", "--placement", placement},
			wantErr: "tst restores to an empty database (empty), which has no production state to return to: name the release to run",
		},
		{
			name:   "rollback prints the statement, asks for the environment's name and dispatches the workflow with the reason and the release left to it",
			remote: "git@github.com:impulseframework/harbor.git",
			args:   []string{"rollback", "prd", "--reason", "v0.1.4 mangled the invoices", "--placement", placement},
			in:     "prd\n",
			wantOut: []string{
				"=== ROLLBACK of prd: harbor returns to the release live before the live one, asked for by you: v0.1.4 mangled the invoices ===",
				"Nothing of the database: it stays as the live release left it, and the earlier release runs on it. No migration runs, no backup is taken, nothing is restored; the earlier release's build runs again and deploys as a release does. The database is returned by bedrock restore, in a run of its own.",
				"Type the environment's name (prd) to ask for it, anything else to stop:",
				"Asked, as octocat, for prd to return to the release live before the live one: the operations workflow of impulseframework/harbor runs it (https://github.com/impulseframework/harbor/actions/workflows/operations.yml). Its job reads the environment's records for what is live and what it returns to, prints the statement, runs the rollback trigger and waits for the approval in Cloud Build, thirty minutes at most; the record names you and the reason.",
			},
			wantDispatch: "operations.yml master action=rollback environment=prd reason=v0.1.4 mangled the invoices release=",
		},
		{
			name:   "rollback --to names the release to return to, which must exist",
			remote: "git@github.com:impulseframework/harbor.git",
			args:   []string{"rollback", "prd", "--to", "v0.1.4", "--reason", "back to the known good", "--placement", placement},
			in:     "prd\n",
			wantOut: []string{
				"=== ROLLBACK of prd: harbor returns to v0.1.4, asked for by you: back to the known good ===",
			},
			wantDispatch: "operations.yml master action=rollback environment=prd reason=back to the known good release=v0.1.4",
		},
		{
			name:   "rollback reaches an environment whose release builds keep no release backup: it needs none",
			remote: "git@github.com:impulseframework/harbor.git",
			args:   []string{"rollback", "stg", "--reason", "why", "--placement", placement},
			in:     "stg\n",
			wantOut: []string{
				"=== ROLLBACK of stg: harbor returns to the release live before the live one, asked for by you: why ===",
			},
			wantDispatch: "operations.yml master action=rollback environment=stg reason=why release=",
		},
		{
			name:    "rollback is not asked for when the name typed differs",
			remote:  "git@github.com:impulseframework/harbor.git",
			args:    []string{"rollback", "prd", "--reason", "why", "--placement", placement},
			in:      "no\n",
			wantErr: `"no" is not prd: the rollback was not asked for, and nothing changed`,
		},
		{
			name:    "rollback without a reason is refused",
			remote:  "git@github.com:impulseframework/harbor.git",
			args:    []string{"rollback", "prd", "--placement", placement},
			wantErr: "a rollback says why it was asked for: --reason, in a sentence",
		},
		{
			name:    "rollback refuses a reason carrying a | character",
			remote:  "git@github.com:impulseframework/harbor.git",
			args:    []string{"rollback", "prd", "--reason", "a | b", "--placement", placement},
			wantErr: "the reason carries a | character, which the build's substitutions cannot",
		},
		{
			name:   "restore --before prints the statement, asks for the environment's name and dispatches the release's cut, production included",
			remote: "git@github.com:impulseframework/harbor.git",
			args:   []string{"restore", "prd", "--before", "v0.1.4", "--reason", "the migration mangled the invoices", "--placement", placement},
			in:     "prd\n",
			wantOut: []string{
				"=== RESTORE of prd: harbor's database returns to the data as it was before v0.1.4 (its pre-release backup), on its live release, asked for by you: the migration mangled the invoices ===",
				"The application goes into maintenance. The live database is kept as the forensic copy and a backup of it is taken. The backup is restored into the database's next generation;",
				"Type the environment's name (prd) to ask for it, anything else to stop:",
				"Asked, as octocat, for prd's database to return to the data as it was before v0.1.4 (its pre-release backup): the operations workflow of impulseframework/harbor runs it (https://github.com/impulseframework/harbor/actions/workflows/operations.yml). Its job reads the environment's records for the release that stays and the backup, prints them, runs the version trigger with the restore instruction and waits for the approval in Cloud Build, thirty minutes at most; the record names you and the reason.",
			},
			wantDispatch: "operations.yml master action=restore backup= before=v0.1.4 database= environment=prd reason=the migration mangled the invoices release=",
		},
		{
			name:   "restore --at names a moment, passed as @<moment>, and --of the generation it is read from",
			remote: "git@github.com:impulseframework/harbor.git",
			args:   []string{"restore", "prd", "--at", "2026-10-05T04:00:00+02:00", "--of", "imp-prd-gbl-harbor-db-2", "--placement", placement},
			in:     "prd\n",
			wantOut: []string{
				"=== RESTORE of prd: harbor's database returns to the data as of 2026-10-05T04:00:00+02:00 (a backup of imp-prd-gbl-harbor-db-2 made as of then), on its live release, asked for by you: no reason given ===",
			},
			wantDispatch: "operations.yml master action=restore backup=@2026-10-05T02:00:00Z before= database=imp-prd-gbl-harbor-db-2 environment=prd reason= release=",
		},
		{
			name:   "restore --backup names a backup by its resource name, with the release that runs on it",
			remote: "git@github.com:impulseframework/harbor.git",
			args:   []string{"restore", "stg", "v0.1.4", "--backup", "projects/p/instances/i/backups/imp-prd-gbl-harbor-db-forensic-20261005-0430", "--placement", placement},
			in:     "stg\n",
			wantOut: []string{
				"=== RESTORE of stg: harbor's database returns to the backup projects/p/instances/i/backups/imp-prd-gbl-harbor-db-forensic-20261005-0430, on v0.1.4, asked for by you: no reason given ===",
			},
			wantDispatch: "operations.yml master action=restore backup=projects/p/instances/i/backups/imp-prd-gbl-harbor-db-forensic-20261005-0430 before= database= environment=stg reason= release=v0.1.4",
		},
		{
			name:    "restore is not asked for when the name typed differs",
			remote:  "git@github.com:impulseframework/harbor.git",
			args:    []string{"restore", "prd", "--before", "v0.1.4", "--placement", placement},
			in:      "no\n",
			wantErr: `"no" is not prd: the restore was not asked for, and nothing changed`,
		},
		{
			name:    "restore refuses two sources",
			remote:  "git@github.com:impulseframework/harbor.git",
			args:    []string{"restore", "prd", "--before", "v0.1.4", "--at", "2026-10-05T04:00:00Z", "--placement", placement},
			wantErr: "--before, --at and --backup name the backup three ways: give one",
		},
		{
			name:    "restore refuses --of without --at",
			remote:  "git@github.com:impulseframework/harbor.git",
			args:    []string{"restore", "prd", "--before", "v0.1.4", "--of", "imp-prd-gbl-harbor-db", "--placement", placement},
			wantErr: "--of names the generation a moment is read from: it goes with --at",
		},
		{
			name:    "restore refuses a moment that is not RFC 3339",
			remote:  "git@github.com:impulseframework/harbor.git",
			args:    []string{"restore", "prd", "--at", "yesterday", "--placement", placement},
			wantErr: `--at "yesterday" is not an RFC 3339 moment (2026-10-05T04:30:00Z)`,
		},
		{
			name:    "restore refuses a backup that is not a resource name",
			remote:  "git@github.com:impulseframework/harbor.git",
			args:    []string{"restore", "prd", "--backup", "yesterday", "--placement", placement},
			wantErr: `--backup "yesterday" is not a backup's resource name (projects/<p>/instances/<i>/backups/<b>); bedrock backups prd lists them`,
		},
		{
			name:   "backups dispatches the list action",
			remote: "git@github.com:impulseframework/harbor.git",
			args:   []string{"backups", "prd", "--placement", placement},
			wantOut: []string{
				"Asked, as octocat, for the releases, backups and database generations of prd: the operations workflow of impulseframework/harbor reads them from the deployment records and writes them into its run's summary (https://github.com/impulseframework/harbor/actions/workflows/operations.yml). bedrock restore takes --before, --backup or --at from there.",
			},
			wantDispatch: "operations.yml master action=list environment=prd release=",
		},
		{
			name:   "maintenance off dispatches the maintenance action",
			remote: "git@github.com:impulseframework/harbor.git",
			args:   []string{"maintenance", "off", "stg", "--placement", placement},
			wantOut: []string{
				"Asked, as octocat, for stg to be taken out of maintenance: the operations workflow of impulseframework/harbor runs the live release's build with the maintenance instruction (https://github.com/impulseframework/harbor/actions/workflows/operations.yml), which moves the traffic back to the revision the maintenance revision displaced and resumes the queue, and deploys nothing.",
			},
			wantDispatch: "operations.yml master action=maintenance environment=stg release=",
		},
		{
			name:    "rollback --to refuses a release that does not exist",
			remote:  "git@github.com:impulseframework/harbor.git",
			args:    []string{"rollback", "prd", "--to", "v0.9.9", "--reason", "why", "--placement", placement},
			in:      "prd\n",
			wantErr: "no release v0.9.9 in impulseframework/harbor: an operation names a release that exists",
		},
		{
			name:   "rerun dispatches the operations workflow with the run action, production included",
			remote: "git@github.com:impulseframework/harbor.git",
			args:   []string{"rerun", "prd", "v0.1.4", "--placement", placement},
			wantOut: []string{
				"Asked, as octocat, for prd to run v0.1.4 again: the operations workflow of impulseframework/harbor runs it (https://github.com/impulseframework/harbor/actions/workflows/operations.yml). The release's tag build runs again from the start, waits for its approval in Cloud Build where prd requires one, and its record names you.",
			},
			wantDispatch: "operations.yml master action=run environment=prd release=v0.1.4",
		},
		{
			name:    "rerun refuses a release that does not exist",
			remote:  "git@github.com:impulseframework/harbor.git",
			args:    []string{"rerun", "stg", "v0.9.9", "--placement", placement},
			wantErr: "no release v0.9.9 in impulseframework/harbor: an operation names a release that exists",
		},
		{
			name:    "restore refuses production's own restore: production's database is restored to a backup",
			remote:  "git@github.com:impulseframework/harbor.git",
			args:    []string{"restore", "prd", "v0.1.4", "--placement", placement},
			wantErr: "prd is production, whose database is restored to a backup: --before <release>, --at <moment> or --backup <name> (bedrock backups prd lists them); it is never emptied by a run",
		},
		{
			name:    "restore refuses a release that does not exist",
			remote:  "git@github.com:impulseframework/harbor.git",
			args:    []string{"restore", "tst", "v0.9.9", "--placement", placement},
			wantErr: "no release v0.9.9 in impulseframework/harbor: an operation names a release that exists",
		},
		{
			name:    "restore refuses a tag of the wrong shape",
			remote:  "git@github.com:impulseframework/harbor.git",
			args:    []string{"restore", "tst", "0.1.4", "--placement", placement},
			wantErr: `"0.1.4" is not a release tag`,
		},
		{
			name:    "restore refuses an environment the placement does not list",
			remote:  "git@github.com:impulseframework/harbor.git",
			args:    []string{"restore", "qa", "v0.1.4", "--placement", placement},
			wantErr: `"qa" is not one of the environments (tst, stg, prd)`,
		},
		{
			name:    "the repository needs an origin on GitHub",
			args:    []string{"hotfix", "start", "v0.1.4", "--placement", placement},
			wantErr: "names no origin remote",
		},
		{
			name:   "hotfix start creates the line at the release",
			remote: "git@github.com:impulseframework/harbor.git",
			args:   []string{"hotfix", "start", "v0.1.4", "--placement", placement},
			wantOut: []string{
				"Created hotfix/0.1.x in impulseframework/harbor at c4 (v0.1.4); the line's next release is v0.1.5.",
				"Next: open the fix pull request against hotfix/0.1.x and merge it; release-please releases the branch as v0.1.5, and the tag runs through the environments like any release. When the hotfix is out, bring it to master with bedrock hotfix merge v0.1.5: a branch at the release's commit and its pull request into master, squash-merged; hotfix/0.1.x is never the pull request's head.",
			},
		},
		{
			name:   "hotfix merge makes the merge-back branch and its pull request",
			remote: "git@github.com:impulseframework/harbor.git",
			prepare: func(_ *githubtest.Server, repo *githubtest.Repo) {
				repo.Refs["refs/heads/hotfix/0.1.x"] = github.Object{Type: "commit", SHA: "h5"}
				repo.Refs["refs/tags/v0.1.5"] = github.Object{Type: "commit", SHA: "h5"}
				repo.Ancestry["h5"] = []string{"h5", "c4"}
				repo.Releases = map[string]github.Release{"v0.1.5": {TagName: "v0.1.5", Body: "### Bug Fixes\n\n* the fix"}}
			},
			args: []string{"hotfix", "merge", "v0.1.5", "--placement", placement},
			wantOut: []string{
				"Created merge-back/v0.1.5 in impulseframework/harbor at h5 (v0.1.5) and opened pull request #1 into master: https://github.com/impulseframework/harbor/pull/1",
				"Its title, \"fix: v0.1.5\", is the commit line the squash merge carries and release-please reads",
				"the merge deletes merge-back/v0.1.5, and hotfix/0.1.x stays as it is.",
			},
		},
		{
			name:   "hotfix merge refuses a release on master",
			remote: "git@github.com:impulseframework/harbor.git",
			prepare: func(_ *githubtest.Server, repo *githubtest.Repo) {
				repo.Refs["refs/heads/hotfix/0.1.x"] = github.Object{Type: "commit", SHA: "c4"}
			},
			args:    []string{"hotfix", "merge", "v0.1.4", "--placement", placement},
			wantErr: "tag v0.1.4 (commit c4) is already on master: nothing to merge back",
		},
		{
			name:   "hotfix start skips a patch master has already cut",
			remote: "git@github.com:impulseframework/harbor.git",
			prepare: func(_ *githubtest.Server, repo *githubtest.Repo) {
				repo.Refs["refs/tags/v0.1.5"] = github.Object{Type: "commit", SHA: "c5"}
				repo.Refs["refs/heads/master"] = github.Object{Type: "commit", SHA: "c5"}
			},
			args: []string{"hotfix", "start", "v0.1.4", "--placement", placement},
			wantOut: []string{
				"Created hotfix/0.1.x in impulseframework/harbor at a commit on c4 (v0.1.4) whose message names the line's next release, v0.1.6, since master has already cut v0.1.5.",
				"release-please releases the branch as v0.1.6",
			},
		},
		{
			name:   "hotfix start reports an existing line",
			remote: "git@github.com:impulseframework/harbor.git",
			prepare: func(_ *githubtest.Server, repo *githubtest.Repo) {
				repo.Refs["refs/heads/hotfix/0.1.x"] = github.Object{Type: "commit", SHA: "c4"}
			},
			args:    []string{"hotfix", "start", "v0.1.4", "--placement", placement},
			wantOut: []string{"hotfix/0.1.x already exists in impulseframework/harbor at c4: open the fix pull request against it."},
		},
		{
			name:   "hotfix start refuses an existing line that does not carry the release",
			remote: "git@github.com:impulseframework/harbor.git",
			prepare: func(_ *githubtest.Server, repo *githubtest.Repo) {
				repo.Refs["refs/heads/hotfix/0.1.x"] = github.Object{Type: "commit", SHA: "c3"}
				repo.Ancestry["c3"] = []string{"c3"}
				repo.Ancestry["c4"] = []string{"c4", "c3"}
			},
			args:    []string{"hotfix", "start", "v0.1.4", "--placement", placement},
			wantErr: "hotfix/0.1.x already exists in impulseframework/harbor at c3, which does not carry v0.1.4: the line started from an earlier release and serves the patches after it",
		},
		{
			name:    "hotfix start refuses a tag of the wrong shape",
			remote:  "git@github.com:impulseframework/harbor.git",
			args:    []string{"hotfix", "start", "0.1.4", "--placement", placement},
			wantErr: `tag "0.1.4" is not a release tag`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server, repo := labGitHub(t)
			if tt.prepare != nil {
				tt.prepare(server, repo)
			}
			dir := harborRepo(t, tt.remote)
			d := deps{
				domains: noCloudDomains, secrets: noSecretManager, projects: noProjects, cwd: dir, interactive: never,
				github: func(context.Context) (*github.Client, error) {
					return server.Client(), nil
				},
			}
			out, err := execute(d, tt.in, tt.args...)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Execute() error = %v, wantErr %q; output:\n%s", err, tt.wantErr, out)
				}
				if len(repo.Dispatches) != 0 {
					t.Errorf("a refused operation dispatched %+v", repo.Dispatches)
				}

				return
			}
			if err != nil {
				t.Fatalf("Execute() error = %v; output:\n%s", err, out)
			}
			if tt.wantDispatch != "" {
				if len(repo.Dispatches) != 1 {
					t.Fatalf("dispatches = %+v, want one", repo.Dispatches)
				}
				if got := dispatched(repo.Dispatches[0]); got != tt.wantDispatch {
					t.Errorf("dispatch = %q, want %q", got, tt.wantDispatch)
				}
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out, want) {
					t.Errorf("output lacks %q:\n%s", want, out)
				}
			}
		})
	}
}

// dispatched is a workflow_dispatch event on one line: the file, the ref and the inputs
// by name.
func dispatched(d githubtest.Dispatch) string {
	parts := []string{d.File, d.Ref}
	for _, name := range slices.Sorted(maps.Keys(d.Inputs)) {
		parts = append(parts, name+"="+d.Inputs[name])
	}

	return strings.Join(parts, " ")
}

// TestMigrationOperations: the three operations on an environment's migrations dispatch
// the operations workflow with their inputs as the signed-in person, and production, a
// missing release, a release that does not exist and a version that is not one are
// refused, production before any call reaches GitHub.
func TestMigrationOperations(t *testing.T) {
	t.Parallel()

	placement := filepath.Join(fixtureApp, "..", "placement.json")
	const remote = "git@github.com:impulseframework/harbor.git"
	tests := []struct {
		name         string
		args         []string
		wantOut      []string
		wantDispatch string
		// wantNoCalls says the refusal came before any call to GitHub.
		wantNoCalls bool
		wantErr     string
	}{
		{
			name:         "version dispatches the migration job with the version action",
			args:         []string{"migration", "version", "tst", "--release", "v0.1.4", "--placement", placement},
			wantOut:      []string{"Asked, as octocat, for tst's migration version at v0.1.4: the operations workflow of impulseframework/harbor runs it (https://github.com/impulseframework/harbor/actions/workflows/operations.yml). The migrate command prints what each migrations table says about the database, in the build log and in the run's summary; nothing else deploys."},
			wantDispatch: "operations.yml master action=version environment=tst release=v0.1.4",
		},
		{
			name:         "rerun dispatches the migration job with the rerun action",
			args:         []string{"migration", "rerun", "stg", "--release", "v0.1.4", "--placement", placement},
			wantOut:      []string{"Asked, as octocat, for stg to run v0.1.4 again: the operations workflow of impulseframework/harbor runs it", "The migrate command continues a file that stopped from its failed statement once the cause is fixed, and the release continues"},
			wantDispatch: "operations.yml master action=rerun environment=stg release=v0.1.4",
		},
		{
			name:         "force dispatches the migration job with the table and the version",
			args:         []string{"migration", "force", "tst", "40", "--release", "v0.1.4", "--placement", placement},
			wantOut:      []string{"Asked, as octocat, for tst's schema migrations table to be set to version 40 and v0.1.4 to continue: the operations workflow of impulseframework/harbor runs it", "The migrate command sets the version and prints the row before and after, then the migrations run from it and the release continues to the service, the traffic shift and the record, which names you."},
			wantDispatch: "operations.yml master action=force environment=tst release=v0.1.4 table=schema version=40",
		},
		{
			name:         "force of the data table to no version",
			args:         []string{"migration", "force", "stg", "none", "--release", "v0.1.4", "--table", "data", "--placement", placement},
			wantOut:      []string{"Asked, as octocat, for stg's data migrations table to be set to no version and v0.1.4 to continue"},
			wantDispatch: "operations.yml master action=force environment=stg release=v0.1.4 table=data version=-1",
		},
		{
			name:        "production is refused before any call",
			args:        []string{"migration", "version", "prd", "--release", "v0.1.4", "--placement", placement},
			wantNoCalls: true,
			wantErr:     "prd is production: no migration operation runs there, since production's migrations are the platform operator's (the README, When the migration fails); a release is run again in production with bedrock rerun",
		},
		{
			name:        "a force in production is refused before any call",
			args:        []string{"migration", "force", "prd", "40", "--release", "v0.1.4", "--placement", placement},
			wantNoCalls: true,
			wantErr:     "prd is production",
		},
		{
			name:        "a missing release is refused",
			args:        []string{"migration", "version", "tst", "--placement", placement},
			wantNoCalls: true,
			wantErr:     `required flag(s) "release" not set`,
		},
		{
			name:    "a release that does not exist is refused",
			args:    []string{"migration", "rerun", "tst", "--release", "v0.9.9", "--placement", placement},
			wantErr: "no release v0.9.9 in impulseframework/harbor: an operation names a release that exists",
		},
		{
			name:        "a release of the wrong shape is refused",
			args:        []string{"migration", "version", "tst", "--release", "0.1.4", "--placement", placement},
			wantNoCalls: true,
			wantErr:     `"0.1.4" is not a release tag`,
		},
		{
			name:        "a version that is not an integer is refused",
			args:        []string{"migration", "force", "tst", "forty", "--release", "v0.1.4", "--placement", placement},
			wantNoCalls: true,
			wantErr:     `"forty" is not a version: an integer 0 or above, or none for no version`,
		},
		{
			name:        "a version below -1 is refused",
			args:        []string{"migration", "force", "--release", "v0.1.4", "--placement", placement, "tst", "--", "-2"},
			wantNoCalls: true,
			wantErr:     `"-2" is not a version`,
		},
		{
			name:         "-1 after -- is no version too",
			args:         []string{"migration", "force", "--release", "v0.1.4", "--placement", placement, "tst", "--", "-1"},
			wantOut:      []string{"for tst's schema migrations table to be set to no version"},
			wantDispatch: "operations.yml master action=force environment=tst release=v0.1.4 table=schema version=-1",
		},
		{
			name:        "an unknown table is refused",
			args:        []string{"migration", "force", "tst", "40", "--release", "v0.1.4", "--table", "rows", "--placement", placement},
			wantNoCalls: true,
			wantErr:     `unknown table "rows" (the tables are schema and data)`,
		},
		{
			name:        "an environment the placement does not list is refused",
			args:        []string{"migration", "version", "qa", "--release", "v0.1.4", "--placement", placement},
			wantNoCalls: true,
			wantErr:     `"qa" is not one of the environments (tst, stg, prd)`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server, repo := labGitHub(t)
			dir := harborRepo(t, remote)
			d := deps{
				domains: noCloudDomains, secrets: noSecretManager, projects: noProjects, cwd: dir, interactive: never,
				github: func(context.Context) (*github.Client, error) {
					return server.Client(), nil
				},
			}
			out, err := execute(d, "", tt.args...)
			if tt.wantNoCalls && len(server.Calls) != 0 {
				t.Errorf("GitHub was called before the refusal: %v", server.Calls)
			}
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Execute() error = %v, wantErr %q; output:\n%s", err, tt.wantErr, out)
				}
				if len(repo.Dispatches) != 0 {
					t.Errorf("dispatches = %+v, want none", repo.Dispatches)
				}

				return
			}
			if err != nil {
				t.Fatalf("Execute() error = %v; output:\n%s", err, out)
			}
			if len(repo.Dispatches) != 1 {
				t.Fatalf("dispatches = %+v, want one", repo.Dispatches)
			}
			if got := dispatched(repo.Dispatches[0]); got != tt.wantDispatch {
				t.Errorf("dispatch = %q, want %q", got, tt.wantDispatch)
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out, want) {
					t.Errorf("output lacks %q:\n%s", want, out)
				}
			}
		})
	}
}
