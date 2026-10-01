// envstack.go plans, tests and applies the environment's application stack: in a tag
// build, as the apply identity, after the image build and before the migrations, so an
// infrastructure change rides the release that carries it under the release's own gate; in
// a pull-request build, a plan for every environment as that environment's plan identity,
// so the plan a reviewer approves is each environment's.

package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/check"
)

const (
	// environmentsSub is the promotion order, comma-separated; planIdentitiesSub names
	// each environment's plan identity (env=email, comma-separated), the reader a
	// pull-request build plans that environment as.
	environmentsSub   = "_ENVIRONMENTS"
	planIdentitiesSub = "_PLAN_IDENTITIES"
	// StackPlanFile is the tag build's saved plan of the environment's stack, in the
	// workspace, which the apply applies.
	StackPlanFile = "stack.plan"
	// StackPlanJSONFile is that plan's JSON, beside it, whose summary the record carries.
	StackPlanJSONFile = "stack-plan.json"
	// stackPlanFact is the plan's summary line, appended for the steps after.
	stackPlanFact = "STACK_PLAN"
	// enabledState is a secret version's state when a revision can mount it.
	enabledState = "ENABLED"
	// pullRequestBuildNotice is a tag-build step's answer in a pull-request build.
	pullRequestBuildNotice = "Pull-request build: the stack is planned for every environment before the pull request's own, and applied nowhere."
)

// StackPlan is what a plan of the environment's stack would do: the counts tofu prints and
// each change. The deployment record carries the tag build's.
type StackPlan struct {
	Add     int           `json:"add"`
	Change  int           `json:"change"`
	Destroy int           `json:"destroy"`
	Changes []StackChange `json:"changes,omitempty"`
}

// StackChange is one planned change: the resource's address and tofu's actions.
type StackChange struct {
	Address string   `json:"address"`
	Actions []string `json:"actions"`
}

// Summary is the plan's one line, as tofu prints it.
func (p *StackPlan) Summary() string {
	return fmt.Sprintf("Plan: %d to add, %d to change, %d to destroy.", p.Add, p.Change, p.Destroy)
}

// print says what the plan does: the summary, then one change per line.
func (p *StackPlan) print(out io.Writer) {
	fmt.Fprintln(out, p.Summary())
	for _, c := range p.Changes {
		fmt.Fprintf(out, "  %s %s\n", strings.Join(c.Actions, "+"), c.Address)
	}
}

// stackPlan reads a plan's JSON (tofu show -json) into what it would do, no-ops and reads
// left out; a replace counts one to add and one to destroy, as tofu counts it.
func stackPlan(data []byte) (*StackPlan, error) {
	var plan planDocument
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, errors.Wrap(err, "json.Unmarshal(): the plan's JSON")
	}
	p := &StackPlan{}
	for _, c := range plan.ResourceChanges {
		actions := slices.DeleteFunc(slices.Clone(c.Change.Actions), func(a string) bool {
			return a == "no-op" || a == "read"
		})
		if len(actions) == 0 {
			continue
		}
		for _, a := range actions {
			switch a {
			case actionCreate:
				p.Add++
			case actionUpdate:
				p.Change++
			case actionDelete:
				p.Destroy++
			}
		}
		p.Changes = append(p.Changes, StackChange{Address: c.Address, Actions: actions})
	}

	return p, nil
}

