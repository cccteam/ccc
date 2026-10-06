// secrets.go holds the organization's two secrets, the private keys of the GitHub Apps
// the layers and the pipelines act as: where each lives (the container a layer creates,
// in the project the placement records) and where its version is pinned for the layers
// to read. bedrock secret add and pin take their names in the infrastructure repository,
// the way they take an environment and a variable in an application's repository.

package org

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/go-playground/errors/v5"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"

	"github.com/cccteam/ccc/bedrock/internal/secret"
)

// The organization's secrets, by the name the commands take.
const (
	// InfrastructureKey is the infrastructure GitHub App's private key: the container
	// 0-bootstrap creates in the boot project, its version pinned in placement.json
	// (githubInfrastructureKeyVersion), from which the layers workflow mints 1-org's
	// GitHub token.
	InfrastructureKey = "github-infrastructure-key"
	// DeployerKey is the deployer GitHub App's private key: the container 2-env creates
	// in each environment's project, its version pinned per environment in 2-env's
	// terraform.tfvars (github_deployer_key_secret_versions), from which the pipeline
	// talks back on a pull request.
	DeployerKey = "github-deployer-key"
	// DeployerKeyVersions is the attribute of 2-env's placement the deployer key's
	// versions are pinned in, by environment, each a version's resource name.
	DeployerKeyVersions = "github_deployer_key_secret_versions"
	// deployerAppID is the attribute of 2-env's placement naming the deployer app.
	deployerAppID = "github_deployer_app_id"
	// placementFile is the organization placement at the repository root.
	placementFile = "placement.json"
)

// Secrets are the organization's secrets, by the name the commands take.
var Secrets = []string{InfrastructureKey, DeployerKey}

// secretVersionRE is a pinned version: a positive number, never latest.
var secretVersionRE = regexp.MustCompile(`^[1-9]\d*$`)

// ValidateSecret checks that the name is one of the organization's secrets.
func ValidateSecret(name string) error {
	if !slices.Contains(Secrets, name) {
		return errors.Newf("%q is not an organization secret: one of %s", name, strings.Join(Secrets, ", "))
	}

	return nil
}

// SecretPerEnvironment reports whether the secret has a container in each environment's
// project (the deployer key), rather than one in the boot project.
func SecretPerEnvironment(name string) bool {
	return name == DeployerKey
}

// IsOrganizationPlacement reports whether the file is an organization placement: JSON
// that names the organization (organizationId). A file that is absent, or that is not
// JSON, is not one: an application's placement beside its stack names no organization,
// and whatever reads it reports its own faults.
func IsOrganizationPlacement(file string) (bool, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}

		return false, errors.Wrap(err, "os.ReadFile()")
	}

	return organizationID(data) != "", nil
}

// organizationID is the organization a placement's JSON names, or empty when it names
// none or is not JSON.
func organizationID(data []byte) string {
	var named struct {
		OrganizationID string `json:"organizationId"`
	}
	if json.Unmarshal(data, &named) != nil {
		return ""
	}

	return named.OrganizationID
}

// InfrastructureKeySecret is the Secret Manager container in the boot project that holds
// the infrastructure GitHub App's private key, as 0-bootstrap names it.
func (p *Placement) InfrastructureKeySecret() string {
	return p.Prefix + "-boot-gbl-github-infrastructure-key"
}

// DeployerKeySecret is the Secret Manager container in the environment's project that
// holds the deployer GitHub App's private key, as 2-env names it.
func (p *Placement) DeployerKeySecret(env string) string {
	return p.Prefix + "-" + env + "-gbl-github-deployer-key"
}

// SecretContainer is the secret's container: the infrastructure key's in the boot
// project, the deployer key's in the environment's.
func (p *Placement) SecretContainer(name, env string) string {
	if SecretPerEnvironment(name) {
		return p.DeployerKeySecret(env)
	}

	return p.InfrastructureKeySecret()
}

