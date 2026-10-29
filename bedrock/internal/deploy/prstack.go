// prstack.go is a pull request's own environment: the application's stack applied into
// the pull request's state prefix as the apply identity. It runs in the stack's pr-stack
// trigger (deploy pr-stack, on the /gcbrun comment, beside the preview build) and in its
// check-and-plan trigger (deploy pr-stack --if-exists, after the environments' plans,
// when the pull request already has a preview), never from cloudbuild.yaml, which the
// pull request controls: the guard on the plan is only real when the pull request cannot
// edit the steps around it. One command does the whole of it: the plan is saved, the
// guard reads it and lets only the pull request's own resources through, the apply
// applies exactly that plan (a destroy on /gcbrun down), and what the stack now says is
// written to the records bucket for the preview build (outputs.go). bedrock runs tofu
// for each step, in the OpenTofu image, from the stack directory of the checkout, as the
// build's own identity, which is the apply identity in both triggers.

package deploy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/derive"
	"github.com/cccteam/ccc/bedrock/internal/render"
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
	// prHostnameFact is the pull request's hostname, from its stack's output, and
	// prHostnamesFact each site's (site=hostname) for a sites-layout application.
	prHostnameFact  = "PR_HOSTNAME"
	prHostnamesFact = "PR_HOSTNAMES"
	// tagBuildNotice is a pull-request step's answer in a tag build, whose environment's
	// stack the stack steps plan and apply after the image build.
	tagBuildNotice = "Tag build: no pull-request stack; the environment's stack is planned and applied after the image build (deploy stack)."
)

// stack is a stack in the checkout, driven through tofu as an identity: impersonated,
// when the build runs as another identity (the pipeline's deploy identity, which acts as
// the apply identity in a tag build and as each environment's plan identity in the
// trigger-held job), or the build's own, when the build already runs as it (a
// trigger-held job, which is the apply identity).
type stack struct {
	run         Runner
	dir         string
	identity    string
	impersonate bool
	out         io.Writer
	// clients is what the apply step opens beyond tofu (Firestore, as the apply
	// identity) in a restore run.
	clients *Clients
	// dataDir is the directory tofu keeps its providers and backend cache in
	// (TF_DATA_DIR) when a command by hand drives several stacks of a checkout in one
	// run; empty in a build, which has one stack and tofu's default.
	dataDir string
}

// The flags every tofu command runs with: a variable (plan, destroy, import), no
// prompts, no color in the log.
const (
	varFlag     = "-var"
	noInputFlag = "-input=false"
	noColorFlag = "-no-color"
	// lockTimeoutFlag is how long the pull request's plan and apply wait for the state
	// lock. Two builds work the same stack, the check's refresh on a push and the /gcbrun
	// build on a comment, and the second takes its turn after the first instead of
	// failing on the lock, which tofu gives up on at once by default. Two thirds of the
	// stack step's 45 minutes, leaving the plan and apply their own time.
	lockTimeoutFlag = "-lock-timeout=30m"
)

// newStack is the pull request's stack as the apply identity: impersonated when the
// build runs as another identity, as itself when the build is the apply identity.
func newStack(clients *Clients, w Workspace, build *Build, out io.Writer) *stack {
	identity := build.Substitutions[applyIdentitySub]

	return &stack{
		run:         clients.Exec,
		dir:         filepath.Join(string(w), stackDir),
		identity:    identity,
		impersonate: !build.runsAs(identity),
		out:         out,
		clients:     clients,
	}
}

// env is what tofu runs with: the identity to impersonate, the data directory of a run
// by hand, or nothing.
func (s *stack) env() []string {
	var env []string
	if s.impersonate {
		env = append(env, "GOOGLE_IMPERSONATE_SERVICE_ACCOUNT="+s.identity)
	}
	if s.dataDir != "" {
		env = append(env, "TF_DATA_DIR="+s.dataDir)
	}

	return env
}