// PlanEnvironmentStack plans the environment's stack in a tag build, as the apply identity,
// after the image build and before the migrations: tofu init on the environment's state
// prefix, tofu plan saved to the workspace with its JSON beside it, the summary printed and
// appended (STACK_PLAN), then the tests. A failing plan or test stops the build with the
// stack unapplied. The plan is this build's own: the plan a reviewer approved on the pull
// request was bound to the state of that moment, and a release bundles several pull
// requests. A pull-request build plans every environment earlier instead.
func PlanEnvironmentStack(ctx context.Context, clients *Clients, w Workspace, out io.Writer) error {
	subs, ok, err := tagBuildStep(w, out)
	if err != nil || !ok {
		return err
	}
	identity := subs[applyIdentitySub]
	if identity == "" {
		return errors.Newf("%s names no apply identity (%s): the stack's triggers carry it", BuildFile, applyIdentitySub)
	}
	s := newEnvironmentStack(clients, w, identity, out)
	if err := s.initEnvironment(ctx, subs[appSub], subs[envSub], identity); err != nil {
		return err
	}
	plan := filepath.Join(string(w), StackPlanFile)
	if err := s.tofu(ctx, "plan", "-input=false", "-no-color", "-out="+plan, "-var", "environment="+subs[envSub]); err != nil {
		return err
	}
	shown, err := s.tofuOutput(ctx, "show", "-json", plan)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(string(w), StackPlanJSONFile), shown, 0o600); err != nil {
		return errors.Wrapf(err, "os.WriteFile(): %s", StackPlanJSONFile)
	}
	p, err := stackPlan(shown)
	if err != nil {
		return err
	}
	p.print(out)
	if err := testStack(ctx, clients, s.dir, identity, subs[projectSub], shown, out); err != nil {
		return err
	}

	return w.Append(map[string]string{stackPlanFact: p.Summary()})
}

// ApplyEnvironmentStack applies the plan PlanEnvironmentStack saved, in a tag build, as the
// apply identity, and says what it did; a plan with no change applies nothing.
func ApplyEnvironmentStack(ctx context.Context, clients *Clients, w Workspace, out io.Writer) error {
	subs, ok, err := tagBuildStep(w, out)
	if err != nil || !ok {
		return err
	}
	data, err := os.ReadFile(filepath.Join(string(w), StackPlanJSONFile))
	if err != nil {
		return errors.Wrapf(err, "os.ReadFile(): %s (deploy stack plan writes it)", StackPlanJSONFile)
	}
	p, err := stackPlan(data)
	if err != nil {
		return err
	}
	if len(p.Changes) == 0 {
		fmt.Fprintf(out, "Nothing to apply: %s's stack matches the code.\n", subs[envSub])

		return nil
	}
	s := newEnvironmentStack(clients, w, subs[applyIdentitySub], out)
	if err := s.tofu(ctx, "apply", "-input=false", "-no-color", filepath.Join(string(w), StackPlanFile)); err != nil {
		return err
	}
	fmt.Fprintf(out, "Applied %s's stack: %d added, %d changed, %d destroyed.\n", subs[envSub], p.Add, p.Change, p.Destroy)

	return nil
}

// PlanEnvironments plans the stack for every environment in a pull-request build, each as
// that environment's plan identity, a reader, without the state lock: the plan the
// reviewer approves is each environment's, and a pull-request build in tst can never
// change another environment. Each plan runs the tests; one comment on the pull request
// carries every summary; a failing plan or test stops the build, which is the required
// check, and is said on the pull request. A tag build plans its own environment later
// instead (deploy stack plan).
func PlanEnvironments(ctx context.Context, clients *Clients, w Workspace, out io.Writer) error {
	env, build, ok, err := pullRequestStep(w, out)
	if err != nil || !ok {
		return err
	}
	subs := build.Substitutions
	if env[downFact] == trueValue {
		fmt.Fprintln(out, "/gcbrun down: nothing to plan for the environments.")

		return nil
	}
	identities, err := planIdentities(subs)
	if err != nil {
		return err
	}
	var summaries []string
	for _, e := range strings.Split(subs[environmentsSub], ",") {
		identity := identities[e]
		s := newEnvironmentStack(clients, w, identity, out)
		if err := s.initEnvironment(ctx, subs[appSub], e, identity); err != nil {
			return err
		}
		plan := filepath.Join(string(w), "environment-"+e+".plan")
		if err := s.tofu(ctx, "plan", "-input=false", "-no-color", "-lock=false", "-out="+plan, "-var", "environment="+e); err != nil {
			return refuse(ctx, clients, build, env, fmt.Sprintf("The plan of %s's stack failed; the build log says why (build %s).", e, build.ID), err, out)
		}
		shown, err := s.tofuOutput(ctx, "show", "-json", plan)
		if err != nil {
			return err
		}
		p, err := stackPlan(shown)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "%s: ", e)
		p.print(out)
		if err := testStack(ctx, clients, s.dir, identity, subs[projectSub], shown, out); err != nil {
			return refuse(ctx, clients, build, env, fmt.Sprintf("The plan of %s's stack failed a test (build %s): %v", e, build.ID, errors.Cause(err)), err, out)
		}
		summaries = append(summaries, fmt.Sprintf("**%s**: %s%s", e, p.Summary(), changeList(p)))
	}
	// The pull request's own stack is initialized next, against another prefix.
	if err := os.RemoveAll(filepath.Join(string(w), stackDir, ".terraform")); err != nil {
		return errors.Wrap(err, "os.RemoveAll()")
	}
	pr, err := speaker(ctx, clients, build, env, false)
	if err != nil || pr == nil {
		return err
	}

	return pr.comment(ctx, fmt.Sprintf("The stack's plan for each environment (build %s), the same tests passed in each:\n\n%s", build.ID, strings.Join(summaries, "\n")), out)
}

