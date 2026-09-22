package advise

import (
	"context"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/impulse/internal/app"
	"github.com/cccteam/ccc/impulse/internal/check"
)

var update = flag.Bool("update", false, "rewrite the golden briefs under testdata")

// fakeExec answers each go run by its command line.
type fakeExec struct {
	t       *testing.T
	answers map[string]fakeAnswer
	calls   []string
}

type fakeAnswer struct {
	out string
	err error
}

func (f *fakeExec) Run(_ context.Context, _ string, _ []string, name string, args ...string) ([]byte, error) {
	line := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, line)
	a, ok := f.answers[line]
	if !ok {
		f.t.Fatalf("unexpected command %q", line)
	}

	return []byte(a.out), a.err
}

var errExit = errors.New("exit status 1")

// TestBrief pins the brief as goldens: the run over a program that raised a warning and a
// finding, with the pinning tests absent, and the skipped generation over a program in the
// runner shape, with the pinning tests present. The root is fixed after collection so the
// golden does not carry this machine's paths.
func TestBrief(t *testing.T) {
	t.Parallel()

	const (
		flatRun    = "go run ./cmd/generate/resourcegenerator -audit"
		joinPath   = "Warning: Beacon resolves its tenant through HarborId, Harbors.SectorId, so its lists scan all of Beacons, every tenant, and no index on Beacons changes that; a table listed at volume carries the tenant key on the row"
		cascade    = "Audit: BeaconLog stores files on BeaconLogs, whose rows the database deletes by cascade, interleaved in Beacons, so a cascade releases none of their objects and the sweep removes them; an application that cares deletes the rows by patch first (README section 13)"
		generation = "2026/09/22 10:00:00 Finished Resource generation in 1s\n"
	)

	tests := []struct {
		name         string
		files        map[string]string
		skipGenerate bool
		answers      map[string]fakeAnswer
		golden       string
		wantCalls    []string
		wantErr      string
	}{
		{
			name:      "a run with a warning and a finding, nothing pinned yet",
			answers:   map[string]fakeAnswer{flatRun: {out: generation + joinPath + "\n" + cascade + "\n"}},
			golden:    "run.golden",
			wantCalls: []string{flatRun},
		},
		{
			name:         "generation skipped, the pinning tests present",
			files:        map[string]string{"cmd/generate/resourcegenerator/warnings_test.go": "package main\n", "pkg/deploy/deploy_test.go": "package deploy_test\n"},
			skipGenerate: true,
			golden:       "skip.golden",
		},
		{
			name:    "a program that does not take -audit",
			answers: map[string]fakeAnswer{flatRun: {out: "flag provided but not defined: -audit\n", err: errExit}},
			wantErr: "cmd/generate/resourcegenerator/main.go: the program does not take -audit; adopt the runner shape (README, impulse audit), or pass --skip-generate to read its pinned warnings instead",
		},
		{
			name:    "a program that fails",
			answers: map[string]fakeAnswer{flatRun: {out: "emulator not running\nexit status 1\n", err: errExit}},
			wantErr: "cmd/generate/resourcegenerator/main.go: go run ./cmd/generate/resourcegenerator -audit failed:\nemulator not running\nexit status 1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			if err := os.CopyFS(root, os.DirFS(filepath.Join("..", "app", "testdata", "flat"))); err != nil {
				t.Fatalf("copy fixture: %v", err)
			}
			for rel, content := range tt.files {
				abs := filepath.Join(root, filepath.FromSlash(rel))
				if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(abs, []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			a, err := app.Discover(root)
			if err != nil {
				t.Fatalf("app.Discover() error = %v", err)
			}
			exec := &fakeExec{t: t, answers: tt.answers}
			brief, err := Collect(context.Background(), &check.Env{App: a, Exec: exec, SkipGenerate: tt.skipGenerate}, tt.skipGenerate)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("Collect() error = %v, want %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("Collect() error = %v", err)
			}
			if diff := cmp.Diff(tt.wantCalls, exec.calls); diff != "" {
				t.Errorf("commands mismatch (-want +got):\n%s", diff)
			}
			brief.App.Root = "/w/lighthouse"
			got := brief.String()

			golden := filepath.Join("testdata", tt.golden)
			if *update {
				if err := os.WriteFile(golden, []byte(got), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("%s: %v (go test ./internal/advise -update writes it)", golden, err)
			}
			if diff := cmp.Diff(string(want), got); diff != "" {
				t.Errorf("brief mismatch (-golden +got); when the change is meant, go test ./internal/advise -update:\n%s", diff)
			}
		})
	}
}

// TestCounts pins the warning and finding counts the command reports.
func TestCounts(t *testing.T) {
	t.Parallel()

	b := &Brief{Programs: []Program{
		{Lines: []string{"Warning: a", "Audit: b"}},
		{Lines: []string{"Warning: c"}},
	}}
	if got := b.Warnings(); got != 2 {
		t.Errorf("Warnings() = %d, want 2", got)
	}
	if got := b.Findings(); got != 1 {
		t.Errorf("Findings() = %d, want 1", got)
	}
}
