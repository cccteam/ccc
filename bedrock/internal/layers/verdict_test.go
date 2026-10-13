package layers

import (
	"strings"
	"testing"

	"github.com/cccteam/ccc/bedrock/internal/org"
)

// lacking is the error tofu reports for an object read without the attribute named.
func lacking(attribute string) diagnostic {
	return diagnostic{Summary: unsupportedSum, Detail: `This object does not have an attribute named "` + attribute + `".`}
}

// The plans the cases are made of: 1-org's plan creating the role output 2-env reads,
// 2-env tst's failing on it, and the failures that are not that.
var (
	createsRole   = map[string][]string{"storage_bucket_creator_role": {createAction}, "spanner_admin_role": {"no-op"}}
	invalidRef    = diagnostic{Summary: "Reference to undeclared resource", Detail: `A managed resource "google_project" "x" has not been declared in the root module.`}
	failedPlan    = "Error: Unsupported attribute\n"
	succeededPlan = "Plan: 1 to add, 0 to change, 0 to destroy.\n"
	// outputsOnlyPlan is what OpenTofu prints for a plan that changes outputs alone: no Plan
	// line and no "No changes.".
	outputsOnlyPlan = "OpenTofu will perform the following actions:\n\nChanges to Outputs:\n  + storage_bucket_creator_role = \"projects/x/roles/y\"\n\nYou can apply this plan to save these new output values to the OpenTofu state, without changing any real infrastructure.\n"
)

// succeeded is a run's plan that succeeded with the text and the output changes.
func succeeded(name, text string, outputs map[string][]string) *planResult {
	return &planResult{run: org.LayerRun{Name: name}, ran: true, ok: true, text: text, outputs: outputs}
}

// failed is a run's plan that failed with the errors.
func failed(name string, diagnostics ...diagnostic) *planResult {
	return &planResult{run: org.LayerRun{Name: name}, ran: true, text: failedPlan, diagnostics: diagnostics}
}