// changeList is a plan's changes for a comment: one line each, after the summary; none
// for a plan with no change.
func changeList(p *StackPlan) string {
	if len(p.Changes) == 0 {
		return ""
	}
	var lines []string
	for _, c := range p.Changes {
		lines = append(lines, "  - `"+strings.Join(c.Actions, "+")+" "+c.Address+"`")
	}

	return "\n" + strings.Join(lines, "\n")
}

// planIdentities reads each environment's plan identity off the substitutions, and
// refuses an environment of the promotion order that names none: its 2-env is not applied
// with a bedrock that makes the plan identities.
func planIdentities(subs map[string]string) (map[string]string, error) {
	identities := map[string]string{}
	for _, pair := range strings.Split(subs[planIdentitiesSub], ",") {
		env, identity, _ := strings.Cut(pair, "=")
		if env != "" {
			identities[env] = identity
		}
	}
	for _, e := range strings.Split(subs[environmentsSub], ",") {
		if e == "" {
			return nil, errors.Newf("%s names no environment (%s): the stack's triggers carry the promotion order", BuildFile, environmentsSub)
		}
		if identities[e] == "" {
			return nil, errors.Newf("%s names no plan identity for %s (%s): apply 2-env for %s with this bedrock, which makes one per application, then the application's stack, whose triggers carry it", BuildFile, e, planIdentitiesSub, e)
		}
	}

	return identities, nil
}

// refuse says a refusal on the pull request (with the repository's token when the
// environment has no deployer app) and answers the error.
func refuse(ctx context.Context, clients *Clients, build *Build, env map[string]string, body string, cause error, out io.Writer) error {
	pr, err := speaker(ctx, clients, build, env, true)
	if err != nil {
		return err
	}
	if pr != nil {
		if err := pr.comment(ctx, body, out); err != nil {
			return err
		}
	}

	return cause
}

// tagBuildStep reads the workspace for a step that runs only in a tag build; ok is false,
// said on out, in a pull-request build.
func tagBuildStep(w Workspace, out io.Writer) (subs map[string]string, ok bool, err error) {
	build, err := w.Build()
	if err != nil {
		return nil, false, err
	}
	if build.Substitutions[prNumberSub] != "" {
		fmt.Fprintln(out, pullRequestBuildNotice)

		return build.Substitutions, false, nil
	}

	return build.Substitutions, true, nil
}

// newEnvironmentStack is the environment's stack in the checkout, driven through tofu as
// the identity (the apply identity in a tag build, an environment's plan identity in a
// pull-request build).
func newEnvironmentStack(clients *Clients, w Workspace, identity string, out io.Writer) *stack {
	return &stack{
		run: clients.Exec,
		dir: filepath.Join(string(w), stackDir),
		env: []string{"GOOGLE_IMPERSONATE_SERVICE_ACCOUNT=" + identity},
		out: out,
	}
}

// initEnvironment points the stack at the environment's state prefix, 3-app/<app>/<env>,
// afresh: the backend cache of an earlier init, another environment's or a pull request's,
// is removed first.
func (s *stack) initEnvironment(ctx context.Context, app, env, identity string) error {
	if err := os.RemoveAll(filepath.Join(s.dir, ".terraform")); err != nil {
		return errors.Wrap(err, "os.RemoveAll()")
	}
	prefix := "3-app/" + app + "/" + env
	fmt.Fprintf(s.out, "=== %s's stack at %s as %s ===\n", env, prefix, identity)

	return s.tofu(ctx, "init", "-input=false", "-no-color",
		"-backend-config=prefix="+prefix,
		"-backend-config=impersonate_service_account="+identity)
}

