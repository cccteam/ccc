package migration

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// The two directories every case may use.
const (
	migrations = "schema/migrations"
	seed       = "schema/devseed"
)

// repo is a temporary git repository on master.
type repo struct {
	t    *testing.T
	root string
}

func newRepo(t *testing.T) *repo {
	t.Helper()
	r := &repo{t: t, root: t.TempDir()}
	r.git("init", "-q", "-b", "master")

	return r
}

func (r *repo) git(args ...string) string {
	r.t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", r.root, "-c", "user.name=t", "-c", "user.email=t@example.com"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}

	return string(out)
}

// write puts files in the working tree, each as its own content.
func (r *repo) write(names ...string) {
	r.t.Helper()
	for _, name := range names {
		p := filepath.Join(r.root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			r.t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("-- "+name+"\n"), 0o600); err != nil {
			r.t.Fatal(err)
		}
	}
}

// commit writes the files and commits them all.
func (r *repo) commit(msg string, names ...string) {
	r.t.Helper()
	r.write(names...)
	r.git("add", "-A")
	r.git("commit", "-q", "-m", msg)
}

// pair is the up and down file of one migration.
func pair(dir, stem string) []string {
	return []string{dir + "/" + stem + ".up.sql", dir + "/" + stem + ".down.sql"}
}

func join(lists ...[]string) []string {
	var all []string
	for _, l := range lists {
		all = append(all, l...)
	}

	return all
}

// list is the sorted names in a directory of the working tree.
func list(t *testing.T, root, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(dir)))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}

	return names
}

