// firstapply.go is the stack trigger's step: an environment's stack applied for the first
// time, from the default branch, as the apply identity, before any release exists there.
// The trigger is 2-env's, one per application and environment, run as the deploy identity
// and started by the application's operations workflow (its apply action), so no person
// holds a cloud role for an application's first apply.

package deploy

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	"github.com/go-playground/errors/v5"
)

// FirstApply applies the environment's stack for the first time. It reads its build (the
// stack trigger carries the application, the environment, the apply identity and the
// records bucket), refuses a stack a release has run on, points tofu at the environment's
// state prefix as the apply identity, plans with the environment alone (no
// generated-secret pin: the secret containers come with this apply, and the first release
// derives the pins), prints the plan's summary and applies it.
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
	if err := refuseReleased(ctx, clients.Storage, records, app, env); err != nil {
		return err
	}
	s := &stack{run: clients.Exec, dir: filepath.Join(string(w), stackDir), identity: identity, impersonate: !build.runsAs(identity), out: out, clients: clients}
	if err := s.initEnvironment(ctx, app, env, identity); err != nil {
		return err
	}
	plan := filepath.Join(string(w), StackPlanFile)
	if err := s.tofu(ctx, planCommand, noInputFlag, noColorFlag, "-out="+plan, varFlag, "environment="+env); err != nil {
		return err
	}
	shown, err := s.tofuOutput(ctx, "show", "-json", plan)
	if err != nil {
		return err
	}
	p, err := stackPlan(shown)
	if err != nil {
		return err
	}
	p.print(out)
	if len(p.Changes) == 0 {
		fmt.Fprintf(out, "Nothing to apply: %s's stack matches the code already.\n", env)

		return nil
	}
	if err := s.tofu(ctx, "apply", noInputFlag, noColorFlag, plan); err != nil {
		return err
	}
	fmt.Fprintf(out, "Applied %s's stack for the first time: %d added, %d changed, %d destroyed. A release's build applies it from here on.\n", env, p.Add, p.Change, p.Destroy)

	return nil
}

// refuseReleased refuses the first apply of a stack a release has run on, which its
// deployment records say (the stack's id, and a release record under it): a release's
// build applies an applied stack, with the generated secrets' pins this apply does not
// carry, and the operations workflow's run action runs the live release again.
func refuseReleased(ctx context.Context, open StoreFunc, bucket, app, env string) error {
	store, err := open(ctx)
	if err != nil {
		return err
	}
	defer store.Close()
	prefix, err := releasesPrefix(ctx, store, bucket, app, env)
	if err != nil || prefix == "" {
		return err
	}
	objects, err := store.List(ctx, bucket, prefix)
	if err != nil {
		return errors.Wrapf(err, "listing gs://%s/%s", bucket, prefix)
	}
	if len(objects) == 0 {
		return nil
	}

	return errors.Newf("%s's stack has deployment records (%d under gs://%s/%s): a release has run there, and a release's build applies the stack with the generated secrets' pins this first apply does not carry. To apply the stack again, run the live release again (the operations workflow's run action)", env, len(objects), bucket, prefix)
}
