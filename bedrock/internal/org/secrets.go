// secrets.go holds the three secrets a root organization keeps for every organization
// in the Google Cloud organization, the private keys of the GitHub Apps the layers and
// the pipelines act as: where each lives (one secret per App, which the root
// organization's bootstrap creates in its root project), the alias every reader names
// (current, on each secret), and where the version it points at is pinned (the root
// organization's placement.json). A hosting organization names the root organization
// that holds them (root) and pins nothing. bedrock secret add and pin take the secrets'
// names in the root organization's infrastructure repository, the way they take an
// environment and a variable in an application's repository; bedrock org secrets check
// proves a version there before the alias moves.

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
	// InfrastructureKey is the infrastructure GitHub App's private key: the secret a
	// root organization's bootstrap creates in its root project, the version its alias
	// points at pinned in that placement.json (githubInfrastructureKeyVersion), from
	// which a run of the triggers in every organization mints its second layer's GitHub
	// token.
	InfrastructureKey = "github-infrastructure-key"
	// DeployerKey is the deployer GitHub App's private key, held the same way
	// (githubDeployerKeyVersion in the root organization), from which the plan trigger's run posts the layers'
	// plans and every application's pipeline talks back on a pull request.
	DeployerKey = "github-deployer-key"
	// ReleaseKey is the release GitHub App's private key, held the same way
	// (githubReleaseKeyVersion), from which each application's release trigger in a
	// hosting organization mints the token release-please cuts the releases with.
	ReleaseKey = "github-release-key"
	// Token is the GitHub Bot Account's token: the secret a root organization's bootstrap
	// creates in its root project (<prefix>-root-gbl-github-token), the version its alias
	// points at pinned in that placement.json (githubTokenVersion), which every Cloud
	// Build connection in every organization authorizes with.
	Token = "github-token"
	// PlacementFile is the organization placement at the repository root.
	PlacementFile = "placement.json"
)

// Keys are the three GitHub Apps' keys, by the name the commands take; Secrets are every
// organization secret the commands take, the keys and the Bot Account's token.
var (
	Keys    = []string{InfrastructureKey, DeployerKey, ReleaseKey}
	Secrets = []string{InfrastructureKey, DeployerKey, ReleaseKey, Token}
)

// KeyAlias is the version alias every reader of a key names in place of a number
// (projects/<project>/secrets/<secret>/versions/current): the root organization's
// placement pins the version it points at, its 0-bootstrap renders it on the secret, and
// the apply moves it, which switches every reader in every organization at once.
const KeyAlias = "current"

// RootPlace names the root organization whose boot project holds the GitHub Apps' keys,
// in a hosting organization's placement (root).
type RootPlace struct {
	// Organization is the root organization's prefix, which names its secrets
	// (<prefix>-root-gbl-github-<app>-key).
	Organization string `json:"organization"`
	// Project is the root organization's root project, where the secrets live.
	Project string `json:"project"`
}

// HostingOrganization is a hosting organization under a root organization, in the root
// organization's placement (hostingOrganizations): one the root organization creates by
// pull request (bedrock org create; README.md, "Creating a hosting organization"), or one
// recorded as it stands. Its folder is recorded once the organization is created, and
// the root organization's grants to its identities render from then on: a grant names
// an identity that exists.
type HostingOrganization struct {
	// Prefix is the hosting organization's prefix.
	Prefix string `json:"prefix"`
	// BootProject is its boot project, where its boot identity lives; chosen when the
	// organization is recorded (bedrock org create picks the suffix), so the project's
	// id is known before anything is made.
	BootProject string `json:"bootProject"`
	// Folder is the numeric id of the organization's folder at the organization node,
	// recorded by the creation's hand-off (or by hand, for an organization that stood
	// before the root organization). Empty while the organization is not created yet.
	Folder string `json:"folder,omitempty"`
}

// Created reports whether the hosting organization's folder is recorded: the creation
// has handed it off, and the root organization's grants to its identities render.
func (h HostingOrganization) Created() bool {
	return h.Folder != ""
}