// backendArgs point tofu's state at the prefix, impersonating the identity when the
// build is not it.
func (s *stack) backendArgs(prefix string) []string {
	args := []string{"init", noInputFlag, noColorFlag, "-backend-config=prefix=" + prefix}
	if s.impersonate {
		args = append(args, "-backend-config=impersonate_service_account="+s.identity)
	}

	return args
}

// tofuBinary is the OpenTofu binary the stack runs; planCommand is its plan subcommand.
const (
	tofuBinary  = "tofu"
	planCommand = "plan"
)

func (s *stack) tofu(ctx context.Context, args ...string) error {
	return s.run.Run(ctx, Command{Dir: s.dir, Env: s.env(), Name: tofuBinary, Args: args}, s.out)
}

// tofuCaptured runs tofu with its output on the log and answers a copy of it, for a
// command whose text the run reads back after a failure.
func (s *stack) tofuCaptured(ctx context.Context, args ...string) (string, error) {
	var captured bytes.Buffer
	err := s.run.Run(ctx, Command{Dir: s.dir, Env: s.env(), Name: tofuBinary, Args: args}, io.MultiWriter(s.out, &captured))

	return captured.String(), err
}

func (s *stack) tofuOutput(ctx context.Context, args ...string) ([]byte, error) {
	return s.run.Output(ctx, Command{Dir: s.dir, Env: s.env(), Name: tofuBinary, Args: args}, s.out)
}

// pullRequestPrefix is the pull request's state prefix, 3-app/<app>/<env>/pr<N>.
func pullRequestPrefix(subs map[string]string, number string) string {
	return statePrefix(subs[appSub], subs[envSub]) + "/pr" + number
}

// init points the stack at the pull request's state prefix, afresh.
func (s *stack) init(ctx context.Context, subs map[string]string, number string) error {
	if err := os.RemoveAll(filepath.Join(s.dir, ".terraform")); err != nil {
		return errors.Wrap(err, "os.RemoveAll()")
	}
	prefix := pullRequestPrefix(subs, number)
	fmt.Fprintf(s.out, "=== Pull request %s: stack at %s as %s ===\n", number, prefix, s.identity)

	return s.tofu(ctx, s.backendArgs(prefix)...)
}

// initAt points the stack at the prefix in the run's data directory, afresh: one data
// directory serves every stack of a run by hand, and -reconfigure moves its backend from
// one prefix to the next. As the person, no identity is impersonated.
func (s *stack) initAt(ctx context.Context, prefix string) error {
	return s.tofu(ctx, "init", noInputFlag, noColorFlag, reconfigure, backendPrefix+prefix)
}

// PullRequestStack is the pull request's stack job (deploy pr-stack): the build read
// through the Cloud Build API, the latest /gcbrun comment read with the repository's
// token, the pull request's stack planned, guarded and applied into its own prefix (or
// destroyed on /gcbrun down), and the outcome written to the records bucket for the
// preview build. With ifExists (deploy pr-stack --if-exists, the check-and-plan trigger's
// last step), the stack is applied again only when the pull request has a preview
// standing, so a push keeps it current, and a reload-db in the latest comment is left to
// the build that comment ran. A failure is written for the preview build too, with its
// reason, so that build stops at once rather than waiting.
func PullRequestStack(ctx context.Context, clients *Clients, w Workspace, req *BuildRequest, ifExists bool, out io.Writer) error {
	build, builds, err := readBuild(ctx, clients, req)
	if err != nil {
		return err
	}
	if err := pullRequestBuild(build); err != nil {
		return err
	}
	trigger, steps := render.StackTrigger, render.StackTriggerSteps
	if ifExists {
		trigger, steps = render.CheckTrigger, render.CheckTriggerSteps
	}
	staleSteps(build, trigger, steps, out)
	j := &stackJob{clients: clients, builds: builds, w: w, build: build, req: req, refresh: ifExists, out: out}
	outputs := newStackOutputs(build)
	done, err := j.run(ctx, outputs)
	if err != nil {
		outputs.fail(err)
		if werr := writeStackOutputs(ctx, clients.Storage, build, outputs, clients.now(), out); werr != nil {
			fmt.Fprintf(out, "The failure could not be written for the preview build (%v); that build waits its full time instead.\n", werr)
		}

		return err
	}
	if !done {
		return nil
	}

	return writeStackOutputs(ctx, clients.Storage, build, outputs, clients.now(), out)
}

