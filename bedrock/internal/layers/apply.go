// apply.go is bedrock org apply: the apply trigger's run on a push to the default
// branch, or started by hand for one layer.

package layers

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/deploy"
	"github.com/cccteam/ccc/bedrock/internal/github"
	"github.com/cccteam/ccc/bedrock/internal/org"
)

// applySummaryRE is the line a plan or an apply ends with, which the run's summary
// carries.
var applySummaryRE = regexp.MustCompile(`(?m)^(Apply complete.*|No changes\..*|Error:.*)$`)

// Apply applies the layers in layer order, one at a time, each as its layer's apply
// identity, stopping at the first failure. Without _LAYER it applies every run whose
// layer changed since the run's last successful apply: a marker object per run in the
// state bucket holds the commit that apply was of, written after each successful apply,
// and a run with no marker is applied. _LAYER names one layer to apply whatever changed
// (_ENVIRONMENT one environment of 2-env). After each apply of 2-env, Firebase's
// browser key in the environment project is restricted to the sign-in APIs.
func Apply(ctx context.Context, clients *Clients, req *Request, out io.Writer) error {
	r, err := start(ctx, clients, req, applyCommand, out)
	if err != nil {
		return err
	}
	if r.p.StateBucket == "" {
		return errors.New("placement.json records no stateBucket: the layers' state and the apply markers live there (0-bootstrap/README.md, step 1), and bedrock org render reads it")
	}
	store, err := clients.Store(ctx)
	if err != nil {
		return err
	}
	defer store.Close()
	runs, err := r.appliedRuns(ctx, store)
	if err != nil {
		return err
	}
	if len(runs) == 0 {
		fmt.Fprintln(out, "No layer changes since the last applies: nothing to apply.")

		return nil
	}
	if err := r.recorded(runs); err != nil {
		return err
	}
	githubToken, err := r.tokenFor(ctx, runs)
	if err != nil {
		return err
	}
	for _, lr := range runs {
		if err := r.applyOne(ctx, store, lr, githubToken); err != nil {
			return err
		}
	}

	return nil
}

// appliedRuns is what the run applies: the run _LAYER names, else every run whose
// layer changed since its marker's commit, and every run without a marker.
func (r *run) appliedRuns(ctx context.Context, store deploy.Store) ([]org.LayerRun, error) {
	if runs, ok, err := r.namedRuns(); ok || err != nil {
		return runs, err
	}
	markers, err := store.List(ctx, r.p.StateBucket, org.MarkerPrefix+"/")
	if err != nil {
		return nil, err
	}
	commit := r.subs[commitSub]
	changed := map[string][]string{}
	var runs []org.LayerRun
	for _, lr := range r.p.LayerRuns() {
		if !contains(markers, lr.Marker()) {
			fmt.Fprintf(r.out, "%s: never applied by the trigger (no marker gs://%s/%s); applying it.\n", lr.Name, r.p.StateBucket, lr.Marker())
			runs = append(runs, lr)

			continue
		}
		data, err := store.Read(ctx, r.p.StateBucket, lr.Marker())
		if err != nil {
			return nil, err
		}
		since := strings.TrimSpace(string(data))
		if since == commit {
			fmt.Fprintf(r.out, "%s: applied at %s already.\n", lr.Name, commit)

			continue
		}
		layers, ok := changed[since]
		if !ok {
			layers, err = r.changedLayers(ctx, since, commit)
			if err != nil {
				return nil, err
			}
			changed[since] = layers
		}
		if layers == nil || contains(layers, lr.Layer) {
			fmt.Fprintf(r.out, "%s: changed since its last apply at %s; applying it.\n", lr.Name, since)
			runs = append(runs, lr)

			continue
		}
		fmt.Fprintf(r.out, "%s: unchanged since its last apply at %s.\n", lr.Name, since)
	}

	return runs, nil
}

// changedLayers is the layers changed between the marker's commit and this one; nil,
// meaning every layer, when GitHub no longer knows the marker's commit (a rewritten
// history).
func (r *run) changedLayers(ctx context.Context, since, commit string) ([]string, error) {
	files, err := r.changedFiles(ctx, since, commit)
	if github.NotFound(err) {
		fmt.Fprintf(r.out, "GitHub does not know commit %s, which a marker names: every layer counts as changed.\n", since)

		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	layers := layersOf(files)
	if layers == nil {
		layers = []string{}
	}

	return layers, nil
}

// applyOne plans and applies one run as its apply identity, records the marker, and for
// 2-env restricts Firebase's browser key.
func (r *run) applyOne(ctx context.Context, store deploy.Store, lr org.LayerRun, githubToken string) error {
	fmt.Fprintf(r.out, "=== apply of %s as %s ===\n", lr.Name, lr.Apply)
	t, err := newTofu(r.clients.Exec, r.req.Dir, lr, lr.Apply, githubToken, r.out)
	if err != nil {
		return err
	}
	defer t.close()
	if err := t.init(ctx); err != nil {
		return errors.Wrapf(err, "the init of %s failed; the log says why", lr.Name)
	}
	planText, ok := t.plan(ctx, true)
	if !ok {
		return errors.Newf("the plan of %s failed; the log says why", lr.Name)
	}
	applyText, err := t.apply(ctx)
	fmt.Fprintf(r.out, "%s: %s\n", lr.Name, lastSummary(planText+applyText))
	if err != nil {
		return errors.Wrapf(err, "the apply of %s failed; the log says why", lr.Name)
	}
	commit := r.subs[commitSub]
	if err := store.Write(ctx, r.p.StateBucket, lr.Marker(), []byte(commit)); err != nil {
		return err
	}
	fmt.Fprintf(r.out, "Recorded the apply of %s at %s in gs://%s/%s.\n", lr.Name, commit, r.p.StateBucket, lr.Marker())
	if lr.Environment == "" {
		return nil
	}
	keys, err := r.clients.Keys(ctx, lr.Apply)
	if err != nil {
		return err
	}

	return org.RestrictBrowserKeys(ctx, keys, r.p.Projects[lr.Environment], r.out)
}

// lastSummary is the last summary line tofu printed, or that there is none.
func lastSummary(text string) string {
	lines := applySummaryRE.FindAllString(text, -1)
	if len(lines) == 0 {
		return noSummary
	}

	return strings.TrimSpace(lines[len(lines)-1])
}

// contains reports whether the list holds the word.
func contains(list []string, word string) bool {
	for _, w := range list {
		if w == word {
			return true
		}
	}

	return false
}