// SecretProject is the project holding the secret's container as the placement records
// it: the boot project (projects.boot) for the infrastructure key, the environment's
// (projects.<env>) for the deployer key. A project the placement does not record yet is
// refused.
func (p *Placement) SecretProject(name, env string) (string, error) {
	key, from := bootProject, "the seed's"
	if SecretPerEnvironment(name) {
		key, from = env, "1-org's project_ids"
	}
	project, ok := p.Projects[key]
	if !ok {
		return "", errors.Newf("placement.json records no project for %s (projects.%s): record %s value there, or pass --project", key, key, from)
	}

	return project, nil
}

// secretLayer names the layer that creates the secret's container, for a sentence.
func secretLayer(name, env string) string {
	if SecretPerEnvironment(name) {
		return envLayer + " in " + env
	}

	return bootstrapLayer
}

// secretWhat names the secret in a sentence: the app's key, and the environment.
func secretWhat(name, env string) string {
	if SecretPerEnvironment(name) {
		return "the deployer GitHub App's private key in " + env
	}

	return "the infrastructure GitHub App's private key"
}

// secretArgs are the secret's arguments on a command line: its name, and the
// environment for the deployer key.
func secretArgs(name, env string) string {
	if SecretPerEnvironment(name) {
		return name + " " + env
	}

	return name
}

// SecretAddRequest is what secret add was asked to do with an organization secret.
type SecretAddRequest struct {
	// Secret is the organization secret's name; Env the environment, for the deployer
	// key alone.
	Secret string
	Env    string
	// Project and Container are where the version goes.
	Project   string
	Container string
	// Value is the secret value, stored as given.
	Value []byte
}

// SecretAddResult is what secret add did: the version added, and where.
type SecretAddResult struct {
	Secret    string
	Env       string
	Project   string
	Container string
	Version   string
}

// AddSecret stores the value as a new version of the secret's container. The container
// is the layer's own resource, created by its apply and not adopted, so one the project
// lacks is refused rather than created: the layer's next apply would fail on it.
func AddSecret(ctx context.Context, open secret.ClientFunc, req *SecretAddRequest) (*SecretAddResult, error) {
	if err := ValidateSecret(req.Secret); err != nil {
		return nil, err
	}
	if len(req.Value) == 0 {
		return nil, errors.Newf("the value of %s is empty: nothing to add", req.Secret)
	}
	client, err := open(ctx, req.Project)
	if err != nil {
		return nil, err
	}
	if c, ok := client.(io.Closer); ok {
		defer c.Close()
	}
	name := "projects/" + req.Project + "/secrets/" + req.Container
	_, exists, err := client.GetSecret(ctx, name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.Newf("project %s has no container %s: the apply of %s creates it; apply the layer, then add the value", req.Project, req.Container, secretLayer(req.Secret, req.Env))
	}
	version, err := client.AddSecretVersion(ctx, name, req.Value)
	if err != nil {
		return nil, err
	}

	return &SecretAddResult{Secret: req.Secret, Env: req.Env, Project: req.Project, Container: req.Container, Version: version}, nil
}

// Write prints what was added and the pin to make.
func (r *SecretAddResult) Write(w io.Writer) {
	fmt.Fprintf(w, "Added version %s of %s in project %s: %s.\n", r.Version, r.Container, r.Project, secretWhat(r.Secret, r.Env))
	fmt.Fprintf(w, "Pin it: bedrock secret pin %s %s\n", secretArgs(r.Secret, r.Env), r.Version)
}

// SecretPinRequest is what secret pin was asked to do with an organization secret.
type SecretPinRequest struct {
	// Secret is the organization secret's name; Env the environment, for the deployer
	// key alone; Version the version's number.
	Secret  string
	Env     string
	Version string
	// Dir is the infrastructure repository's root, holding placement.json and 2-env.
	Dir string
	// Project and Container are where the version is verified.
	Project   string
	Container string
}

// SecretPinResult is what secret pin found and did.
type SecretPinResult struct {
	Secret  string
	Env     string
	Version string
	// File is where the pin went, and Value what it holds: the version's number in
	// placement.json, its resource name in 2-env's placement.
	File  string
	Value string
	// Unchanged is true when the version was pinned already and nothing was written.
	Unchanged bool
	// Project and Container are where Secret Manager confirmed the version enabled.
	Project   string
	Container string
	// Rendered is the number of owned files org render wrote after the placement
	// changed (the infrastructure key).
	Rendered int
	// AppUnset is true when the app's id is not recorded beside its key: placement.json's
	// githubInfrastructureAppId, or 2-env's github_deployer_app_id.
	AppUnset bool
}

