package check

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/impulse/internal/app"
)

// fixture discovers one of the app package's fixture applications.
func fixture(t *testing.T, name string) *app.App {
	t.Helper()

	a, err := app.Discover(filepath.Join("..", "app", "testdata", name))
	if err != nil {
		t.Fatalf("app.Discover(%s) error = %v", name, err)
	}

	return a
}

// fixtureCopy copies a fixture application into a temporary directory and discovers it,
// for checks that modify files under --fix.
func fixtureCopy(t *testing.T, name string) *app.App {
	t.Helper()

	dst := t.TempDir()
	if err := os.CopyFS(dst, os.DirFS(filepath.Join("..", "app", "testdata", name))); err != nil {
		t.Fatalf("copy fixture: %v", err)
	}

	a, err := app.Discover(dst)
	if err != nil {
		t.Fatalf("app.Discover(copy of %s) error = %v", name, err)
	}

	return a
}

// fakeExec answers commands from a table keyed by the command line.
type fakeExec struct {
	t       *testing.T
	answers map[string]fakeAnswer
	calls   []string
	envs    [][]string
}

type fakeAnswer struct {
	out string
	err error
	// run, when set, stands in for the command's side effects on the tree.
	run func()
}

func (f *fakeExec) Run(_ context.Context, _ string, extraEnv []string, name string, args ...string) ([]byte, error) {
	line := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, line)
	f.envs = append(f.envs, extraEnv)
	a, ok := f.answers[line]
	if !ok {
		f.t.Fatalf("unexpected command %q", line)
	}
	if a.run != nil {
		a.run()
	}

	return []byte(a.out), a.err
}

func TestSelect(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		only    []string
		want    []string
		wantErr string
	}{
		{name: "all", want: []string{
			"generator-program", "options", "tenancy-wired", "outlet-wired", "sites-wired", "auth-wired", "auths-wired", "skipauth", "emulator-version", "prettier-ignore", "eslint-ignore",
			"package-manager",
			"paging", "rpc-execute", "multi-site", "env-template", "pins", "gowork-off", "regen",
		}},
		{name: "subset keeps run order", only: []string{regen{}.Name(), pins{}.Name()}, want: []string{pins{}.Name(), regen{}.Name()}},
		{name: "unknown", only: []string{"nope"}, wantErr: `unknown check "nope"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			checks, err := Select(tt.only)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Select() error = %v, want %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("Select() error = %v", err)
			}
			var names []string
			for _, c := range checks {
				names = append(names, c.Name())
				if c.Describe() == "" {
					t.Errorf("%s has no description", c.Name())
				}
			}
			if diff := cmp.Diff(tt.want, names); diff != "" {
				t.Errorf("Select() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestReportAndFailed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		results    []Result
		wantFailed bool
		wantOut    string
	}{
		{
			name:       "pass and skip",
			results:    []Result{pass("a", "fine"), skip("longer", "nothing to do")},
			wantFailed: false,
			wantOut:    "PASS  a       fine\nSKIP  longer  nothing to do\n",
		},
		{
			name:       "fail with details",
			results:    []Result{warn("w", "hmm", "d1"), fail("f", "bad", "d2", "d3")},
			wantFailed: true,
			wantOut:    "WARN  w  hmm\n      d1\nFAIL  f  bad\n      d2\n      d3\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			Report(&buf, tt.results)
			if diff := cmp.Diff(tt.wantOut, buf.String()); diff != "" {
				t.Errorf("Report() mismatch (-want +got):\n%s", diff)
			}
			if got := Failed(tt.results); got != tt.wantFailed {
				t.Errorf("Failed() = %v, want %v", got, tt.wantFailed)
			}
		})
	}
}

func TestOutputLines(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		out  string
		keep int
		want []string
	}{
		{name: "empty", out: "", keep: 3, want: nil},
		{name: "short", out: "a\nb\n", keep: 3, want: []string{"a", "b"}},
		{name: "trimmed to tail", out: "a\nb\nc\nd\n", keep: 2, want: []string{"... 2 earlier lines omitted", "c", "d"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(tt.want, outputLines([]byte(tt.out), tt.keep)); diff != "" {
				t.Errorf("outputLines() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
