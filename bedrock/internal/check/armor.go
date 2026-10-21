// armor.go reads the stack's terraform.tfvars for a Cloud Armor policy the working tree
// removes in one step, and for a sign-in throttle threshold the policy could not take.
// The policy of an environment is on while cloud_armor names the environment with
// "preview" or "enforce", attached to the backend services; removing the entry detaches
// it and destroys it in one apply, which fails while the policy is attached. So an
// environment on at the default branch is set to "off" first (the policy kept,
// detached), applied, and then removed; the check refuses the one-step removal, the
// only form of the change the apply refuses, where the pull request is. The throttle's
// count and interval (cloud_armor_sign_in_count, cloud_armor_sign_in_interval) are
// positive whole numbers; the check refuses any other where the pull request is, as the
// stack's own validation does at plan.

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
	"github.com/cccteam/ccc/bedrock/internal/render"
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

// SignInThresholdFinding is one sign-in throttle variable in terraform.tfvars whose value
// the policy could not take: not a number, not whole, or not above zero.
type SignInThresholdFinding struct {
	// Variable is the variable, cloud_armor_sign_in_count or cloud_armor_sign_in_interval.
	Variable string
	// Value is the value as the file writes it, for the message.
	Value string
}

// Problem says what the variable is and what a value of it is.
func (f *SignInThresholdFinding) Problem() string {
	if f.Variable == render.SignInCountVariable {
		return f.Variable + " in " + tfvarsFile + " is " + f.Value + "; the sign-in throttle's count is a positive whole number: the POSTs one client address may make to a sign-in route in the interval before its further ones answer 429"
	}

	return f.Variable + " in " + tfvarsFile + " is " + f.Value + "; the sign-in throttle's interval is a positive whole number of seconds: how long the count is taken over"
}

// scanSignInThreshold reads the working tree's terraform.tfvars for the sign-in
// throttle's variables: a value that is not a number, not whole, or not above zero is a
// finding, the count's first. A variable left out takes the stack's default, and there
// is nothing to refuse; no file holds neither.
func scanSignInThreshold(dir string) ([]SignInThresholdFinding, error) {
	file := filepath.Join(dir, tfvarsFile)
	src, err := os.ReadFile(file)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}

		return nil, errors.Wrap(err, "os.ReadFile()")
	}
	body, err := parseTfvars(src, file)
	if err != nil {
		return nil, err
	}
	var findings []SignInThresholdFinding
	for _, name := range []string{render.SignInCountVariable, render.SignInIntervalVariable} {
		attr, ok := body.Attributes[name]
		if !ok {
			continue
		}
		v, diags := attr.Expr.Value(nil)
		if diags.HasErrors() {
			return nil, errors.Wrapf(diags, "%s in %s", name, file)
		}
		if ok, written := positiveWhole(v, src, attr.Expr.Range()); !ok {
			findings = append(findings, SignInThresholdFinding{Variable: name, Value: written})
		}
	}

	return findings, nil
}

// positiveWhole reports a value that is a number, whole and above zero, with the value as
// the source writes it, for a message when it is not.
func positiveWhole(v cty.Value, src []byte, at hcl.Range) (ok bool, written string) {
	written = string(at.SliceBytes(src))
	if v.IsNull() || !v.Type().Equals(cty.Number) {
		return false, written
	}
	n := v.AsBigFloat()

	return n.Sign() > 0 && n.IsInt(), written
}

// parseTfvars reads the placement source as native HCL, named for a message by file.
func parseTfvars(src []byte, file string) (*hclsyntax.Body, error) {
	f, diags := hclsyntax.ParseConfig(src, file, hcl.InitialPos)
	if diags.HasErrors() {
		return nil, errors.Wrap(diags, "hclsyntax.ParseConfig()")
	}
	body, ok := f.Body.(*hclsyntax.Body)
	if !ok {
		return nil, errors.Newf("%s: not native HCL syntax", file)
	}

	return body, nil
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
	body, err := parseTfvars(src, file)
	if err != nil {
		return nil, err
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
