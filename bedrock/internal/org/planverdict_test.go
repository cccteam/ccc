package org

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// planResult is one run's plan as its tofu plan job leaves it for the verdict.
type planResult struct {
	name    string
	status  int
	plan    string
	errors  string
	outputs string
}

// lacking is the error tofu reports for an object read without the attribute named.
func lacking(attribute string) string {
	return `{"summary": "Unsupported attribute", "detail": "This object does not have an attribute named \"` + attribute + `\"."}`
}

// The plans the cases are made of: 1-org's plan creating the role output 2-env reads,
// 2-env tst's failing on it, and the failures that are not that.
const (
	createsRole   = `{"storage_bucket_creator_role": ["create"], "spanner_admin_role": ["no-op"]}`
	invalidRef    = `{"summary": "Reference to undeclared resource", "detail": "A managed resource \"google_project\" \"x\" has not been declared in the root module."}`
	failedPlan    = "Error: Unsupported attribute\n"
	succeededPlan = "Plan: 1 to add, 0 to change, 0 to destroy.\n"
	planFailed    = "The plan failed; the log says why."
	// noErrors is a plan's errors when it has none; noOutputs its output changes when it
	// failed.
	noErrors  = "[]"
	noOutputs = "{}"
)

// TestPlanVerdict runs the verdict script the layers workflow carries over the results its
// tofu plan jobs leave: a plan that succeeded passes; a plan that failed only on outputs a
// plan before it in layer order creates passes, planned after that run applies; any other
// failure fails, a plan that did not run included.
func TestPlanVerdict(t *testing.T) {
	t.Parallel()

	for _, tool := range []string{"bash", "jq"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("no %s to run the verdict with", tool)
		}
	}
	org := planResult{name: "1-org", plan: succeededPlan, errors: noErrors, outputs: createsRole}
	tst := planResult{name: "2-env tst", status: 1, plan: failedPlan, errors: "[" + lacking("storage_bucket_creator_role") + "]", outputs: noOutputs}
	tests := []struct {
		name    string
		results map[string]planResult
		run     string
		order   []string
		want    string
		pass    bool
	}{
		{
			name:    "a plan that succeeded passes with its summary",
			results: map[string]planResult{"1-org": org},
			run:     "1-org",
			order:   []string{"1-org"},
			want:    "Plan: 1 to add, 0 to change, 0 to destroy.",
			pass:    true,
		},
		{
			name:    "a plan that failed only on an output the plan of a layer before it creates passes, planned after that layer applies",
			results: map[string]planResult{"1-org": org, "2-env-tst": tst},
			run:     "2-env-tst",
			order:   []string{"1-org", "2-env-tst"},
			want:    "planned after 1-org applies: this plan reads the output storage_bucket_creator_role, which 1-org's plan creates, and the merge applies 1-org first.",
			pass:    true,
		},
		{
			name: "outputs of two earlier plans: planned after both apply",
			results: map[string]planResult{
				"1-org":     {name: "1-org", plan: succeededPlan, errors: noErrors, outputs: `{"a": ["create"], "b": ["create"]}`},
				"2-shr":     {name: "2-shr", plan: succeededPlan, errors: noErrors, outputs: `{"c": ["create"]}`},
				"2-env-tst": {name: "2-env tst", status: 1, plan: failedPlan, errors: "[" + lacking("c") + ", " + lacking("a") + ", " + lacking("b") + ", " + lacking("a") + "]", outputs: noOutputs},
			},
			run:   "2-env-tst",
			order: []string{"1-org", "2-shr", "2-env-tst"},
			want:  "planned after 1-org and 2-shr apply: this plan reads outputs their plans create (a and b from 1-org; c from 2-shr), and the merge applies them first.",
			pass:  true,
		},
		{
			name:    "an earlier environment of 2-env counts as the layer before: the merge applies it first",
			results: map[string]planResult{"2-env-tst": {name: "2-env tst", plan: succeededPlan, errors: noErrors, outputs: `{"github_token_secret": ["create"]}`}, "2-env-stg": {name: "2-env stg", status: 1, plan: failedPlan, errors: "[" + lacking("github_token_secret") + "]", outputs: noOutputs}},
			run:     "2-env-stg",
			order:   []string{"2-env-tst", "2-env-stg"},
			want:    "planned after 2-env tst applies: this plan reads the output github_token_secret, which 2-env tst's plan creates, and the merge applies 2-env tst first.",
			pass:    true,
		},
		{
			name:    "a real failure still fails, whatever the earlier plans create",
			results: map[string]planResult{"1-org": org, "2-env-tst": {name: "2-env tst", status: 1, plan: failedPlan, errors: "[" + invalidRef + "]", outputs: noOutputs}},
			run:     "2-env-tst",
			order:   []string{"1-org", "2-env-tst"},
			want:    planFailed,
		},
		{
			name:    "a real failure beside an attribute an earlier plan creates still fails",
			results: map[string]planResult{"1-org": org, "2-env-tst": {name: "2-env tst", status: 1, plan: failedPlan, errors: "[" + lacking("storage_bucket_creator_role") + ", " + invalidRef + "]", outputs: noOutputs}},
			run:     "2-env-tst",
			order:   []string{"1-org", "2-env-tst"},
			want:    planFailed,
		},
		{
			name:    "an attribute no earlier plan creates fails",
			results: map[string]planResult{"1-org": {name: "1-org", plan: succeededPlan, errors: noErrors, outputs: `{"other_role": ["create"]}`}, "2-env-tst": tst},
			run:     "2-env-tst",
			order:   []string{"1-org", "2-env-tst"},
			want:    planFailed,
		},
		{
			name:    "an output the earlier plan changes rather than creates fails: it exists, and the attribute is missing for another reason",
			results: map[string]planResult{"1-org": {name: "1-org", plan: succeededPlan, errors: noErrors, outputs: `{"storage_bucket_creator_role": ["update"]}`}, "2-env-tst": tst},
			run:     "2-env-tst",
			order:   []string{"1-org", "2-env-tst"},
			want:    planFailed,
		},
		{
			name:    "an output a layer the merge applies after this one creates fails",
			results: map[string]planResult{"2-env-tst": tst, "2-env-stg": {name: "2-env stg", plan: succeededPlan, errors: noErrors, outputs: `{"storage_bucket_creator_role": ["create"]}`}},
			run:     "2-env-tst",
			order:   []string{"2-env-tst", "2-env-stg"},
			want:    planFailed,
		},
		{
			name:    "an earlier plan that failed creates nothing",
			results: map[string]planResult{"1-org": {name: "1-org", status: 1, plan: failedPlan, errors: "[" + invalidRef + "]", outputs: noOutputs}, "2-env-tst": tst},
			run:     "2-env-tst",
			order:   []string{"1-org", "2-env-tst"},
			want:    planFailed,
		},
		{
			name:    "a layer the pull request does not touch planned nothing: the failure stands",
			results: map[string]planResult{"2-env-tst": tst},
			run:     "2-env-tst",
			order:   []string{"2-env-tst"},
			want:    planFailed,
		},
		{
			name:    "a failed plan that recorded no error fails",
			results: map[string]planResult{"1-org": org, "2-env-tst": {name: "2-env tst", status: 1, plan: failedPlan, errors: noErrors, outputs: noOutputs}},
			run:     "2-env-tst",
			order:   []string{"1-org", "2-env-tst"},
			want:    planFailed,
		},
		{
			name:    "a run missing from the layer order fails",
			results: map[string]planResult{"1-org": org, "2-env-tst": tst},
			run:     "2-env-tst",
			order:   []string{"1-org"},
			want:    planFailed,
		},
		{
			name:    "a plan that did not run fails",
			results: map[string]planResult{"1-org": org},
			run:     "2-env-tst",
			order:   []string{"1-org", "2-env-tst"},
			want:    "The plan did not run; the log of its tofu plan job says why.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			script := filepath.Join(dir, "plan-verdict.sh")
			writeTestFile(t, script, planVerdictScript)
			results := filepath.Join(dir, "plans")
			for slug, r := range tt.results {
				run := filepath.Join(results, "plan-"+slug)
				writeTestFile(t, filepath.Join(run, "name"), r.name+"\n")
				writeTestFile(t, filepath.Join(run, "status"), strconv.Itoa(r.status)+"\n")
				writeTestFile(t, filepath.Join(run, "plan.txt"), r.plan)
				writeTestFile(t, filepath.Join(run, "errors.json"), r.errors)
				writeTestFile(t, filepath.Join(run, "outputs.json"), r.outputs)
			}
			cmd := exec.CommandContext(t.Context(), "bash", append([]string{script, results, tt.run}, tt.order...)...)
			var stderr strings.Builder
			cmd.Stderr = &stderr
			out, err := cmd.Output()
			var exit *exec.ExitError
			switch {
			case err == nil && !tt.pass:
				t.Errorf("the verdict passed, want it to fail")
			case err != nil && !errors.As(err, &exit):
				t.Fatalf("bash: %v\n%s", err, stderr.String())
			case err != nil && tt.pass:
				t.Errorf("the verdict failed (%v), want it to pass; stderr:\n%s", err, stderr.String())
			}
			if got := strings.TrimSpace(string(out)); got != tt.want {
				t.Errorf("the verdict printed %q, want %q; stderr:\n%s", got, tt.want, stderr.String())
			}
		})
	}
}

