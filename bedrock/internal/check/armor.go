// armor.go reads the stack's terraform.tfvars for a Cloud Armor policy the working tree
// removes in one step. The policy of an environment is on while cloud_armor names the
// environment with "preview" or "enforce", attached to the backend services; removing
// the entry detaches it and destroys it in one apply, which fails while the policy is
// attached. So an environment on at the default branch is set to "off" first (the policy
// kept, detached), applied, and then removed; the check refuses the one-step removal, the
// only form of the change the apply refuses, where the pull request is.

package check

import (
	"context"
	"os"
	"path/filepath"
	"slices"

	"github.com/go-playground/errors/v5"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"

	"github.com/cccteam/ccc/bedrock/internal/gitcmd"
	"github.com/cccteam/ccc/bedrock/internal/secret"
)

// The placement file and its Cloud Armor map, and the mode that keeps a policy detached.
const (
	tfvarsFile    = "terraform.tfvars"
	cloudArmorKey = "cloud_armor"
	cloudArmorOff = "off"
)

// CloudArmorFinding is one environment whose Cloud Armor entry the working tree removes
// while the default branch has the policy on.
type CloudArmorFinding struct {
	// Environment is the environment, the entry's key in cloud_armor.
	Environment string
	// Mode is the entry's value at the default branch: "preview" or "enforce".
	Mode string
	// Ref is the ref the default branch was read at (origin/<branch>, or the local branch).
	Ref string
}

// scanCloudArmor compares the working tree's cloud_armor map with the default branch's:
// an environment on there ("preview" or "enforce") and absent here is a finding, in the
// default branch's order. A directory outside a git working tree, a repository without
// the default branch, and a default branch without the file have nothing to compare, and
// the scan says nothing.
func scanCloudArmor(ctx context.Context, dir, branch string) ([]CloudArmorFinding, error) {
	now, err := cloudArmorModes(dir)
	if err != nil {
		return nil, err
	}
	if !gitcmd.InWorkTree(ctx, dir) {
		return nil, nil
	}
	ref, ok := defaultBranchRef(ctx, dir, branch)
	if !ok {
		return nil, nil
	}
	prefix, err := gitcmd.Prefix(ctx, dir)
	if err != nil {
		return nil, err
	}
	committed, ok, err := gitcmd.Show(ctx, dir, ref, prefix+tfvarsFile)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	was, err := parseCloudArmor(committed, ref+":"+prefix+tfvarsFile)
	if err != nil {
		return nil, err
	}
	var findings []CloudArmorFinding
	for _, env := range was.order {
		mode := was.modes[env]
		if _, kept := now.modes[env]; kept || mode == cloudArmorOff {
			continue
		}
		findings = append(findings, CloudArmorFinding{Environment: env, Mode: mode, Ref: ref})
	}

	return findings, nil
}

// defaultBranchRef is the ref the default branch is read at here, and false when the
// repository has no copy of the branch: nothing to compare with, not a refusal.
func defaultBranchRef(ctx context.Context, dir, branch string) (string, bool) {
	ref, _, err := gitcmd.DefaultRef(ctx, dir, branch)

	return ref, err == nil
}

// cloudArmor is a cloud_armor map: each environment's mode, and the environments in the
// file's order.
type cloudArmor struct {
	modes map[string]string
	order []string
}

// cloudArmorModes reads the working tree's map. No file and no map hold no entry.
func cloudArmorModes(dir string) (*cloudArmor, error) {
	file := filepath.Join(dir, tfvarsFile)
	src, err := os.ReadFile(file)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &cloudArmor{modes: map[string]string{}}, nil
		}

		return nil, errors.Wrap(err, "os.ReadFile()")
	}

	return parseCloudArmor(src, file)
}

// parseCloudArmor reads the map from the placement source, named for a message by
// file. A map that is not written out ({ ... }), a key that is not a string and a value
// that is not a literal string are refused.
func parseCloudArmor(src []byte, file string) (*cloudArmor, error) {
	f, diags := hclsyntax.ParseConfig(src, file, hcl.InitialPos)
	if diags.HasErrors() {
		return nil, errors.Wrap(diags, "hclsyntax.ParseConfig()")
	}
	body, ok := f.Body.(*hclsyntax.Body)
	if !ok {
		return nil, errors.Newf("%s: not native HCL syntax", file)
	}
	c := &cloudArmor{modes: map[string]string{}}
	attr, ok := body.Attributes[cloudArmorKey]
	if !ok {
		return c, nil
	}
	obj, ok := attr.Expr.(*hclsyntax.ObjectConsExpr)
	if !ok {
		return nil, errors.Newf("%s in %s is not a map written out ({ ... })", cloudArmorKey, file)
	}
	for _, item := range obj.Items {
		env, err := secret.ObjectKey(item.KeyExpr)
		if err != nil {
			return nil, errors.Wrapf(err, "%s in %s", cloudArmorKey, file)
		}
		v, diags := item.ValueExpr.Value(nil)
		if diags.HasErrors() {
			return nil, errors.Wrapf(diags, "%s.%s in %s", cloudArmorKey, env, file)
		}
		if v.IsNull() || !v.Type().Equals(cty.String) {
			return nil, errors.Newf("%s.%s in %s is not a string", cloudArmorKey, env, file)
		}
		if !slices.Contains(c.order, env) {
			c.order = append(c.order, env)
		}
		c.modes[env] = v.AsString()
	}

	return c, nil
}
