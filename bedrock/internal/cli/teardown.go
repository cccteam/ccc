// teardown.go is teardown [<env>] and destroy [<env>]: an environment's stack taken down
// by a person at their own computer, to what it keeps or whole, with --all for every
// environment and every pull-request stack.

package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/go-playground/errors/v5"
	"github.com/spf13/cobra"

	"github.com/cccteam/ccc/bedrock/internal/deploy"
	"github.com/cccteam/ccc/bedrock/internal/derive"
	"github.com/cccteam/ccc/bedrock/internal/where"
)

const (
	teardownUse = "teardown"
	destroyUse  = "destroy"
	allFlag     = "all"
	// confirmFlag goes on without the name typed back; nobody is who the record names
	// when the system says nothing.
	confirmFlag = "yes"
	nobody      = "unknown"
)

// teardownFlags are the two commands' flags.
type teardownFlags struct {
	dir, app, placement string
	all, yes            bool
}

// newTeardown is teardown [<env>].
func newTeardown(d deps) *cobra.Command {
	var f teardownFlags
	cmd := &cobra.Command{
		Use:   teardownUse + " [<env>]",
		Short: "Take an environment down to what it keeps: the services and the pipeline go, the data and the names stay",
		Long: `teardown destroys, in the environment, what a pipeline makes again: the Cloud Run services and jobs
with their invoker grants, the endpoint groups and backend services, the Cloud Build triggers and
the sweep's schedule, the scheduled routes' jobs and identity, the Cloud Armor policy and the
request-log exclusion. It keeps the Spanner database and every generation of it, the file stores
with their policies, the secret containers with their accessors, the identities with their grants,
the Firestore database with its indexes, fields and rules, the Firebase key, the migrate log bucket,
the Cloud Tasks queue with its grants, and the stack's id, so the environment's records and names
stay its own. A kept resource that depends on a destroyed one goes with it (the migrate-log sink
names the version trigger and the services in its filter), since tofu keeps an excluded resource's
dependencies; the next apply makes it again. It runs by a person at their own computer with their
own sign-in (gcloud auth application-default login), driving tofu in the stack directory against
the environment's state prefix the way the first apply by hand does: in production, an apply with
teardown=true first, recording in the state that the services' deletion protection is lifted;
then a destroy plan with every kept resource excluded, read back and held against what the
teardown names (anything beyond or short of it stops the run with nothing applied), and its
apply; then a record of what went, by whom and when, beside the stack's id in the records
bucket (<app>/stacks/<env>/teardown.json). A run that finds nothing left to destroy writes the
record when the stack has none since its live release (an earlier run's write failed after its
apply). It refuses while 2-net routes a host to the environment's backend service (Google
refuses the backend's deletion while the load balancer uses it): take the hosts out of 2-net
first, with bedrock org teardown or the application's removal from placement.json through an
organization pull request. It asks for the environment typed back before anything runs (--yes
stands in for it where there is no terminal). With --all
it takes every environment down in promotion order, then destroys every pull-request stack
under the integration environment's prefix whole, the way the sweep destroys a closed pull
request's.
Bringing an environment back is the stack's apply by hand from the checkout of the release it
ran (the README's "Applying"), which makes the services and the triggers again, then bedrock rerun
<env> <release> through them.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return d.takeDown(cmd, args, &f, false)
		},
	}
	teardownFlagSet(cmd, &f)

	return cmd
}

// newDestroy is destroy [<env>].
func newDestroy(d deps) *cobra.Command {
	var f teardownFlags
	cmd := &cobra.Command{
		Use:   destroyUse + " [<env>]",
		Short: "Destroy an environment's stack whole, its database, files and Firestore database included",
		Long: `destroy destroys everything the environment's stack holds: the Spanner database and every generation
of it, the file stores and their objects, the Firestore database, the secret containers and their
versions, the identities, the Cloud Tasks queue, the migrate logs, the Firebase key, the services,
the jobs, the triggers, the schedules and the stack's id. It runs by a person at their own
computer with their own sign-in (gcloud auth application-default login), driving tofu in the stack
directory against the environment's state prefix the way the first apply by hand does: in
production, an apply with destroy=true first, recording in the state that every protection is
lifted (the services' deletion protection, the database's deletion and drop protection, the file
stores' force-destroy, the Firestore database's delete protection); then a destroy plan, read back
and held against the stack's whole state (anything short of it stops the run with nothing
applied), and its apply; then a record of what went, by whom and when, in the records bucket
(<app>/stacks/<env>/teardown.json), where the stack's own id object is gone with the stack. The
deployment records stay in the records bucket under <app>/<env>/<id>/, read by nothing; the
Spanner backups the pipeline and the schedules took stay until their retention ends; Google keeps
the names of the task queue, the Firebase key and the migrate log bucket for a while, and a stack
applied again takes an id of its own. It refuses while 2-net routes a host to the environment's
backend service (Google refuses the backend's deletion while the load balancer uses it): take the
hosts out of 2-net first, with bedrock org teardown or the application's removal from
placement.json through an organization pull request. It asks for the environment typed back
before anything runs (--yes stands in for it where there is no terminal). With --all it destroys
every environment in promotion order, then every pull-request stack under the integration
environment's prefix, the way the sweep destroys a closed pull request's. Nothing here is undone
by bedrock.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return d.takeDown(cmd, args, &f, true)
		},
	}
	teardownFlagSet(cmd, &f)

	return cmd
}

