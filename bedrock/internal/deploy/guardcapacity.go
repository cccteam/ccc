// guardcapacity.go is the Spanner capacity guard: before an environment's stack or a
// pull request's is planned, a count of the databases the instance holds against Google's
// rule for its size, so a stack whose database does not exist yet is refused in a sentence
// here when the instance is full, and not by Google's refusal inside the apply, after the
// first environment's stack already stands or with the pull request's environment half
// made.

package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"regexp"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/derive"
)

// The environment layer's state prefix in the state bucket (the stack's remote state
// reads it there), and its output naming the instance the environment's databases live on.
const (
	envLayerPrefix        = "2-env/"
	spannerInstanceOutput = "spanner_instance"
)

// pullRequestDatabaseRE is a pull-request stack's database on the instance, in any
// generation: <app>-pr<N>-db.
var pullRequestDatabaseRE = regexp.MustCompile(`-pr\d+-db(-\d+)?$`)

// capacityTarget is what the guard counts for: the stack's database (an environment's,
// <prefix>-<env>-gbl-<app>-db, or a pull request's, <app>-pr<N>-db), on the instance the
// environment's databases live on (2-env's output in the state bucket), read as the
// identity.
type capacityTarget struct {
	identity, bucket, env, database string
	// impersonate says the build is not the identity and reads as it; false reads with
	// the build's own credentials, which are the identity's (a service account is granted
	// no token for itself).
	impersonate bool
}

// spannerInstance is 2-env's spanner_instance output: the project and name of the instance
// the environment's databases live on, the environment's own or the shared one.
type spannerInstance struct {
	Project string `json:"project"`
	Name    string `json:"name"`
}

// guardSpannerCapacity counts the databases on the environment's instance (2-env's
// spanner_instance output, read from the layer's state as the identity, or directly when
// the build runs as the identity: a service account is granted no token for itself) when the stack's
// database, in any generation, is not among them, against the databases the instance
// holds at its processing units (derive.SpannerDatabaseCapacity), and answers the refusal
// when it is full: the database, the instance, its count (and how many of them are
// pull-request stacks', whose closing is the first way out then) and its size, the
// setting that grows it (2-spn's processing_units for the shared instance, 2-env's
// spanner_instances for an environment's own) and the other way out, bedrock destroy of
// an application the organization no longer needs. A database already there needs no room, and the
// guard says so. Without a Spanner client or a store opener (tests of the command
// alone), without 2-env's state in the environment or its output (a 2-env applied by an
// older bedrock), or while the identity may not read the instance (a 1-org whose roles
// an older bedrock rendered), nothing is counted, said in the log: the guard meets the
// limit early where it can and never stands in for Google's own refusal.
func guardSpannerCapacity(ctx context.Context, clients *Clients, t capacityTarget, out io.Writer) (refusal string, err error) {
	if t.impersonate && (clients.SpannerAs == nil || clients.StorageAs == nil) || !t.impersonate && (clients.Spanner == nil || clients.Storage == nil) {
		return "", nil
	}
	identity, env, database := t.identity, t.env, t.database
	instance, ok, err := environmentInstance(ctx, clients, t, out)
	if err != nil || !ok {
		return "", err
	}
	name := "projects/" + instance.Project + "/instances/" + instance.Name
	s, err := openSpanner(ctx, clients, t)
	if err != nil {
		return "", err
	}
	databases, err := s.Databases(ctx, name)
	if err != nil {
		if isForbidden(err) {
			fmt.Fprintf(out, "Spanner capacity guard: %s may not list the databases of %s yet (%v); nothing is counted.\n", identity, name, errors.Cause(err))

			return "", nil
		}

		return "", errors.Wrapf(err, "listing the databases of %s as %s", name, identity)
	}
	for _, d := range databases {
		if base := path.Base(d); base == database || strings.HasPrefix(base, database+"-") {
			fmt.Fprintf(out, "Spanner capacity guard passed: %s holds %s.\n", name, base)

			return "", nil
		}
	}
	units, err := s.ProcessingUnits(ctx, name)
	if err != nil {
		if isForbidden(err) {
			fmt.Fprintf(out, "Spanner capacity guard: %s may not read %s yet (%v); nothing is counted.\n", identity, name, errors.Cause(err))

			return "", nil
		}

		return "", errors.Wrapf(err, "reading %s as %s", name, identity)
	}
	capacity := derive.SpannerDatabaseCapacity(units)
	if len(databases) < capacity {
		fmt.Fprintf(out, "Spanner capacity guard passed: %s holds %d of the %d databases its %d processing units allow, with room for %s.\n", name, len(databases), capacity, units, database)

		return "", nil
	}
	setting := "processing_units in 2-spn/terraform.tfvars"
	if instance.Project == identityProject(identity) {
		setting = "spanner_instances." + env + ".processing_units in 2-env/terraform.tfvars"
	}
	refusal = fmt.Sprintf("%s%s does not exist, and %s holds its maximum: %d databases%s, what its %d processing units allow (ten for every hundred below a node); %sraise %s (100 or 200; beyond 200 is a change to the variable's validation), or bedrock destroy an application the organization no longer needs, which frees its databases",
		rejected, database, name, len(databases), pullRequestDatabases(databases), units, pullRequestWayOut(databases), setting)
	fmt.Fprintln(out, refusal)

	return refusal, nil
}

