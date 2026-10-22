// capacity.go counts the databases the placement declares on the shared Spanner instance
// against what the instance holds: Google's rule (derive.SpannerDatabaseCapacity) applied
// to the processing units 2-spn declares, for the environments 2-env puts on the shared
// instance. bedrock org register refuses a registration past it, and bedrock org check
// says when a placement is, so the limit is met in a sentence here and not by Google's
// refusal inside a stack's apply.

package org

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-playground/errors/v5"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"

	"github.com/cccteam/ccc/bedrock/internal/derive"
	"github.com/cccteam/ccc/bedrock/internal/secret"
)

// The seeded values the count reads, each a person's file (org check does not compare
// them): 2-spn's processing units, and 2-env's placement of each environment's databases.
const (
	spnTfvarsFile       = "2-spn/terraform.tfvars"
	envTfvarsFile       = "2-env/terraform.tfvars"
	processingUnitsKey  = "processing_units"
	spannerInstancesKey = "spanner_instances"
	instancePlacement   = "placement"
	ownInstance         = "own"
	// defaultProcessingUnits is the shared instance's size when 2-spn's terraform.tfvars
	// does not set it: the variable's default, the smallest a multi-region instance can be.
	defaultProcessingUnits = 100
)

// SharedCapacity is the shared Spanner instance's capacity against the placement: the
// environments whose databases live on it (2-env's spanner_instances; by default every
// environment but the integration environment, which has an instance of its own), its
// processing units (2-spn's terraform.tfvars; the variable's default when unset), the
// databases Google lets an instance of that size hold, and the databases the registered
// applications declare there, one each per environment on it.
type SharedCapacity struct {
	Environments    []string
	ProcessingUnits int
	Capacity        int
	Applications    int
	Declared        int
}

// Exceeded reports a placement that declares more databases than the instance holds.
func (c SharedCapacity) Exceeded() bool {
	return c.Declared > c.Capacity
}

// Problem is the sentence for a placement past the instance: what is declared, what the
// instance holds at its size, and the two ways out.
func (c SharedCapacity) Problem() string {
	return fmt.Sprintf("the placement declares %d databases on the shared Spanner instance (%d applications, one database each in %s), and the instance holds %d at %d processing units (ten for every hundred below a node): raise %s in %s (100 or 200; beyond 200 is a change to the variable's validation, the decision of 2026-09-25 in JOURNAL.md), or bedrock destroy an application the organization no longer needs, which frees its databases",
		c.Declared, c.Applications, strings.Join(c.Environments, " and "), c.Capacity, c.ProcessingUnits, processingUnitsKey, spnTfvarsFile)
}

// SharedCapacity reads the shared instance's capacity against the placement from the
// organization checkout's seeded values: 2-env's spanner_instances and 2-spn's
// processing_units, each at its default when the file or the attribute is not there.
func (p *Placement) SharedCapacity(dir string) (SharedCapacity, error) {
	environments, err := sharedEnvironments(dir)
	if err != nil {
		return SharedCapacity{}, err
	}
	units, err := sharedProcessingUnits(dir)
	if err != nil {
		return SharedCapacity{}, err
	}
	c := SharedCapacity{Environments: environments, ProcessingUnits: units, Capacity: derive.SpannerDatabaseCapacity(units), Applications: len(p.Applications)}
	c.Declared = c.Applications * len(environments)

	return c, nil
}

// sharedEnvironments are the environments whose databases 2-env puts on the shared
// instance, in promotion order: those spanner_instances in the file places there, or
// leaves out (2-env's lookup places an environment it does not name on the shared
// instance); every environment but the integration environment when the file does not
// set the map (the variable's default, the ruling of 2026-09-26).
func sharedEnvironments(dir string) ([]string, error) {
	file := envTfvarsFile
	body, err := readValues(dir, file)
	if err != nil {
		return nil, err
	}
	own := map[string]bool{tstEnvironment: true}
	if attr, ok := attribute(body, spannerInstancesKey); ok {
		obj, ok := attr.Expr.(*hclsyntax.ObjectConsExpr)
		if !ok {
			return nil, errors.Newf("%s in %s is not a map written out ({ ... })", spannerInstancesKey, file)
		}
		own = map[string]bool{}
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
			own[env] = placement.AsString() == ownInstance
		}
	}
	var shared []string
	for _, env := range Environments {
		if !own[env] {
			shared = append(shared, env)
		}
	}

	return shared, nil
}

// sharedProcessingUnits are the shared instance's processing units: processing_units in
// the file, a whole number above zero, or the variable's default when the file or the
// attribute is not there.
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
	if v.IsNull() || !v.Type().Equals(cty.Number) {
		return 0, errors.Newf("%s in %s is not a number", processingUnitsKey, file)
	}
	units, accuracy := v.AsBigFloat().Int64()
	if accuracy != 0 || units <= 0 {
		return 0, errors.Newf("%s in %s is not a whole number above zero", processingUnitsKey, file)
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
