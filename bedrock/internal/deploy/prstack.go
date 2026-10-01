// prstack.go is a pull request's own environment: the application's stack applied into
// the pull request's state prefix as the apply identity (2-env grants the deploy identity
// the right to impersonate it in every environment; a tag build applies the environment's
// stack as it, envstack.go). Three steps, each its own
// command: the plan is saved, the guard reads it and lets only the pull request's own
// resources through, and the apply applies exactly that plan. bedrock runs tofu for each,
// in the OpenTofu image, from the stack directory of the checkout.

package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-playground/errors/v5"
)

// The files the stack's steps leave in the workspace, and what they read.
const (
	// PlanFile is the saved plan, and PlanJSONFile the plan as tofu show -json writes
	// it, which the guard reads.
	PlanFile     = "pr.plan"
	PlanJSONFile = "pr-plan.json"
	// stackDir is the stack's directory in the checkout.
	stackDir = "infrastructure"
	// applyIdentitySub names the identity the pull request's stack is applied as.
	applyIdentitySub = "_APPLY_IDENTITY"
	// replaceDatabaseFact says the plan replaces the pull request's database, which the
	// apply then tells the pull request about.
	replaceDatabaseFact = "REPLACE_DATABASE"
	// prHostnameFact is the pull request's hostname, from its stack's output.
	prHostnameFact = "PR_HOSTNAME"
	// tagBuildNotice is a pull-request step's answer in a tag build, whose environment's
	// stack the stack steps plan and apply after the image build.
	tagBuildNotice = "Tag build: no pull-request stack; the environment's stack is planned and applied after the image build (deploy stack)."
)

// stack is the pull request's stack in the checkout, driven through tofu as the apply
// identity.
type stack struct {
	run Runner
	dir string
	env []string
	out io.Writer
}

func newStack(clients *Clients, w Workspace, subs map[string]string, out io.Writer) *stack {
	return &stack{
		run: clients.Exec,
		dir: filepath.Join(string(w), stackDir),
		env: []string{"GOOGLE_IMPERSONATE_SERVICE_ACCOUNT=" + subs[applyIdentitySub]},
		out: out,
	}
}

func (s *stack) tofu(ctx context.Context, args ...string) error {
	return s.run.Run(ctx, Command{Dir: s.dir, Env: s.env, Name: "tofu", Args: args}, s.out)
}

func (s *stack) tofuOutput(ctx context.Context, args ...string) ([]byte, error) {
	return s.run.Output(ctx, Command{Dir: s.dir, Env: s.env, Name: "tofu", Args: args}, s.out)
}

// init points the stack at the pull request's state prefix, 3-app/<app>/<env>/pr<N>.
func (s *stack) init(ctx context.Context, subs map[string]string, number string) error {
	prefix := "3-app/" + subs[appSub] + "/" + subs[envSub] + "/pr" + number
	fmt.Fprintf(s.out, "=== Pull request %s: stack at %s as %s ===\n", number, prefix, subs[applyIdentitySub])

	return s.tofu(ctx, "init", "-input=false", "-no-color",
		"-backend-config=prefix="+prefix,
		"-backend-config=impersonate_service_account="+subs[applyIdentitySub])
}

// PlanStack plans the pull request's stack and saves the plan: the destroy on /gcbrun
// down; else the stack, without a database of its own on shared-db, and with the pull
// request's database replaced when resolve decided it is recreated and it exists. The
// plan and its JSON are left in the workspace for the guard and the apply.
func PlanStack(ctx context.Context, clients *Clients, w Workspace, out io.Writer) error {
	env, build, ok, err := pullRequestStep(w, out)
	if err != nil || !ok {
		return err
	}
	subs := build.Substitutions
	s := newStack(clients, w, subs, out)
	if err := s.init(ctx, subs, subs[prNumberSub]); err != nil {
		return err
	}
	plan := filepath.Join(string(w), PlanFile)
	args := []string{"plan", "-input=false", "-no-color", "-out=" + plan, "-var", "environment=" + subs[envSub], "-var", "pull_request=" + subs[prNumberSub]}
	switch {
	case env[downFact] == trueValue:
		fmt.Fprintln(out, "=== /gcbrun down: planning the destroy of the pull request's environment ===")
		args = append(args, "-destroy")
	case env[sharedDBFact] == trueValue:
		fmt.Fprintf(out, "=== /gcbrun shared-db: the site runs against %s's database; no database of its own ===\n", subs[envSub])
		args = append(args, "-var", "shared_database=true")
	case env[reloadDBFact] == trueValue:
		replace, err := s.replaceDatabase(ctx, subs[appSub], env[reloadReasonFact], w)
		if err != nil {
			return err
		}
		args = append(args, replace...)
	}
	if err := s.tofu(ctx, args...); err != nil {
		return err
	}
	shown, err := s.tofuOutput(ctx, "show", "-json", plan)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(string(w), PlanJSONFile), shown, 0o600); err != nil {
		return errors.Wrapf(err, "os.WriteFile(): %s", PlanJSONFile)
	}

	return nil
}