// CreatedHostingOrganizations are the hosting organizations whose folder is recorded,
// the ones 0-bootstrap grants on the shared secrets.
func (p *Placement) CreatedHostingOrganizations() []HostingOrganization {
	var created []HostingOrganization
	for _, h := range p.HostingOrganizations {
		if h.Created() {
			created = append(created, h)
		}
	}

	return created
}

// HostingOrganization is the record of the hosting organization with the prefix, nil
// when there is none.
func (p *Placement) HostingOrganization(prefix string) *HostingOrganization {
	for i := range p.HostingOrganizations {
		if p.HostingOrganizations[i].Prefix == prefix {
			return &p.HostingOrganizations[i]
		}
	}

	return nil
}

// BootIdentityMember is the hosting organization's boot identity as an IAM member, the
// principal the root organization grants the three secrets' policy administration.
func (h HostingOrganization) BootIdentityMember() string {
	return "serviceAccount:" + h.Prefix + "-boot-gbl-tofu@" + h.BootProject + ".iam.gserviceaccount.com"
}

// memberPrefixes start an IAM member a key operator may be.
var memberPrefixes = []string{"user:", "group:", "serviceAccount:"}

// validateKeys holds a hosting organization's placement to naming the root organization
// that holds the keys and the Cloud Build token, to reading the token from there, and to
// recording nothing of what that organization's placement holds: no key version, no
// key operators, no hosting organizations, and no larger runner, which is the GitHub
// organization's and declared once in the root organization's placement.
func (p *Placement) validateKeys() error {
	if p.Root == nil || p.Root.Organization == "" || p.Root.Project == "" {
		return errors.New("root is empty: a hosting organization reads the GitHub Apps' keys and the Cloud Build token from the root organization that holds them; record its prefix (root.organization) and its boot project (root.project)")
	}
	if !prefixRE.MatchString(p.Root.Organization) {
		return errors.Newf("root.organization %q is not an organization's prefix (2 to 4 lowercase alphanumeric characters starting with a letter)", p.Root.Organization)
	}
	if p.Root.Organization == p.Prefix {
		return errors.Newf("root.organization %q is this organization's own prefix: a hosting organization holds no key; name the root organization that holds them", p.Root.Organization)
	}
	if !projectIDRE.MatchString(p.Root.Project) {
		return errors.Newf("root.project %q is not a project id", p.Root.Project)
	}
	for name, value := range map[string]string{
		"githubInfrastructureKeyVersion": p.GithubInfrastructureKeyVersion, "githubDeployerKeyVersion": p.GithubDeployerKeyVersion, "githubReleaseKeyVersion": p.GithubReleaseKeyVersion,
		"githubTokenVersion": p.GithubTokenVersion,
	} {
		if value != "" {
			return errors.Newf("%s is set, and a hosting organization pins no key: the root organization %s pins the version in its own placement.json, and every reader here names the alias %s; remove it", name, p.Root.Organization, KeyAlias)
		}
	}
	if len(p.KeyOperators) > 0 || len(p.HostingOrganizations) > 0 {
		return errors.New("keyOperators and hostingOrganizations are a root organization's, which holds the keys; a hosting organization names it (root) and records neither")
	}
	if p.CiLargeRunner != "" || p.CiLargeRunnerSize != "" || p.CiLargeRunnerMaximum != 0 {
		return errors.Newf("ciLargeRunner, ciLargeRunnerSize and ciLargeRunnerMaximum are the GitHub organization's larger runner, declared once in the root organization %s's placement.json, whose apply sets the variable and makes the runner; remove them here", p.Root.Organization)
	}
	if p.CloudBuildTokenSecretVersion != "" && p.TokenSecretProject() != p.Root.Project {
		return errors.Newf("cloudBuildTokenSecretVersion names a secret in %s, and a hosting organization's connections read the token the root organization %s recorded once, in its root project %s (root.project): record that organization's cloudBuildTokenSecretVersion (its placement.json), which its hostingOrganizations grant gives this organization's boot identity the policy of", p.TokenSecretProject(), p.Root.Organization, p.Root.Project)
	}

	return nil
}

