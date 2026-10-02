package ci_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/impulse/app"
	"github.com/cccteam/ccc/impulse/ci"
	"github.com/cccteam/ccc/impulse/internal/skeleton"
)

// candidates are the embedded skeletons, each rendered once per test that needs the tree.
var candidates = []string{"solo", "tenanted", "outlets", "sites"}

// renderCandidate renders one embedded skeleton under its own placeholder module path and
// discovers it.
func renderCandidate(t *testing.T, name string) *app.App {
	t.Helper()

	all, err := skeleton.Candidates()
	if err != nil {
		t.Fatalf("skeleton.Candidates() error = %v", err)
	}
	modulePath := ""
	for _, c := range all {
		if c.Name == name {
			modulePath = c.ModulePath
		}
	}
	if modulePath == "" {
		t.Fatalf("no candidate %q", name)
	}
	dir := t.TempDir()
	if _, err := skeleton.Render(&skeleton.Options{Candidate: name, Dir: dir, ModulePath: modulePath}); err != nil {
		t.Fatalf("skeleton.Render(%s) error = %v", name, err)
	}
	a, err := app.Discover(dir)
	if err != nil {
		t.Fatalf("app.Discover() error = %v", err)
	}

	return a
}

// jobLine is a job id at the workflow's job indentation: two spaces, the id, a colon.
var jobLine = regexp.MustCompile(`^ {2}([A-Za-z_][A-Za-z0-9_-]*):\s*$`)

// jobIDs reads the top-level job ids of a rendered workflow: every two-space-indented key
// after the jobs: line.
func jobIDs(t *testing.T, workflow []byte) []string {
	t.Helper()

	var ids []string
	inJobs := false
	for _, line := range strings.Split(string(workflow), "\n") {
		if line == "jobs:" {
			inJobs = true

			continue
		}
		if !inJobs {
			continue
		}
		if m := jobLine.FindStringSubmatch(line); m != nil {
			ids = append(ids, m[1])
		}
	}
	if len(ids) == 0 {
		t.Fatal("no job ids read from the rendered workflow")
	}

	return ids
}

// TestChecksAreTheRenderedJobs pins the one place for names: Checks lists exactly the job
// ids Render writes, in order, and every fixed check is among them.
func TestChecksAreTheRenderedJobs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		candidate string
		want      []string
	}{
		{name: "solo", candidate: "solo", want: []string{"title", "go", "angular-web", "image", "secrets", "migrations"}},
		{name: "tenanted", candidate: "tenanted", want: []string{"title", "go", "angular-web", "image", "secrets", "migrations"}},
		{name: "outlets", candidate: "outlets", want: []string{"title", "go", "angular-web", "image", "secrets", "migrations"}},
		{name: "sites", candidate: "sites", want: []string{"title", "go", "angular-console", "angular-portal", "image", "secrets", "migrations"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a := renderCandidate(t, tt.candidate)
			rendered, err := ci.Render(a)
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			got := ci.Checks(a)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Checks() mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(got, jobIDs(t, rendered)); diff != "" {
				t.Errorf("Checks() and the rendered jobs disagree (-checks +jobs):\n%s", diff)
			}
			for _, fixed := range ci.FixedChecks {
				found := false
				for _, c := range got {
					if c == fixed {
						found = true
					}
				}
				if !found {
					t.Errorf("Checks() lacks the fixed check %q", fixed)
				}
			}
		})
	}
}