// pullRequestDatabases says how many of the instance's databases are pull-request stacks',
// for the count; nothing when none is.
func pullRequestDatabases(databases []string) string {
	n := 0
	for _, d := range databases {
		if pullRequestDatabaseRE.MatchString(path.Base(d)) {
			n++
		}
	}
	if n == 0 {
		return ""
	}

	return fmt.Sprintf(", %d of them pull-request stacks'", n)
}

// pullRequestWayOut is the way out an instance holding pull-request stacks' databases
// offers first: a pull request closed or merged, whose stack the sweep takes down.
func pullRequestWayOut(databases []string) string {
	if pullRequestDatabases(databases) == "" {
		return ""
	}

	return "close or merge a pull request whose stack is up (the sweep takes a closed one's down, /gcbrun down takes it down now), "
}

// environmentTarget is an environment stack's database on the environment's instance.
func environmentTarget(identity string, impersonate bool, p *derive.Placement, app, env string) capacityTarget {
	return capacityTarget{identity: identity, impersonate: impersonate, bucket: p.StateBucket, env: env, database: p.Prefix + "-" + env + "-gbl-" + app + "-db"}
}

// pullRequestTarget is a pull request's own database, on the integration environment's
// instance.
func pullRequestTarget(identity string, impersonate bool, p *derive.Placement, app, env, number string) capacityTarget {
	return capacityTarget{identity: identity, impersonate: impersonate, bucket: p.StateBucket, env: env, database: app + "-pr" + number + "-db"}
}

// guardCapacity runs the Spanner capacity guard for a build with no pull request to
// answer, the stack trigger's first apply and a tag build's plan: a refusal is the error
// that stops the build.
func guardCapacity(ctx context.Context, clients *Clients, identity string, impersonate bool, p *derive.Placement, app, env string, out io.Writer) error {
	refusal, err := guardSpannerCapacity(ctx, clients, environmentTarget(identity, impersonate, p, app, env), out)
	if err != nil {
		return err
	}
	if refusal != "" {
		return errors.New(refusal)
	}

	return nil
}

// openStore opens Cloud Storage as the target's identity, or directly when the build runs
// as it; openSpanner the same for Spanner.
func openStore(ctx context.Context, clients *Clients, t capacityTarget) (Store, error) {
	if t.impersonate {
		return clients.StorageAs(ctx, t.identity)
	}

	return clients.Storage(ctx)
}

func openSpanner(ctx context.Context, clients *Clients, t capacityTarget) (Spanner, error) {
	if t.impersonate {
		return clients.SpannerAs(ctx, t.identity)
	}

	return clients.Spanner(ctx)
}

// environmentInstance reads the instance the environment's databases live on from 2-env's
// state in the bucket as the identity, or directly when the build runs as it; false, said
// in the log, when the layer has no state there or its state names no instance.
func environmentInstance(ctx context.Context, clients *Clients, t capacityTarget, out io.Writer) (spannerInstance, bool, error) {
	bucket, env := t.bucket, t.env
	store, err := openStore(ctx, clients, t)
	if err != nil {
		return spannerInstance{}, false, err
	}
	defer store.Close()
	state, ok, err := readState(ctx, store, bucket, envLayerPrefix+env)
	if err != nil {
		return spannerInstance{}, false, err
	}
	if !ok {
		fmt.Fprintf(out, "Spanner capacity guard: 2-env has no state in %s (gs://%s/%s%s); nothing is counted.\n", env, bucket, envLayerPrefix, env)

		return spannerInstance{}, false, nil
	}
	output, ok := state.Outputs[spannerInstanceOutput]
	if !ok {
		fmt.Fprintf(out, "Spanner capacity guard: 2-env's state in %s names no %s output (a 2-env applied by an older bedrock); nothing is counted.\n", env, spannerInstanceOutput)

		return spannerInstance{}, false, nil
	}
	var instance spannerInstance
	if err := json.Unmarshal(output.Value, &instance); err != nil {
		return spannerInstance{}, false, errors.Wrapf(err, "json.Unmarshal(): 2-env's %s output in %s", spannerInstanceOutput, env)
	}
	if instance.Project == "" || instance.Name == "" {
		fmt.Fprintf(out, "Spanner capacity guard: 2-env's %s output in %s names no instance; nothing is counted.\n", spannerInstanceOutput, env)

		return spannerInstance{}, false, nil
	}

	return instance, true, nil
}
