package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitRepo turns the fixture application repository into a git repository on master with
// everything committed, so the renumber can read the default branch.
func gitRepo(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.com"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	run("init", "-q", "-b", "master")
	run("add", "-A")
	run("commit", "-q", "-m", "the application")
	run("checkout", "-q", "-b", "feature")
}

// gitTag tags the repository's HEAD.
func gitTag(t *testing.T, dir, name string) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", "-C", dir, "tag", name)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git tag %s: %v\n%s", name, err, out)
	}
}

// migrationFiles writes empty migration files under the application.
func migrationFiles(t *testing.T, dir string, names ...string) {
	t.Helper()
	for _, name := range names {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("-- "+name+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMigrationCheck(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		files []string
		// git commits the application, migrations included, so the checkout can say
		// whether a release tag is in it; tag adds one.
		git, tag   bool
		wantCode   int
		wantOut    []string
		wantNotOut []string
	}{
		{
			name:     "no migrations directory: nothing to check",
			wantCode: 0,
			wantOut:  []string{"schema/migrations: no migrations\n", "schema/devseed: no migrations\n"},
		},
		{
			name:     "a sound directory prints its span",
			files:    []string{"schema/migrations/000001_Init.up.sql", "schema/migrations/000001_Init.down.sql", "schema/migrations/000002_Audit.up.sql", "schema/migrations/000003_Sites.up.sql"},
			wantCode: 0,
			wantOut:  []string{"schema/migrations: 3 migration(s), 000001 to 000003, one sequence\n"},
		},
		{
			name:     "a gap already on the default branch is refused: the whole directory is read, not a pull request's changes",
			files:    []string{"schema/migrations/000001_Init.up.sql", "schema/migrations/000002_Audit.up.sql", "schema/migrations/000004_Late.up.sql"},
			wantCode: 1,
			wantOut:  []string{"schema/migrations: 3 migration(s), 000001 to 000004\n", "  refused  schema/migrations: gap: no migration 000003 between 000001 and 000004; the sequence is contiguous so nothing is skipped\n", "(1 problem(s) above): the CI migrations job refuses them on every pull request, and bedrock check in every build."},
		},
		{
			name:     "a gap in a checkout without a release tag names the pack as the fix",
			files:    []string{"schema/migrations/000001_Init.up.sql", "schema/migrations/000002_Audit.up.sql", "schema/migrations/000004_Late.up.sql"},
			git:      true,
			wantCode: 1,
			wantOut:  []string{"  refused  schema/migrations: gap: no migration 000003 between 000001 and 000004", "           no release tag is in this checkout: until the first release a committed migration may still change, and bedrock migration renumber (go generate ./... runs it) packs the whole sequence from 000001, committed migrations included\n"},
		},
		{
			name:       "a gap in a released checkout names no pack: the committed migrations are frozen",
			files:      []string{"schema/migrations/000001_Init.up.sql", "schema/migrations/000002_Audit.up.sql", "schema/migrations/000004_Late.up.sql"},
			git:        true,
			tag:        true,
			wantCode:   1,
			wantOut:    []string{"  refused  schema/migrations: gap: no migration 000003 between 000001 and 000004"},
			wantNotOut: []string{"no release tag"},
		},
		{
			name:     "the seed directory beside the schema is read by the same rule",
			files:    []string{"schema/migrations/000001_Init.up.sql", "schema/devseed/000001_People.up.sql", "schema/devseed/000003_Sites.up.sql"},
			wantCode: 1,
			wantOut:  []string{"schema/migrations: 1 migration(s), 000001 to 000001, one sequence\n", "schema/devseed: 2 migration(s), 000001 to 000003\n", "  refused  schema/devseed: gap: no migration 000002 between 000001 and 000003"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			app := copyRepo(t, fixtureApp)
			dir := filepath.Join(app, "infrastructure")
			if code, out := run(t, "render", "--app", app, "--out", dir, "--placement", placement); code != 0 {
				t.Fatalf("render exit = %d, output:\n%s", code, out)
			}
			migrationFiles(t, app, tt.files...)
			if tt.git {
				if err := os.Remove(filepath.Join(app, ".git")); err != nil {
					t.Fatal(err)
				}
				gitRepo(t, app)
			}
			if tt.tag {
				gitTag(t, app, "v0.1.0")
			}
			code, out := run(t, "migration", "check", "--app", app, "--dir", dir, "--placement", placement)
			if code != tt.wantCode {
				t.Errorf("migration check exit = %d, want %d; output:\n%s", code, tt.wantCode, out)
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out, want) {
					t.Errorf("migration check output lacks %q:\n%s", want, out)
				}
			}
			for _, s := range tt.wantNotOut {
				if strings.Contains(out, s) {
					t.Errorf("migration check output holds %q, want it absent:\n%s", s, out)
				}
			}
		})
	}
}

// TestRenderRefusesBrokenSequence: render writes the stack and then refuses a migrations
// directory that breaks the sequence rule, before the first push.
func TestRenderRefusesBrokenSequence(t *testing.T) {
	t.Parallel()

	app := copyRepo(t, fixtureApp)
	migrationFiles(t, app, "schema/migrations/000001_Init.up.sql", "schema/migrations/000003_Sites.up.sql")
	dir := filepath.Join(app, "infrastructure")
	code, out := run(t, "render", "--app", app, "--out", dir, "--placement", placement)
	if code != 1 {
		t.Errorf("render exit = %d, want 1; output:\n%s", code, out)
	}
	for _, want := range []string{
		"Rendered the harbor stack into " + dir,
		"  refused  schema/migrations: gap: no migration 000002 between 000001 and 000003; the sequence is contiguous so nothing is skipped\n",
		"(1 problem(s) above): fix them before the first push; bedrock check refuses them in every build, and the CI migrations job on every pull request.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("render output lacks %q:\n%s", want, out)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "outputs.tf")); err != nil {
		t.Errorf("the stack is written before the migrations are read: %v", err)
	}
}

func TestMigrationRenumber(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// committed are the migrations master holds; own the branch's, written after
		// the branch is cut.
		committed []string
		own       []string
		// released tags the committed state as a release: the committed schema
		// migrations are frozen from then on.
		released bool
		// cwd is where the command runs, relative to the repository.
		cwd       string
		wantOut   []string
		wantFiles []string
		wantErr   string
	}{
		{
			name:      "no release tag: the whole sequence is packed from 000001, a committed hole closed",
			committed: []string{"schema/migrations/000001_Init.up.sql", "schema/migrations/000001_Init.down.sql", "schema/migrations/000002_Audit.up.sql", "schema/migrations/000004_Late.up.sql", "schema/migrations/000004_Late.down.sql"},
			own:       []string{"schema/migrations/000005_Sites.up.sql", "schema/migrations/000005_Sites.down.sql"},
			wantOut:   []string{"schema/migrations: 000004_Late -> 000003_Late (up, down)", "schema/migrations: 000005_Sites -> 000004_Sites (up, down)", "Renumbered 2 migration(s) to follow refs/heads/master at ", "No release tag is in this checkout, so the whole sequence is packed from 000001, committed migrations included; the freeze starts at the first release."},
			wantFiles: []string{"schema/migrations/000001_Init.down.sql", "schema/migrations/000001_Init.up.sql", "schema/migrations/000002_Audit.up.sql", "schema/migrations/000003_Late.down.sql", "schema/migrations/000003_Late.up.sql", "schema/migrations/000004_Sites.down.sql", "schema/migrations/000004_Sites.up.sql"},
		},
		{
			name:      "the branch's migration moves past the one master took, from a package directory",
			committed: []string{"schema/migrations/000001_Init.up.sql", "schema/migrations/000001_Init.down.sql", "schema/migrations/000002_Audit.up.sql", "schema/migrations/000002_Audit.down.sql"},
			own:       []string{"schema/migrations/000002_Sites.up.sql", "schema/migrations/000002_Sites.down.sql"},
			released:  true,
			cwd:       filepath.Join("cmd", "generate"),
			wantOut:   []string{"schema/migrations: 000002_Sites -> 000003_Sites (up, down)", "Renumbered 1 migration(s) to follow refs/heads/master at "},
			wantFiles: []string{"schema/migrations/000001_Init.down.sql", "schema/migrations/000001_Init.up.sql", "schema/migrations/000002_Audit.down.sql", "schema/migrations/000002_Audit.up.sql", "schema/migrations/000003_Sites.down.sql", "schema/migrations/000003_Sites.up.sql"},
		},
		{
			name:      "nothing to renumber prints nothing",
			committed: []string{"schema/migrations/000001_Init.up.sql"},
			own:       []string{"schema/migrations/000002_Sites.up.sql"},
			cwd:       ".",
			wantFiles: []string{"schema/migrations/000001_Init.up.sql", "schema/migrations/000002_Sites.up.sql"},
		},
		{
			name:    "no migrations directory at all: nothing to do",
			cwd:     ".",
			wantOut: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			repo := appRepo(t)
			if err := os.Remove(filepath.Join(repo, ".git")); err != nil {
				t.Fatal(err)
			}
			migrationFiles(t, repo, tt.committed...)
			gitRepo(t, repo)
			if tt.released {
				gitTag(t, repo, "v0.1.0")
			}
			migrationFiles(t, repo, tt.own...)
			d := deps{domains: noCloudDomains, secrets: noSecretManager, projects: noProjects, cwd: filepath.Join(repo, tt.cwd), interactive: never}
			out, err := execute(d, "", "migration", "renumber")
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Execute() error = %v, wantErr %q; output:\n%s", err, tt.wantErr, out)
				}

				return
			}
			if err != nil {
				t.Fatalf("Execute() error = %v; output:\n%s", err, out)
			}
			if len(tt.wantOut) == 0 && out != "" {
				t.Errorf("output = %q, want none", out)
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out, want) {
					t.Errorf("output lacks %q:\n%s", want, out)
				}
			}
			var got []string
			for _, dir := range []string{"schema/migrations"} {
				entries, err := os.ReadDir(filepath.Join(repo, filepath.FromSlash(dir)))
				if err != nil {
					continue
				}
				for _, e := range entries {
					got = append(got, dir+"/"+e.Name())
				}
			}
			if strings.Join(got, " ") != strings.Join(tt.wantFiles, " ") {
				t.Errorf("files = %v, want %v", got, tt.wantFiles)
			}
		})
	}
}

func TestMigrationConsolidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		committed []string
		own       []string
		wantOut   []string
		wantFiles []string
		wantErr   string
	}{
		{
			name:      "folds the committed sequence into the latest index, staged, and says what accepts it",
			committed: []string{"schema/migrations/000001_Init.up.sql", "schema/migrations/000001_Init.down.sql", "schema/migrations/000002_Audit.up.sql", "schema/migrations/000003_Sites.up.sql", "schema/migrations/000003_Sites.down.sql"},
			wantOut: []string{
				"schema/migrations: 000001_Init, 000002_Audit, 000003_Sites folded into 000003_Consolidated (up, down)",
				"  000002_Audit had no down file; the down file reverses the others.",
				"Consolidated 3 migration(s) of refs/heads/master at ",
				" into schema/migrations/000003_Consolidated.up.sql: the up files in order, each under a comment naming it and closed with a semicolon, and the down files in reverse order. The folded files are removed and the change is staged; review the diff.",
				"The pull-request workflow's migrations job and the pipeline's guard read the folded files as committed migrations deleted and refuse the pull request today",
			},
			wantFiles: []string{"schema/migrations/000003_Consolidated.down.sql", "schema/migrations/000003_Consolidated.up.sql"},
		},
		{
			name:      "one migration is nothing to consolidate",
			committed: []string{"schema/migrations/000001_Init.up.sql"},
			wantOut:   []string{"Nothing to consolidate: schema/migrations holds one migration or none as refs/heads/master holds it."},
			wantFiles: []string{"schema/migrations/000001_Init.up.sql"},
		},
		{
			name:      "a migration of the branch's own is refused",
			committed: []string{"schema/migrations/000001_Init.up.sql", "schema/migrations/000002_Audit.up.sql"},
			own:       []string{"schema/migrations/000003_Sites.up.sql"},
			wantErr:   "schema/migrations is not as master holds it (git status lists a change under it)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			repo := appRepo(t)
			if err := os.Remove(filepath.Join(repo, ".git")); err != nil {
				t.Fatal(err)
			}
			migrationFiles(t, repo, tt.committed...)
			gitRepo(t, repo)
			gitTag(t, repo, "v0.1.0")
			migrationFiles(t, repo, tt.own...)
			d := deps{domains: noCloudDomains, secrets: noSecretManager, projects: noProjects, cwd: repo, interactive: never}
			out, err := execute(d, "", "migration", "consolidate")
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
					t.Errorf("output lacks %q:\n%s", want, out)
				}
			}
			entries, err := os.ReadDir(filepath.Join(repo, "schema", "migrations"))
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, e := range entries {
				got = append(got, "schema/migrations/"+e.Name())
			}
			if strings.Join(got, " ") != strings.Join(tt.wantFiles, " ") {
				t.Errorf("files = %v, want %v", got, tt.wantFiles)
			}
		})
	}
}