// teardownFlagSet declares the two commands' flags.
func teardownFlagSet(cmd *cobra.Command, f *teardownFlags) {
	cmd.Flags().StringVar(&f.dir, "dir", "", "the infrastructure root (default: found from the working directory)")
	cmd.Flags().StringVar(&f.app, "app", "", "the application (default: the one application layer)")
	cmd.Flags().StringVar(&f.placement, "placement", "", "placement file (default: placement.json in the stack directory)")
	cmd.Flags().BoolVar(&f.all, allFlag, false, "every environment in promotion order, then every pull-request stack")
	cmd.Flags().BoolVar(&f.yes, confirmFlag, false, "go on without the name typed back, where there is no terminal to type it at")
}

// teardownTarget finds the stack the commands act on: the infrastructure root (--dir, or
// the working directory's), the application (--app, or the one application layer), its
// stack directory and its placement (--placement, or placement.json in the stack).
func (d deps) teardownTarget(f *teardownFlags) (stackDir, app string, p *derive.Placement, err error) {
	infraRoot, _, err := d.infrastructureRoot(f.dir)
	if err != nil {
		return "", "", nil, err
	}
	app = f.app
	if app == "" {
		app, err = where.SingleApplication(infraRoot)
		if err != nil {
			return "", "", nil, err
		}
	}
	stackDir = where.LayerDir(infraRoot, app)
	file := f.placement
	if file == "" {
		file = filepath.Join(stackDir, placementFile)
	}
	p, err = derive.ReadPlacement(file)
	if err != nil {
		return "", "", nil, err
	}

	return stackDir, app, p, nil
}

// confirmTeardown asks for the word typed back at the terminal, unless --yes stands in
// for it; false means the person stopped the run.
func (d deps) confirmTeardown(cmd *cobra.Command, command, word string, yes bool) (bool, error) {
	if yes {
		return true, nil
	}
	if !d.interactive() {
		return false, errors.Newf("bedrock %s asks for %s typed back at the terminal, and this run has none: --%s stands in for it", command, word, confirmFlag)
	}
	typed, err := d.asker(cmd).Line(fmt.Sprintf("Type %s to go on; anything else stops:", word))
	if err != nil {
		return false, err
	}
	if typed != word {
		fmt.Fprintln(cmd.OutOrStdout(), "Stopped: nothing was destroyed.")

		return false, nil
	}

	return true, nil
}