// TestWorkflowCarriesPlanVerdict reads the verdict script out of the rendered workflow's
// here-document and finds it the script TestPlanVerdict runs, line for line.
func TestWorkflowCarriesPlanVerdict(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		opens  string
		closes string
	}{
		{name: "the plan job writes the script out and runs it", opens: "cat > \"$RUNNER_TEMP/plan-verdict.sh\" <<'VERDICT'\n", closes: "\n" + verdictIndent + "VERDICT\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			workflow := renderedFile(t, WorkflowFile)
			start := strings.Index(workflow, tt.opens)
			if start < 0 {
				t.Fatalf("%s lacks the here-document %q", WorkflowFile, tt.opens)
			}
			body := workflow[start+len(tt.opens):]
			end := strings.Index(body, tt.closes)
			if end < 0 {
				t.Fatalf("%s's here-document does not close with %q", WorkflowFile, tt.closes)
			}
			lines := strings.Split(body[:end], "\n")
			for i, line := range lines {
				lines[i] = strings.TrimPrefix(line, verdictIndent)
			}
			if got := strings.Join(lines, "\n") + "\n"; got != planVerdictScript {
				t.Errorf("the workflow carries a verdict other than planverdict.sh:\n%s", got)
			}
			if !strings.Contains(body[end:], `summary=$(bash "$RUNNER_TEMP/plan-verdict.sh" "$plans" "$SLUG" $order)`) {
				t.Errorf("%s does not run the verdict over the plans in layer order", WorkflowFile)
			}
		})
	}
}

// writeTestFile writes content to path, its directory made first.
func writeTestFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
