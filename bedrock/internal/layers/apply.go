// apply.go is bedrock org apply: the apply trigger's run on a push to the default
// branch, or started by hand for one layer.

package layers

import (
	"context"
	"fmt"
	"io"
	"os"
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
	tofus, err := r.initialized(ctx, runs, githubToken)
	if err != nil {
		return err
	}
	defer closeAll(tofus)
	for i, lr := range runs {
		if err := r.applyOne(ctx, store, lr, tofus[i]); err != nil {
			return err
		}
	}

	return nil
}

// initialized initializes every run, in order, as its apply identity, before any
// applies. A run's init writes the empty state of a layer that has never applied (the
// gcs backend creates a state the first time anything opens it), and a later layer reads
// that state through terraform_remote_state as a reader; without this pass the read
// would be the write, at the other layer's prefix, which the reading layer's identity
// may not do (2-env tst reads 2-net and stg before either applies in a new
// organization). The runs are closed by the caller.
func (r *run) initialized(ctx context.Context, runs []org.LayerRun, githubToken string) ([]*tofu, error) {
	tofus := make([]*tofu, 0, len(runs))
	for _, lr := range runs {
		t, err := newTofu(r.clients.Exec, r.req.Dir, lr, lr.Apply, githubToken, r.out)
		if err != nil {
			closeAll(tofus)

			return nil, err
		}
		tofus = append(tofus, t)
		fmt.Fprintf(r.out, "=== init of %s as %s ===\n", lr.Name, lr.Apply)
		if err := t.init(ctx); err != nil {
			closeAll(tofus)

			return nil, errors.Wrapf(err, "the init of %s failed; the log says why", lr.Name)
		}
	}

	return tofus, nil
}

// closeAll removes the runs' working directories.
func closeAll(tofus []*tofu) {
	for _, t := range tofus {
		t.close()
	}
}

// appliedRuns is what the run applies: the run _LAYER names, else every run whose
// layer changed since its marker's commit, and every run without a marker.
func (r *run) appliedRuns(ctx context.Context, store deploy.Store) ([]org.LayerRun, error) {
	if runs, ok, err := r.namedRuns(); ok || err != nil {
		return runs, err
	}
	commit := r.subs[commitSub]
	changed := map[string][]string{}
	var runs []org.LayerRun
	for _, lr := range r.p.LayerRuns() {
		// Each marker is read by its name: the applier's grant on the bucket is
		// conditioned on the markers' prefix, and a listing is the bucket's own
		// permission, which no condition on an object's name grants.
		data, err := store.Read(ctx, r.p.StateBucket, lr.Marker())
		if errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(r.out, "%s: never applied by the trigger (no marker gs://%s/%s); applying it.\n", lr.Name, r.p.StateBucket, lr.Marker())
			runs = append(runs, lr)

			continue
		}
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

// applyOne plans and applies one run, initialized already, as its apply identity,
// records the marker, and for 2-env restricts Firebase's browser key.
func (r *run) applyOne(ctx context.Context, store deploy.Store, lr org.LayerRun, t *tofu) error {
	fmt.Fprintf(r.out, "=== apply of %s as %s ===\n", lr.Name, lr.Apply)
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
