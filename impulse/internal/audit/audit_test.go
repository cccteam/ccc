package audit

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/impulse/app"
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

			a, err := app.Discover(filepath.Join("..", "..", "app", "testdata", tt.fixture))
			if err != nil {
				t.Fatalf("app.Discover() error = %v", err)
			}
			exec := &fakeExec{t: t, answers: tt.answers}
			var out strings.Builder
			failed := Run(context.Background(), a, exec, &out)
			if failed != tt.wantFailed {
				t.Errorf("Run() failed = %v, want %v", failed, tt.wantFailed)
			}
			// The fixtures declare no flag and construct no engine, so every run ends in the
			// two empty sections.
			if diff := cmp.Diff(tt.wantOut+featuresHeading+"\n"+noFeaturesLine+"\n"+enginesHeading+"\n"+noEnginesLine+"\n", out.String()); diff != "" {
				t.Errorf("output mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantCalls, exec.calls); diff != "" {
				t.Errorf("commands mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestWriteFeatures(t *testing.T) {
	t.Parallel()

	const (
		declared = "package resources\n\nimport \"github.com/cccteam/ccc/resource\"\n\n// Debriefs lets crews debrief.\nconst Debriefs resource.Feature = \"debriefs\"\n\n// Hyperdrive jumps.\nconst Hyperdrive resource.Feature = \"hyperdrive\"\n"
		gated    = "package resources\n\n// Debrief is gated.\n//\n// @resource\n// @feature(Debriefs)\ntype Debrief struct {\n\tID string `spanner:\"Id\"`\n}\n"
	)
	tests := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{
			name: "no flag",
			want: "feature flags\n  none declared\n",
		},
		{
			name: "every flag with its gates and reads, tests marked, or that nothing uses it",
			files: map[string]string{
				"pkg/resources/features.go":       declared,
				"pkg/resources/debriefs.go":       gated,
				"app/debriefs.go":                 "package app\n\nimport \"example.com/harbor/pkg/resources\"\n\nvar on = resources.Debriefs\n",
				"web/angular.json":                "{}\n",
				"web/console/src/app/nav.spec.ts": "expect(Feature.Debriefs).toBe('debriefs');\n",
			},
			want: "feature flags\n" +
				"  Debriefs (debriefs, pkg/resources/features.go:6): Debriefs lets crews debrief.\n" +
				"    gates Debrief (pkg/resources/debriefs.go:6)\n" +
				"    read in app/debriefs.go:5, web/console/src/app/nav.spec.ts:1 (test)\n" +
				"  Hyperdrive (hyperdrive, pkg/resources/features.go:9): Hyperdrive jumps.\n" +
				"    gates nothing and is read nowhere (the feature-flags check fails on it)\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			files := map[string]string{"go.mod": "module example.com/harbor\n\ngo 1.26.6\n"}
			for rel, content := range tt.files {
				files[rel] = content
			}
			for rel, content := range files {
				if err := os.MkdirAll(filepath.Dir(filepath.Join(root, rel)), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, rel), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			a, err := app.Discover(root)
			if err != nil {
				t.Fatalf("app.Discover() error = %v", err)
			}
			var out strings.Builder
			WriteFeatures(&out, a)
			if diff := cmp.Diff(tt.want, out.String()); diff != "" {
				t.Errorf("WriteFeatures() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestWriteEngines(t *testing.T) {
	t.Parallel()

	const (
		signaled   = "package staff\n\nimport \"github.com/cccteam/access\"\n\nfunc New(signals access.ChangeSignal) error {\n\t_, err := access.New(nil, access.WithChangeSignal(signals))\n\n\treturn err\n}\n"
		unsignaled = "package members\n\nimport \"github.com/cccteam/access\"\n\nfunc New() error {\n\t_, err := access.New(nil)\n\n\treturn err\n}\n"
		forwarding = "package devices\n\nimport \"github.com/cccteam/access\"\n\nfunc New(opts ...access.Option) error {\n\t_, err := access.New(nil, opts...)\n\n\treturn err\n}\n"
	)
	tests := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{
			name: "no engine",
			want: "permission engines\n  none constructed\n",
		},
		{
			name: "every engine with whether it is handed a change signal",
			files: map[string]string{
				"pkg/auth/devices/devices.go": forwarding,
				"pkg/auth/members/members.go": unsignaled,
				"pkg/auth/staff/staff.go":     signaled,
			},
			want: "permission engines\n" +
				"  pkg/auth/devices (pkg/auth/devices/devices.go:6): forwards its options, so a change signal cannot be read here (the change-signal check fails on it)\n" +
				"  pkg/auth/members (pkg/auth/members/members.go:6): no change signal (the change-signal check fails on it)\n" +
				"  pkg/auth/staff (pkg/auth/staff/staff.go:6): handed a change signal (access.WithChangeSignal)\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			files := map[string]string{"go.mod": "module example.com/harbor\n\ngo 1.26.6\n"}
			for rel, content := range tt.files {
				files[rel] = content
			}
			for rel, content := range files {
				if err := os.MkdirAll(filepath.Dir(filepath.Join(root, rel)), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, rel), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			a, err := app.Discover(root)
			if err != nil {
				t.Fatalf("app.Discover() error = %v", err)
			}
			var out strings.Builder
			WriteEngines(&out, a)
			if diff := cmp.Diff(tt.want, out.String()); diff != "" {
				t.Errorf("WriteEngines() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
