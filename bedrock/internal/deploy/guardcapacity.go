// guardcapacity.go is the Spanner capacity guard: before an environment's stack is
// planned, a count of the databases the environment's instance holds against Google's
// rule for its size, so a stack whose database does not exist yet is refused in a sentence
// here when the instance is full, and not by Google's refusal inside the apply, after the
// first environment's stack already stands.

package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path"
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

// spannerInstance is 2-env's spanner_instance output: the project and name of the instance
// the environment's databases live on, the environment's own or the shared one.
type spannerInstance struct {
	Project string `json:"project"`
	Name    string `json:"name"`
}

// guardSpannerCapacity counts the databases on the environment's instance (2-env's
// spanner_instance output, read from the layer's state as the identity) when the stack's
// database, in any generation, is not among them, against the databases the instance
// holds at its processing units (derive.SpannerDatabaseCapacity), and answers the refusal
// when it is full: the database, the instance, its count and its size, the setting that
// grows it (2-spn's processing_units for the shared instance, 2-env's spanner_instances
// for an environment's own) and the other way out, bedrock destroy of an application
// the organization no longer needs. A database already there needs no room, and the
// guard says so. Without a Spanner client or a store opener (tests of the command
// alone), without 2-env's state in the environment or its output (a 2-env applied by an
// older bedrock), or while the identity may not read the instance (a 1-org whose roles
// an older bedrock rendered), nothing is counted, said in the log: the guard meets the
// limit early where it can and never stands in for Google's own refusal.
func guardSpannerCapacity(ctx context.Context, clients *Clients, identity, bucket, prefix, app, env string, out io.Writer) (refusal string, err error) {
	if clients.SpannerAs == nil || clients.StorageAs == nil {
		return "", nil
	}
	instance, ok, err := environmentInstance(ctx, clients, identity, bucket, env, out)
	if err != nil || !ok {
		return "", err
	}
	name := "projects/" + instance.Project + "/instances/" + instance.Name
	database := prefix + "-" + env + "-gbl-" + app + "-db"
	s, err := clients.SpannerAs(ctx, identity)
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
	refusal = fmt.Sprintf("%s%s does not exist, and %s holds its maximum: %d databases, what its %d processing units allow (ten for every hundred below a node); raise %s (100 or 200; beyond 200 is a change to the variable's validation), or bedrock destroy an application the organization no longer needs, which frees its databases",
		rejected, database, name, len(databases), units, setting)
	fmt.Fprintln(out, refusal)

	return refusal, nil
}

// guardCapacity runs the Spanner capacity guard for a build with no pull request to
// answer, the stack trigger's first apply and a tag build's plan: a refusal is the error
// that stops the build.
func guardCapacity(ctx context.Context, clients *Clients, identity string, p *derive.Placement, app, env string, out io.Writer) error {
	refusal, err := guardSpannerCapacity(ctx, clients, identity, p.StateBucket, p.Prefix, app, env, out)
	if err != nil {
		return err
	}
	if refusal != "" {
		return errors.New(refusal)
	}

	return nil
}

// environmentInstance reads the instance the environment's databases live on from 2-env's
// state in the bucket as the identity; false, said in the log, when the layer has no
// state there or its state names no instance.
func environmentInstance(ctx context.Context, clients *Clients, identity, bucket, env string, out io.Writer) (spannerInstance, bool, error) {
	store, err := clients.StorageAs(ctx, identity)
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