// TestMigrationRenumberFollowsPatchLine: a fix branch cut from a patch line follows the
// line's committed sequence, not the default branch's, which has moved on.
func TestMigrationRenumberFollowsPatchLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// line are the migrations committed on patch/0.1.x after it is cut; master the
		// ones master adds after the line is cut; own the fix branch's.
		line      []string
		master    []string
		own       []string
		wantOut   []string
		wantFiles []string
	}{
		{
			name:      "the fix's migration keeps its index: master took it, the line did not",
			master:    []string{"schema/migrations/000002_Master.up.sql"},
			own:       []string{"schema/migrations/000002_Fix.up.sql"},
			wantFiles: []string{"schema/migrations/000001_Init.up.sql", "schema/migrations/000002_Fix.up.sql"},
		},
		{
			name:      "the fix's migration follows the line's own later one",
			line:      []string{"schema/migrations/000002_Line.up.sql"},
			master:    []string{"schema/migrations/000002_Master.up.sql"},
			own:       []string{"schema/migrations/000002_Fix.up.sql"},
			wantOut:   []string{"schema/migrations: 000002_Fix -> 000003_Fix (up)", "Renumbered 1 migration(s) to follow refs/heads/patch/0.1.x at "},
			wantFiles: []string{"schema/migrations/000001_Init.up.sql", "schema/migrations/000002_Line.up.sql", "schema/migrations/000003_Fix.up.sql"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			repo := appRepo(t)
			if err := os.Remove(filepath.Join(repo, ".git")); err != nil {
				t.Fatal(err)
			}
			migrationFiles(t, repo, "schema/migrations/000001_Init.up.sql")
			gitRepo(t, repo)
			run := func(args ...string) {
				t.Helper()
				cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@example.com"}, args...)...)
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
				}
			}
			run("checkout", "-q", "-b", "patch/0.1.x", "master")
			if len(tt.line) > 0 {
				migrationFiles(t, repo, tt.line...)
				run("add", "-A")
				run("commit", "-q", "-m", "the line's migration")
			}
			run("checkout", "-q", "master")
			migrationFiles(t, repo, tt.master...)
			run("add", "-A")
			run("commit", "-q", "-m", "master moves on")
			run("checkout", "-q", "-b", "fix", "patch/0.1.x")
			migrationFiles(t, repo, tt.own...)
			d := deps{domains: noCloudDomains, secrets: noSecretManager, projects: noProjects, cwd: repo, interactive: never}
			out, err := execute(d, "", "migration", "renumber")
			if err != nil {
				t.Fatalf("Execute() error = %v; output:\n%s", err, out)
			}
			if len(tt.wantOut) == 0 && out != "" {
				t.Errorf("output = %q, want none", out)
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out, want) {
					t.Errorf("output lacks %q:\n%s", want, out)
				}
			}
			entries, err := os.ReadDir(filepath.Join(repo, "schema", "migrations"))
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, e := range entries {
				got = append(got, "schema/migrations/"+e.Name())
			}
			if strings.Join(got, " ") != strings.Join(tt.wantFiles, " ") {
				t.Errorf("files = %v, want %v", got, tt.wantFiles)
			}
		})
	}
}