// TestRenderEqualsTheCandidates is the golden per candidate: the committed
// .github/workflows/ci.yml of every embedded skeleton is what Render produces over the
// rendered tree, byte for byte. A change to the template or the pins is a change to the
// four files in the same commit (impulse render in a rendered candidate writes them).
func TestRenderEqualsTheCandidates(t *testing.T) {
	t.Parallel()

	for _, candidate := range candidates {
		t.Run(candidate, func(t *testing.T) {
			t.Parallel()

			sub, err := skeleton.FS(candidate)
			if err != nil {
				t.Fatalf("skeleton.FS() error = %v", err)
			}
			committed, err := fs.ReadFile(sub, ci.File)
			if err != nil {
				t.Fatalf("the %s candidate carries no %s: %v", candidate, ci.File, err)
			}
			if _, err := fs.Stat(sub, ".github/codeql-config.yml"); err == nil {
				t.Errorf("the %s candidate still carries .github/codeql-config.yml, which nothing reads", candidate)
			}
			a := renderCandidate(t, candidate)
			rendered, err := ci.Render(a)
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			if diff := cmp.Diff(string(committed), string(rendered)); diff != "" {
				t.Errorf("%s: the committed %s differs from the rendering (-committed +rendered); run impulse render in a rendered candidate and copy the file back:\n%s", candidate, ci.File, diff)
			}
			d, err := ci.Compare(a)
			if err != nil {
				t.Fatalf("Compare() error = %v", err)
			}
			if d != nil {
				t.Errorf("Compare() over the rendered candidate = %s, want nil", d)
			}
		})
	}
}