// PinSecret writes the version where the layers read it, after Secret Manager confirms
// it is enabled: the infrastructure key's number in placement.json, followed by org
// render, which writes it into the layers workflow; the deployer key's resource name in
// 2-env's placement, under the environment. A version that is not a number (latest
// among them) is refused, and a version pinned already leaves every file as it is.
func PinSecret(ctx context.Context, open secret.ClientFunc, p *Placement, req *SecretPinRequest) (*SecretPinResult, error) {
	if err := ValidateSecret(req.Secret); err != nil {
		return nil, err
	}
	if !secretVersionRE.MatchString(req.Version) {
		return nil, errors.Newf("version %q: an organization secret is pinned by its number, never %s", req.Version, secret.Latest)
	}
	r := &SecretPinResult{Secret: req.Secret, Env: req.Env, Version: req.Version, Project: req.Project, Container: req.Container}
	if SecretPerEnvironment(req.Secret) {
		return pinDeployerKey(ctx, open, req, r)
	}
	r.File, r.Value = filepath.Join(req.Dir, placementFile), req.Version
	r.AppUnset = p.GithubInfrastructureAppID == ""
	if p.GithubInfrastructureKeyVersion == req.Version {
		r.Unchanged = true

		return r, nil
	}
	if err := verifySecret(ctx, open, req); err != nil {
		return nil, err
	}
	p.GithubInfrastructureKeyVersion = req.Version
	files, err := Render(p)
	if err != nil {
		return nil, err
	}
	if err := p.Write(r.File); err != nil {
		return nil, err
	}
	written, err := Write(files, req.Dir)
	if err != nil {
		return nil, err
	}
	r.Rendered = written.Owned

	return r, nil
}

// pinDeployerKey writes the deployer key's version, by resource name, into 2-env's
// placement under the environment.
func pinDeployerKey(ctx context.Context, open secret.ClientFunc, req *SecretPinRequest, r *SecretPinResult) (*SecretPinResult, error) {
	r.File = filepath.Join(req.Dir, filepath.FromSlash(EnvTfvars))
	r.Value = "projects/" + req.Project + "/secrets/" + req.Container + "/versions/" + req.Version
	layer, err := os.OpenRoot(filepath.Join(req.Dir, envLayer))
	if err != nil {
		return nil, errors.Wrap(err, "os.OpenRoot()")
	}
	defer layer.Close()
	src, err := layer.ReadFile(tfvarsFile)
	if err != nil {
		return nil, errors.Wrapf(err, "os.Root.ReadFile(): %s", EnvTfvars)
	}
	pinned, appSet, err := deployerValues(src)
	if err != nil {
		return nil, err
	}
	r.AppUnset = !appSet
	if pinned[req.Env] == r.Value {
		r.Unchanged = true

		return r, nil
	}
	if err := verifySecret(ctx, open, req); err != nil {
		return nil, err
	}
	out, err := secret.SetMapEntry(src, EnvTfvars, DeployerKeyVersions, req.Env, r.Value)
	if err != nil {
		return nil, err
	}
	info, err := layer.Stat(tfvarsFile)
	if err != nil {
		return nil, errors.Wrap(err, "os.Root.Stat()")
	}
	if err := layer.WriteFile(tfvarsFile, out, info.Mode().Perm()); err != nil {
		return nil, errors.Wrapf(err, "os.Root.WriteFile(): %s", EnvTfvars)
	}

	return r, nil
}

