// capacity.go counts the databases the placement declares on each Spanner instance
// against what the instance holds: Google's rule (derive.SpannerDatabaseCapacity) applied
// to the processing units the layers declare, for the shared instance (2-spn) and for
// each environment's own (2-env). bedrock org register refuses a registration past an
// instance and says what is left for pull-request stacks, and bedrock org check says when
// a placement is past one, so the limit is met in a sentence here and not by Google's
// refusal inside a stack's apply.

package org

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/go-playground/errors/v5"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"

	"github.com/cccteam/ccc/bedrock/internal/derive"
	"github.com/cccteam/ccc/bedrock/internal/secret"
)

// The seeded values the count reads, each a person's file (org check does not compare
// them): 2-spn's processing units, and 2-env's placement and size of each environment's
// databases.
const (
	spnTfvarsFile       = "2-spn/terraform.tfvars"
	envTfvarsFile       = "2-env/terraform.tfvars"
	processingUnitsKey  = "processing_units"
	spannerInstancesKey = "spanner_instances"
	instancePlacement   = "placement"
	ownInstance         = "own"
	// defaultProcessingUnits is an instance's size when the values do not set it: the
	// variables' default, the smallest a multi-region instance can be.
	defaultProcessingUnits = 100
)

// InstanceCapacity is one Spanner instance's capacity against the placement: the shared
// instance (2-spn's) or an environment's own (2-env's), the environments whose databases
// live on it, its processing units, the databases Google lets an instance of that size
// hold, and the databases the registered applications declare there, one each per
// environment on it.
type InstanceCapacity struct {
	// Environment is the environment whose own instance this is; empty for the shared one.
	Environment     string
	Environments    []string
	ProcessingUnits int
	Capacity        int
	Applications    int
	Declared        int
}

// Shared reports the shared instance.
func (c InstanceCapacity) Shared() bool {
	return c.Environment == ""
}

// Name names the instance for a sentence.
func (c InstanceCapacity) Name() string {
	if c.Shared() {
		return "the shared Spanner instance"
	}

	return c.Environment + "'s own Spanner instance"
}

// Setting names the value that grows the instance: 2-spn's processing units for the
// shared instance, the environment's entry in 2-env's spanner_instances for its own.
func (c InstanceCapacity) Setting() string {
	if c.Shared() {
		return processingUnitsKey + " in " + spnTfvarsFile
	}

	return spannerInstancesKey + "." + c.Environment + "." + processingUnitsKey + " in " + envTfvarsFile
}

// Exceeded reports a placement that declares more databases than the instance holds.
func (c InstanceCapacity) Exceeded() bool {
	return c.Declared > c.Capacity
}

// Room is what the instance has left once the applications' databases are there: the
// databases pull-request stacks may hold, on the instance that holds the integration
// environment's.
func (c InstanceCapacity) Room() int {
	return c.Capacity - c.Declared
}

// Problem is the sentence for a placement past the instance: what is declared, what the
// instance holds at its size, and the two ways out.
func (c InstanceCapacity) Problem() string {
	return fmt.Sprintf("the placement declares %d databases on %s (%d applications, one database each in %s), and the instance holds %d at %d processing units (ten for every hundred below a node): raise %s (100 or 200; beyond 200 is a change to the variable's validation, the decision of 2026-09-25 in JOURNAL.md), or bedrock destroy an application the organization no longer needs, which frees its databases",
		c.Declared, c.Name(), c.Applications, strings.Join(c.Environments, " and "), c.Capacity, c.ProcessingUnits, c.Setting())
}

// PullRequestRoom is the sentence for the instance that holds the integration
// environment's databases, where every pull request's stack holds a database of its own
// unless it shares the environment's: what the applications take, and what is left.
func (c InstanceCapacity) PullRequestRoom(integration string) string {
	if c.Room() <= 0 {
		return fmt.Sprintf("%s: the applications' databases take all %d it holds, so no pull-request stack can hold a database of its own there (one shares %s's with /gcbrun shared-db) until %s is raised or an application is destroyed", c.Name(), c.Capacity, integration, c.Setting())
	}

	return fmt.Sprintf("%s: %d of the %d databases it holds go to the applications, %d are left for pull-request stacks across every application (each holds a database of its own unless it shares %s's)", c.Name(), c.Declared, c.Capacity, c.Room(), integration)
}

// InstanceCapacities reads each instance's capacity against the placement from the
// organization checkout's seeded values, 2-env's spanner_instances and 2-spn's
// processing_units, each at its default when the file or the attribute is not there: the
// shared instance first, when any environment is on it, then each environment's own in
// promotion order.
func (p *Placement) InstanceCapacities(dir string) ([]InstanceCapacity, error) {
	instances, err := environmentInstances(dir)
	if err != nil {
		return nil, err
	}
	var capacities []InstanceCapacity
	var shared []string
	for _, env := range Environments {
		if !instances[env].own {
			shared = append(shared, env)
		}
	}
	if len(shared) > 0 {
		units, err := sharedProcessingUnits(dir)
		if err != nil {
			return nil, err
		}
		capacities = append(capacities, p.instanceCapacity("", shared, units))
	}
	for _, env := range Environments {
		if instances[env].own {
			capacities = append(capacities, p.instanceCapacity(env, []string{env}, instances[env].units))
		}
	}

	return capacities, nil
}

