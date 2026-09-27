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

func TestMigrationRenumber(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// committed are the migrations master holds; own the branch's, written after
		// the branch is cut.
		committed []string
		own       []string
		// cwd is where the command runs, relative to the repository.
		cwd       string
		wantOut   []string
		wantFiles []string
		wantErr   string
	}{
		{
			name:      "the branch's migration moves past the one master took, from a package directory",
			committed: []string{"schema/migrations/000001_Init.up.sql", "schema/migrations/000001_Init.down.sql", "schema/migrations/000002_Audit.up.sql", "schema/migrations/000002_Audit.down.sql"},
			own:       []string{"schema/migrations/000002_Sites.up.sql", "schema/migrations/000002_Sites.down.sql"},
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
