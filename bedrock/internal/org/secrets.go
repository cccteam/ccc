// secrets.go holds the organization's two secrets, the private keys of the GitHub Apps
// the layers and the pipelines act as: where each lives (the container 0-bootstrap
// creates in the boot project) and where its version is pinned for the triggers' runs to
// read (placement.json). bedrock secret add and pin take their names in the
// infrastructure repository, the way they take an environment and a variable in an
// application's repository.

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

	"github.com/cccteam/ccc/bedrock/internal/secret"
)

// The organization's secrets, by the name the commands take.
const (
	// InfrastructureKey is the infrastructure GitHub App's private key: the container
	// 0-bootstrap creates in the boot project, its version pinned in placement.json
	// (githubInfrastructureKeyVersion), from which a run of the triggers mints 1-org's
	// GitHub token.
	InfrastructureKey = "github-infrastructure-key"
	// DeployerKey is the deployer GitHub App's private key: the container 0-bootstrap
	// creates in the boot project, its version pinned in placement.json
	// (githubDeployerKeyVersion), from which the plan trigger's run posts the layers'
	// plans and every application's pipeline talks back on a pull request.
	DeployerKey = "github-deployer-key"
	// ReleaseKey is the release GitHub App's private key: the container 0-bootstrap
	// creates in the boot project, its version pinned in placement.json
	// (githubReleaseKeyVersion), from which each application's release trigger mints the
	// token release-please cuts the releases with.
	ReleaseKey = "github-release-key"
	// PlacementFile is the organization placement at the repository root.
	PlacementFile = "placement.json"
)

// Secrets are the organization's secrets, by the name the commands take.
var Secrets = []string{InfrastructureKey, DeployerKey, ReleaseKey}

// secretVersionRE is a pinned version: a positive number, never latest.
var secretVersionRE = regexp.MustCompile(`^[1-9]\d*$`)

