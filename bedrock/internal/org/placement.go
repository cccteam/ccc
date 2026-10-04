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
)

const (
	// regionCount is the number of regions the model runs in: a primary and a secondary.
	regionCount = 2
	// githubLoginLength is the longest login GitHub gives an account.
	githubLoginLength = 39
	// replaceMe marks a value only a seed step can supply.
	replaceMe = "REPLACEME"
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
)

// The environments' entitlements, by the key placement.json names each under
// (entitlementDurations): the secret operator (bedrock secret add and pin), the Spanner
// admin and viewer of the environment's databases, and the layer administrator, who acts
// as 2-env's apply identity for a recovery. Each has a default longest grant, and
// Privileged Access Manager admits a longest grant between 30 minutes and 7 days.
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
	// from which alone the layers workflow applies.
	GithubDefaultBranch string `json:"githubDefaultBranch"`
	// GithubInfrastructureAppID is the App ID of the infrastructure GitHub App, whose
	// installation token the layers workflow mints for 1-org's GitHub provider: digits,
	// from the app's settings page. Empty until the app exists; a run of 1-org through
	// the workflow refuses until it is recorded.
	GithubInfrastructureAppID string `json:"githubInfrastructureAppId,omitempty"`
	// GithubInfrastructureKeyVersion is the number of the Secret Manager version holding
	// the infrastructure app's private key, in the container 0-bootstrap creates in the
	// boot project (<prefix>-boot-gbl-github-infrastructure-key): pinned, never latest.
	// Empty until a version was added.
	GithubInfrastructureKeyVersion string `json:"githubInfrastructureKeyVersion,omitempty"`
	// GithubInfrastructureTeam is the slug of the organization's infrastructure team,
	// whose approval a change to an application's workflow and Cloud Build files
	// needs; empty for none.
	GithubInfrastructureTeam string `json:"githubInfrastructureTeam,omitempty"`
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
	// ContactDomains are the domains Essential Contacts may belong to, each with its
	// leading @.
	ContactDomains []string `json:"contactDomains"`
	// Applications are the application codes registered in every environment, in the
	// order they were added; empty for an organization with none yet. bedrock org
	// register adds one; the layers' applications.auto.tfvars are rendered from it.
	Applications []string `json:"applications"`
	// Projects are the projects by key: boot, the project the seed chose, and shr, net,
	// spn, tst, stg and prd, the ids 1-org's apply chose (its project_ids output). The
	// environment projects are what the applications' identities and backends are named
	// under; every project names the layer identities the layers workflow signs in as.
	// Absent until the seed or 1-org has run; REPLACEME is rendered in their place.
	Projects map[string]string `json:"projects,omitempty"`
	// ProjectNumbers are the projects' numbers by the same keys: boot from the seed (the
	// layers workflow names the boot project's identity provider by it), the rest from
	// 1-org's project_numbers output, recorded beside Projects. An application's placement
	// records its environments' ids and numbers together (bedrock org register writes
	// them into its first placement, org render prints the block), for the operations
	// workflow that starts a restore from GitHub.
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
	// Config is the instance configuration, a multi-region one whose read-write replicas
	// are the two regions (nam10 for us-central1 and us-west3).
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
	if err := p.validateGithubApps(); err != nil {
		return err
	}
	if err := p.validateProjects(); err != nil {
		return err
	}
	if err := p.validateTeamGroups(); err != nil {
		return err
	}
	if err := p.validateEntitlementDurations(); err != nil {
		return err
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

// WorkflowUnwired names the placement values the layers workflow still lacks, as the
// keys a person records: projectNumbers.boot (the identity provider is named by the
// boot project's number) and projects.<key> for every project whose layer identities
// the workflow signs in as. Empty once every layer can run.
func (p *Placement) WorkflowUnwired() []string {
	var missing []string
	if _, ok := p.ProjectNumbers[bootProject]; !ok {
		missing = append(missing, "projectNumbers."+bootProject)
	}
	for _, key := range ProjectKeys {
		if _, ok := p.Projects[key]; !ok {
			missing = append(missing, "projects."+key)
		}
	}

	return missing
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
// release app's slug, the labels and the environment projects' ids and numbers, with
// Cloud Run's sample image to create the services with, the application's code as its
// repository's name, and the first environment's database seeded. Approvals, the
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