// replaceDatabase is the plan's -replace of the pull request's database when it exists
// (a first build has none yet, and it is created), and records that it does.
func (s *stack) replaceDatabase(ctx context.Context, app, reason string, w Workspace) ([]string, error) {
	address := "google_spanner_database." + app + "[0]"
	// A state not yet written lists nothing, and a listing that fails is a state with
	// no database: the plan then creates it.
	listed, listErr := s.tofuOutput(ctx, "state", "list")
	if exists := listErr == nil && hasLine(string(listed), address); !exists {
		fmt.Fprintf(s.out, "=== The pull request has no database yet to recreate (%s); it is created ===\n", reason)

		return nil, nil
	}
	fmt.Fprintf(s.out, "=== The pull request's database is recreated: %s ===\n", reason)
	if err := w.Append(map[string]string{replaceDatabaseFact: trueValue}); err != nil {
		return nil, err
	}

	return []string{"-replace=" + address}, nil
}

// ApplyStack applies the saved plan. After a destroy nothing deploys (SKIP_DEPLOY); else
// the stack's substitutions output names the pull request's services and jobs and its
// hostname, which go to the environment file for the steps after, and a database the
// build recreated without being asked is said on the pull request.
func ApplyStack(ctx context.Context, clients *Clients, w Workspace, out io.Writer) error {
	env, build, ok, err := pullRequestStep(w, out)
	if err != nil || !ok {
		return err
	}
	subs := build.Substitutions
	s := newStack(clients, w, subs, out)
	if env[downFact] == trueValue {
		if err := deleteBuildJobs(ctx, clients, s, subs, subs[prNumberSub], out); err != nil {
			return err
		}
	}
	if err := s.tofu(ctx, "apply", "-input=false", "-no-color", filepath.Join(string(w), PlanFile)); err != nil {
		return err
	}
	if env[downFact] == trueValue {
		fmt.Fprintln(out, "=== /gcbrun down: the pull request's environment is gone; nothing deploys ===")

		return w.Append(map[string]string{skipDeploy: trueValue})
	}
	facts, err := s.facts(ctx)
	if err != nil {
		return err
	}
	if err := w.Append(facts); err != nil {
		return err
	}
	fmt.Fprintf(out, "The pull request's stack names %s=%s %s=%s", services, facts[services], migrateJobFact, facts[migrateJobFact])
	if facts[jobsJobFact] != "" {
		fmt.Fprintf(out, " %s=%s", jobsJobFact, facts[jobsJobFact])
	}
	fmt.Fprintf(out, " %s=%s\n", prHostnameFact, facts[prHostnameFact])
	if env[replaceDatabaseFact] != trueValue || env[reloadReasonFact] == gcbrun+" reload-db" {
		return nil
	}
	pr, err := speaker(ctx, clients, build, env, false)
	if err != nil || pr == nil {
		return err
	}

	return pr.comment(ctx, fmt.Sprintf("The pull request's database is recreated this build: %s (build %s).", env[reloadReasonFact], build.ID), out)
}

// facts reads the stack's substitutions output from its state into the facts the deploy
// steps read (stackFacts).
func (s *stack) facts(ctx context.Context) (map[string]string, error) {
	data, err := s.tofuOutput(ctx, "output", "-json", "substitutions")
	if err != nil {
		return nil, err
	}

	return stackFacts(data)
}

// stackFacts reads the stack's substitutions output (a map of the trigger's
// substitutions for this pull request) into the facts the deploy steps read: the
// services, the migrate job, the job process's job when the application has one, and the
// hostname.
func stackFacts(data []byte) (map[string]string, error) {
	var subs map[string]string
	if err := json.Unmarshal(data, &subs); err != nil {
		return nil, errors.Wrap(err, "json.Unmarshal(): the stack's substitutions output")
	}
	facts := map[string]string{services: subs["_SERVICES"], migrateJobFact: subs["_MIGRATE_JOB"], prHostnameFact: subs["_HOSTNAME"]}
	if facts[services] == "" || facts[migrateJobFact] == "" {
		return nil, errors.New("the pull request's stack named no services or no migrate job (its substitutions output has no _SERVICES or _MIGRATE_JOB)")
	}
	if job, ok := subs["_JOBS_JOB"]; ok {
		if job == "" {
			return nil, errors.New("the pull request's stack names an empty _JOBS_JOB")
		}
		facts[jobsJobFact] = job
	}

	return facts, nil
}

// pullRequestStep reads the workspace for a step that runs only in a pull-request build;
// ok is false, said on out, in a tag build.
func pullRequestStep(w Workspace, out io.Writer) (env map[string]string, build *Build, ok bool, err error) {
	env, err = w.Environment()
	if err != nil {
		return nil, nil, false, err
	}
	build, err = w.Build()
	if err != nil {
		return nil, nil, false, err
	}
	if build.Substitutions[prNumberSub] == "" {
		fmt.Fprintln(out, tagBuildNotice)

		return nil, nil, false, nil
	}

	return env, build, true, nil
}

// hasLine reports whether text holds line exactly.
func hasLine(text, line string) bool {
	for _, l := range strings.Split(text, "\n") {
		if strings.TrimSpace(l) == line {
			return true
		}
	}

	return false
}