// ValidateSecret checks that the name is one of the organization's secrets.
func ValidateSecret(name string) error {
	if !slices.Contains(Secrets, name) {
		return errors.Newf("%q is not an organization secret: one of %s", name, strings.Join(Secrets, ", "))
	}

	return nil
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

// DeployerKeySecret is the Secret Manager container in the boot project that holds the
// deployer GitHub App's private key, as 0-bootstrap names it.
func (p *Placement) DeployerKeySecret() string {
	return p.Prefix + "-boot-gbl-github-deployer-key"
}

// ReleaseKeySecret is the Secret Manager container in the boot project that holds the
// release GitHub App's private key, as 0-bootstrap names it.
func (p *Placement) ReleaseKeySecret() string {
	return p.Prefix + "-boot-gbl-github-release-key"
}

// SecretContainer is the secret's container in the boot project.
func (p *Placement) SecretContainer(name string) string {
	switch name {
	case DeployerKey:
		return p.DeployerKeySecret()
	case ReleaseKey:
		return p.ReleaseKeySecret()
	default:
		return p.InfrastructureKeySecret()
	}
}

// SecretProject is the project holding the organization's secrets as the placement
// records it: the boot project (projects.boot). One not recorded yet is refused.
func (p *Placement) SecretProject() (string, error) {
	project, ok := p.Projects[bootProject]
	if !ok {
		return "", errors.Newf("placement.json records no project for %s (projects.%s): record the seed's value there, or pass --project", bootProject, bootProject)
	}

	return project, nil
}

// SecretVersionName is the resource name of the secret's pinned version in the boot
// project, or empty while the placement pins none or records no boot project.
func (p *Placement) SecretVersionName(name string) string {
	version := *p.pinned(name)
	project, ok := p.Projects[bootProject]
	if version == "" || !ok {
		return ""
	}

	return "projects/" + project + "/secrets/" + p.SecretContainer(name) + "/versions/" + version
}

// DeployerKeyVersionName is the resource name of the deployer key's pinned version,
// which 2-env's output carries to every application's pipeline; empty until pinned.
func (p *Placement) DeployerKeyVersionName() string {
	return p.SecretVersionName(DeployerKey)
}

// ReleaseKeyVersionName is the resource name of the release key's pinned version, which
// 2-env's output carries to every application's release trigger; empty until pinned.
func (p *Placement) ReleaseKeyVersionName() string {
	return p.SecretVersionName(ReleaseKey)
}

// pinned is where the placement pins the secret's version, and app the App ID recorded
// beside it (the release app's is required of every placement).
func (p *Placement) pinned(name string) *string {
	switch name {
	case DeployerKey:
		return &p.GithubDeployerKeyVersion
	case ReleaseKey:
		return &p.GithubReleaseKeyVersion
	default:
		return &p.GithubInfrastructureKeyVersion
	}
}

func (p *Placement) app(name string) string {
	switch name {
	case DeployerKey:
		return p.GithubDeployerAppID
	case ReleaseKey:
		return p.GithubReleaseAppID
	default:
		return p.GithubInfrastructureAppID
	}
}

// secretWhat names the secret in a sentence.
func secretWhat(name string) string {
	switch name {
	case DeployerKey:
		return "the deployer GitHub App's private key"
	case ReleaseKey:
		return "the release GitHub App's private key"
	default:
		return "the infrastructure GitHub App's private key"
	}
}

// pinKey is the placement key the secret's version is pinned under.
func pinKey(name string) string {
	switch name {
	case DeployerKey:
		return "githubDeployerKeyVersion"
	case ReleaseKey:
		return "githubReleaseKeyVersion"
	default:
		return "githubInfrastructureKeyVersion"
	}
}

// appKey is the placement key the secret's app is named under, and appWhat what to
// record there.
func appKey(name string) string {
	switch name {
	case DeployerKey:
		return "githubDeployerAppId"
	case ReleaseKey:
		return "githubReleaseAppId"
	default:
		return "githubInfrastructureAppId"
	}
}

// SecretAddRequest is what secret add was asked to do with an organization secret.
type SecretAddRequest struct {
	// Secret is the organization secret's name.
	Secret string
	// Project and Container are where the version goes.
	Project   string
	Container string
	// Value is the secret value, stored as given.
	Value []byte
}

// SecretAddResult is what secret add did: the version added, and where.
type SecretAddResult struct {
	Secret    string
	Project   string
	Container string
	Version   string
}

// AddSecret stores the value as a new version of the secret's container. The container
// is 0-bootstrap's own resource, created by its apply and not adopted, so one the project
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
		return nil, errors.Newf("project %s has no container %s: the apply of %s creates it; apply the layer, then add the value", req.Project, req.Container, bootstrapLayer)
	}
	version, err := client.AddSecretVersion(ctx, name, req.Value)
	if err != nil {
		return nil, err
	}

	return &SecretAddResult{Secret: req.Secret, Project: req.Project, Container: req.Container, Version: version}, nil
}

// Write prints what was added and the pin to make.
func (r *SecretAddResult) Write(w io.Writer) {
	fmt.Fprintf(w, "Added version %s of %s in project %s: %s.\n", r.Version, r.Container, r.Project, secretWhat(r.Secret))
	fmt.Fprintf(w, "Pin it: bedrock secret pin %s %s\n", r.Secret, r.Version)
}

// SecretPinRequest is what secret pin was asked to do with an organization secret.
type SecretPinRequest struct {
	// Secret is the organization secret's name; Version the version's number.
	Secret  string
	Version string
	// Dir is the infrastructure repository's root, holding placement.json.
	Dir string
	// Project and Container are where the version is verified.
	Project   string
	Container string
}

// SecretPinResult is what secret pin found and did.
type SecretPinResult struct {
	Secret  string
	Version string
	// File is where the pin went (placement.json) and Key the key it went under.
	File string
	Key  string
	// Unchanged is true when the version was pinned already and nothing was written.
	Unchanged bool
	// Project and Container are where Secret Manager confirmed the version enabled.
	Project   string
	Container string
	// Rendered is the number of owned files org render wrote after the placement
	// changed.
	Rendered int
	// AppUnset is true when the app's id is not recorded beside its key.
	AppUnset bool
}