// TokenSecretProject is the project of the secret cloudBuildTokenSecretVersion names
// (projects/<project>/...), empty while unrecorded.
func (p *Placement) TokenSecretProject() string {
	parts := strings.Split(p.CloudBuildTokenSecretVersion, "/")
	if len(parts) < 2 || parts[0] != "projects" {
		return ""
	}

	return parts[1]
}

// validateKeyHolding holds a root organization's placement to what its keys' grants
// render from: no keys of another organization's, key operators that are IAM members,
// and hosting organizations each with a prefix of its own and a boot project.
func (p *Placement) validateKeyHolding() error {
	if p.Root != nil {
		return errors.New("root is set, and a root organization holds the GitHub Apps' keys itself, in its root project: root names the root organization a hosting organization reads them from")
	}
	for _, member := range p.KeyOperators {
		if !slices.ContainsFunc(memberPrefixes, func(prefix string) bool { return strings.HasPrefix(member, prefix) && len(member) > len(prefix) }) {
			return errors.Newf("keyOperators entry %q is not an IAM member (%s)", member, strings.Join(memberPrefixes, ", "))
		}
	}
	seen := map[string]bool{}
	for _, h := range p.HostingOrganizations {
		if !prefixRE.MatchString(h.Prefix) {
			return errors.Newf("hostingOrganizations entry %q is not an organization's prefix (2 to 4 lowercase alphanumeric characters starting with a letter)", h.Prefix)
		}
		if h.Prefix == p.Prefix {
			return errors.Newf("hostingOrganizations names %q, this organization's own prefix: it lists the hosting organizations that read the keys held here", h.Prefix)
		}
		if seen[h.Prefix] {
			return errors.Newf("hostingOrganizations names %q twice", h.Prefix)
		}
		seen[h.Prefix] = true
		if !projectIDRE.MatchString(h.BootProject) {
			return errors.Newf("hostingOrganizations entry %q: bootProject %q is not a project id", h.Prefix, h.BootProject)
		}
		if h.Folder != "" && !projectNumberRE.MatchString(h.Folder) {
			return errors.Newf("hostingOrganizations entry %q: folder %q is not a folder's numeric id", h.Prefix, h.Folder)
		}
	}

	return nil
}

// RootOrganization is the prefix of the organization whose boot project holds the keys,
// which names the secrets: a root organization's own, a hosting organization's
// root.organization (empty while unrecorded).
func (p *Placement) RootOrganization() string {
	if p.IsRoot() {
		return p.Prefix
	}
	if p.Root == nil {
		return ""
	}

	return p.Root.Organization
}

// RootProject is the project holding the keys: a root organization's root project
// (projects.root, empty until the bootstrap records it), a hosting organization's
// root.project.
func (p *Placement) RootProject() string {
	if p.IsRoot() {
		return p.Projects[RootProjectKey]
	}
	if p.Root == nil {
		return ""
	}

	return p.Root.Project
}

// KeysElsewhere refuses, in a hosting organization, what only the root organization that
// holds the keys does: adding, checking and pinning a version. Nil for a root
// organization.
func (p *Placement) KeysElsewhere() error {
	if p.IsRoot() {
		return nil
	}

	return errors.Newf("the GitHub Apps' keys live in the root organization %s (root.organization in placement.json), in its root project %s: add, check and pin a key in that organization's infrastructure repository; this organization reads each secret's alias %s, which that organization's placement pins", p.RootOrganization(), p.RootProject(), KeyAlias)
}

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

// InfrastructureKeySecret is the Secret Manager secret holding the infrastructure GitHub
// App's private key, as the root organization's 0-bootstrap names it in its boot
// project.
func (p *Placement) InfrastructureKeySecret() string {
	return p.RootOrganization() + "-" + RootProjectKey + "-gbl-github-infrastructure-key"
}

