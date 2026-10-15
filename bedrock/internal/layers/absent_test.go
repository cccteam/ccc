package layers

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/cccteam/ccc/bedrock/internal/org"
)

// TestPlanUnapplied: a run with no state object yet is not planned and passes, planned
// after its first apply; the check is the plan identity's own read of the bucket, and
// the other runs are planned as before.
func TestPlanUnapplied(t *testing.T) {
	t.Parallel()

	runs := map[string]tofuRun{
		"2-env tst": {text: succeededPlan},
		"2-env stg": {text: succeededPlan},
		"2-env prd": {text: succeededPlan},
	}
	f := newFlow(t, pullRequestSubs(), deployerAppID, runs)
	f.store.withoutState("2-shr")
	dir := checkout(t, func(*org.Placement) {})
	var out bytes.Buffer
	if err := Plan(t.Context(), f.clients(), request(dir), &out); err != nil {
		t.Fatalf("Plan() = %v, want no error; output:\n%s", err, out.String())
	}
	wantOut := []string{
		"=== plan of 2-shr as imp-shr-gbl-plan@imp-shr-gbl-core-7a8b.iam.gserviceaccount.com ===\nNo state at gs://imp-boot-gbl-state-a1b2/2-shr/default.tfstate: 2-shr has never applied, so there is nothing to plan against; the merge applies it.\n",
		"2-shr: " + firstApply + "\n",
		"2-env tst: Plan: 1 to add, 0 to change, 0 to destroy.\n",
		"Posted the plans on pull request #7 as the deployer app.\n",
	}
	for _, want := range wantOut {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
	for _, line := range f.runner.lines() {
		if strings.HasPrefix(line, "2-shr:") {
			t.Errorf("tofu ran for the unapplied run: %s", line)
		}
	}
	wantIdentities := []string{
		"imp-shr-gbl-plan@imp-shr-gbl-core-7a8b.iam.gserviceaccount.com",
		"imp-tst-gbl-plan@imp-tst-gbl-core-1a2b.iam.gserviceaccount.com",
		"imp-stg-gbl-plan@imp-stg-gbl-core-3c4d.iam.gserviceaccount.com",
		"imp-prd-gbl-plan@imp-prd-gbl-core-5e6f.iam.gserviceaccount.com",
	}
	got := slices.Clone(f.store.identities)
	slices.Sort(got)
	slices.Sort(wantIdentities)
	if !slices.Equal(got, wantIdentities) {
		t.Errorf("the bucket was read as %v, want each run's plan identity %v", got, wantIdentities)
	}
	comments := f.repo.Comments[7]
	var found bool
	for _, c := range comments {
		if strings.HasPrefix(c.Body, markerOf("2-shr")) {
			found = true
			if !strings.Contains(c.Body, firstApply) || !strings.Contains(c.Body, "No state at gs://imp-boot-gbl-state-a1b2/2-shr/default.tfstate") {
				t.Errorf("2-shr's comment:\n%s\nwant the first-apply verdict and the log line", c.Body)
			}
		}
	}
	if !found {
		t.Errorf("no comment for 2-shr among %d", len(comments))
	}
}
