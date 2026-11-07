// record.go is how a root organization creates a hosting organization by pull request
// (README.md, "Creating a hosting organization"): the record a person writes, the
// values the root organization supplies, and what the creation run (internal/create)
// renders for the new repository once the seed has made the folder and the project.
//
// The record is the new organization's own placement, kept in the root organization's
// repository under organizations/<prefix>/placement.json: the hosting shape, written by
// a person with what is the organization's to decide (its prefix, apps domain, regions,
// Spanner configuration, team groups, contact domains, labels), and filled by bedrock org
// found with what the root organization supplies (the Google Cloud organization, the
// billing account, the GitHub organization and its apps, the Cloud Build authorization,
// the break-glass account, the bedrock pin, and the root organization itself) and the
// boot project's id, whose suffix is chosen then so that the id is known before anything
// is made. placement.json lists the organization (hostingOrganizations) by prefix and
// boot project; the folder is recorded once the creation has made it.

package org

import (
	"crypto/rand"
	"encoding/hex"
	"path/filepath"
	"slices"
	"strings"

	"github.com/go-playground/errors/v5"
)

// RecordsDir is the directory of the root organization's repository that holds the
// creation records, one directory per hosting organization.
const RecordsDir = "organizations"

// suffixBytes is how many random bytes the boot project's suffix is made of: two, four
// hex characters, what the project factory module would have generated.
const suffixBytes = 2

// RecordPath is the path, under the root organization's repository, of the
// hosting organization's record: its placement.
func RecordPath(dir, prefix string) string {
	return filepath.Join(dir, RecordsDir, prefix, PlacementFile)
}

// CreatingIdentity is the root organization's creating identity, which creates a hosting
// organization from the apply trigger's run: a service account in the root project.
func (p *Placement) CreatingIdentity() string {
	return p.Prefix + "-create-gbl-tofu@" + p.Projects[RootProjectKey] + ".iam.gserviceaccount.com"
}

// CreatingIdentityMember is the creating identity as an IAM member.
func (p *Placement) CreatingIdentityMember() string {
	return "serviceAccount:" + p.CreatingIdentity()
}

// Seed is what the creation learns as it makes the organization's folders: their
// numeric ids, which the seeded tfvars of the new repository carry. Nil renders
// REPLACEME, as org new does before the creation has recorded them.
type Seed struct {
	OrganizationFolder string
	TerraformFolder    string
}

// ReadRecord reads the hosting organization's record from the root
// organization's repository: a hosting placement, validated as one.
func ReadRecord(dir, prefix string) (*Placement, error) {
	path := RecordPath(dir, prefix)
	h, err := ReadPlacement(path)
	if err != nil {
		return nil, err
	}
	if h.IsRoot() {
		return nil, errors.Newf("%s describes a root organization (kind), and a record is a hosting organization's placement", path)
	}
	if h.Prefix != prefix {
		return nil, errors.Newf("%s has prefix %q, and the record under %s/%s is that organization's", path, h.Prefix, RecordsDir, prefix)
	}

	return h, nil
}