// stackJob is one run of the pull request's stack job.
type stackJob struct {
	clients *Clients
	builds  Builds
	w       Workspace
	build   *Build
	req     *BuildRequest
	refresh bool
	out     io.Writer
}

// run does the job and fills the outputs; done is false when a refresh found nothing to
// refresh, and then nothing is written.
func (j *stackJob) run(ctx context.Context, outputs *StackOutputs) (bool, error) {
	f, err := j.instruction(ctx)
	if err != nil {
		return false, err
	}
	outputs.Comment = f.CommentID
	if j.refresh && f.Down {
		fmt.Fprintf(j.out, "The latest %s comment says down: the pull request's environment is gone, or going; nothing to refresh.\n", gcbrun)

		return false, nil
	}
	subs := j.build.Substitutions
	s := newStack(j.clients, j.w, j.build, j.out)
	if err := s.init(ctx, subs, subs[prNumberSub]); err != nil {
		return false, err
	}
	if j.refresh {
		standing, err := j.previewStanding(ctx, s)
		if err != nil || !standing {
			return false, err
		}
	}
	if err := f.staleDatabase(ctx, j.clients.Storage, string(j.w), j.out); err != nil {
		return false, err
	}
	env := map[string]string{
		githubTokenFact: f.Token, downFact: flag(f.Down), sharedDBFact: flag(f.SharedDB),
		reloadDBFact: flag(f.ReloadDB), reloadReasonFact: f.ReloadReason,
	}
	// The generated secrets' pins, read from tst's containers as the apply identity:
	// the pull request's stack mounts tst's newest versions and adds none.
	p, err := checkoutPlacement(j.w, "the prefix is named")
	if err != nil {
		return false, err
	}
	m, err := checkoutModel(j.w, p)
	if err != nil {
		return false, err
	}
	if !f.SharedDB {
		refusal, err := guardSpannerCapacity(ctx, j.clients, pullRequestTarget(s.identity, s.impersonate, p, subs[appSub], subs[envSub], subs[prNumberSub]), j.out)
		if err != nil {
			return false, err
		}
		if refusal != "" {
			return false, refuse(ctx, j.clients, j.build, env, fmt.Sprintf("The pull request's stack stopped before its plan (build %s): %s", j.build.ID, refusal), errors.New(refusal), j.out)
		}
	}
	pins, err := generatedPins(ctx, j.clients, s.identity, s.impersonate, m, subs[appSub], subs[envSub], subs[projectSub], false, j.out)
	if err != nil {
		return false, err
	}
	if err := s.plan(ctx, env, subs, generatedArgs(pins)); err != nil {
		return false, err
	}
	if err := guardPlan(ctx, j.clients, j.w, j.build, env, j.out); err != nil {
		return false, err
	}
	facts, templates, err := s.apply(ctx, j.build, env)
	if err != nil {
		return false, err
	}
	if f.Down {
		outputs.Status = StackDestroyed

		return true, nil
	}
	outputs.applied(f, facts)
	outputs.RevisionTemplates = templates
	if env[replaceDatabaseFact] == trueValue {
		outputs.DatabaseRecreated = f.ReloadReason
	}

	return true, nil
}