// DeployerKeySecret is the Secret Manager secret holding the deployer GitHub App's
// private key, as the root organization's bootstrap names it in its root project.
func (p *Placement) DeployerKeySecret() string {
	return p.RootOrganization() + "-" + RootProjectKey + "-gbl-github-deployer-key"
}

// ReleaseKeySecret is the Secret Manager secret holding the release GitHub App's private
// key, as the root organization's bootstrap names it in its root project.
func (p *Placement) ReleaseKeySecret() string {
	return p.RootOrganization() + "-" + RootProjectKey + "-gbl-github-release-key"
}

// SecretContainer is the secret's name in the project that holds the keys and the token.
func (p *Placement) SecretContainer(name string) string {
	switch name {
	case DeployerKey:
		return p.DeployerKeySecret()
	case ReleaseKey:
		return p.ReleaseKeySecret()
	case Token:
		return p.RootOrganization() + "-" + RootProjectKey + "-gbl-github-token"
	default:
		return p.InfrastructureKeySecret()
	}
}

// SecretProject is the project holding the keys as the placement records it: a root
// organization's root project (projects.root), a hosting organization's root.project.
// One not recorded yet is refused.
func (p *Placement) SecretProject() (string, error) {
	project := p.RootProject()
	if project == "" {
		if p.IsRoot() {
			return "", errors.Newf("placement.json records no root project (projects.%s): bedrock org bootstrap records it, or pass --project", RootProjectKey)
		}

		return "", errors.New("placement.json names no root organization (root): the keys live in the root organization's root project, and a hosting organization's placement names it")
	}

	return project, nil
}

// PinnedVersionName is the resource name of the version the placement pins for the key
// (projects/<root project>/secrets/<container>/versions/<number>), as the bootstrap reads
// a key before its apply has pointed the alias at it; empty while the placement pins
// none or records no root project.
func (p *Placement) PinnedVersionName(name string) string {
	project := p.RootProject()
	version := p.KeyVersion(name)
	if project == "" || version == "" {
		return ""
	}

	return "projects/" + project + "/secrets/" + p.SecretContainer(name) + "/versions/" + version
}

// SecretVersionName is the resource name every reader of the key names: the secret's
// alias current in the project that holds the keys, which that organization's apply
// points at the version pinned. Empty while the placement records no such project.
func (p *Placement) SecretVersionName(name string) string {
	project := p.RootProject()
	if project == "" {
		return ""
	}

	return "projects/" + project + "/secrets/" + p.SecretContainer(name) + "/versions/" + KeyAlias
}

// DeployerKeyVersionName is the resource name of the deployer key's alias, which 2-env's
// output carries to every application's pipeline; empty while no project holds the keys.
func (p *Placement) DeployerKeyVersionName() string {
	return p.SecretVersionName(DeployerKey)
}

// ReleaseKeyVersionName is the resource name of the release key's alias, which 2-env's
// output carries to every application's release trigger; empty while no project holds
// the keys.
func (p *Placement) ReleaseKeyVersionName() string {
	return p.SecretVersionName(ReleaseKey)
}

// pinned is where the placement pins the secret's version, and app the App ID recorded
// beside a key (the release app's is required of a hosting organization's placement; the
// token has none).
func (p *Placement) pinned(name string) *string {
	switch name {
	case DeployerKey:
		return &p.GithubDeployerKeyVersion
	case ReleaseKey:
		return &p.GithubReleaseKeyVersion
	case Token:
		return &p.GithubTokenVersion
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
	case Token:
		return ""
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
	case Token:
		return "the GitHub Bot Account's token, which every Cloud Build connection authorizes with"
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
	case Token:
		return "githubTokenVersion"
	default:
		return "githubInfrastructureKeyVersion"
	}
}