// Fill puts into the hosting organization's placement what the root organization
// supplies, and chooses the boot project's suffix when none is chosen yet, so that the
// record is complete before the creation runs: the Google Cloud organization and its
// domain, the billing account (the root organization's unless the record names one), the
// GitHub organization, its Bot Account, default branch and the three apps' ids and
// slug, the Cloud Build authorization (the same connection token, read from the root
// organization's boot project), the break-glass account, the bedrock pin, and the root
// organization itself (root). What the person wrote and the root organization does not
// supply stays. It refuses a record whose prefix is the root organization's or whose
// values contradict the root organization's.
func Fill(root, h *Placement) error {
	if !root.IsRoot() {
		return errors.New("the placement is a hosting organization's, and only a root organization creates another organization")
	}
	if root.Projects[RootProjectKey] == "" || root.StateBucket == "" {
		return errors.New("the root organization's placement records no root project or no state bucket yet (projects.root, stateBucket): bedrock org bootstrap records them")
	}
	if root.DirectoryCustomerID == "" {
		return errors.New("the root organization's placement records no directoryCustomerId: the Google Cloud organization's Cloud Identity customer, which bedrock org bootstrap records and the new organization's 1-org names in its domain-restricted sharing policy")
	}
	if h.Prefix == root.Prefix {
		return errors.Newf("prefix %q is the root organization's own; a hosting organization has a prefix of its own", h.Prefix)
	}
	if h.IsRoot() {
		return errors.New("kind is root, and a record is a hosting organization's placement: leave kind out")
	}
	if h.BillingAccount == "" {
		h.BillingAccount = root.BillingAccount
	}
	if h.SourceRepo == "" {
		h.SourceRepo = h.Prefix + "-infrastructure"
	}
	h.OrganizationDomain, h.OrganizationID, h.DirectoryCustomerID = root.OrganizationDomain, root.OrganizationID, root.DirectoryCustomerID
	h.GithubOrganization, h.GithubBotAccount, h.GithubDefaultBranch = root.GithubOrganization, root.GithubBotAccount, root.GithubDefaultBranch
	h.GithubInfrastructureAppID, h.GithubDeployerAppID, h.GithubReleaseAppID = root.GithubInfrastructureAppID, root.GithubDeployerAppID, root.GithubReleaseAppID
	if h.GithubReleaseAppSlug == "" {
		h.GithubReleaseAppSlug = root.ReleaseAppSlug()
	}
	h.CloudBuildInstallationID, h.CloudBuildTokenSecretVersion = root.CloudBuildInstallationID, root.CloudBuildTokenSecretVersion
	h.BreakGlassAccount = root.BreakGlassAccount
	h.BedrockVersion, h.BedrockSHA256 = root.BedrockVersion, root.BedrockSHA256
	h.Root = &RootPlace{Organization: root.Prefix, Project: root.Projects[RootProjectKey]}
	h.CiLargeRunner, h.CiLargeRunnerSize, h.CiLargeRunnerMaximum = "", "", 0
	if root.TokenSecretRegional() && len(h.Regions) > 0 && h.Regions[0].Name != root.Primary().Name {
		return errors.Newf("regions names %s first, and the Cloud Build token the root organization recorded is a regional secret in %s: a connection reads a token in its own region, so a hosting organization under this root organization runs in %s first", h.Regions[0].Name, root.Primary().Name, root.Primary().Name)
	}
	if h.Projects == nil {
		h.Projects = map[string]string{}
	}
	if h.Projects[bootProject] == "" || strings.Contains(h.Projects[bootProject], replaceMe) {
		suffix, err := randomSuffix()
		if err != nil {
			return err
		}
		h.Projects[bootProject] = h.Prefix + "-boot-gbl-core-" + suffix
	}
	if !strings.HasPrefix(h.Projects[bootProject], h.Prefix+"-boot-gbl-core-") {
		return errors.Newf("projects.boot %q is not %s-boot-gbl-core-<suffix>, the boot project's name", h.Projects[bootProject], h.Prefix)
	}
	h.StateBucket = h.Prefix + "-boot-gbl-state-" + strings.TrimPrefix(h.Projects[bootProject], h.Prefix+"-boot-gbl-core-")
	if slices.Contains([]string{"", replaceMe}, h.Spanner.Config) || h.AppsDomain == "" || len(h.Regions) != regionCount || len(h.TeamGroups) == 0 || h.MonthlyBudget <= 0 {
		return errors.Newf("%s/%s/%s needs what is the organization's own to decide before it is recorded: appsDomain, spanner.config, two regions (the primary first), teamGroups (one group per environment) and monthlyBudget (its spend budget, whole units of the billing account's currency a month); contactDomains, entitlementDurations and labels as wanted", RecordsDir, h.Prefix, PlacementFile)
	}

	return h.Validate()
}

// TokenSecretRegional reports whether the Cloud Build token secret the placement names is
// a regional one (projects/P/locations/L/secrets/N), as the console's host-connection
// flow writes it.
func (p *Placement) TokenSecretRegional() bool {
	return strings.Contains(p.CloudBuildTokenSecretVersion, "/locations/")
}

// TokenSecretLocation is the region of a regional token secret, empty for a global one.
func (p *Placement) TokenSecretLocation() string {
	parts := strings.Split(p.CloudBuildTokenSecretVersion, "/")
	for i, part := range parts {
		if part == "locations" && i+1 < len(parts) {
			return parts[i+1]
		}
	}

	return ""
}