// takeDown is both commands: the stack and the placement found, every environment
// prepared (and refused) before any runs, the warning and the name typed back, the runs
// in order, and the pull-request stacks after them with --all.
func (d deps) takeDown(cmd *cobra.Command, args []string, f *teardownFlags, destroy bool) error {
	out, ctx := cmd.OutOrStdout(), cmd.Context()
	stackDir, app, p, err := d.teardownTarget(f)
	if err != nil {
		return err
	}
	envs, word, err := teardownEnvironments(args, f.all, app, p)
	if err != nil {
		return err
	}
	command := teardownUse
	if destroy {
		command = destroyUse
	}
	dataDir, err := os.MkdirTemp("", "bedrock-"+command+"-")
	if err != nil {
		return errors.Wrap(err, "os.MkdirTemp()")
	}
	defer os.RemoveAll(dataDir)
	request := func(env string) *deploy.TeardownRequest {
		return &deploy.TeardownRequest{Dir: stackDir, DataDir: dataDir, Placement: p, App: app, Env: env, Destroy: destroy, By: takenDownBy(), Now: time.Now}
	}
	var a *deploy.ApplicationTeardown
	if f.all {
		a, err = deploy.PrepareApplication(ctx, d.deploy, request(""), out)
	} else {
		a, err = prepareOne(ctx, d.deploy, request(envs[0]), out)
	}
	if err != nil {
		return err
	}
	if a.Empty() {
		fmt.Fprintf(out, "Nothing of %s to take down.\n", app)

		return nil
	}
	fmt.Fprint(out, teardownWarning(destroy, app, envs, a))
	ok, err := d.confirmTeardown(cmd, command, word, f.yes)
	if err != nil || !ok {
		return err
	}

	return a.Run(ctx, out)
}

// prepareOne is the one environment's teardown as an application's, for the one run.
func prepareOne(ctx context.Context, clients *deploy.Clients, req *deploy.TeardownRequest, out io.Writer) (*deploy.ApplicationTeardown, error) {
	t, err := deploy.PrepareTeardown(ctx, clients, req, out)
	if err != nil {
		return nil, err
	}
	a := &deploy.ApplicationTeardown{}
	if t != nil {
		a.Environments = []*deploy.EnvironmentTeardown{t}
	}

	return a, nil
}

// teardownEnvironments are the environments the run takes down and the word typed back
// to go on: the one named, which must be one of the placement's, or with --all every one
// in promotion order and the application's name.
func teardownEnvironments(args []string, all bool, app string, p *derive.Placement) (envs []string, word string, err error) {
	switch {
	case all && len(args) > 0:
		return nil, "", errors.Newf("--%s takes every environment: name none", allFlag)
	case all:
		return p.Environments, app, nil
	case len(args) == 0:
		return nil, "", errors.Newf("name the environment (%s), or --%s for every one", strings.Join(p.Environments, ", "), allFlag)
	case !slices.Contains(p.Environments, args[0]):
		return nil, "", errors.Newf("%q is not one of the environments (%s)", args[0], strings.Join(p.Environments, ", "))
	default:
		return args[:1], args[0], nil
	}
}

// teardownWarning says what the run does before the name is typed back: what the
// command destroys and keeps, each environment's count by type, the pull-request stacks
// after them, and that nothing is undone.
func teardownWarning(destroy bool, app string, envs []string, a *deploy.ApplicationTeardown) string {
	var b strings.Builder
	in := strings.Join(envs, ", ")
	if destroy {
		fmt.Fprintf(&b, "bedrock destroy destroys %s's stack whole in %s: the database and every generation of it, the file stores and their objects, the Firestore database, the secret containers and their versions, the identities, the task queue, the migrate logs, the Firebase key, the services, the jobs, the triggers and the schedules. The deployment records stay in the records bucket, read by nothing; the Spanner backups the pipeline and the schedules took stay until their retention ends; a stack applied again takes an id of its own.\n\n", app, in)
	} else {
		fmt.Fprintf(&b, "bedrock teardown takes %s's stack in %s down to what it keeps: the Cloud Run services and jobs go, with their endpoint groups and backend services, the triggers, the schedules and the Cloud Armor policy; the database and every generation of it, the file stores, the secret containers, the identities, the Firestore database, the task queue, the migrate logs and the stack's id stay. Bringing an environment back is the stack's apply by hand (the README's \"Applying\") and bedrock rerun <env> <release>.\n\n", app, in)
	}
	b.WriteString(a.Summary())
	b.WriteString("\nNothing here is undone by bedrock.\n\n")

	return b.String()
}

// takenDownBy names who runs the command for the record: the signed-in user at the host.
func takenDownBy() string {
	name := nobody
	if u, err := user.Current(); err == nil && u.Username != "" {
		name = u.Username
	}
	if host, err := os.Hostname(); err == nil && host != "" {
		name += "@" + host
	}

	return name
}