// TestVerdicts judges the plans of a pull request, in layer order: a plan that succeeded
// passes with its summary; a plan that failed only on outputs a plan before it in layer
// order creates passes, planned after that run applies; any other failure fails, a plan
// that did not run included.
func TestVerdicts(t *testing.T) {
	t.Parallel()

	org1 := succeeded("1-org", succeededPlan, createsRole)
	tst := failed("2-env tst", lacking("storage_bucket_creator_role"))
	tests := []struct {
		name    string
		results []*planResult
		// want is the last result's verdict.
		want string
		pass bool
	}{
		{
			name:    "a plan that succeeded passes with its summary",
			results: []*planResult{org1},
			want:    "Plan: 1 to add, 0 to change, 0 to destroy.",
			pass:    true,
		},
		{
			name:    "a plan with no changes passes with that line",
			results: []*planResult{succeeded("2-shr", "No changes. Your infrastructure matches the configuration.\n", nil)},
			want:    "No changes. Your infrastructure matches the configuration.",
			pass:    true,
		},
		{
			name:    "a plan that changes outputs alone passes, saying so, since OpenTofu prints no Plan line for it",
			results: []*planResult{succeeded("1-org", outputsOnlyPlan, createsRole)},
			want:    outputsOnly,
			pass:    true,
		},
		{
			name:    "a plan that succeeded without a summary line says so",
			results: []*planResult{succeeded("1-org", "OpenTofu has been successfully initialized!\n", nil)},
			want:    noSummary,
			pass:    true,
		},
		{
			name:    "a plan that failed only on an output the plan of a layer before it creates passes, planned after that layer applies",
			results: []*planResult{org1, tst},
			want:    "planned after 1-org applies: this plan reads the output storage_bucket_creator_role, which 1-org's plan creates, and the merge applies 1-org first.",
			pass:    true,
		},
		{
			name: "outputs of two earlier plans: planned after both apply",
			results: []*planResult{
				succeeded("1-org", succeededPlan, map[string][]string{"a": {createAction}, "b": {createAction}}),
				succeeded("2-shr", succeededPlan, map[string][]string{"c": {createAction}}),
				failed("2-env tst", lacking("c"), lacking("a"), lacking("b"), lacking("a")),
			},
			want: "planned after 1-org and 2-shr apply: this plan reads outputs their plans create (a and b from 1-org; c from 2-shr), and the merge applies them first.",
			pass: true,
		},
		{
			name: "several outputs of one earlier plan: the outputs, as a list",
			results: []*planResult{
				succeeded("1-org", succeededPlan, map[string][]string{"a": {createAction}, "b": {createAction}, "c": {createAction}}),
				failed("2-env tst", lacking("c"), lacking("a"), lacking("b")),
			},
			want: "planned after 1-org applies: this plan reads the outputs a, b and c, which 1-org's plan creates, and the merge applies 1-org first.",
			pass: true,
		},
		{
			name: "an earlier environment of 2-env counts as the layer before: the merge applies it first",
			results: []*planResult{
				succeeded("2-env tst", succeededPlan, map[string][]string{"github_token_secret": {createAction}}),
				failed("2-env stg", lacking("github_token_secret")),
			},
			want: "planned after 2-env tst applies: this plan reads the output github_token_secret, which 2-env tst's plan creates, and the merge applies 2-env tst first.",
			pass: true,
		},
		{
			name:    "a real failure still fails, whatever the earlier plans create",
			results: []*planResult{org1, failed("2-env tst", invalidRef)},
			want:    planFailed,
		},
		{
			name:    "a real failure beside an attribute an earlier plan creates still fails",
			results: []*planResult{org1, failed("2-env tst", lacking("storage_bucket_creator_role"), invalidRef)},
			want:    planFailed,
		},
		{
			name:    "an attribute no earlier plan creates fails",
			results: []*planResult{succeeded("1-org", succeededPlan, map[string][]string{"other_role": {createAction}}), tst},
			want:    planFailed,
		},
		{
			name:    "an output the earlier plan changes rather than creates fails: it exists, and the attribute is missing for another reason",
			results: []*planResult{succeeded("1-org", succeededPlan, map[string][]string{"storage_bucket_creator_role": {"update"}}), tst},
			want:    planFailed,
		},
		{
			name:    "an output a layer the merge applies after this one creates fails",
			results: []*planResult{tst, succeeded("2-env stg", succeededPlan, map[string][]string{"storage_bucket_creator_role": {createAction}})},
			want:    "Plan: 1 to add, 0 to change, 0 to destroy.",
			pass:    true,
		},
		{
			name:    "an earlier plan that failed creates nothing",
			results: []*planResult{failed("1-org", invalidRef), tst},
			want:    planFailed,
		},
		{
			name:    "a layer the pull request does not touch planned nothing: the failure stands",
			results: []*planResult{tst},
			want:    planFailed,
		},
		{
			name:    "a failed plan that recorded no error fails",
			results: []*planResult{org1, failed("2-env tst")},
			want:    planFailed,
		},
		{
			name:    "a plan that did not run fails",
			results: []*planResult{org1, {run: org.LayerRun{Name: "2-env tst"}}},
			want:    planNotRun,
		},
		{
			name:    "a plan whose run failed on its own fails",
			results: []*planResult{{run: org.LayerRun{Name: "2-env tst"}, ran: true, ok: true, text: succeededPlan, err: errorf("os.MkdirTemp(): no space")}},
			want:    planNotRun,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := verdicts(tt.results)
			if len(got) != len(tt.results) {
				t.Fatalf("verdicts() gave %d verdicts for %d results", len(got), len(tt.results))
			}
			last := got[len(got)-1]
			if last.summary != tt.want {
				t.Errorf("the verdict is %q, want %q", last.summary, tt.want)
			}
			if last.pass != tt.pass {
				t.Errorf("the verdict passes = %t, want %t", last.pass, tt.pass)
			}
		})
	}
}

// TestVerdictsInOrder judges every plan, the earlier ones by what came before them
// alone: 2-env tst's verdict stands on 1-org's creation, and 2-env stg's on both.
func TestVerdictsInOrder(t *testing.T) {
	t.Parallel()

	results := []*planResult{
		succeeded("1-org", succeededPlan, createsRole),
		failed("2-env tst", lacking("storage_bucket_creator_role")),
		failed("2-env stg", lacking("storage_bucket_creator_role"), lacking("records_bucket")),
	}
	want := []string{
		"Plan: 1 to add, 0 to change, 0 to destroy.",
		"planned after 1-org applies: this plan reads the output storage_bucket_creator_role, which 1-org's plan creates, and the merge applies 1-org first.",
		planFailed,
	}
	var got []string
	for _, v := range verdicts(results) {
		got = append(got, v.summary)
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("verdicts() =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