// PinSecret writes the version where the triggers' runs read it, after Secret Manager
// confirms it is enabled: the version's number in placement.json
// (githubInfrastructureKeyVersion, githubDeployerKeyVersion or githubReleaseKeyVersion),
// followed by org render, which writes it into the layers (2-env's outputs of the
// deployer and release keys' versions among them). A version that is not a number (latest among them) is refused, and a version
// pinned already leaves every file as it is.
func PinSecret(ctx context.Context, open secret.ClientFunc, p *Placement, req *SecretPinRequest) (*SecretPinResult, error) {
	if err := ValidateSecret(req.Secret); err != nil {
		return nil, err
	}
	if !secretVersionRE.MatchString(req.Version) {
		return nil, errors.Newf("version %q: an organization secret is pinned by its number, never %s", req.Version, secret.Latest)
	}
	r := &SecretPinResult{Secret: req.Secret, Version: req.Version, Project: req.Project, Container: req.Container, File: filepath.Join(req.Dir, PlacementFile), Key: pinKey(req.Secret)}
	pinned, app := p.pinned(req.Secret), p.app(req.Secret)
	r.AppUnset = app == ""
	if *pinned == req.Version {
		r.Unchanged = true

		return r, nil
	}
	if err := verifySecret(ctx, open, req); err != nil {
		return nil, err
	}
	*pinned = req.Version
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
		return errors.Newf("version %s of %s in project %s is %s, not %s: the triggers' runs could not read it; choose an enabled version", req.Version, req.Container, req.Project, v.State, secret.Enabled)
	}

	return nil
}

// Write prints what was pinned, where, and what to do next.
func (r *SecretPinResult) Write(w io.Writer) {
	if r.Unchanged {
		fmt.Fprintf(w, "%s is already pinned to version %s (%s in %s); nothing to change.\n", r.Secret, r.Version, r.Key, r.File)
		r.writeAppUnset(w)

		return
	}
	fmt.Fprintf(w, "Pinned %s to version %s: %s in %s.\n", r.Secret, r.Version, r.Key, r.File)
	fmt.Fprintf(w, "Secret Manager confirms version %s of %s in project %s is enabled.\n", r.Version, r.Container, r.Project)
	r.writeAppUnset(w)
	if r.Secret == DeployerKey {
		fmt.Fprintf(w, "Next: commit placement.json and the files org render rewrote (%d owned file(s), 2-env's outputs among them) and open the pull request; from its merge on, the plan trigger's run posts the layers' plans as the deployer app from version %s, and each environment's apply of 2-env carries the version to the applications' pipelines through its output github_deployer_key_secret_version.\n", r.Rendered, r.Version)

		return
	}
	fmt.Fprintf(w, "Next: commit placement.json and the files org render rewrote (%d owned file(s)) and open the pull request; from its merge on, the triggers' runs mint 1-org's GitHub token from version %s.\n", r.Rendered, r.Version)
}

// writeAppUnset says, when the app's id is not recorded beside the key, where it goes.
func (r *SecretPinResult) writeAppUnset(w io.Writer) {
	if !r.AppUnset {
		return
	}
	if r.Secret == DeployerKey {
		fmt.Fprintf(w, "placement.json records no %s: record the deployer app's App ID there, from the app's settings page, and run bedrock org render; until both are recorded the plans are posted by nobody and the pipelines talk back through nothing.\n", appKey(r.Secret))

		return
	}
	fmt.Fprintf(w, "placement.json records no %s: record the infrastructure app's App ID there, from the app's settings page, and run bedrock org render; until both are recorded a run of the triggers that plans or applies 1-org stops.\n", appKey(r.Secret))
}