// deployerValues reads, from 2-env's placement, the deployer key's pinned versions by
// environment (none when the attribute is absent or not a map of strings) and whether
// the deployer app's id is set (present and not null).
func deployerValues(src []byte) (pinned map[string]string, appSet bool, err error) {
	f, diags := hclsyntax.ParseConfig(src, EnvTfvars, hcl.InitialPos)
	if diags.HasErrors() {
		return nil, false, errors.Wrapf(diags, "hclsyntax.ParseConfig(): %s", EnvTfvars)
	}
	body, ok := f.Body.(*hclsyntax.Body)
	if !ok {
		return nil, false, errors.Newf("%s: not native HCL syntax", EnvTfvars)
	}
	if attr, ok := body.Attributes[deployerAppID]; ok {
		v, diags := attr.Expr.Value(nil)
		appSet = !diags.HasErrors() && !v.IsNull()
	}
	pinned = map[string]string{}
	attr, ok := body.Attributes[DeployerKeyVersions]
	if !ok {
		return pinned, appSet, nil
	}
	v, diags := attr.Expr.Value(nil)
	if diags.HasErrors() || v.IsNull() || !v.CanIterateElements() {
		return pinned, appSet, nil
	}
	for it := v.ElementIterator(); it.Next(); {
		k, e := it.Element()
		if k.Type() == cty.String && e.Type() == cty.String && !e.IsNull() {
			pinned[k.AsString()] = e.AsString()
		}
	}

	return pinned, appSet, nil
}

// verifySecret asks Secret Manager whether the version exists and is enabled.
func verifySecret(ctx context.Context, open secret.ClientFunc, req *SecretPinRequest) error {
	client, err := open(ctx, req.Project)
	if err != nil {
		return err
	}
	if c, ok := client.(io.Closer); ok {
		defer c.Close()
	}
	v, err := client.GetSecretVersion(ctx, "projects/"+req.Project+"/secrets/"+req.Container+"/versions/"+req.Version)
	if err != nil {
		return err
	}
	if v.State != secret.Enabled {
		return errors.Newf("version %s of %s in project %s is %s, not %s: the layers could not read it; choose an enabled version", req.Version, req.Container, req.Project, v.State, secret.Enabled)
	}

	return nil
}

// Write prints what was pinned, where, and what to do next.
func (r *SecretPinResult) Write(w io.Writer) {
	what := r.Secret
	if r.Env != "" {
		what += " in " + r.Env
	}
	if r.Unchanged {
		fmt.Fprintf(w, "%s is already pinned to version %s (%s); nothing to change.\n", what, r.Version, r.place())
		r.writeAppUnset(w)

		return
	}
	fmt.Fprintf(w, "Pinned %s to version %s: %s.\n", what, r.Version, r.place())
	fmt.Fprintf(w, "Secret Manager confirms version %s of %s in project %s is enabled.\n", r.Version, r.Container, r.Project)
	r.writeAppUnset(w)
	if SecretPerEnvironment(r.Secret) {
		fmt.Fprintf(w, "Next: commit %s and open the pull request; the layers workflow's plan of 2-env shows the change to %s's output github_deployer_key_secret_version and nothing in the other environments, and the merge applies it. Each application's stack in %s passes the new version to its pipeline at its next apply.\n", EnvTfvars, r.Env, r.Env)

		return
	}
	fmt.Fprintf(w, "Next: commit placement.json and the files org render rewrote (%d owned file(s), the layers workflow among them) and open the pull request; from its merge on, the layers workflow mints 1-org's GitHub token from version %s.\n", r.Rendered, r.Version)
}

// place says where the pin is, for a sentence.
func (r *SecretPinResult) place() string {
	if SecretPerEnvironment(r.Secret) {
		return fmt.Sprintf("%s.%s in %s, %s", DeployerKeyVersions, r.Env, r.File, r.Value)
	}

	return "githubInfrastructureKeyVersion in " + r.File
}

// writeAppUnset says, when the app's id is not recorded beside the key, where it goes.
func (r *SecretPinResult) writeAppUnset(w io.Writer) {
	if !r.AppUnset {
		return
	}
	if SecretPerEnvironment(r.Secret) {
		fmt.Fprintf(w, "%s leaves %s unset: set it to the deployer app's App ID, from the app's settings page; until then the pipeline talks back through nothing.\n", EnvTfvars, deployerAppID)

		return
	}
	fmt.Fprintln(w, "placement.json records no githubInfrastructureAppId: record the infrastructure app's App ID there, from the app's settings page, and run bedrock org render; until both are recorded a run of 1-org through the layers workflow stops.")
}
