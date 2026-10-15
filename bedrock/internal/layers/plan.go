// plan.go is bedrock org plan: the plan trigger's run on a pull request.

package layers

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/github"
	"github.com/cccteam/ccc/bedrock/internal/org"
)

// commentCut is where a plan's text is cut in the comment on the pull request;
// initTail is how many of init's last lines a plan that did not run folds in its place.
const (
	commentCut = 60000
	initTail   = 40
)

// Plan plans the layers a pull request changes, side by side, each as its layer's plan
// identity, judges every plan once all have finished (verdict.go), posts one comment per
// layer on the pull request as the deployer app, kept current across pushes, and fails
// when any plan fails. _LAYER names one layer to plan whatever the pull request changes
// (_ENVIRONMENT one environment of 2-env).
func Plan(ctx context.Context, clients *Clients, req *Request, out io.Writer) error {
	r, err := start(ctx, clients, req, planCommand, out)
	if err != nil {
		return err
	}
	number, err := r.pullRequest()
	if err != nil {
		return err
	}
	runs, err := r.plannedRuns(ctx)
	if err != nil {
		return err
	}
	if len(runs) == 0 {
		fmt.Fprintln(out, "No layer changes in this pull request: nothing to plan.")

		return nil
	}
	if err := r.recorded(runs); err != nil {
		return err
	}
	githubToken, err := r.tokenFor(ctx, runs)
	if err != nil {
		return err
	}
	results := r.planAll(ctx, runs, githubToken)
	verdicts := verdicts(results)
	for i, v := range verdicts {
		fmt.Fprintf(out, "%s: %s\n", results[i].run.Name, v.summary)
	}
	if err := r.comment(ctx, number, results, verdicts); err != nil {
		return err
	}
	var failed []string
	for i, v := range verdicts {
		if !v.pass {
			failed = append(failed, results[i].run.Name)
		}
	}
	if len(failed) > 0 {
		return errors.Newf("the plan of %s failed; the log says why", joined(failed))
	}

	return nil
}

// pullRequest is the number of the pull request the run plans.
func (r *run) pullRequest() (int, error) {
	number, err := strconv.Atoi(r.subs[prNumberSub])
	if err != nil || number <= 0 {
		return 0, errors.Newf("%s is %q: the plan trigger runs on a pull request, and Cloud Build names it there", prNumberSub, r.subs[prNumberSub])
	}

	return number, nil
}

// plannedRuns is what the run plans: the layer _LAYER names, else the layers the pull
// request changes against its base.
func (r *run) plannedRuns(ctx context.Context) ([]org.LayerRun, error) {
	if runs, ok, err := r.namedRuns(); ok || err != nil {
		return runs, err
	}
	base := r.subs[baseBranchSub]
	if base == "" {
		return nil, errors.Newf("%s is not set: the plan trigger runs on a pull request, and Cloud Build names its base there", baseBranchSub)
	}
	files, err := r.changedFiles(ctx, base, r.subs[commitSub])
	if err != nil {
		return nil, err
	}
	layers := layersOf(files)
	fmt.Fprintf(r.out, "The pull request changes %d file(s) against %s; layers to plan: %s.\n", len(files), base, nothingOr(layers))

	return r.runsOf(layers), nil
}

// namedRuns is the run _LAYER names (and _ENVIRONMENT narrows), when it is set.
func (r *run) namedRuns() (runs []org.LayerRun, named bool, err error) {
	layer, env := r.subs[layerSub], r.subs[environmentSub]
	if layer == "" {
		if env != "" {
			return nil, false, errors.Newf("%s is set to %s without %s: name the layer too (%s=2-env)", environmentSub, env, layerSub, layerSub)
		}

		return nil, false, nil
	}
	for _, lr := range r.p.LayerRuns() {
		if lr.Layer == layer && (env == "" || lr.Environment == env) {
			runs = append(runs, lr)
		}
	}
	if len(runs) == 0 {
		return nil, true, errors.Newf("%s is %s and %s is %q: no such run; the layers are %s, and %s names one of %s for 2-env", layerSub, layer, environmentSub, env, strings.Join(org.Layers, ", "), environmentSub, strings.Join(org.Environments, ", "))
	}
	fmt.Fprintf(r.out, "%s names %s: planning it alone.\n", layerSub, joined(names(runs)))

	return runs, true, nil
}

// tokenFor mints 1-org's GitHub token when 1-org is among the runs.
func (r *run) tokenFor(ctx context.Context, runs []org.LayerRun) (string, error) {
	for _, lr := range runs {
		if lr.Layer == orgLayer {
			return r.infrastructureToken(ctx)
		}
	}

	return "", nil
}

// planAll plans every run side by side, printing each run's output whole as it
// finishes, and answers the results in the runs' order.
func (r *run) planAll(ctx context.Context, runs []org.LayerRun, githubToken string) []*planResult {
	results := make([]*planResult, len(runs))
	var wg sync.WaitGroup
	var mu sync.Mutex
	for i, lr := range runs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res := r.planOne(ctx, lr, githubToken)
			mu.Lock()
			defer mu.Unlock()
			fmt.Fprintf(r.out, "=== plan of %s as %s ===\n%s", lr.Name, lr.Plan, res.log)
			if res.err != nil {
				fmt.Fprintf(r.out, "%v\n", res.err)
			}
			results[i] = res
		}()
	}
	wg.Wait()

	return results
}