// instruction reads the latest /gcbrun comment with the repository's token: its options
// (shared-db, reload-db, down) and its id, which the preview build matches the outputs
// by. A refresh has no comment of its own: the latest one's mode carries over, no
// comment at all means a plain stack, and a reload-db there is that build's, not the
// refresh's.
func (j *stackJob) instruction(ctx context.Context) (*Facts, error) {
	subs := j.build.Substitutions
	f := &Facts{PullRequest: subs[prNumberSub], Environment: subs[envSub], Substitutions: subs, RunMigrations: true}
	token, err := repositoryToken(ctx, j.builds, subs, j.req.Project, j.req.Location)
	if err != nil {
		return nil, err
	}
	f.Token = token
	repository, err := repositoryFullName(subs)
	if err != nil {
		return nil, err
	}
	number, err := strconv.Atoi(f.PullRequest)
	if err != nil || number <= 0 {
		return nil, errors.Newf("%s %q is not a pull request number", prNumberSub, f.PullRequest)
	}
	all, err := j.clients.Comments(ctx, token, repository, number)
	if err != nil {
		return nil, err
	}
	switch found := f.latestComment(all); {
	case !found && j.refresh:
		fmt.Fprintf(j.out, "No %s comment on pull request %s: the stack is applied plain.\n", gcbrun, f.PullRequest)

		return f, nil
	case !found:
		return nil, errors.Newf("no %s comment found on pull request %s", gcbrun, f.PullRequest)
	}
	if err := f.parseInstruction(); err != nil {
		return nil, err
	}
	fmt.Fprintf(j.out, "COMMENT_BODY=%s\n", f.Comment)
	if j.refresh && f.ReloadDB && f.ReloadReason == gcbrun+" reload-db" {
		fmt.Fprintln(j.out, "The latest comment's reload-db was that build's; this refresh recreates nothing unless the migrations the database applied are gone from the tree.")
		f.ReloadDB, f.ReloadReason = false, ""
	}

	return f, nil
}

// previewStanding reports whether the pull request has a preview to refresh: its state,
// initialized at its own prefix, holds resources. A pull request that was never built
// has an empty state, and one taken down has a state that holds nothing; both are said
// on out.
func (j *stackJob) previewStanding(ctx context.Context, s *stack) (bool, error) {
	number := j.build.Substitutions[prNumberSub]
	listed, err := s.tofuOutput(ctx, "state", "list")
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(string(listed)) == "" {
		fmt.Fprintf(j.out, "Pull request %s has no preview standing (its state holds no resources): nothing to refresh; a %s comment stands one up.\n", number, gcbrun)

		return false, nil
	}
	fmt.Fprintf(j.out, "Pull request %s has a preview: its stack is applied again so the preview follows the push.\n", number)

	return true, nil
}