// TokenSecretName is the token secret's resource name without its version
// (projects/P[/locations/L]/secrets/N), empty while unrecorded.
func (p *Placement) TokenSecretName() string {
	name, _, found := strings.Cut(p.CloudBuildTokenSecretVersion, "/versions/")
	if !found {
		return ""
	}

	return name
}

// randomSuffix is four hex characters from the system's randomness.
func randomSuffix() (string, error) {
	b := make([]byte, suffixBytes)
	if _, err := rand.Read(b); err != nil {
		return "", errors.Wrap(err, "rand.Read()")
	}

	return hex.EncodeToString(b), nil
}

// Record adds the hosting organization to the root organization's placement
// (hostingOrganizations: its prefix and its boot project), or updates the boot project
// of a record that has it, leaving a recorded folder as it is. It reports whether the
// placement changed.
func (p *Placement) Record(h *Placement) bool {
	if existing := p.HostingOrganization(h.Prefix); existing != nil {
		if existing.BootProject == h.Projects[bootProject] {
			return false
		}
		existing.BootProject = h.Projects[bootProject]

		return true
	}
	p.HostingOrganizations = append(p.HostingOrganizations, HostingOrganization{Prefix: h.Prefix, BootProject: h.Projects[bootProject]})

	return true
}

// Pending are the hosting organizations recorded but not created yet: the ones without a
// folder, which the creation run creates in record order.
func (p *Placement) Pending() []HostingOrganization {
	var pending []HostingOrganization
	for _, h := range p.HostingOrganizations {
		if !h.Created() {
			pending = append(pending, h)
		}
	}

	return pending
}

// SecretPolicyAdminRole is the id of the custom role a root organization's bootstrap
// makes in its root project (projects/<root project>/roles/secretPolicyAdmin): a
// secret's IAM policy and nothing else, which each created hosting organization's boot
// identity and the creating identity hold on the shared secrets.
const SecretPolicyAdminRole = "secretPolicyAdmin"

// SharedSecrets are the secrets a root organization holds for every organization, by
// resource name: the three GitHub Apps' keys, and the Cloud Build token once the
// authorization is recorded.
func (p *Placement) SharedSecrets() []string {
	secrets := make([]string, 0, len(Keys)+1)
	for _, name := range Keys {
		secrets = append(secrets, "projects/"+p.Projects[RootProjectKey]+"/secrets/"+p.SecretContainer(name))
	}
	if token := p.TokenSecretName(); token != "" {
		secrets = append(secrets, token)
	}

	return secrets
}

// BootLayerRoles are the roles the boot layer identity holds on the organization's
// folder, and nowhere higher: the list 0-bootstrap's var.boot_layer_roles carries, which
// the creation grants before the layer's first apply. The template's
// list and this one say the same; a test holds them together.
var BootLayerRoles = []string{
	"roles/cloudbuild.builds.editor",
	"roles/cloudbuild.connectionAdmin",
	"roles/iam.serviceAccountAdmin",
	"roles/resourcemanager.folderAdmin",
	"roles/resourcemanager.projectCreator",
	"roles/resourcemanager.projectDeleter",
	"roles/resourcemanager.projectIamAdmin",
	"roles/serviceusage.serviceUsageAdmin",
}

// BootProjectRoles are the roles the boot layer identity holds on the boot project alone,
// 0-bootstrap's var.boot_project_roles, which the creation grants with
// BootLayerRoles: Custom Roles Administrator, for the custom roles the layer makes there,
// which Google binds on a project or at the organization node and never on a folder.
var BootProjectRoles = []string{
	"roles/iam.roleAdmin",
}

// ProjectLabels are the labels the seed gives the boot project and the state bucket, as
// the layers label what they make: the model's own, with the placement's.
func (p *Placement) ProjectLabels(layer, environment string) map[string]string {
	labels := map[string]string{
		"terraform": "true", "terraform_source_path": layer, "source_repo": p.SourceRepo, "organization": p.Prefix, "environment": environment,
	}
	for k, v := range p.Labels {
		labels[k] = v
	}

	return labels
}

// RandomSuffix is four hex characters from the system's randomness: the suffix a root
// project's id and its bucket's name take at the bootstrap.
func RandomSuffix() (string, error) {
	return randomSuffix()
}
