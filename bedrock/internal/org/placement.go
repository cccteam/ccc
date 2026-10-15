// Package org renders an organization foundation: the six layers of the CCC provisioning
// model (the bootstrap, the organization, the shared registry, the shared Spanner
// instance, the network, and the environments) from an organization placement, the way
// the application stack is rendered from an application. The layers' .tf files and
// their READMEs are owned and rewritten on every render; each layer's terraform.tfvars,
// the journal and the ignore rules are seeded once and then a person's.
package org

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/derive"
	"github.com/cccteam/ccc/bedrock/internal/release"
)

const (
	// regionCount is the number of regions the model runs in: a primary and a secondary.
	regionCount = 2
	// githubLoginLength is the longest login GitHub gives an account.
	githubLoginLength = 39
	// replaceMe marks a value only a seed step can supply.
	replaceMe = "REPLACEME"
	// defaultApprovingReviews is the approving reviews a pull request needs before it
	// merges when the placement leaves approvingReviews out.
	defaultApprovingReviews = 1
)

var (
	prefixRE        = regexp.MustCompile(`^[a-z][a-z0-9]{1,3}$`)
	regionCodeRE    = regexp.MustCompile(`^[a-z][a-z0-9]{2}$`)
	applicationRE   = regexp.MustCompile(`^[a-z][a-z0-9]{0,5}$`)
	labelRE         = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,62}$`)
	projectNumberRE = regexp.MustCompile(`^\d+$`)
	// groupAddressRE is a group's address as the Workspace Admin console names it: a
	// mailbox at a domain, with no member prefix in front.
	groupAddressRE = regexp.MustCompile(`^[^@\s:/]+@[^@\s:/]+\.[^@\s:/]+$`)
	// githubLoginRE is a GitHub login: letters and digits, single hyphens between them.
	githubLoginRE = regexp.MustCompile(`^[A-Za-z0-9]+(-[A-Za-z0-9]+)*$`)
	// appSlugRE is a GitHub App's slug: lowercase letters and digits, hyphens between.
	appSlugRE = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	// secretVersionNameRE is a Secret Manager version by its resource name, regional or
	// global, numbered and never latest.
	secretVersionNameRE = regexp.MustCompile(`^projects/[^/]+(/locations/[^/]+)?/secrets/[^/]+/versions/[1-9][0-9]*$`)
	// sha256RE is a SHA-256 checksum as sha256sum prints it.
	sha256RE = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// The environments' entitlements, by the key placement.json names each under
// (entitlementDurations): the secret operator (bedrock secret add and pin), the Spanner
// admin and viewer of the environment's databases, and the layer administrator, who holds
// 2-env's apply identity's roles for a recovery (with the layer state entitlement, its
// slot in the state bucket, for the same longest grant). Each has a default longest
// grant, and Privileged Access Manager admits a longest grant between 30 minutes and 7
// days.
const (
	EntitlementSecretOperator     = "secretOperator"
	EntitlementSpannerAdmin       = "spannerAdmin"
	EntitlementSpannerViewer      = "spannerViewer"
	EntitlementLayerAdministrator = "layerAdministrator"

	shortestGrant = 30 * time.Minute
	longestGrant  = 7 * 24 * time.Hour
)

// Entitlements are the environments' entitlements in the order the layers declare them.
var Entitlements = []string{EntitlementSecretOperator, EntitlementSpannerAdmin, EntitlementSpannerViewer, EntitlementLayerAdministrator}

// entitlementDefaults are the longest grants when the placement sets none: an hour to
// add a secret, two to administer a database, four to read one or to recover a layer.
var entitlementDefaults = map[string]time.Duration{
	EntitlementSecretOperator:     time.Hour,
	EntitlementSpannerAdmin:       2 * time.Hour,
	EntitlementSpannerViewer:      4 * time.Hour,
	EntitlementLayerAdministrator: 4 * time.Hour,
}

// Placement is what the organization decided before any layer exists: its naming
// prefix, its domains, its billing account, its regions, its GitHub organization, and the
// few constants the layers restate. Values the seed step chooses (the boot project's
// suffix, the state bucket) are not here until it has run.
type Placement struct {
	// Prefix is the organization's naming prefix: every resource name starts with
	// <prefix>-<env>.
	Prefix string `json:"prefix"`
	// OrganizationDomain is the Google Cloud organization's domain, which is also the
	// Workspace domain the directory sign-ins restrict logins to.
	OrganizationDomain string `json:"organizationDomain"`
	// OrganizationID is the organization's numeric ID, for the seed steps.
	OrganizationID string `json:"organizationId"`
	// BillingAccount is the billing account projects are linked to.
	BillingAccount string `json:"billingAccount"`
	// AppsDomain is the domain the applications' hostnames hang under, never the
	// identity domain.
	AppsDomain string `json:"appsDomain"`
	// ZoneReplacement, while true, renders 2-net's apps zone and its parked zones without
	// prevent_destroy, so the change that makes a zone again (a new appsDomain, a
	// replacement) can be planned and applied; false, the default, renders the rule that
	// refuses it. It is set for the recreation alone and cleared once everything points
	// at the new zone (2-net/README.md, "Making a zone again"), which org check and
	// domain check remind of while it is set.
	ZoneReplacement bool `json:"zoneReplacement,omitempty"`
	// GithubOrganization is the GitHub organization holding the application
	// repositories and this one.
	GithubOrganization string `json:"githubOrganization"`
	// GithubMachineAccount is the GitHub login of the organization's machine account: a
	// GitHub user that belongs to the organization as an owner and acts for no person. It
	// signs in to GitHub in the browser step that authorizes the Cloud Build GitHub
	// connection, and owns the personal access token that can stand in for that step.
	GithubMachineAccount string `json:"githubMachineAccount"`
	// GithubReleaseAppID is the App ID of the release GitHub App, the only actor that
	// creates, moves or deletes a release tag of an application's repository: digits,
	// from the app's settings page. The id, not the slug: a private app cannot be
	// read by its slug with the operator's token.
	GithubReleaseAppID string `json:"githubReleaseAppId"`
	// GithubReleaseAppSlug is the same app's slug, the name in its page's address
	// (github.com/apps/<slug>): the releases it cuts carry <slug>[bot] as their author,
	// and an application's pipeline accepts a release from that author alone, so each
	// application's placement records it (releaseApp), from here.
	GithubReleaseAppSlug string `json:"githubReleaseAppSlug"`
	// GithubDefaultBranch is the default branch of every application repository, from
	// which alone the operations workflow's Environments deploy, and of this repository,
	// from which alone the apply trigger applies the layers.
	GithubDefaultBranch string `json:"githubDefaultBranch"`
	// GithubInfrastructureAppID is the App ID of the infrastructure GitHub App, whose
	// installation token a run of the triggers mints for 1-org's GitHub provider: digits,
	// from the app's settings page. Empty until the app exists; a run that plans or
	// applies 1-org refuses until it is recorded.
	GithubInfrastructureAppID string `json:"githubInfrastructureAppId,omitempty"`
	// GithubInfrastructureKeyVersion is the number of the Secret Manager version holding
	// the infrastructure app's private key, in the container 0-bootstrap creates in the
	// boot project (<prefix>-boot-gbl-github-infrastructure-key): pinned, never latest.
	// Empty until a version was added.
	GithubInfrastructureKeyVersion string `json:"githubInfrastructureKeyVersion,omitempty"`
	// GithubDeployerAppID is the App ID of the deployer GitHub App, which the
	// applications' pipelines talk back on a pull request as and the plan trigger's run
	// posts the layers' plans as: digits, from the app's settings page. Empty until the
	// app exists; until then the plans are in the run's log alone.
	GithubDeployerAppID string `json:"githubDeployerAppId,omitempty"`
	// GithubDeployerKeyVersion is the number of the Secret Manager version holding the
	// deployer app's private key, in the container 0-bootstrap creates in the boot
	// project (<prefix>-boot-gbl-github-deployer-key): pinned, never latest. Empty until
	// a version was added.
	GithubDeployerKeyVersion string `json:"githubDeployerKeyVersion,omitempty"`
	// GithubReleaseKeyVersion is the number of the Secret Manager version holding the
	// release GitHub App's private key, in the container 0-bootstrap creates in the boot
	// project (<prefix>-boot-gbl-github-release-key): pinned, never latest. Empty until
	// pinned; 2-env then carries the version to each application's release trigger, which
	// mints the token release-please cuts the releases with.
	GithubReleaseKeyVersion string `json:"githubReleaseKeyVersion,omitempty"`
	// CloudBuildInstallationID is the installation id of the Google Cloud Build GitHub
	// App on the GitHub organization (digits, from the installation's settings page),
	// which every Cloud Build connection names: the boot project's for the layers and
	// each environment's for the applications. Recorded after the browser authorization
	// in the boot project's Cloud Build console (0-bootstrap/README.md); empty until
	// then, and the connections, the repository links and the triggers wait.
	CloudBuildInstallationID string `json:"cloudBuildInstallationId,omitempty"`
	// CloudBuildTokenSecretVersion is the resource name of the Secret Manager version
	// holding the GitHub token the connections authorize with, which the same browser
	// step writes into the boot project: projects/<boot project>/locations/<region>/
	// secrets/<name>/versions/<n> as the console writes it (a regional secret in the
	// connection's region), or projects/<boot project>/secrets/<name>/versions/<n> for a
	// global one. A numbered version, never latest. Empty with CloudBuildInstallationID.
	CloudBuildTokenSecretVersion string `json:"cloudBuildTokenSecretVersion,omitempty"`
	// BedrockVersion is the bedrock the triggers' runs fetch at their first step and run
	// (bedrock upgrade moves it): a release (v0.5.0), whose linux/amd64 binary
	// BedrockSHA256 is the checksum of, or a commit pin (the pseudo-version the Go
	// module proxy names a commit by), which the run builds with go install and carries
	// no checksum. Empty until recorded, and a run of the triggers stops at its first
	// step saying so.
	BedrockVersion string `json:"bedrockVersion,omitempty"`
	BedrockSHA256  string `json:"bedrockSha256,omitempty"`
	// GithubInfrastructureTeam is the slug of the organization's infrastructure team,
	// whose approval a change to an application's workflow and Cloud Build files
	// needs; empty for none.
	GithubInfrastructureTeam string `json:"githubInfrastructureTeam,omitempty"`
	// ApprovingReviews is how many approving reviews a pull request needs before it
	// merges, on every application repository's default branch and hotfix lines and on
	// this repository's default branch: one when left out, 0 for none (a writer merges
	// alone once the checks pass). A pointer, so that 0 and left out differ. 1-org
	// renders it into github.tf, an owned file bedrock org check compares with the
	// render, so nobody lowers it in a file of their own.
	ApprovingReviews *int `json:"approvingReviews,omitempty"`
	// CiLargeRunner names the larger runner the applications' CI runs the jobs its
	// //impulse:ci line chooses on (the two test legs and the image build without a
	// line): a runner's name, a runner label or a runner group of the organization, set
	// once as the organization's Actions variable CI_LARGE_RUNNER, which every
	// application's workflow reads. Empty for GitHub's standard runner, and no variable.
	CiLargeRunner string `json:"ciLargeRunner,omitempty"`
	// CiLargeRunnerSize, when set, has 1-org create the larger runner itself: a
	// GitHub-hosted runner named CiLargeRunner, of this machine size as GitHub lists them
	// (4-core, 8-core, 16-core, 32-core, 64-core, 96-core, or an arm- size), on GitHub's
	// current Ubuntu, in a runner group of the application repositories alone. Empty
	// when CiLargeRunner names a runner or a group the organization already has.
	CiLargeRunnerSize string `json:"ciLargeRunnerSize,omitempty"`
	// CiLargeRunnerMaximum caps how many of the created runner run at once (GitHub's
	// maximum_runners), the spend's ceiling; 0 leaves GitHub's default.
	CiLargeRunnerMaximum int `json:"ciLargeRunnerMaximum,omitempty"`
	// SourceRepo is this repository's name, the source_repo label every resource carries.
	SourceRepo string `json:"sourceRepo"`
	// StateBucket is the seeded state bucket every backend block names; empty until the
	// seed has run, and REPLACEME is rendered in its place.
	StateBucket string `json:"stateBucket,omitempty"`
	// Operator is the bootstrap administrator's account, who seeds and applies by hand.
	Operator string `json:"operator"`
	// TeamGroups are the environments' team groups by environment, each a group's
	// address (team-tst@example.com, with no member prefix: the layers write group: in
	// front). Each person's access to an environment comes from its group: the group
	// holds the release approval where a release waits for one, and its members ask for
	// the environment's entitlements. One per environment; the same address may serve
	// several, but production's group is not the first environment's.
	TeamGroups map[string]string `json:"teamGroups"`
	// EntitlementDurations are the longest grants of the environments' entitlements, by
	// entitlement (Entitlements) as a duration (1h, 90m); an entitlement not named takes
	// its default. Between 30 minutes and 7 days.
	EntitlementDurations map[string]string `json:"entitlementDurations,omitempty"`
	// Regions are the two Cloud Run regions, the primary first.
	Regions []Region `json:"regions"`
	// Spanner is the shared instance's configuration.
	Spanner Spanner `json:"spanner"`
	// OutlierDetection is the outlier detection thresholds of the backend service 2-env
	// renders for the pull-request environments, and those an application's first
	// placement takes for its own backend service; a field left out, or the whole block,
	// takes its default (derive.OutlierDetection).
	OutlierDetection *derive.OutlierDetection `json:"outlierDetection,omitempty"`
	// CloudArmor is the Cloud Armor policy an application's first placement takes for
	// its backend services: the rule sets in order at their sensitivities, the fields a
	// set leaves alone, and the paths allowed ahead of the sets; a block left out is
	// bedrock's defaults (derive.CloudArmor). An environment turns the policy on in the
	// application's terraform.tfvars; nothing of the organization's carries one.
	CloudArmor *derive.CloudArmor `json:"cloudArmor,omitempty"`
	// ContactDomains are the domains Essential Contacts may belong to, each with its
	// leading @.
	ContactDomains []string `json:"contactDomains"`
	// Applications are the application codes registered in every environment, in the
	// order they were added; empty for an organization with none yet. bedrock org
	// register adds one; the layers' applications.auto.tfvars are rendered from it.
	Applications []string `json:"applications"`
	// Teardown says the organization is torn down: bedrock org teardown sets it, and
	// 2-net renders with no hosts and its load balancer's listeners gone (serving =
	// false in applications.auto.tfvars), the address, the certificate and its DNS
	// authorization kept. A person flips it back on a pull request once every
	// application's stack is applied again.
	Teardown bool `json:"teardown,omitempty"`
	// Projects are the projects by key: boot, the project the seed chose, and shr, net,
	// spn, tst, stg and prd, the ids 1-org's apply chose (its project_ids output). The
	// environment projects are what the applications' identities and backends are named
	// under; every project names the layer identities the triggers' runs impersonate.
	// Absent until the seed or 1-org has run; REPLACEME is rendered in their place.
	Projects map[string]string `json:"projects,omitempty"`
	// ProjectNumbers are the projects' numbers by the same keys: boot from the seed, the
	// rest from 1-org's project_numbers output, recorded beside Projects. 0-bootstrap
	// grants each recorded environment project's Cloud Build service agent the read of
	// the connections' token secret by its number. An application's placement records
	// its environments' ids and numbers together (bedrock org register writes them into
	// its first placement, org render prints the block), for the operations workflow
	// that starts a restore from GitHub.
	ProjectNumbers map[string]string `json:"projectNumbers,omitempty"`
	// Labels are the labels every project and bucket carries beyond the model's own.
	Labels map[string]string `json:"labels"`
}

// The model's environments, by name.
const (
	tstEnvironment = "tst"
	stgEnvironment = "stg"
	prdEnvironment = "prd"
)

// Environments are the model's environments in promotion order; pull requests deploy
// to the first, production is the last.
var Environments = []string{tstEnvironment, stgEnvironment, prdEnvironment}

// The model's other project keys: the boot project, and the three shared projects.
const (
	bootProject = "boot"
	shrProject  = "shr"
	netProject  = "net"
	spnProject  = "spn"
)

// ProjectKeys are the model's projects by key, in layer order: the boot project (the
// seed's, 0-bootstrap's and 1-org's layers run there), the shared projects, then the
// environment projects.
var ProjectKeys = []string{bootProject, shrProject, netProject, spnProject, tstEnvironment, stgEnvironment, prdEnvironment}

// Region is one region: its name and the three-letter code resource names carry.
type Region struct {
	Name string `json:"name"`
	Code string `json:"code"`
}

// Spanner is the shared instance's configuration.
type Spanner struct {
	// Config is the instance configuration, a multi-region one bedrock knows
	// (SpannerConfigs) whose read-write replicas are the two regions (nam10 for
	// us-central1 and us-west3); any other is refused, naming the ones that fit.
	Config string `json:"config"`
}

// ReadPlacement reads and validates a placement file.
func ReadPlacement(path string) (*Placement, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.Wrap(err, "os.ReadFile()")
	}
	var p Placement
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, errors.Wrapf(err, "json.Unmarshal(): %s", path)
	}
	if err := p.Validate(); err != nil {
		return nil, errors.Wrapf(err, "%s", path)
	}

	return &p, nil
}

// Validate refuses a placement a layer could not be rendered from.
func (p *Placement) Validate() error {
	if !prefixRE.MatchString(p.Prefix) {
		return errors.Newf("prefix %q is not 2 to 4 lowercase alphanumeric characters starting with a letter", p.Prefix)
	}
	for name, value := range map[string]string{
		"organizationDomain": p.OrganizationDomain, "organizationId": p.OrganizationID, "billingAccount": p.BillingAccount,
		"appsDomain": p.AppsDomain, "githubOrganization": p.GithubOrganization, "sourceRepo": p.SourceRepo,
		"githubReleaseAppId": p.GithubReleaseAppID, "githubDefaultBranch": p.GithubDefaultBranch,
		"operator": p.Operator, "spanner.config": p.Spanner.Config,
	} {
		if strings.TrimSpace(value) == "" {
			return errors.Newf("%s is empty", name)
		}
	}
	if len(p.Regions) != regionCount {
		return errors.Newf("regions has %d entries; the model runs in two, the primary first", len(p.Regions))
	}
	for _, r := range p.Regions {
		if r.Name == "" || !regionCodeRE.MatchString(r.Code) {
			return errors.Newf("region %q needs a name and a three-character code", r.Name)
		}
	}
	if err := p.validateSpanner(); err != nil {
		return err
	}
	if err := p.OutlierDetection.Validate(); err != nil {
		return err
	}
	if err := p.CloudArmor.Validate(); err != nil {
		return err
	}
	for _, validate := range []func() error{
		p.validateGithubApps, p.validateApprovingReviews, p.validateCloudBuild, p.validateBedrockPin,
		p.validateCiLargeRunner, p.validateProjects, p.validateTeamGroups, p.validateEntitlementDurations,
	} {
		if err := validate(); err != nil {
			return err
		}
	}
	for _, d := range p.ContactDomains {
		if !strings.HasPrefix(d, "@") || len(d) < 3 {
			return errors.Newf("contact domain %q is not @<domain>", d)
		}
	}
	for _, a := range p.Applications {
		if !applicationRE.MatchString(a) {
			return errors.Newf("application %q is not 1 to 6 lowercase alphanumeric characters starting with a letter", a)
		}
	}
	for k, v := range p.Labels {
		if !labelRE.MatchString(k) || v == "" {
			return errors.Newf("label %q = %q is not a lowercase key with a value", k, v)
		}
	}

	return nil
}

// validateGithubApps refuses a GitHub App named by its slug instead of its App ID, a
// release app's slug that is not one, a key version that is not a version's number
// (latest among them), and a machine account that is not named or not a GitHub login.
func (p *Placement) validateGithubApps() error {
	if strings.TrimSpace(p.GithubMachineAccount) == "" {
		return errors.New("githubMachineAccount is empty: the GitHub login of the organization's machine account, an owner of the organization, which authorizes the Cloud Build GitHub connection in the browser and owns the personal access token that can stand in for it")
	}
	if len(p.GithubMachineAccount) > githubLoginLength || !githubLoginRE.MatchString(p.GithubMachineAccount) {
		return errors.Newf("githubMachineAccount %q is not a GitHub login (letters, digits and single hyphens, at most %d characters)", p.GithubMachineAccount, githubLoginLength)
	}
	if !projectNumberRE.MatchString(p.GithubReleaseAppID) {
		return errors.Newf("githubReleaseAppId %q is not an App ID (digits)", p.GithubReleaseAppID)
	}
	if !appSlugRE.MatchString(p.GithubReleaseAppSlug) {
		return errors.Newf("githubReleaseAppSlug %q is not a GitHub App's slug (lowercase letters, digits and single hyphens, as in github.com/apps/<slug>): the release app's slug, which every application's placement records as the author of its releases", p.GithubReleaseAppSlug)
	}
	if p.GithubInfrastructureAppID != "" && !projectNumberRE.MatchString(p.GithubInfrastructureAppID) {
		return errors.Newf("githubInfrastructureAppId %q is not an App ID (digits)", p.GithubInfrastructureAppID)
	}
	if p.GithubInfrastructureKeyVersion != "" && !projectNumberRE.MatchString(p.GithubInfrastructureKeyVersion) {
		return errors.Newf("githubInfrastructureKeyVersion %q is not a secret version's number (digits, never latest)", p.GithubInfrastructureKeyVersion)
	}
	if p.GithubDeployerAppID != "" && !projectNumberRE.MatchString(p.GithubDeployerAppID) {
		return errors.Newf("githubDeployerAppId %q is not an App ID (digits)", p.GithubDeployerAppID)
	}
	if p.GithubDeployerKeyVersion != "" && !projectNumberRE.MatchString(p.GithubDeployerKeyVersion) {
		return errors.Newf("githubDeployerKeyVersion %q is not a secret version's number (digits, never latest)", p.GithubDeployerKeyVersion)
	}
	if p.GithubReleaseKeyVersion != "" && !projectNumberRE.MatchString(p.GithubReleaseKeyVersion) {
		return errors.Newf("githubReleaseKeyVersion %q is not a secret version's number (digits, never latest)", p.GithubReleaseKeyVersion)
	}

	return nil
}

// validateApprovingReviews refuses a negative number of approving reviews; left out is
// one, and 0 is none.
func (p *Placement) validateApprovingReviews() error {
	if p.ApprovingReviews != nil && *p.ApprovingReviews < 0 {
		return errors.Newf("approvingReviews %d is negative: the approving reviews a pull request needs before it merges, 0 for none", *p.ApprovingReviews)
	}

	return nil
}

// ApprovingReviewCount is the approving reviews a pull request needs before it merges,
// on every application repository's default branch and hotfix lines and on this
// repository's default branch: the placement's approvingReviews, or one when it is left
// out.
func (p *Placement) ApprovingReviewCount() int {
	if p.ApprovingReviews == nil {
		return defaultApprovingReviews
	}

	return *p.ApprovingReviews
}

// validateCloudBuild refuses an installation id that is not one, a token secret version
// that is not a version's resource name or is latest, and one of the two recorded without
// the other: the browser authorization produces both at once.
func (p *Placement) validateCloudBuild() error {
	if p.CloudBuildInstallationID != "" && !projectNumberRE.MatchString(p.CloudBuildInstallationID) {
		return errors.Newf("cloudBuildInstallationId %q is not an installation id (digits, from the installation's settings page)", p.CloudBuildInstallationID)
	}
	if p.CloudBuildTokenSecretVersion != "" && !secretVersionNameRE.MatchString(p.CloudBuildTokenSecretVersion) {
		return errors.Newf("cloudBuildTokenSecretVersion %q is not a secret version's resource name (projects/<project>[/locations/<region>]/secrets/<secret>/versions/<number>, a number and never latest)", p.CloudBuildTokenSecretVersion)
	}
	if (p.CloudBuildInstallationID == "") != (p.CloudBuildTokenSecretVersion == "") {
		return errors.New("cloudBuildInstallationId and cloudBuildTokenSecretVersion are recorded together: the browser authorization in the boot project's Cloud Build console gives both (0-bootstrap/README.md, \"The Cloud Build GitHub authorization\")")
	}

	return nil
}

// validateBedrockPin refuses a pin that is neither a release nor a commit pin, a release
// without its binary's checksum or with one that is not a checksum, and a commit pin with
// a checksum (go install verifies it against Go's checksum database instead).
func (p *Placement) validateBedrockPin() error {
	switch {
	case p.BedrockVersion == "" && p.BedrockSHA256 != "":
		return errors.New("bedrockSha256 is recorded without bedrockVersion: bedrock upgrade records both")
	case p.BedrockVersion == "":
		return nil
	case release.IsVersion(p.BedrockVersion):
		if !sha256RE.MatchString(p.BedrockSHA256) {
			return errors.Newf("bedrockVersion %s is a release, and bedrockSha256 %q is not the SHA-256 checksum of its %s: bedrock upgrade %s records it from the release's %s", p.BedrockVersion, p.BedrockSHA256, release.PipelineAsset(), p.BedrockVersion, release.ChecksumsFile)
		}
	case release.IsCommitPin(p.BedrockVersion):
		if p.BedrockSHA256 != "" {
			return errors.Newf("bedrockVersion %s is a commit pin, which carries no checksum (go install verifies it against Go's checksum database): drop bedrockSha256", p.BedrockVersion)
		}
	default:
		return errors.Newf("bedrockVersion %q is neither a release (v0.5.0) nor a commit pin (the pseudo-version the Go module proxy names a commit by): bedrock upgrade records one", p.BedrockVersion)
	}

	return nil
}

// Pinned reports whether the placement pins a bedrock.
func (p *Placement) Pinned() bool {
	return p.BedrockVersion != ""
}

// CommitPin reports whether the pin is a commit pin, built with go install.
func (p *Placement) CommitPin() bool {
	return release.IsCommitPin(p.BedrockVersion)
}

// The shapes of a GitHub-hosted runner's name and machine size.
var (
	hostedRunnerNameRE = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)
	machineSizeRE      = regexp.MustCompile(`^(arm-)?\d+-core$`)
)

// validateCiLargeRunner refuses a machine size without the runner's name, a name the
// created runner cannot carry, a size that is not one of GitHub's shapes, and a cap
// without a size (it caps the runner this layer creates).
func (p *Placement) validateCiLargeRunner() error {
	if p.CiLargeRunnerSize == "" {
		if p.CiLargeRunnerMaximum != 0 {
			return errors.Newf("ciLargeRunnerMaximum %d caps the runner 1-org creates, and ciLargeRunnerSize is empty: set the size, or drop the cap", p.CiLargeRunnerMaximum)
		}

		return nil
	}
	if !machineSizeRE.MatchString(p.CiLargeRunnerSize) {
		return errors.Newf("ciLargeRunnerSize %q is not a GitHub machine size (4-core, 8-core, 16-core, 32-core, 64-core, 96-core, or arm-4-core and up; GET /orgs/<org>/actions/hosted-runners/machine-sizes lists them)", p.CiLargeRunnerSize)
	}
	if p.CiLargeRunner == "" {
		return errors.Newf("ciLargeRunnerSize %s names the runner 1-org creates, and ciLargeRunner, its name, is empty", p.CiLargeRunnerSize)
	}
	if !hostedRunnerNameRE.MatchString(p.CiLargeRunner) {
		return errors.Newf("ciLargeRunner %q cannot name a GitHub-hosted runner (1 to 64 of letters, digits, '.', '-' and '_')", p.CiLargeRunner)
	}
	if p.CiLargeRunnerMaximum < 0 {
		return errors.Newf("ciLargeRunnerMaximum %d is negative", p.CiLargeRunnerMaximum)
	}

	return nil
}

// validateProjects refuses a project or a project number under a key the model lacks, an
// empty project id, and a number that is not one.
func (p *Placement) validateProjects() error {
	for key, id := range p.Projects {
		if !slices.Contains(ProjectKeys, key) {
			return errors.Newf("projects names %q, which is not one of %s", key, strings.Join(ProjectKeys, ", "))
		}
		if strings.TrimSpace(id) == "" {
			return errors.Newf("projects.%s is empty", key)
		}
	}
	for key, number := range p.ProjectNumbers {
		if !slices.Contains(ProjectKeys, key) {
			return errors.Newf("projectNumbers names %q, which is not one of %s", key, strings.Join(ProjectKeys, ", "))
		}
		if !projectNumberRE.MatchString(number) {
			return errors.Newf("projectNumbers.%s %q is not a project number (digits)", key, number)
		}
	}

	return nil
}

// validateTeamGroups refuses a placement that names no group for an environment, a
// group under an environment the model lacks, a person (user:) or any member prefix in
// place of a group's address, and production's group being the first environment's.
func (p *Placement) validateTeamGroups() error {
	for env := range p.TeamGroups {
		if !slices.Contains(Environments, env) {
			return errors.Newf("teamGroups names %q, which is not one of %s", env, strings.Join(Environments, ", "))
		}
	}
	for _, env := range Environments {
		group, ok := p.TeamGroups[env]
		switch {
		case !ok || strings.TrimSpace(group) == "":
			return errors.Newf("teamGroups.%s is empty: each environment names the group whose members approve its releases and ask for its entitlements", env)
		case strings.HasPrefix(group, "user:"):
			return errors.Newf("teamGroups.%s %q is a person (user:): an environment's team is a group, and nothing names a person", env, group)
		case !groupAddressRE.MatchString(group):
			return errors.Newf("teamGroups.%s %q is not a group's address (name@domain, with no member prefix: the layers write group: in front)", env, group)
		}
	}
	if p.TeamGroups[prdEnvironment] == p.TeamGroups[tstEnvironment] {
		return errors.Newf("teamGroups.%s is teamGroups.%s (%s): production's team group is not the first environment's", prdEnvironment, tstEnvironment, p.TeamGroups[prdEnvironment])
	}

	return nil
}

// validateEntitlementDurations refuses a duration under an entitlement the model lacks,
// one that is not a duration, and one outside what Privileged Access Manager admits.
func (p *Placement) validateEntitlementDurations() error {
	for name, value := range p.EntitlementDurations {
		if !slices.Contains(Entitlements, name) {
			return errors.Newf("entitlementDurations names %q, which is not one of %s", name, strings.Join(Entitlements, ", "))
		}
		d, err := time.ParseDuration(value)
		if err != nil {
			return errors.Newf("entitlementDurations.%s %q is not a duration (1h, 90m)", name, value)
		}
		if d < shortestGrant || d > longestGrant {
			return errors.Newf("entitlementDurations.%s %s is outside what Privileged Access Manager admits: between %s and 7 days", name, value, shortestGrant)
		}
	}

	return nil
}

// EntitlementDuration is the longest grant of the entitlement as the layers declare it,
// in seconds (3600s): the placement's, or the default.
func (p *Placement) EntitlementDuration(name string) string {
	d := entitlementDefaults[name]
	if value, ok := p.EntitlementDurations[name]; ok {
		if parsed, err := time.ParseDuration(value); err == nil {
			d = parsed
		}
	}

	return strconv.FormatInt(int64(d/time.Second), 10) + "s"
}

// TeamGroup is the environment's team group as an IAM member (group:<address>).
func (p *Placement) TeamGroup(env string) string {
	return "group:" + p.TeamGroups[env]
}

// TeamGroupAddress is the environment's team group as the address the Workspace Admin
// console names it by, where the approvals are mailed.
func (p *Placement) TeamGroupAddress(env string) string {
	return p.TeamGroups[env]
}

// ApprovalEnvironments are the environments whose version triggers wait for a release's
// approval, where the team group holds the approval and an entitlement takes one: every
// environment but the first, the rule an application's placement derives its own list
// from when it names none.
func (*Placement) ApprovalEnvironments() []string {
	return Environments[1:]
}

// SharedInstanceEnvironments are the environments whose databases live on the shared
// instance 2-spn creates, where that layer declares the Spanner entitlements: every
// environment but the first, whose instance is its own.
func (*Placement) SharedInstanceEnvironments() []string {
	return Environments[1:]
}

// Outlier is the outlier detection thresholds the pull-request backend is rendered with:
// the placement's, with the defaults where it sets none.
func (p *Placement) Outlier() derive.Outlier {
	return p.OutlierDetection.Resolved()
}

// Primary is the primary region.
func (p *Placement) Primary() Region {
	return p.Regions[0]
}

// Secondary is the secondary region.
func (p *Placement) Secondary() Region {
	return p.Regions[1]
}

// Bucket is the state bucket every backend block names: the seeded one, or the
// REPLACEME form the seed step's README says to substitute.
func (p *Placement) Bucket() string {
	if p.StateBucket != "" {
		return p.StateBucket
	}

	return p.Prefix + "-boot-gbl-state-" + replaceMe
}

// labelKeys lists the placement's label keys, sorted.
func (p *Placement) labelKeys() []string {
	keys := make([]string, 0, len(p.Labels))
	for k := range p.Labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	return keys
}

// Project is the project id under a key (boot, or an environment or shared project),
// or the REPLACEME form until the seed or 1-org has run and the placement records it.
func (p *Placement) Project(key string) string {
	if id, ok := p.Projects[key]; ok {
		return id
	}

	return p.Prefix + "-" + key + "-gbl-core-" + replaceMe
}

// BootProject is the boot project's id, or its REPLACEME form until the seed's values
// are recorded.
func (p *Placement) BootProject() string {
	return p.Project(bootProject)
}

// AppsZone is where the apps domain's zone is: the network project, empty until the
// placement records 1-org's project_ids (projects.net), and the name 2-net's dns.tf gives
// the zone, <prefix>-net-gbl-dns-apps.
func (p *Placement) AppsZone() (project, zone string) {
	return p.Projects[netProject], p.Prefix + "-" + netProject + "-gbl-dns-apps"
}

// UnrecordedProjects names the projects the placement does not record yet, as the keys
// a person records (projects.<key>): the layer identities of such a project are rendered
// with REPLACEME, and a run of the triggers refuses to plan or apply its layer. Empty
// once every layer can run.
func (p *Placement) UnrecordedProjects() []string {
	var missing []string
	for _, key := range ProjectKeys {
		if _, ok := p.Projects[key]; !ok {
			missing = append(missing, "projects."+key)
		}
	}

	return missing
}

// Connected reports whether the placement records the Cloud Build GitHub authorization:
// the installation id and the token secret's version, which the connections, the
// repository links and the triggers take.
func (p *Placement) Connected() bool {
	return p.CloudBuildInstallationID != "" && p.CloudBuildTokenSecretVersion != ""
}

// ConnectionUnset names the placement keys of the Cloud Build GitHub authorization that
// are not recorded yet, in the order the README records them; none when both are.
func (p *Placement) ConnectionUnset() []string {
	var unset []string
	if p.CloudBuildInstallationID == "" {
		unset = append(unset, "cloudBuildInstallationId")
	}
	if p.CloudBuildTokenSecretVersion == "" {
		unset = append(unset, "cloudBuildTokenSecretVersion")
	}

	return unset
}

// TokenSecret is the connections' token secret by the parts of its resource name: its
// project, its location (empty for a global secret) and its id.
type TokenSecret struct {
	Project  string
	Location string
	Secret   string
}

// TokenSecret reads the token secret's parts off the version's resource name, or nil
// while the placement records none.
func (p *Placement) TokenSecret() *TokenSecret {
	if !p.Connected() {
		return nil
	}
	parts := strings.Split(p.CloudBuildTokenSecretVersion, "/")
	// projects/P/secrets/S/versions/N, or projects/P/locations/L/secrets/S/versions/N.
	if len(parts) == regionalSecretParts {
		return &TokenSecret{Project: parts[1], Location: parts[3], Secret: parts[5]}
	}

	return &TokenSecret{Project: parts[1], Secret: parts[3]}
}

// regionalSecretParts is how many segments a regional secret version's name has.
const regionalSecretParts = 8

// The runner identities of the Cloud Build triggers, by the suffix of their account ids
// in the boot project: the planner runs the plan trigger's builds, the applier the apply
// trigger's; each impersonates the layer identities of its kind.
const (
	plannerSuffix = "planner"
	applierSuffix = "applier"
	// MarkerPrefix is the prefix in the state bucket under which bedrock org apply keeps
	// one marker object per layer run, holding the commit its last successful apply was
	// of: applied/<layer>, or applied/2-env/<env>.
	MarkerPrefix = "applied"
)

// ConnectionName is the boot project's Cloud Build connection, as 0-bootstrap names it:
// regional, in the primary region.
func (p *Placement) ConnectionName() string {
	return p.Prefix + "-boot-" + p.Primary().Code + "-github"
}

// RepositoryLinkName is the infrastructure repository's link under the connection, which
// the triggers watch.
func (p *Placement) RepositoryLinkName() string {
	return p.Prefix + "-boot-" + p.Primary().Code + "-infrastructure-repo"
}

// PlanTriggerName is the plan trigger, as 0-bootstrap names it.
func (p *Placement) PlanTriggerName() string {
	return p.Prefix + "-boot-" + p.Primary().Code + "-layers-plan"
}

// ApplyTriggerName is the apply trigger, as 0-bootstrap names it.
func (p *Placement) ApplyTriggerName() string {
	return p.Prefix + "-boot-" + p.Primary().Code + "-layers-apply"
}

// PlannerIdentity is the email of the plan trigger's runner identity, in the boot
// project.
func (p *Placement) PlannerIdentity() string {
	return p.identityEmail("org", bootProject, plannerSuffix)
}

// ApplierIdentity is the email of the apply trigger's runner identity, in the boot
// project.
func (p *Placement) ApplierIdentity() string {
	return p.identityEmail("org", bootProject, applierSuffix)
}

// identityEmail is a layer identity's email: <prefix>-<name>-gbl-<suffix> in the project
// under the key, as 0-bootstrap and 1-org name them.
func (p *Placement) identityEmail(name, projectKey, suffix string) string {
	return p.Prefix + "-" + name + "-gbl-" + suffix + "@" + p.Project(projectKey) + ".iam.gserviceaccount.com"
}

// LayerRun is one run of the triggers: a layer, the environment it is applied in (2-env
// alone; empty for the rest), the state prefix its init names (2-env alone), and the
// identities the run impersonates for its tofu: the apply identity on the apply trigger,
// the plan identity on the plan trigger. Name is the run as the commands and the plan
// comments call it.
type LayerRun struct {
	Name        string
	Layer       string
	Environment string
	StatePrefix string
	Apply       string
	Plan        string
}

// Marker is the marker object bedrock org apply keeps for the run in the state bucket.
func (r LayerRun) Marker() string {
	if r.Environment == "" {
		return MarkerPrefix + "/" + r.Layer
	}

	return MarkerPrefix + "/" + r.Layer + "/" + r.Environment
}

// Unrecorded reports whether an identity of the run is under a project the placement
// does not record yet (REPLACEME).
func (r LayerRun) Unrecorded() bool {
	return strings.Contains(r.Apply, replaceMe) || strings.Contains(r.Plan, replaceMe)
}

// LayerRuns is every run of the triggers in layer order: the five single-run layers,
// then 2-env once per environment in promotion order. An identity under a project the
// placement does not record carries REPLACEME, which the commands refuse to run.
func (p *Placement) LayerRuns() []LayerRun {
	runs := make([]LayerRun, 0, len(Layers)-1+len(Environments))
	for _, layer := range Layers {
		if layer == envLayer {
			for _, env := range Environments {
				runs = append(runs, LayerRun{
					Name: layer + " " + env, Layer: layer, Environment: env, StatePrefix: layer + "/" + env,
					Apply: p.identityEmail(env, env, "tofu"), Plan: p.identityEmail(env, env, "plan"),
				})
			}

			continue
		}
		lp := layerProjects[layer]
		runs = append(runs, LayerRun{
			Name: layer, Layer: layer, StatePrefix: layer,
			Apply: p.identityEmail(lp.name, lp.project, "tofu"), Plan: p.identityEmail(lp.name, lp.project, "plan"),
		})
	}

	return runs
}

// ApplicationProjects is the block an application's placement records for its
// environments' projects (id and number, as 1-org's project_ids and project_numbers
// outputs name them), for the environments this placement records both of, and the
// environments it does not. Production is among them: a release is run again there from
// GitHub (bedrock rerun), though it is never restored by a run.
func (p *Placement) ApplicationProjects() (block string, missing []string) {
	var lines []string
	for _, env := range Environments {
		id, number := p.Projects[env], p.ProjectNumbers[env]
		if id == "" || number == "" {
			missing = append(missing, env)

			continue
		}
		lines = append(lines, fmt.Sprintf("    %q: {\"id\": %q, \"number\": %q}", env, id, number))
	}
	if len(lines) == 0 {
		return "", missing
	}

	return "  \"projects\": {\n" + strings.Join(lines, ",\n") + "\n  }", missing
}

// ApplicationPlacement is the first placement of a registered application, every field
// from this placement but the bedrock pin, which is the bedrock writing it (a release with
// its pipeline binary's checksum, or a commit pin with none): the prefix, the model's
// environments, the regions, the domains, the state bucket, the default branch, the
// release app's slug, the labels, the outlier detection thresholds and the environment
// projects' ids and numbers, with Cloud Run's sample image to create the services with,
// the application's code as its repository's name, and the first environment's database
// seeded. Approvals, the
// maintenance windows, the build machine and the instance caps are left to their
// defaults, the team's to write. It is refused while this placement records no state
// bucket or lacks an environment project's id or number, which 1-org's outputs give.
func (p *Placement) ApplicationPlacement(app, bedrockVersion, bedrockSHA256 string) (*derive.Placement, error) {
	if p.StateBucket == "" {
		return nil, errors.New("placement.json records no stateBucket yet: the seed's state bucket goes there (step 2 of the hand steps, 0-bootstrap/README.md) before an application's placement can name it")
	}
	if _, missing := p.ApplicationProjects(); len(missing) > 0 {
		return nil, errors.Newf("placement.json records no project id and number for %s yet: record 1-org's project_ids and project_numbers outputs there (projects, projectNumbers), since an application's placement names its environments' projects, then register again", prose(missing))
	}
	regions := make([]derive.Region, 0, len(p.Regions))
	for _, r := range p.Regions {
		regions = append(regions, derive.Region{Name: r.Name, Code: r.Code})
	}
	projects := make(map[string]derive.Project, len(Environments))
	for _, env := range Environments {
		projects[env] = derive.Project{ID: p.Projects[env], Number: p.ProjectNumbers[env]}
	}
	var labels map[string]string
	if len(p.Labels) > 0 {
		labels = maps.Clone(p.Labels)
	}
	a := &derive.Placement{
		Prefix:           p.Prefix,
		Environments:     slices.Clone(Environments),
		Regions:          regions,
		AppsDomain:       p.AppsDomain,
		HostedDomain:     p.OrganizationDomain,
		StateBucket:      p.StateBucket,
		PlaceholderImage: derive.PlaceholderImage,
		DefaultBranch:    p.GithubDefaultBranch,
		Repository:       app,
		ReleaseApp:       p.GithubReleaseAppSlug,
		BedrockVersion:   bedrockVersion,
		BedrockSHA256:    bedrockSHA256,
		Labels:           labels,
		Seed:             []string{Environments[0]},
		Projects:         projects,
		OutlierDetection: p.OutlierDetection.Clone(),
		CloudArmor:       p.CloudArmor.Clone(),
	}
	if err := a.Validate(); err != nil {
		return nil, errors.Wrapf(err, "%s's placement", app)
	}

	return a, nil
}

// Register adds an application to the placement: a code of one to six lowercase
// alphanumeric characters starting with a letter, not registered yet.
func (p *Placement) Register(app string) error {
	if !applicationRE.MatchString(app) {
		return errors.Newf("application %q is not 1 to 6 lowercase alphanumeric characters starting with a letter", app)
	}
	if slices.Contains(p.Applications, app) {
		return errors.Newf("application %q is registered already", app)
	}
	p.Applications = append(p.Applications, app)

	return nil
}

// Write puts the placement back in its file, indented the way a person reads it.
func (p *Placement) Write(path string) error {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return errors.Wrap(err, "json.MarshalIndent()")
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return errors.Wrapf(err, "os.WriteFile(): %s", path)
	}

	return nil
}
