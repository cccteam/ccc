// stackteardown.go is the stack trigger's teardown and destroy: the pipeline's path to
// take an application's environment down, for a person who is not an organization
// owner. The operations workflow's teardown and destroy actions run 2-env's stack
// trigger with the action, as the deploy identity, which becomes the apply identity;
// the build runs the code bedrock teardown and bedrock destroy run by hand, waits for
// the team group's approval in Cloud Build where the environment's builds do, and the
// record names the GitHub actor and the approver.

package deploy

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/go-playground/errors/v5"
)

// stackActionSub is the stack trigger's action: first (the apply, its default), teardown
// or destroy, as the operations workflow sets it.
const stackActionSub = "_STACK_ACTION"

// StackTeardown tears the environment's stack down, or destroys it with destroy, in the
// stack trigger's build: it reads its build (the trigger carries the application, the
// environment, the apply identity and the records bucket; the workflow's run carries the
// requester), the checkout's placement, and does what the hand command does
// (PrepareTeardown, Run) as the apply identity, its summary printed first. The refusals
// are the hand command's: a host 2-net still routes, a plan beyond or short of what the
// command means to destroy. The record names the requester and the build's approver.
func StackTeardown(ctx context.Context, clients *Clients, w Workspace, req *BuildRequest, destroy bool, out io.Writer) error {
	build, _, err := readBuild(ctx, clients, req)
	if err != nil {
		return err
	}
	subs := build.Substitutions
	app, env, identity, requester := subs[appSub], subs[envSub], subs[applyIdentitySub], subs[requesterSub]
	for _, v := range []struct{ name, value string }{{appSub, app}, {envSub, env}, {applyIdentitySub, identity}} {
		if v.value == "" {
			return errors.Newf("%s is not set: the stack trigger carries it", v.name)
		}
	}
	command := teardownVar
	if destroy {
		command = destroyVar
	}
	if requester == "" {
		return errors.Newf("%s is not set: the operations workflow's %s action passes the GitHub actor, whom the record names", requesterSub, command)
	}
	p, err := checkoutPlacement(w, "the state bucket and the environments are read from")
	if err != nil {
		return err
	}
	dataDir, err := os.MkdirTemp("", "bedrock-stack-"+command+"-")
	if err != nil {
		return errors.Wrap(err, "os.MkdirTemp()")
	}
	defer os.RemoveAll(dataDir)
	t := &TeardownRequest{
		Dir: filepath.Join(string(w), stackDir), DataDir: dataDir, Placement: p, App: app, Env: env, Destroy: destroy, Backup: destroy,
		By: requester, Approval: buildApproval(build), Now: time.Now, Identity: identity, Impersonate: !build.runsAs(identity),
	}
	fmt.Fprintf(out, "=== %s of %s's stack in %s as %s, asked for by %s ===\n", command, app, env, identity, requester)
	prepared, err := PrepareTeardown(ctx, clients, t, out)
	if err != nil {
		return err
	}
	if prepared == nil {
		return nil
	}
	fmt.Fprint(out, prepared.Summary())

	return prepared.Run(ctx, out)
}

// buildApproval is the build's approval as the record keeps it, nil where the build
// needed none.
func buildApproval(build *Build) *Approval {
	approver, at, comment := build.approver()
	if approver == "" {
		return nil
	}

	return &Approval{Approver: approver, At: at, Comment: comment}
}