// planOne plans one run as its plan identity, without the state lock, and reads its
// errors or its output changes. A run with no state yet is not planned: it has never
// applied, and its plan identity could not even initialize (tofu writes an empty state
// and its lock for a workspace that has none, and the identity writes nothing).
func (r *run) planOne(ctx context.Context, lr org.LayerRun, githubToken string) *planResult {
	var log bytes.Buffer
	res := &planResult{run: lr}
	defer func() { res.log = log.String() }()
	absent, err := r.stateAbsent(ctx, lr)
	if err != nil {
		res.err = err

		return res
	}
	if absent {
		res.unapplied = true
		fmt.Fprintf(&log, "No state at gs://%s/%s/%s: %s has never applied, so there is nothing to plan against; the merge applies it.\n", r.p.StateBucket, lr.StatePrefix, stateObject, lr.Name)

		return res
	}
	t, err := newTofu(r.clients.Exec, r.req.Dir, lr, lr.Plan, githubToken, &log)
	if err != nil {
		res.err = err

		return res
	}
	defer t.close()
	if err := t.init(ctx); err != nil {
		fmt.Fprintf(&log, "%v\n", err)

		return res
	}
	res.ran = true
	res.text, res.ok = t.plan(ctx, false)
	if res.ok {
		res.outputs, res.err = t.outputChanges(ctx)

		return res
	}
	res.diagnostics, res.err = t.errorDiagnostics()

	return res
}

// stateAbsent reports whether the run's state object is not in the bucket: the run has
// never applied. The read is the plan identity's own, which holds read on its prefix;
// nothing is checked while the placement records no bucket.
func (r *run) stateAbsent(ctx context.Context, lr org.LayerRun) (bool, error) {
	if r.p.StateBucket == "" {
		return false, nil
	}
	store, err := r.clients.StoreAs(ctx, lr.Plan)
	if err != nil {
		return false, errors.Wrapf(err, "opening the state bucket as %s", lr.Plan)
	}
	defer store.Close()
	_, err = store.Read(ctx, r.p.StateBucket, lr.StatePrefix+"/"+stateObject)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, errors.Wrapf(err, "reading %s's state as %s", lr.Name, lr.Plan)
	}

	return false, nil
}

// comment posts each plan on the pull request as the deployer app: one comment per
// run, found by its marker line and updated when it is there, created when not. The
// plans stay in the log when the deployer app is not recorded.
func (r *run) comment(ctx context.Context, number int, results []*planResult, verdicts []verdict) error {
	token, reason, err := r.deployerToken(ctx)
	if err != nil {
		return err
	}
	if reason != "" {
		fmt.Fprintln(r.out, reason+".")

		return nil
	}
	gh := r.clients.GitHub(token)
	existing, err := gh.IssueComments(ctx, r.owner, r.repo, number)
	if err != nil {
		return err
	}
	for i, res := range results {
		body := commentBody(res, verdicts[i].summary, r.subs[commitSub])
		if id, ok := findComment(existing, markerOf(res.run.Name)); ok {
			if err := gh.UpdateComment(ctx, r.owner, r.repo, id, body); err != nil {
				return err
			}

			continue
		}
		if err := gh.CreateComment(ctx, r.owner, r.repo, number, body); err != nil {
			return err
		}
	}
	fmt.Fprintf(r.out, "Posted the plans on pull request #%d as the deployer app.\n", number)

	return nil
}

// lastLines is the last n lines of text, or all of it when it is shorter, or a note
// when it is empty.
func lastLines(text string, n int) string {
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return "(nothing was printed; the run's log has what there is)\n"
	}
	lines := strings.Split(text, "\n")
	if len(lines) > n {
		lines = append([]string{"..."}, lines[len(lines)-n:]...)
	}

	return strings.Join(lines, "\n") + "\n"
}

// markerOf is the line a run's comment starts with, by which a later push finds it.
func markerOf(name string) string {
	return "<!-- layers: " + name + " -->"
}

// findComment is the comment starting with the marker, when there is one.
func findComment(comments []github.Comment, marker string) (int64, bool) {
	for _, c := range comments {
		if strings.HasPrefix(c.Body, marker) {
			return c.ID, true
		}
	}

	return 0, false
}

// commentBody is a plan's comment: the marker, the heading, the identity and the
// commit with the verdict, and the plan's text folded under it, cut when long. A plan
// that did not run folds the end of what its init printed instead, where the refusal
// is (an identity it could not become, a backend it could not read).
func commentBody(res *planResult, summary, commit string) string {
	text := res.text
	switch {
	case res.unapplied:
		text = lastLines(res.log, initTail)
	case !res.ran:
		text = "tofu init failed, so nothing was planned; the last of what it printed:\n\n" + lastLines(res.log, initTail)
	}
	switch {
	case len(text) > commentCut:
		text = text[:commentCut] + "\n... cut at 60000 characters; the run's log has all of it\n"
	case !strings.HasSuffix(text, "\n"):
		text += "\n"
	}

	return markerOf(res.run.Name) + "\n### plan of " + res.run.Name + "\n\nAs `" + res.run.Plan + "` at " + commit + ": " + summary + "\n\n<details><summary>the plan</summary>\n\n````text\n" + text + "````\n\n</details>\n"
}

// names is the runs' names.
func names(runs []org.LayerRun) []string {
	out := make([]string, 0, len(runs))
	for _, lr := range runs {
		out = append(out, lr.Name)
	}

	return out
}

// nothingOr is the words joined, or "none".
func nothingOr(words []string) string {
	if len(words) == 0 {
		return "none"
	}

	return joined(words)
}
