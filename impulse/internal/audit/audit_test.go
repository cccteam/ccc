package audit

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/impulse/internal/app"
)

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

func TestRun(t *testing.T) {
	t.Parallel()

	const (
		flatRun    = "go run ./cmd/generate/resourcegenerator -audit"
		pilotsRun  = "go run ./cmd/generate/resourcegenerator_pilots -audit"
		sharedRun  = "go run ./cmd/generate/resourcegenerator_shared -audit"
		tugsRun    = "go run ./cmd/generate/resourcegenerator_tugs -audit"
		cascade    = "Audit: RefitTask stores files on RefitTasks, whose rows the database deletes by cascade, interleaved in Refits, so a cascade releases none of their objects and the sweep removes them; an application that cares deletes the rows by patch first (README section 13)"
		joinPath   = "Warning: Ship resolves its tenant through HangarId, Hangars.SectorId, so its lists scan all of Ships, every tenant, and no index on Ships changes that; a table listed at volume carries the tenant key on the row"
		generation = "2026/09/22 10:00:00 Finished Resource generation in 1s\n"
	)

	tests := []struct {
		name       string
		fixture    string
		answers    map[string]fakeAnswer
		wantFailed bool
		wantOut    string
		wantCalls  []string
	}{
		{
			name:      "no findings, one program run from the directive's target",
			fixture:   "flat",
			answers:   map[string]fakeAnswer{flatRun: {out: generation}},
			wantOut:   "cmd/generate/resourcegenerator/main.go (go run ./cmd/generate/resourcegenerator -audit)\n  no findings\n",
			wantCalls: []string{flatRun},
		},
		{
			name:      "warnings and findings, in the order printed",
			fixture:   "flat",
			answers:   map[string]fakeAnswer{flatRun: {out: generation + joinPath + "\n" + generation + cascade + "\n"}},
			wantOut:   "cmd/generate/resourcegenerator/main.go (go run ./cmd/generate/resourcegenerator -audit)\n  " + joinPath + "\n  " + cascade + "\n",
			wantCalls: []string{flatRun},
		},
		{
			name:    "every program of the sites layout, each by its own directory",
			fixture: "sites",
			answers: map[string]fakeAnswer{pilotsRun: {out: generation}, sharedRun: {out: cascade + "\n"}, tugsRun: {out: generation}},
			wantOut: "cmd/generate/resourcegenerator_pilots/main.go (go run ./cmd/generate/resourcegenerator_pilots -audit)\n  no findings\n" +
				"cmd/generate/resourcegenerator_shared/main.go (go run ./cmd/generate/resourcegenerator_shared -audit)\n  " + cascade + "\n" +
				"cmd/generate/resourcegenerator_tugs/main.go (go run ./cmd/generate/resourcegenerator_tugs -audit)\n  no findings\n",
			wantCalls: []string{pilotsRun, sharedRun, tugsRun},
		},
		{
			name:       "a program that does not take -audit",
			fixture:    "flat",
			answers:    map[string]fakeAnswer{flatRun: {out: "flag provided but not defined: -audit\nUsage of /tmp/go-build/b001/exe/resourcegenerator:\nexit status 2\n", err: errExit}},
			wantFailed: true,
			wantOut:    "cmd/generate/resourcegenerator/main.go (go run ./cmd/generate/resourcegenerator -audit)\n  FAIL  the program does not take -audit; adopt the runner shape (README, impulse audit)\n",
			wantCalls:  []string{flatRun},
		},
		{
			name:       "a program that fails",
			fixture:    "flat",
			answers:    map[string]fakeAnswer{flatRun: {out: "emulator not running\nexit status 1\n", err: errExit}},
			wantFailed: true,
			wantOut:    "cmd/generate/resourcegenerator/main.go (go run ./cmd/generate/resourcegenerator -audit)\n  FAIL  go run ./cmd/generate/resourcegenerator -audit failed\n        emulator not running\n        exit status 1\n",
			wantCalls:  []string{flatRun},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a, err := app.Discover(filepath.Join("..", "app", "testdata", tt.fixture))
			if err != nil {
				t.Fatalf("app.Discover() error = %v", err)
			}
			exec := &fakeExec{t: t, answers: tt.answers}
			var out strings.Builder
			failed := Run(context.Background(), a, exec, &out)
			if failed != tt.wantFailed {
				t.Errorf("Run() failed = %v, want %v", failed, tt.wantFailed)
			}
			if diff := cmp.Diff(tt.wantOut, out.String()); diff != "" {
				t.Errorf("output mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantCalls, exec.calls); diff != "" {
				t.Errorf("commands mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