// instanceCapacity is one instance's capacity against the registered applications.
func (p *Placement) instanceCapacity(environment string, environments []string, units int) InstanceCapacity {
	c := InstanceCapacity{Environment: environment, Environments: environments, ProcessingUnits: units, Capacity: derive.SpannerDatabaseCapacity(units), Applications: len(p.Applications)}
	c.Declared = c.Applications * len(environments)

	return c
}

// IntegrationInstance is the instance that holds the integration environment's
// databases, and with them every pull request's: the first environment's own, or the
// shared one.
func IntegrationInstance(capacities []InstanceCapacity) (InstanceCapacity, bool) {
	for _, c := range capacities {
		if slices.Contains(c.Environments, tstEnvironment) {
			return c, true
		}
	}

	return InstanceCapacity{}, false
}

// environmentInstance is where 2-env puts an environment's databases: an instance of its
// own, of the processing units, or the shared instance.
type environmentInstance struct {
	own   bool
	units int
}

// environmentInstances reads where each environment's databases live from 2-env's values:
// spanner_instances in the file (an environment it leaves out is on the shared instance,
// as 2-env's lookup places it, and an own instance without processing_units has the
// default), or the variable's default when the file does not set the map: the
// integration environment on an instance of its own, the others on the shared one (the
// ruling of 2026-09-26).
func environmentInstances(dir string) (map[string]environmentInstance, error) {
	file := envTfvarsFile
	body, err := readValues(dir, file)
	if err != nil {
		return nil, err
	}
	instances := map[string]environmentInstance{tstEnvironment: {own: true, units: defaultProcessingUnits}}
	attr, ok := attribute(body, spannerInstancesKey)
	if !ok {
		return instances, nil
	}
	obj, ok := attr.Expr.(*hclsyntax.ObjectConsExpr)
	if !ok {
		return nil, errors.Newf("%s in %s is not a map written out ({ ... })", spannerInstancesKey, file)
	}
	instances = map[string]environmentInstance{}
	for _, item := range obj.Items {
		env, err := secret.ObjectKey(item.KeyExpr)
		if err != nil {
			return nil, errors.Wrapf(err, "%s in %s", spannerInstancesKey, file)
		}
		v, diags := item.ValueExpr.Value(nil)
		if diags.HasErrors() {
			return nil, errors.Wrapf(diags, "%s.%s in %s", spannerInstancesKey, env, file)
		}
		if !v.Type().IsObjectType() || !v.Type().HasAttribute(instancePlacement) {
			return nil, errors.Newf("%s.%s in %s sets no %s", spannerInstancesKey, env, file, instancePlacement)
		}
		placement := v.GetAttr(instancePlacement)
		if placement.IsNull() || !placement.Type().Equals(cty.String) {
			return nil, errors.Newf("%s.%s.%s in %s is not a string", spannerInstancesKey, env, instancePlacement, file)
		}
		units := defaultProcessingUnits
		if v.Type().HasAttribute(processingUnitsKey) {
			if units, err = processingUnits(v.GetAttr(processingUnitsKey), spannerInstancesKey+"."+env+"."+processingUnitsKey, file); err != nil {
				return nil, err
			}
		}
		instances[env] = environmentInstance{own: placement.AsString() == ownInstance, units: units}
	}

	return instances, nil
}

// sharedProcessingUnits are the shared instance's processing units: processing_units in
// 2-spn's values, or the variable's default when the file or the attribute is not there.
func sharedProcessingUnits(dir string) (int, error) {
	file := spnTfvarsFile
	body, err := readValues(dir, file)
	if err != nil {
		return 0, err
	}
	attr, ok := attribute(body, processingUnitsKey)
	if !ok {
		return defaultProcessingUnits, nil
	}
	v, diags := attr.Expr.Value(nil)
	if diags.HasErrors() {
		return 0, errors.Wrapf(diags, "%s in %s", processingUnitsKey, file)
	}

	return processingUnits(v, processingUnitsKey, file)
}

// processingUnits is a processing-units value, a whole number above zero, named for a
// message by what and file.
func processingUnits(v cty.Value, what, file string) (int, error) {
	if v.IsNull() || !v.Type().Equals(cty.Number) {
		return 0, errors.Newf("%s in %s is not a number", what, file)
	}
	units, accuracy := v.AsBigFloat().Int64()
	if accuracy != 0 || units <= 0 {
		return 0, errors.Newf("%s in %s is not a whole number above zero", what, file)
	}

	return int(units), nil
}

// readValues parses a seeded values file of the checkout as native HCL, the file named
// as the layers name it (its path under the checkout, for a message); nil when there is
// no file, which sets nothing.
func readValues(dir, file string) (*hclsyntax.Body, error) {
	src, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(file)))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}

		return nil, errors.Wrap(err, "os.ReadFile()")
	}
	f, diags := hclsyntax.ParseConfig(src, file, hcl.InitialPos)
	if diags.HasErrors() {
		return nil, errors.Wrapf(diags, "parsing %s", file)
	}
	body, ok := f.Body.(*hclsyntax.Body)
	if !ok {
		return nil, errors.Newf("%s: not native HCL syntax", file)
	}

	return body, nil
}

// attribute is the top-level attribute of the body, and false without one (or without a
// body, when there was no file).
func attribute(body *hclsyntax.Body, name string) (*hclsyntax.Attribute, bool) {
	if body == nil {
		return nil, false
	}
	attr, ok := body.Attributes[name]

	return attr, ok
}