// releaseAppIDField is the placement field that names the release app by its App ID.
const releaseAppIDField = "githubReleaseAppId"

// appKey is the placement key the secret's app is named under, and appWhat what to
// record there.
func appKey(name string) string {
	switch name {
	case DeployerKey:
		return "githubDeployerAppId"
	case ReleaseKey:
		return releaseAppIDField
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

// PinSecret writes the version the secret's alias is to point at, after Secret Manager
// confirms it is enabled: the version's number in the root organization's
// placement.json (githubInfrastructureKeyVersion, githubDeployerKeyVersion or
// githubReleaseKeyVersion), followed by org render, which writes the alias into
// 0-bootstrap's github.tf; the apply moves it, and every reader in every organization
// follows. A version that is not a number (latest among them) is refused, a hosting
// organization is sent to the root organization that holds the keys, and a version
// pinned already leaves every file as it is.
func PinSecret(ctx context.Context, open secret.ClientFunc, p *Placement, req *SecretPinRequest) (*SecretPinResult, error) {
	if err := ValidateSecret(req.Secret); err != nil {
		return nil, err
	}
	if err := p.KeysElsewhere(); err != nil {
		return nil, err
	}
	if !secretVersionRE.MatchString(req.Version) {
		return nil, errors.Newf("version %q: an organization secret is pinned by its number, never %s", req.Version, secret.Latest)
	}
	r := &SecretPinResult{Secret: req.Secret, Version: req.Version, Project: req.Project, Container: req.Container, File: filepath.Join(req.Dir, PlacementFile), Key: pinKey(req.Secret)}
	pinned, app := p.pinned(req.Secret), p.app(req.Secret)
	r.AppUnset = req.Secret != Token && app == ""
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
	written, err := Write(p, files, req.Dir)
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
	fmt.Fprintf(w, "Next: commit placement.json and the %d file(s) org render rewrote, and open the pull request; its apply moves the alias %s of %s to version %s, and every reader in every organization (%s) names the alias, so all of them switch at that apply and nothing changes before it. %s\n", r.Rendered, KeyAlias, r.Container, r.Version, keyReaders(r.Secret), retireOld(r.Secret))
}

// keyReaders names who reads the secret, for the pin's next step.
func keyReaders(name string) string {
	switch name {
	case DeployerKey:
		return "each plan trigger's run posting the layers' plans, and every application's pipeline talking back on its pull requests"
	case ReleaseKey:
		return "every application's release trigger in the hosting organizations"
	case Token:
		return "every Cloud Build connection: the root project's, each boot project's for the layers' triggers and each environment's for the applications'"
	default:
		return "each organization's runs of the triggers that plan or apply its second layer"
	}
}

// retireOld says what to delete on GitHub once the alias has moved: a key on the app's
// settings page, or the Bot Account's old token.
func retireOld(name string) string {
	if name == Token {
		return "Then, on GitHub, as the Bot Account, delete the old token once the apply has gone through clean."
	}

	return "Then, on GitHub, delete the old key on the app's settings page once the next run has gone through clean."
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

// KeyVersion is the version placement.json pins for the secret (github-<app>-key, or
// github-token), empty while none is pinned.
func (p *Placement) KeyVersion(name string) string {
	return *p.pinned(name)
}

// TokenSecretID is the id of the secret a root organization's bootstrap puts the Bot
// Account's token in, in the root project (<prefix>-root-gbl-github-token), which every
// Cloud Build connection authorizes with through its alias current.
func (p *Placement) TokenSecretID() string {
	return p.Prefix + "-" + RootProjectKey + "-gbl-github-token"
}

// PinKeyVersion records the version of the secret (github-<app>-key, or github-token) the
// alias points at: a number, never latest.
func (p *Placement) PinKeyVersion(name, version string) error {
	if !projectNumberRE.MatchString(version) {
		return errors.Newf("version %q of %s is not a secret version's number (digits, never latest)", version, name)
	}
	*p.pinned(name) = version

	return nil
}
