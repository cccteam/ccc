package layers

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/deploy"
)

// applyRunner is tofu in ApplyByHand's tests: it records every command by the layer it
// ran in, with whether the data directory was set, prints what a plan and an apply end
// with, and fails at the command named.
type applyRunner struct {
	ran  []string
	fail string
}

func (r *applyRunner) Run(_ context.Context, c deploy.Command, out io.Writer) error {
	line := filepath.Base(c.Dir) + ": " + c.String()
	for _, e := range c.Env {
		if strings.HasPrefix(e, dataDirEnv+"=") {
			line += " [data " + filepath.Base(strings.TrimPrefix(e, dataDirEnv+"=")) + "]"
		}
	}
	r.ran = append(r.ran, line)
	if c.Args[0] == r.fail {
		fmt.Fprintln(out, "Error: something tofu says")

		return errors.New("tofu failed: exit status 1")
	}
	switch c.Args[0] {
	case planCommand:
		fmt.Fprintln(out, "google_cloudbuild_trigger.plan[0]: Refreshing state...\n\nPlan: 0 to add, 2 to change, 0 to destroy.")
	case applyCommand:
		fmt.Fprintln(out, "Apply complete! Resources: 0 added, 2 changed, 0 destroyed.")
	}

	return nil
}

func (*applyRunner) Output(context.Context, deploy.Command, io.Writer) ([]byte, error) {
	return nil, errors.New("ApplyByHand reads nothing back")
}

// TestApplyByHand: the layer is initialized at its own backend with the data directory
// under the working files, planned into a saved plan and applied from it, as the person
// (no identity in the environment), with tofu's output on the command's; the two summary
// lines come back. A failure stops at the command that failed and says which.
func TestApplyByHand(t *testing.T) {
	t.Parallel()

	const applied = "Apply complete! Resources: 0 added, 2 changed, 0 destroyed."
	tests := []struct {
		name      string
		fail      string
		noRunner  bool
		wantRan   []string
		wantPlan  string
		wantApply string
		wantOut   []string
		wantErr   string
	}{
		{
			name:      "the init, the plan into a saved plan and its apply",
			wantRan:   []string{"0-bootstrap: tofu init -input=false -no-color -reconfigure [data data]", "0-bootstrap: tofu plan -input=false -no-color -out=", "0-bootstrap: tofu apply -input=false -no-color "},
			wantPlan:  "Plan: 0 to add, 2 to change, 0 to destroy.",
			wantApply: applied,
			wantOut:   []string{"Refreshing state...", "Plan: 0 to add, 2 to change, 0 to destroy.\n", applied + "\n"},
		},
		{
			name:    "a failing init stops before the plan",
			fail:    "init",
			wantRan: []string{"0-bootstrap: tofu init"},
			wantErr: "the init of 0-bootstrap failed",
		},
		{
			name:    "a failing plan stops before the apply",
			fail:    planCommand,
			wantRan: []string{"0-bootstrap: tofu init", "0-bootstrap: tofu plan"},
			wantErr: "the plan of 0-bootstrap failed",
		},
		{
			name:    "a failing apply",
			fail:    applyCommand,
			wantRan: []string{"0-bootstrap: tofu init", "0-bootstrap: tofu plan", "0-bootstrap: tofu apply"},
			wantErr: "the apply of 0-bootstrap failed",
		},
		{
			name:     "no runner wired",
			noRunner: true,
			wantErr:  "no program runner is wired",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			runner := &applyRunner{fail: tt.fail}
			var exec deploy.Runner = runner
			if tt.noRunner {
				exec = nil
			}
			var out bytes.Buffer
			got, err := ApplyByHand(t.Context(), exec, t.TempDir(), BootstrapLayer, &out)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ApplyByHand() error = %v, want %q", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("ApplyByHand() error = %v; printed:\n%s", err, out.String())
			}
			if len(runner.ran) != len(tt.wantRan) {
				t.Fatalf("tofu ran %d command(s), want %d:\n%s", len(runner.ran), len(tt.wantRan), strings.Join(runner.ran, "\n"))
			}
			for i, want := range tt.wantRan {
				if !strings.HasPrefix(runner.ran[i], want) {
					t.Errorf("command %d = %q, want it to start with %q", i+1, runner.ran[i], want)
				}
			}
			if len(runner.ran) == 3 {
				plan := strings.TrimPrefix(strings.Fields(runner.ran[1])[5], "-out=")
				if !strings.Contains(runner.ran[2], " "+plan+" ") {
					t.Errorf("the apply %q does not apply the saved plan %q", runner.ran[2], plan)
				}
			}
			for _, e := range runner.ran {
				if strings.Contains(e, identityEnv) {
					t.Errorf("a command impersonates an identity: %q", e)
				}
			}
			if tt.wantErr != "" {
				return
			}
			if got.PlanSummary != tt.wantPlan || got.ApplySummary != tt.wantApply {
				t.Errorf("summaries = %q, %q; want %q, %q", got.PlanSummary, got.ApplySummary, tt.wantPlan, tt.wantApply)
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out.String(), want) {
					t.Errorf("printed:\n%s\nwant it to contain %q", out.String(), want)
				}
			}
		})
	}
}