func TestWorkspaces(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		dirs []string
		want []ci.Workspace
	}{
		{name: "none", dirs: nil, want: []ci.Workspace{}},
		{name: "the flat workspace", dirs: []string{"web"}, want: []ci.Workspace{{Dir: "web", Job: "angular-web"}}},
		{
			name: "sites, sorted by directory",
			dirs: []string{"apps/portal/web", "apps/console/web"},
			want: []ci.Workspace{{Dir: "apps/console/web", Job: "angular-console"}, {Dir: "apps/portal/web", Job: "angular-portal"}},
		},
		{
			name: "other layouts join the segments and drop a trailing web",
			dirs: []string{"web/console", "apps/pilots/gui", "gui", "services/kiosk/web"},
			want: []ci.Workspace{
				{Dir: "apps/pilots/gui", Job: "angular-apps-pilots-gui"},
				{Dir: "gui", Job: "angular-gui"},
				{Dir: "services/kiosk/web", Job: "angular-services-kiosk"},
				{Dir: "web/console", Job: "angular-web-console"},
			},
		},
		{name: "the application root", dirs: []string{"."}, want: []ci.Workspace{{Dir: ".", Job: "angular-root"}}},
		{name: "a character a job id cannot carry", dirs: []string{"apps/my.site/web"}, want: []ci.Workspace{{Dir: "apps/my.site/web", Job: "angular-my-site"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a := &app.App{}
			for _, d := range tt.dirs {
				a.WebApps = append(a.WebApps, app.WebApp{Dir: d})
			}
			if diff := cmp.Diff(tt.want, ci.Workspaces(a)); diff != "" {
				t.Errorf("Workspaces() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// workspaceApp is an application at a temporary root with the given browser workspaces,
// its workflow written from the code when written is set.
func workspaceApp(t *testing.T, dirs []string, written bool) *app.App {
	t.Helper()

	a := &app.App{Root: t.TempDir()}
	for _, d := range dirs {
		a.WebApps = append(a.WebApps, app.WebApp{Dir: d})
	}
	if written {
		if _, err := ci.Write(a); err != nil {
			t.Fatalf("Write() error = %v", err)
		}
	}

	return a
}

func TestCompare(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// app builds the application and its committed file.
		app  func(t *testing.T) *app.App
		want *ci.Difference
		// wantText is the difference's String when a difference is expected.
		wantText string
	}{
		{
			name: "equal",
			app:  func(t *testing.T) *app.App { t.Helper(); return workspaceApp(t, []string{"web"}, true) },
		},
		{
			name: "missing",
			app:  func(t *testing.T) *app.App { t.Helper(); return workspaceApp(t, []string{"web"}, false) },
			want: &ci.Difference{Missing: true}, wantText: ".github/workflows/ci.yml is missing",
		},
		{
			name: "a hand edit",
			app: func(t *testing.T) *app.App {
				t.Helper()
				a := workspaceApp(t, []string{"web"}, true)
				data, err := os.ReadFile(a.Abs(ci.File))
				if err != nil {
					t.Fatal(err)
				}
				edited := strings.Replace(string(data), "  cancel-in-progress: true", "  cancel-in-progress: false", 1)
				if err := os.WriteFile(a.Abs(ci.File), []byte(edited), 0o600); err != nil {
					t.Fatal(err)
				}

				return a
			},
			want:     &ci.Difference{Line: 17, Want: "  cancel-in-progress: true", Got: "  cancel-in-progress: false"},
			wantText: `.github/workflows/ci.yml:17: the code renders "  cancel-in-progress: true"; the file has "  cancel-in-progress: false"`,
		},
		{
			name: "a workspace without its job",
			app: func(t *testing.T) *app.App {
				t.Helper()
				a := workspaceApp(t, []string{"apps/console/web"}, true)
				// A second workspace appears after the file was written.
				a.WebApps = append(a.WebApps, app.WebApp{Dir: "apps/portal/web"})

				return a
			},
			want: &ci.Difference{
				Line: 135,
				Want: "  # The browser workspace at apps/portal/web: bun installs from the lockfile exactly (bun ci), then the package scripts build, lint and test.",
				Got:  "  # The container image, once the application has a Dockerfile (bedrock seeds it): hadolint over the Dockerfile, the build, and Grype over the built image, failing on a high or critical vulnerability. Without a Dockerfile the job passes with nothing to build.",
			},
			wantText: `.github/workflows/ci.yml:135: the code renders "  # The browser workspace at apps/portal/web: bun installs from the lockfile exactly (bun ci), then the package scripts build, lint and test."; the file has "  # The container image, once the application has a Dockerfile (bedrock seeds it): hadolint over the Dockerfile, the build, and Grype over the built image, failing on a high or critical vulnerability. Without a Dockerfile the job passes with nothing to build."`,
		},
		{
			name: "a file that ends early",
			app: func(t *testing.T) *app.App {
				t.Helper()
				a := workspaceApp(t, []string{"web"}, true)
				data, err := os.ReadFile(a.Abs(ci.File))
				if err != nil {
					t.Fatal(err)
				}
				lines := strings.Split(string(data), "\n")
				if err := os.WriteFile(a.Abs(ci.File), []byte(strings.Join(lines[:10], "\n")+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}

				return a
			},
			want:     &ci.Difference{Line: 11, Want: "on:", Got: ""},
			wantText: `.github/workflows/ci.yml:11: the code renders "on:"; the file has ""`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := ci.Compare(tt.app(t))
			if err != nil {
				t.Fatalf("Compare() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Compare() mismatch (-want +got):\n%s", diff)
			}
			if got != nil && got.String() != tt.wantText {
				t.Errorf("String() = %q, want %q", got.String(), tt.wantText)
			}
		})
	}
}

func TestWrite(t *testing.T) {
	t.Parallel()

	a := workspaceApp(t, []string{"web"}, false)
	first, err := ci.Write(a)
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if !first.Written {
		t.Error("first Write() reported nothing written")
	}
	info, err := os.Stat(a.Abs(ci.File))
	if err != nil {
		t.Fatalf("the file was not written: %v", err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Errorf("mode = %o, want 644", info.Mode().Perm())
	}
	again, err := ci.Write(a)
	if err != nil {
		t.Fatalf("second Write() error = %v", err)
	}
	if again.Written {
		t.Error("second Write() rewrote an unchanged file")
	}
	if d, err := ci.Compare(a); err != nil || d != nil {
		t.Errorf("Compare() after Write() = %v, %v; want nil, nil", d, err)
	}
	if _, err := os.Stat(filepath.Join(a.Root, ".github", "workflows")); err != nil {
		t.Errorf("the workflows directory was not created: %v", err)
	}
}