func TestRenumber(t *testing.T) {
	t.Parallel()

	// masterWithTwo commits 000001 and 000002 on master and cuts the branch there.
	masterWithTwo := func(r *repo) {
		r.commit("two migrations", join(pair(migrations, "000001_Init"), pair(migrations, "000002_Widgets"))...)
		r.git("checkout", "-q", "-b", "feature")
	}
	tests := []struct {
		name string
		// setup builds the repository and leaves the working tree on the branch.
		setup func(r *repo)
		dirs  []string
		// branch is the default branch to read; master when empty.
		branch string
		// editable names the directories whose committed files may move down.
		editable    []string
		wantRenames []Rename
		wantNotes   []string
		wantFiles   map[string][]string
		// wantStaged is a line git status --porcelain must show: a tracked rename is
		// staged through git mv.
		wantStaged string
		wantRef    string
		wantErr    string
	}{
		{
			name: "an index the default branch took since the branch was cut moves up",
			setup: func(r *repo) {
				masterWithTwo(r)
				r.commit("sites on the branch", pair(migrations, "000003_Sites")...)
				r.git("checkout", "-q", "master")
				r.commit("audit on master", pair(migrations, "000003_Audit")...)
				r.git("checkout", "-q", "feature")
			},
			wantRenames: []Rename{{Dir: migrations, From: "000003_Sites", To: "000004_Sites", Files: []string{"up", "down"}}},
			wantFiles:   map[string][]string{migrations: {"000001_Init.down.sql", "000001_Init.up.sql", "000002_Widgets.down.sql", "000002_Widgets.up.sql", "000004_Sites.down.sql", "000004_Sites.up.sql"}},
			wantStaged:  "R  schema/migrations/000003_Sites.up.sql -> schema/migrations/000004_Sites.up.sql",
			wantRef:     "refs/heads/master",
		},
		{
			name: "a gap after the default branch's highest closes, an untracked file renamed on disk",
			setup: func(r *repo) {
				masterWithTwo(r)
				r.write(pair(migrations, "000005_Late")...)
			},
			wantRenames: []Rename{{Dir: migrations, From: "000005_Late", To: "000003_Late", Files: []string{"up", "down"}}},
			wantFiles:   map[string][]string{migrations: {"000001_Init.down.sql", "000001_Init.up.sql", "000002_Widgets.down.sql", "000002_Widgets.up.sql", "000003_Late.down.sql", "000003_Late.up.sql"}},
			wantStaged:  "?? schema/migrations/000003_Late.down.sql",
		},
		{
			name: "several own migrations keep their order and become contiguous",
			setup: func(r *repo) {
				masterWithTwo(r)
				r.write(join(pair(migrations, "000004_Second"), pair(migrations, "000007_Third"), pair(migrations, "000003_First"))...)
			},
			wantRenames: []Rename{{Dir: migrations, From: "000007_Third", To: "000005_Third", Files: []string{"up", "down"}}},
			wantFiles:   map[string][]string{migrations: {"000001_Init.down.sql", "000001_Init.up.sql", "000002_Widgets.down.sql", "000002_Widgets.up.sql", "000003_First.down.sql", "000003_First.up.sql", "000004_Second.down.sql", "000004_Second.up.sql", "000005_Third.down.sql", "000005_Third.up.sql"}},
		},
		{
			name: "own migrations already in place: nothing to renumber, other files ignored",
			setup: func(r *repo) {
				masterWithTwo(r)
				r.write(migrations+"/notes.sql", migrations+"/README.md")
				r.write(pair(migrations, "000003_Next")...)
			},
			wantFiles: map[string][]string{migrations: {"000001_Init.down.sql", "000001_Init.up.sql", "000002_Widgets.down.sql", "000002_Widgets.up.sql", "000003_Next.down.sql", "000003_Next.up.sql", "README.md", "notes.sql"}},
		},
		{
			name: "a rename onto another's name waits its turn",
			setup: func(r *repo) {
				masterWithTwo(r)
				r.write(join(pair(migrations, "000002_Sites"), pair(migrations, "000003_Sites"))...)
			},
			wantRenames: []Rename{{Dir: migrations, From: "000003_Sites", To: "000004_Sites", Files: []string{"up", "down"}}, {Dir: migrations, From: "000002_Sites", To: "000003_Sites", Files: []string{"up", "down"}}},
			wantFiles:   map[string][]string{migrations: {"000001_Init.down.sql", "000001_Init.up.sql", "000002_Widgets.down.sql", "000002_Widgets.up.sql", "000003_Sites.down.sql", "000003_Sites.up.sql", "000004_Sites.down.sql", "000004_Sites.up.sql"}},
		},
		{
			name: "no committed migrations: the own files anchor at their lowest index",
			setup: func(r *repo) {
				r.commit("no schema yet", "README.md")
				r.git("checkout", "-q", "-b", "feature")
				r.write(join(pair(migrations, "000100_A"), pair(migrations, "000102_B"))...)
			},
			wantRenames: []Rename{{Dir: migrations, From: "000102_B", To: "000101_B", Files: []string{"up", "down"}}},
			wantFiles:   map[string][]string{migrations: {"000100_A.down.sql", "000100_A.up.sql", "000101_B.down.sql", "000101_B.up.sql"}},
		},
		{
			name: "the seed directory follows its own sequence",
			setup: func(r *repo) {
				r.commit("schema and seed", join(pair(migrations, "000001_Init"), pair(seed, "000001_Marker"))...)
				r.git("checkout", "-q", "-b", "feature")
				r.write(join(pair(seed, "000001_People"), pair(migrations, "000002_Widgets"))...)
			},
			dirs:        []string{migrations, seed},
			wantRenames: []Rename{{Dir: seed, From: "000001_People", To: "000002_People", Files: []string{"up", "down"}}},
			wantFiles:   map[string][]string{migrations: {"000001_Init.down.sql", "000001_Init.up.sql", "000002_Widgets.down.sql", "000002_Widgets.up.sql"}, seed: {"000001_Marker.down.sql", "000001_Marker.up.sql", "000002_People.down.sql", "000002_People.up.sql"}},
		},
		{
			name: "origin's copy of the default branch is read before the local one",
			setup: func(r *repo) {
				masterWithTwo(r)
				r.git("checkout", "-q", "master")
				r.commit("audit on master", pair(migrations, "000003_Audit")...)
				r.git("update-ref", "refs/remotes/origin/master", "HEAD")
				r.git("reset", "-q", "--hard", "HEAD~1")
				r.git("checkout", "-q", "feature")
				r.write(pair(migrations, "000003_Sites")...)
			},
			wantRenames: []Rename{{Dir: migrations, From: "000003_Sites", To: "000004_Sites", Files: []string{"up", "down"}}},
			wantRef:     "refs/remotes/origin/master",
		},
		{
			name: "an up file alone moves; a down file added for a committed migration is left alone",
			setup: func(r *repo) {
				r.commit("up only", migrations+"/000001_Init.up.sql", migrations+"/000002_Widgets.up.sql")
				r.git("checkout", "-q", "-b", "feature")
				r.write(migrations+"/000002_Widgets.down.sql", migrations+"/000002_Sites.up.sql", migrations+"/000009_Orphan.down.sql")
			},
			wantRenames: []Rename{{Dir: migrations, From: "000002_Sites", To: "000003_Sites", Files: []string{"up"}}},
			wantNotes:   []string{"schema/migrations/000002_Widgets.down.sql: a down file added for a committed migration, left alone", "schema/migrations/000009_Orphan.down.sql: a down file with no up file, left alone"},
			wantFiles:   map[string][]string{migrations: {"000001_Init.up.sql", "000002_Widgets.down.sql", "000002_Widgets.up.sql", "000003_Sites.up.sql", "000009_Orphan.down.sql"}},
		},
		{
			name: "a removed committed seed file leaves no gap: the seed files after it move down and the branch's own follow",
			setup: func(r *repo) {
				r.commit("seeds", join(pair(seed, "000001_Marker"), pair(seed, "000002_People"), pair(seed, "000003_Orders"))...)
				r.git("checkout", "-q", "-b", "feature")
				r.git("rm", "-q", seed+"/000002_People.up.sql", seed+"/000002_People.down.sql")
				r.write(pair(seed, "000004_Shipments")...)
			},
			dirs:     []string{seed},
			editable: []string{seed},
			wantRenames: []Rename{
				{Dir: seed, From: "000003_Orders", To: "000002_Orders", Files: []string{"up", "down"}},
				{Dir: seed, From: "000004_Shipments", To: "000003_Shipments", Files: []string{"up", "down"}},
			},
			wantFiles:  map[string][]string{seed: {"000001_Marker.down.sql", "000001_Marker.up.sql", "000002_Orders.down.sql", "000002_Orders.up.sql", "000003_Shipments.down.sql", "000003_Shipments.up.sql"}},
			wantStaged: "R  schema/devseed/000003_Orders.up.sql -> schema/devseed/000002_Orders.up.sql",
		},
		{
			name: "a removed committed schema migration is not closed over: a committed schema migration never moves",
			setup: func(r *repo) {
				masterWithTwo(r)
				r.git("rm", "-q", migrations+"/000001_Init.up.sql", migrations+"/000001_Init.down.sql")
				r.write(pair(migrations, "000003_Sites")...)
			},
			wantFiles: map[string][]string{migrations: {"000002_Widgets.down.sql", "000002_Widgets.up.sql", "000003_Sites.down.sql", "000003_Sites.up.sql"}},
		},
		{
			name: "the branch's own seed files skip the index the default branch took since the branch was cut",
			setup: func(r *repo) {
				r.commit("marker", pair(seed, "000001_Marker")...)
				r.git("checkout", "-q", "-b", "feature")
				r.git("checkout", "-q", "master")
				r.commit("audit on master", pair(seed, "000002_Audit")...)
				r.git("checkout", "-q", "feature")
				r.write(pair(seed, "000002_People")...)
			},
			dirs:        []string{seed},
			editable:    []string{seed},
			wantRenames: []Rename{{Dir: seed, From: "000002_People", To: "000003_People", Files: []string{"up", "down"}}},
			wantFiles:   map[string][]string{seed: {"000001_Marker.down.sql", "000001_Marker.up.sql", "000003_People.down.sql", "000003_People.up.sql"}},
		},
		{
			name: "a committed seed file the default branch's additions would push up is left alone with a note",
			setup: func(r *repo) {
				r.commit("seeds", join(pair(seed, "000001_Marker"), pair(seed, "000002_People"), pair(seed, "000003_Orders"))...)
				r.git("checkout", "-q", "-b", "feature")
				r.git("checkout", "-q", "master")
				r.git("rm", "-q", seed+"/000002_People.up.sql", seed+"/000002_People.down.sql")
				r.git("mv", seed+"/000003_Orders.up.sql", seed+"/000002_Orders.up.sql")
				r.git("mv", seed+"/000003_Orders.down.sql", seed+"/000002_Orders.down.sql")
				r.commit("packed on master", pair(seed, "000003_Zones")...)
				r.git("checkout", "-q", "feature")
				r.write(pair(seed, "000004_Shipments")...)
			},
			dirs:      []string{seed},
			editable:  []string{seed},
			wantNotes: []string{"schema/devseed: master added 000003_Zones since the branch was cut, and 000002_People, which the branch was cut with, would move up past it; a committed seed file only moves down. Merge master into the branch, then run the renumber again"},
			wantFiles: map[string][]string{seed: {"000001_Marker.down.sql", "000001_Marker.up.sql", "000002_People.down.sql", "000002_People.up.sql", "000003_Orders.down.sql", "000003_Orders.up.sql", "000004_Shipments.down.sql", "000004_Shipments.up.sql"}},
		},
		{
			name: "a gap below the seed files the default branch added since the branch was cut is noted; the files above a removed one still move down",
			setup: func(r *repo) {
				r.commit("seeds", join(pair(seed, "000001_Marker"), pair(seed, "000002_People"), pair(seed, "000003_Orders"))...)
				r.git("checkout", "-q", "-b", "feature")
				r.git("checkout", "-q", "master")
				r.commit("audit on master", pair(seed, "000004_Audit")...)
				r.git("checkout", "-q", "feature")
				r.git("rm", "-q", seed+"/000002_People.up.sql", seed+"/000002_People.down.sql")
			},
			dirs:        []string{seed},
			editable:    []string{seed},
			wantRenames: []Rename{{Dir: seed, From: "000003_Orders", To: "000002_Orders", Files: []string{"up", "down"}}},
			wantNotes:   []string{"schema/devseed: a gap stays below 000004_Audit, which master added since the branch was cut; merge master into the branch, then run the renumber again"},
			wantFiles:   map[string][]string{seed: {"000001_Marker.down.sql", "000001_Marker.up.sql", "000002_Orders.down.sql", "000002_Orders.up.sql"}},
		},
		{
			name: "after the merge, the seed files the default branch added move down behind the branch's, in order",
			setup: func(r *repo) {
				r.commit("seeds", join(pair(seed, "000001_Marker"), pair(seed, "000002_People"), pair(seed, "000003_Orders"))...)
				r.git("checkout", "-q", "-b", "feature")
				r.git("checkout", "-q", "master")
				r.commit("audit on master", pair(seed, "000004_Audit")...)
				r.git("checkout", "-q", "feature")
				r.git("rm", "-q", seed+"/000002_People.up.sql", seed+"/000002_People.down.sql")
				r.git("commit", "-q", "-m", "people removed")
				r.git("merge", "-q", "--no-edit", "master")
			},
			dirs:     []string{seed},
			editable: []string{seed},
			wantRenames: []Rename{
				{Dir: seed, From: "000003_Orders", To: "000002_Orders", Files: []string{"up", "down"}},
				{Dir: seed, From: "000004_Audit", To: "000003_Audit", Files: []string{"up", "down"}},
			},
			wantFiles:  map[string][]string{seed: {"000001_Marker.down.sql", "000001_Marker.up.sql", "000002_Orders.down.sql", "000002_Orders.up.sql", "000003_Audit.down.sql", "000003_Audit.up.sql"}},
			wantStaged: "R  schema/devseed/000004_Audit.up.sql -> schema/devseed/000003_Audit.up.sql",
		},
		{
			name: "a directory that does not exist has nothing to renumber",
			setup: func(r *repo) {
				masterWithTwo(r)
			},
			dirs: []string{migrations, seed},
		},
		{
			name: "no default branch to read is refused",
			setup: func(r *repo) {
				masterWithTwo(r)
			},
			branch:  "main",
			wantErr: "no main branch to read here",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := newRepo(t)
			tt.setup(r)
			dirs := tt.dirs
			if dirs == nil {
				dirs = []string{migrations}
			}
			branch := tt.branch
			if branch == "" {
				branch = "master"
			}
			got, err := Renumber(t.Context(), Options{Root: r.root, Dirs: dirs, Branch: branch, Editable: tt.editable})
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Renumber() error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("Renumber() error = %v", err)
			}
			if diff := cmp.Diff(tt.wantRenames, got.Renames); diff != "" {
				t.Errorf("Renames mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantNotes, got.Notes); diff != "" {
				t.Errorf("Notes mismatch (-want +got):\n%s", diff)
			}
			for dir, want := range tt.wantFiles {
				if diff := cmp.Diff(want, list(t, r.root, dir)); diff != "" {
					t.Errorf("%s mismatch (-want +got):\n%s", dir, diff)
				}
			}
			if tt.wantStaged != "" {
				if status := r.git("status", "--porcelain"); !strings.Contains(status, tt.wantStaged) {
					t.Errorf("git status lacks %q:\n%s", tt.wantStaged, status)
				}
			}
			if tt.wantRef != "" && got.Ref != tt.wantRef {
				t.Errorf("Ref = %s, want %s", got.Ref, tt.wantRef)
			}
			if got.Commit == "" {
				t.Error("Commit is empty")
			}
		})
	}
}
