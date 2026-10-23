package layers

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// The 1-org state of an organization with two applications: harbor's repository, its
// rulesets and operations environments among beacon's and the layer's own.
var retireState = []string{
	`module.project["net"].google_project.project`,
	`github_actions_runner_group.applications[0]`,
	`github_repository.app["beacon"]`,
	`github_repository.app["harbor"]`,
	`github_repository.infrastructure`,
	`github_repository_ruleset.release_tags["beacon"]`,
	`github_repository_ruleset.release_tags["harbor"]`,
	`github_repository_ruleset.branch["harbor:default"]`,
	`github_repository_ruleset.branch["harbor:patch"]`,
	`github_repository_ruleset.branch["harborside:default"]`,
	`github_repository_ruleset.infrastructure`,
	`github_repository_environment.operations["harbor-tst"]`,
	`github_repository_environment_deployment_policy.default_branch["harbor-tst"]`,
	`google_service_account.apply["harbor"]`,
}

func TestRetireRepository(t *testing.T) {
	t.Parallel()

	r := &handRunner{runs: map[string]*handRun{orgLayer: {stateList: retireState}}}
	var out bytes.Buffer
	taken, err := RetireRepository(context.Background(), r, t.TempDir(), "harbor", "tok", &out)
	if err != nil {
		t.Fatalf("RetireRepository() = %v", err)
	}
	wantTaken := []string{
		`github_repository.app["harbor"]`,
		`github_repository_ruleset.release_tags["harbor"]`,
		`github_repository_ruleset.branch["harbor:default"]`,
		`github_repository_ruleset.branch["harbor:patch"]`,
		`github_repository_environment.operations["harbor-tst"]`,
		`github_repository_environment_deployment_policy.default_branch["harbor-tst"]`,
	}
	if strings.Join(taken, "\n") != strings.Join(wantTaken, "\n") {
		t.Errorf("taken:\n%s\nwant:\n%s", strings.Join(taken, "\n"), strings.Join(wantTaken, "\n"))
	}
	// The init at the layer's own backend and the listing, then one removal per address,
	// each in 1-org with the GitHub token and the run's data directory.
	want := []string{"1-org: tofu init -input=false -no-color -reconfigure [token] [data 1-org.data]", "1-org: tofu state list [token] [data 1-org.data]"}
	for _, address := range wantTaken {
		want = append(want, "1-org: tofu state rm "+address+" [token] [data 1-org.data]")
	}
	if strings.Join(r.ran, "\n") != strings.Join(want, "\n") {
		t.Errorf("ran:\n%s\nwant:\n%s", strings.Join(r.ran, "\n"), strings.Join(want, "\n"))
	}
}

func TestRetireRepositoryNothingHeld(t *testing.T) {
	t.Parallel()

	r := &handRunner{runs: map[string]*handRun{orgLayer: {stateList: []string{`github_repository.app["beacon"]`, `google_service_account.apply["harbor"]`}}}}
	taken, err := RetireRepository(context.Background(), r, t.TempDir(), "harbor", "tok", &bytes.Buffer{})
	if err != nil {
		t.Fatalf("RetireRepository() = %v", err)
	}
	if len(taken) != 0 {
		t.Errorf("taken = %v, want none: the identity is the layer's to destroy, the other repository is beacon's", taken)
	}
	if len(r.ran) != 2 {
		t.Errorf("ran %d command(s), want the init and the listing alone:\n%s", len(r.ran), strings.Join(r.ran, "\n"))
	}
}

func TestRetireRepositoryNoRunner(t *testing.T) {
	t.Parallel()

	if _, err := RetireRepository(context.Background(), nil, t.TempDir(), "harbor", "tok", &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "no program runner is wired") {
		t.Errorf("RetireRepository() = %v, want the refusal of a nil runner", err)
	}
}