// plan plans the pull request's stack and saves the plan: the destroy on /gcbrun down;
// else the stack, without a database of its own on shared-db, and with the pull
// request's database replaced when the job decided it is recreated and it exists, and
// with the extra arguments (the generated secrets' pins). The plan and its JSON are left
// in the workspace for the guard and the apply. The plan waits for the state lock, as
// the apply does (lockTimeoutFlag): another build of the same stack finishes first.
func (s *stack) plan(ctx context.Context, env, subs map[string]string, extra []string) error {
	w := Workspace(filepath.Dir(s.dir))
	plan := filepath.Join(string(w), PlanFile)
	args := []string{planCommand, noInputFlag, noColorFlag, lockTimeoutFlag, "-out=" + plan, varFlag, "environment=" + subs[envSub], varFlag, "pull_request=" + subs[prNumberSub]}
	args = append(args, extra...)
	switch {
	case env[downFact] == trueValue:
		fmt.Fprintln(s.out, "=== /gcbrun down: planning the destroy of the pull request's environment ===")
		args = append(args, "-destroy")
	case env[sharedDBFact] == trueValue:
		fmt.Fprintf(s.out, "=== /gcbrun shared-db: the site runs against %s's database; no database of its own ===\n", subs[envSub])
		args = append(args, varFlag, "shared_database=true")
	case env[reloadDBFact] == trueValue:
		args = append(args, s.replaceDatabase(ctx, subs[appSub], env[reloadReasonFact], env)...)
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
// (a first build has none yet, and it is created), and notes in env that it does.
func (s *stack) replaceDatabase(ctx context.Context, app, reason string, env map[string]string) []string {
	address := "google_spanner_database." + app + "[0]"
	// A state not yet written lists nothing, and a listing that fails is a state with
	// no database: the plan then creates it.
	listed, listErr := s.tofuOutput(ctx, "state", "list")
	if exists := listErr == nil && hasLine(string(listed), address); !exists {
		fmt.Fprintf(s.out, "=== The pull request has no database yet to recreate (%s); it is created ===\n", reason)

		return nil
	}
	fmt.Fprintf(s.out, "=== The pull request's database is recreated: %s ===\n", reason)
	env[replaceDatabaseFact] = trueValue

	return []string{"-replace=" + address}
}

// apply applies the saved plan, unless it holds no change (a build after another of the
// same tree): then there is nothing to apply and the state is read as it is. After a
// destroy nothing more is read; else the stack's substitutions output names the pull
// request's services, its job process's job, its hostname, the variables its migrate
// command runs with and the build arguments the placement declares (_BUILD_ARG_<NAME>,
// the pull request's own values), which are answered as facts, and a database the job
// recreated without being asked is said on the pull request.
func (s *stack) apply(ctx context.Context, build *Build, env map[string]string) (map[string]string, *RevisionTemplates, error) {
	subs := build.Substitutions
	w := Workspace(filepath.Dir(s.dir))
	if env[downFact] == trueValue {
		if err := deleteBuildJobs(ctx, s.clients, s, subs, subs[prNumberSub], s.out); err != nil {
			return nil, nil, err
		}
	}
	data, err := os.ReadFile(filepath.Join(string(w), PlanJSONFile))
	if err != nil {
		return nil, nil, errors.Wrapf(err, "os.ReadFile(): %s", PlanJSONFile)
	}
	p, err := stackPlan(data)
	if err != nil {
		return nil, nil, err
	}
	if p.Empty() {
		fmt.Fprintln(s.out, "Nothing to apply: the pull request's stack matches the code.")
	} else if err := s.tofu(ctx, "apply", noInputFlag, noColorFlag, lockTimeoutFlag, filepath.Join(string(w), PlanFile)); err != nil {
		return nil, nil, err
	}
	if env[downFact] == trueValue {
		fmt.Fprintln(s.out, "=== /gcbrun down: the pull request's environment is gone ===")

		return nil, nil, nil
	}
	outputs, err := s.outputs(ctx)
	if err != nil {
		return nil, nil, err
	}
	facts, err := factsOf(outputs)
	if err != nil {
		return nil, nil, err
	}
	templates, err := revisionTemplatesOf(outputs)
	if err != nil {
		return nil, nil, err
	}
	fmt.Fprintf(s.out, "The pull request's stack names %s=%s", services, facts[services])
	if facts[jobsJobFact] != "" {
		fmt.Fprintf(s.out, " %s=%s", jobsJobFact, facts[jobsJobFact])
	}
	fmt.Fprintf(s.out, " %s=%s, and the migrate command's settings and secrets (%s, %s).\n", prHostnameFact, facts[prHostnameFact], migrateEnvFact, migrateSecretsFact)
	if names := stackArgumentNames(facts); len(names) > 0 {
		fmt.Fprintf(s.out, "The image build passes the pull request's own build arguments: %s.\n", strings.Join(names, ", "))
	}
	if env[replaceDatabaseFact] != trueValue || env[reloadReasonFact] == gcbrun+" reload-db" {
		return facts, templates, nil
	}
	pr, err := speaker(ctx, s.clients, build, env, false)
	if err != nil || pr == nil {
		return facts, templates, err
	}
	if err := pr.comment(ctx, fmt.Sprintf("The pull request's database is recreated this build: %s (build %s).", env[reloadReasonFact], build.ID), s.out); err != nil {
		return nil, nil, err
	}

	return facts, templates, nil
}

// outputs reads the stack's outputs whole from its state (tofu output -json), each
// output's value by name, so that a state without an output (never applied, or applied
// by a bedrock before the output) is said plainly by its reader, not in tofu's words for
// a missing output.
func (s *stack) outputs(ctx context.Context) (map[string]json.RawMessage, error) {
	data, err := s.tofuOutput(ctx, "output", "-json")
	if err != nil {
		return nil, err
	}
	var outputs map[string]struct {
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(data, &outputs); err != nil {
		return nil, errors.Wrap(err, "json.Unmarshal(): tofu output -json")
	}
	values := make(map[string]json.RawMessage, len(outputs))
	for name, output := range outputs {
		values[name] = output.Value
	}

	return values, nil
}

// facts reads the stack's substitutions output from its state into the facts the deploy
// steps read (factsOf).
func (s *stack) facts(ctx context.Context) (map[string]string, error) {
	outputs, err := s.outputs(ctx)
	if err != nil {
		return nil, err
	}

	return factsOf(outputs)
}

// factsOf reads the substitutions output off the stack's outputs into the facts the
// deploy steps read (stackFacts).
func factsOf(outputs map[string]json.RawMessage) (map[string]string, error) {
	substitutions, ok := outputs[substitutionsOutput]
	if !ok {
		return nil, errors.Newf("the stack's state has no %s output: never applied, or applied by a bedrock before the output", substitutionsOutput)
	}

	return stackFacts(substitutions)
}

// stackFacts reads the stack's substitutions output (a map of the trigger's
// substitutions as the stack now stands) into the facts the deploy steps read: the
// services, the job process's job when the application has one, the hostname, the
// variables the migrate command runs with (MIGRATE_ENV, from _MIGRATE_ENV), the secrets
// it reads (MIGRATE_SECRETS, from _MIGRATE_SECRETS) and the databases it reaches
// (MIGRATE_DATABASES, from _MIGRATE_DATABASES), which the migrate step requires and a
// stack applied by an older bedrock lacks, and the build arguments
// the placement declares (_BUILD_ARG_<NAME>, by their substitutions' names), which a
// pull-request build's image build reads as the pull request's own.
func stackFacts(data []byte) (map[string]string, error) {
	var subs map[string]string
	if err := json.Unmarshal(data, &subs); err != nil {
		return nil, errors.Wrap(err, "json.Unmarshal(): the stack's substitutions output")
	}
	facts := map[string]string{services: subs["_SERVICES"], prHostnameFact: subs["_HOSTNAME"]}
	if facts[services] == "" {
		return nil, errors.New("the stack named no services (its substitutions output has no _SERVICES)")
	}
	if sites := subs[hostnamesSub]; sites != "" {
		facts[prHostnamesFact] = sites
	}
	for sub, fact := range map[string]string{nextHostnameSub: nextHostnameFact, nextHostnamesSub: nextHostnamesFact} {
		if next := subs[sub]; next != "" {
			facts[fact] = next
		}
	}
	if job, ok := subs["_JOBS_JOB"]; ok {
		if job == "" {
			return nil, errors.New("the stack names an empty _JOBS_JOB")
		}
		facts[jobsJobFact] = job
	}
	if settings := subs[migrateEnvSub]; settings != "" {
		facts[migrateEnvFact] = settings
	}
	if databases := subs[migrateDatabasesSub]; databases != "" {
		facts[migrateDatabasesFact] = databases
	}
	if secrets := subs[migrateSecretsSub]; secrets != "" {
		facts[migrateSecretsFact] = secrets
	}
	for name, value := range subs {
		if strings.HasPrefix(name, derive.BuildArgumentPrefix) {
			facts[name] = value
		}
	}

	return facts, nil
}

// stackArgumentNames are the build arguments the facts carry (_BUILD_ARG_<NAME>), by the
// argument's name, sorted: what the log names, never their values.
func stackArgumentNames(facts map[string]string) []string {
	var names []string
	for _, key := range slices.Sorted(maps.Keys(facts)) {
		if name, ok := strings.CutPrefix(key, derive.BuildArgumentPrefix); ok && name != "" {
			names = append(names, name)
		}
	}

	return names
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
