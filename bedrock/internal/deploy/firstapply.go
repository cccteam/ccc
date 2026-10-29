// firstapply.go is the stack trigger's step: an environment's stack applied when no
// release's build can, from the default branch, as the apply identity: for the first time,
// before any release exists there, and again after a teardown, which destroyed the
// triggers a release's build would run in. The trigger is 2-env's, one per application and
// environment, run as the deploy identity and started by the application's operations
// workflow (its apply action), so no person holds a cloud role for it.

package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/go-playground/errors/v5"
)

// teardownRecordName is the record a teardown leaves beside the stack's id
// (<app>/stacks/<env>/teardown.json): its kind, the stack it took down and when.
const teardownRecordName = "teardown.json"

// FirstApply applies the environment's stack when no release's build can. It reads its
// build (the stack trigger carries the application, the environment, the apply identity
// and the records bucket), refuses a live stack (one a release has run on and no teardown
// has taken down since), points tofu at the environment's state prefix as the apply
// identity, untaints the Cloud Run services and jobs a failed apply left tainted (they
// exist; the plan updates them in place, where a replacement would meet production's
// deletion protection), plans with the environment and the generated secrets' pins
// derived the way a pull request's plan derives them (each container's newest enabled
// version; nothing while a container has none, since the first release adds it), prints
// the plan's summary and applies it.
func FirstApply(ctx context.Context, clients *Clients, w Workspace, req *BuildRequest, out io.Writer) error {
	build, _, err := readBuild(ctx, clients, req)
	if err != nil {
		return err
	}
	subs := build.Substitutions
	app, env, identity, records := subs[appSub], subs[envSub], subs[applyIdentitySub], subs[recordsBucket]
	for _, v := range []struct{ name, value string }{{appSub, app}, {envSub, env}, {applyIdentitySub, identity}, {recordsBucket, records}} {
		if v.value == "" {
			return errors.Newf("%s is not set: the stack trigger carries it", v.name)
		}
	}
	tornDown, err := refuseLive(ctx, clients.Storage, records, app, env, out)
	if err != nil {
		return err
	}
	p, err := checkoutPlacement(w, "the generated secrets are read from")
	if err != nil {
		return err
	}
	m, err := checkoutModel(w, p)
	if err != nil {
		return err
	}
	s := &stack{run: clients.Exec, dir: filepath.Join(string(w), stackDir), identity: identity, impersonate: !build.runsAs(identity), out: out, clients: clients}
	if err := s.initEnvironment(ctx, app, env, identity); err != nil {
		return err
	}
	if err := guardCapacity(ctx, clients, identity, s.impersonate, p, app, env, out); err != nil {
		return err
	}
	pins, err := generatedPins(ctx, clients, identity, s.impersonate, m, app, env, identityProject(identity), false, out)
	if err != nil {
		return err
	}
	if err := s.untaintCloudRun(ctx, out); err != nil {
		return err
	}
	plan := filepath.Join(string(w), StackPlanFile)
	args := append([]string{planCommand, noInputFlag, noColorFlag, "-out=" + plan, varFlag, "environment=" + env}, generatedArgs(pins)...)
	if err := s.tofu(ctx, args...); err != nil {
		return err
	}
	shown, err := s.tofuOutput(ctx, "show", "-json", plan)
	if err != nil {
		return err
	}
	pl, err := stackPlan(shown)
	if err != nil {
		return err
	}
	pl.print(out)
	if pl.Empty() {
		fmt.Fprintf(out, "Nothing to apply: %s's stack matches the code already.\n", env)

		return nil
	}
	if err := s.tofu(ctx, "apply", noInputFlag, noColorFlag, plan); err != nil {
		return err
	}
	if tornDown {
		fmt.Fprintf(out, "Applied %s's stack again after its teardown: %s. Its triggers are back; the operations workflow's run action runs the release it ran, and a release's build applies it from here on.\n", env, pl.appliedCounts())

		return nil
	}
	fmt.Fprintf(out, "Applied %s's stack for the first time: %s. A release's build applies it from here on.\n", env, pl.appliedCounts())

	return nil
}

// refuseLive refuses the apply of a live stack: one with deployment records (the stack's
// id, and a release record under it) and no teardown since. A release's build applies a
// live stack, and the operations workflow's run action runs the live release again. A
// teardown record beside the stack's id, for this stack and newer than its newest live
// release, says the stack is down (the teardown destroyed the triggers a release's build
// would run in), and the apply goes on; it answers whether it did. A destroy takes the
// stack's id with the stack, so a destroyed stack has no records to read and is applied
// as for the first time.
func refuseLive(ctx context.Context, open StoreFunc, bucket, app, env string, out io.Writer) (bool, error) {
	store, err := open(ctx)
	if err != nil {
		return false, err
	}
	defer store.Close()
	id, err := stackID(ctx, store, bucket, app, env)
	if err != nil || id == "" {
		return false, err
	}
	prefix := releasesPath(app, env, id)
	objects, err := store.List(ctx, bucket, prefix)
	if err != nil {
		return false, errors.Wrapf(err, "listing gs://%s/%s", bucket, prefix)
	}
	if len(objects) == 0 {
		return false, nil
	}
	record := app + "/" + stacksSegment + "/" + env + "/" + teardownRecordName
	data, err := store.Read(ctx, bucket, record)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, errors.Wrapf(err, "reading gs://%s/%s", bucket, record)
	}
	var teardown struct {
		Kind    string `json:"kind"`
		StackID string `json:"stackId"`
		At      string `json:"at"`
	}
	if data != nil {
		if err := json.Unmarshal(data, &teardown); err != nil {
			return false, errors.Wrapf(err, "json.Unmarshal(): gs://%s/%s", bucket, record)
		}
	}
	live, err := newestLiveRelease(ctx, store, bucket, app, env)
	if err != nil {
		return false, err
	}
	if teardown.Kind == "teardown" && teardown.StackID == id && after(teardown.At, live) {
		fmt.Fprintf(out, "%s's stack was torn down at %s (gs://%s/%s): it is applied again, with the generated secrets' pins derived from their containers.\n", env, teardown.At, bucket, record)

		return true, nil
	}

	return false, errors.Newf("%s's stack is live: it has deployment records (%d under gs://%s/%s) and no teardown since. A release's build applies a live stack; to apply it again, run the live release again (the operations workflow's run action). After a teardown this apply brings the stack back", env, len(objects), bucket, prefix)
}

// after reports whether the moment is after the live record's, or there is no live record.
func after(at string, live *Record) bool {
	if live == nil {
		return true
	}
	t, err := time.Parse(time.RFC3339, at)
	if err != nil {
		return false
	}
	l, err := time.Parse(time.RFC3339, live.Timestamp)
	if err != nil {
		return false
	}

	return t.After(l)
}