// testStack runs the tests a plan of the environment's stack passes before it is applied,
// the same on a pull request and in the tag build: no authoritative IAM resource in the
// stack (a *_iam_binding or *_iam_policy replaces every member of its role on each apply),
// and every secret version a planned revision template pins exists and is enabled, read
// from Secret Manager as the identity the plan ran as, so a revision never fails to start
// on a mount the plan could not see. The migrations' sequence rule is GuardMigrations',
// earlier in the same build.
func testStack(ctx context.Context, clients *Clients, dir, identity, project string, plan []byte, out io.Writer) error {
	found, err := check.ScanAuthoritative(dir)
	if err != nil {
		return err
	}
	if len(found) > 0 {
		lines := make([]string, 0, len(found))
		for _, a := range found {
			lines = append(lines, fmt.Sprintf("%s:%d %s", a.Path, a.Line, a.Address))
		}

		return errors.Newf("%sthe stack declares an authoritative IAM resource, which replaces every member of its role on each apply: %s", rejected, strings.Join(lines, ", "))
	}
	mounts, err := plannedMounts(plan, project)
	if err != nil {
		return err
	}
	if len(mounts) == 0 {
		fmt.Fprintln(out, "Tests passed: no authoritative IAM resource; no secret version pinned.")

		return nil
	}
	secrets, err := clients.SecretsAs(ctx, identity)
	if err != nil {
		return err
	}
	defer secrets.Close()
	for _, m := range mounts {
		state, err := secrets.State(ctx, m)
		if err != nil {
			return errors.Newf("%sa planned revision template pins secret version %s, which could not be read (%v); every pinned version exists and is enabled before the stack is applied", rejected, m, err)
		}
		if state != enabledState {
			return errors.Newf("%sa planned revision template pins secret version %s, which is %s; a revision would fail to start on it", rejected, m, state)
		}
	}
	fmt.Fprintf(out, "Tests passed: no authoritative IAM resource; %d pinned secret version(s) exist and are enabled.\n", len(mounts))

	return nil
}

// plannedMounts are the secret versions the planned Cloud Run services and jobs pin
// (template, containers, env, value_source, secret_key_ref), as version resource names,
// sorted and unique; a secret named without its project is the planned resource's
// project's, else the build's.
func plannedMounts(data []byte, project string) ([]string, error) {
	var plan planDocument
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, errors.Wrap(err, "json.Unmarshal(): the plan's JSON")
	}
	var mounts []string
	for _, c := range plan.ResourceChanges {
		if (c.Type != "google_cloud_run_v2_service" && c.Type != "google_cloud_run_v2_job") || c.Change.After == nil {
			continue
		}
		owner := project
		if p, ok := c.Change.After["project"].(string); ok && p != "" {
			owner = p
		}
		for _, ref := range secretRefs(c.Change.After) {
			secret, _ := ref["secret"].(string)
			version, _ := ref["version"].(string)
			if secret == "" || version == "" {
				continue
			}
			if !strings.Contains(secret, "/") {
				secret = "projects/" + owner + "/secrets/" + secret
			}
			if name := secret + "/versions/" + version; !slices.Contains(mounts, name) {
				mounts = append(mounts, name)
			}
		}
	}
	sort.Strings(mounts)

	return mounts, nil
}

// secretRefs walks a planned value for every secret_key_ref block, at any depth.
func secretRefs(v any) []map[string]any {
	var refs []map[string]any
	switch x := v.(type) {
	case map[string]any:
		for key, child := range x {
			if key != "secret_key_ref" {
				refs = append(refs, secretRefs(child)...)

				continue
			}
			list, _ := child.([]any)
			for _, e := range list {
				if m, ok := e.(map[string]any); ok {
					refs = append(refs, m)
				}
			}
		}
	case []any:
		for _, e := range x {
			refs = append(refs, secretRefs(e)...)
		}
	}

	return refs
}
