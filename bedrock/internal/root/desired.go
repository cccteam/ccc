// desired.go is the root organization as the placement says it should stand: its
// identities and their roles, its secrets and their policies, its zones and their record
// sets, its Cloud Build connection and triggers, and the shape of its repository's rules
// on GitHub. The engine compares each with what stands and makes it so.

package root

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/cccteam/ccc/bedrock/internal/github"
	"github.com/cccteam/ccc/bedrock/internal/org"
)

// The roles the engine grants, by the words IAM uses.
const (
	roleDNSAdmin             = "roles/dns.admin"
	roleDNSReader            = "roles/dns.reader"
	roleSecretAdmin          = "roles/secretmanager.admin"
	roleSecretViewer         = "roles/secretmanager.viewer"
	roleSecretAccessor       = "roles/secretmanager.secretAccessor"
	roleSecretVersionAdder   = "roles/secretmanager.secretVersionAdder"
	roleBuildsEditor         = "roles/cloudbuild.builds.editor"
	roleBuildsViewer         = "roles/cloudbuild.builds.viewer"
	roleConnectionAdmin      = "roles/cloudbuild.connectionAdmin"
	roleConnectionViewer     = "roles/cloudbuild.connectionViewer"
	roleReadTokenAccessor    = "roles/cloudbuild.readTokenAccessor"
	roleLogWriter            = "roles/logging.logWriter"
	roleRoleAdmin            = "roles/iam.roleAdmin"
	roleServiceUsageConsumer = "roles/serviceusage.serviceUsageConsumer"
	roleBrowser              = "roles/browser"
	roleSecurityReviewer     = "roles/iam.securityReviewer"
	roleImpersonator         = "roles/iam.serviceAccountTokenCreator"
	roleServiceAccountUser   = "roles/iam.serviceAccountUser"
	roleObjectUser           = "roles/storage.objectUser"
	roleFolderCreator        = "roles/resourcemanager.folderCreator"
	roleBillingAdmin         = "roles/billing.admin"
	rolePolicyAdmin          = "roles/orgpolicy.policyAdmin"
	rolePolicyViewer         = "roles/orgpolicy.policyViewer"
)

// Services are the APIs the root project needs: Cloud Build for the triggers, Cloud DNS
// for the zones, Secret Manager for the keys and the token, IAM and its credentials for
// the identities and the creation's impersonation, Cloud Resource Manager and Service
// Usage for the creation of hosting organizations, Cloud Billing and its budgets for
// their billing links and budgets, Cloud Storage for the state bucket, Cloud Logging
// for the triggers' logs.
var Services = []string{
	"billingbudgets.googleapis.com", "cloudbilling.googleapis.com", "cloudbuild.googleapis.com", "cloudresourcemanager.googleapis.com",
	"dns.googleapis.com", "iam.googleapis.com", "iamcredentials.googleapis.com", "logging.googleapis.com", "orgpolicy.googleapis.com",
	"secretmanager.googleapis.com", "serviceusage.googleapis.com", "storage.googleapis.com",
}

// The lock and the marker the apply keeps in the state bucket.
const (
	LockObject   = "locks/root"
	MarkerObject = org.MarkerPrefix + "/root"
)

// Desired is the root organization as the placement says it should stand.
type Desired struct {
	p *org.Placement
	// ProjectNumber is the root project's number, for the Cloud Build service agent.
	ProjectNumber string
}

// NewDesired reads the placement: the root project's number from projectNumbers.
func NewDesired(p *org.Placement) *Desired {
	return &Desired{p: p, ProjectNumber: p.ProjectNumbers[org.RootProjectKey]}
}

// Project is the root project; Location its primary region, where Cloud Build lives.
func (d *Desired) Project() string {
	return d.p.Projects[org.RootProjectKey]
}

// Location is the region the connection, the triggers and the bucket are in.
func (d *Desired) Location() string {
	return d.p.Primary().Name
}

// Labels are the labels every resource of the root organization carries.
func (d *Desired) Labels() map[string]string {
	return d.p.ProjectLabels(org.RootProjectKey, org.RootProjectKey)
}

// Planner is the plan trigger's runner identity, as an email.
func (d *Desired) Planner() string {
	return d.p.PlannerIdentity()
}

// Applier is the apply trigger's runner identity, as an email.
func (d *Desired) Applier() string {
	return d.p.ApplierIdentity()
}

// Creator is the creating identity, as an email: the one the apply impersonates to
// create a hosting organization.
func (d *Desired) Creator() string {
	return d.p.CreatingIdentity()
}

func member(email string) string {
	return "serviceAccount:" + email
}

// ServiceAgent is the root project's Cloud Build service agent, which reads the token
// for the connection and runs the triggers' builds as the runner identities.
func (d *Desired) ServiceAgent() string {
	return "serviceAccount:service-" + d.ProjectNumber + "@gcp-sa-cloudbuild.iam.gserviceaccount.com"
}

// Identity is one service account the bootstrap makes in the root project.
type Identity struct {
	Email       string
	AccountID   string
	DisplayName string
	Description string
}

// Identities are the three identities of the root project, in creation order.
func (d *Desired) Identities() []Identity {
	prefix := d.p.Prefix

	return []Identity{
		{Email: d.Planner(), AccountID: prefix + "-root-gbl-planner", DisplayName: "Root planner - " + prefix + "-root-gbl", Description: "Runs the plan trigger's builds: says what a pull request's merge would change in the root organization, and posts it."},
		{Email: d.Applier(), AccountID: prefix + "-root-gbl-applier", DisplayName: "Root applier - " + prefix + "-root-gbl", Description: "Runs the apply trigger's builds: makes the root organization as placement.json says, and creates the hosting organizations recorded there as the creating identity."},
		{Email: d.Creator(), AccountID: prefix + "-create-gbl-tofu", DisplayName: "Creation SA - " + prefix + "-create-gbl", Description: "Creating identity for " + prefix + ". Creates a hosting organization from the apply trigger's run: the folder at the organization node, the foundation under it, the first applies, the billing grants, and the hand-off."},
	}
}

// ProjectRoles are the roles each identity holds on the root project.
func (d *Desired) ProjectRoles() map[string][]string {
	return map[string][]string{
		member(d.Planner()): {roleBuildsViewer, roleConnectionViewer, roleDNSReader, roleLogWriter, roleSecretViewer, roleServiceUsageConsumer},
		member(d.Applier()): {roleBuildsEditor, roleBuildsViewer, roleConnectionAdmin, roleDNSAdmin, roleLogWriter, roleRoleAdmin, roleSecretAdmin, roleServiceUsageConsumer},
		member(d.Creator()): {roleServiceUsageConsumer},
	}
}

// FolderRoles are the roles the two runners hold on the organization's folder: the reads
// bedrock org check's leftover report takes.
func (d *Desired) FolderRoles() map[string][]string {
	return map[string][]string{
		member(d.Planner()): {roleBrowser, roleSecurityReviewer},
		member(d.Applier()): {roleBrowser, roleSecurityReviewer},
	}
}

// Grant is one binding on one resource.
type Grant struct {
	Resource string
	Role     string
	Member   string
}

// ServiceAccountGrants are the bindings on the identities themselves: the Cloud Build
// service agent runs the triggers' builds as the two runners, and the applier creates
// hosting organizations as the creating identity.
func (d *Desired) ServiceAccountGrants() []Grant {
	return []Grant{
		{Resource: d.Planner(), Role: roleServiceAccountUser, Member: d.ServiceAgent()},
		{Resource: d.Applier(), Role: roleServiceAccountUser, Member: d.ServiceAgent()},
		{Resource: d.Creator(), Role: roleImpersonator, Member: member(d.Applier())},
	}
}

// OrganizationGrants are the roles at the organization node, which the bootstrap grants
// once and nothing of a hosting organization holds: the creating identity's Folder
// Creator, for each hosting organization's folder; the applier's Organization Policy
// Administrator and the planner's Organization Policy Viewer, for the baseline of
// organization policies the root organization sets on each hosting organization's
// folders, since Google grants those two roles at the organization node and nowhere
// lower.
func (d *Desired) OrganizationGrants() []Grant {
	return []Grant{
		{Resource: d.p.OrganizationID, Role: roleFolderCreator, Member: member(d.Creator())},
		{Resource: d.p.OrganizationID, Role: rolePolicyAdmin, Member: member(d.Applier())},
		{Resource: d.p.OrganizationID, Role: rolePolicyViewer, Member: member(d.Planner())},
	}
}

// BillingGrants are the creating identity's roles on the billing account: Billing
// Account Administrator, with which each hosting organization's projects are linked and
// its identities granted their billing roles, and its budget made.
func (d *Desired) BillingGrants() []Grant {
	return []Grant{{Resource: d.p.BillingAccount, Role: roleBillingAdmin, Member: member(d.Creator())}}
}

// MarkerCondition is the condition on the applier's bucket grant: the markers and the
// lock, nothing else in the bucket.
func (d *Desired) MarkerCondition() *Condition {
	objects := "projects/_/buckets/" + d.p.StateBucket + "/objects/"

	return &Condition{
		Title:       d.p.Prefix + "-root-gbl-applied-markers",
		Description: "The markers of the applies and the creations, and the apply's lock.",
		Expression:  fmt.Sprintf("resource.name.startsWith(%q) || resource.name.startsWith(%q)", objects+org.MarkerPrefix+"/", objects+"locks/"),
	}
}

// SecretPolicyAdmin is the custom role in the root project that reads and sets a
// secret's IAM policy and nothing else: what each hosting organization's boot identity
// and the creating identity hold on the shared secrets.
func (d *Desired) SecretPolicyAdmin() Role {
	return Role{
		Title:       "Secret Policy Admin",
		Description: "Reads and sets a secret's IAM policy, for the organizations whose layers grant their identities the read of a GitHub App's key held here; no payload, no version, no change to the secret.",
		Permissions: []string{"secretmanager.secrets.get", "secretmanager.secrets.getIamPolicy", "secretmanager.secrets.setIamPolicy"},
	}
}

// SecretPolicyAdminID is the role's full id.
func (d *Desired) SecretPolicyAdminID() string {
	return "projects/" + d.Project() + "/roles/" + org.SecretPolicyAdminRole
}

// SecretSpec is one secret of the root project: its id, whether its alias current is kept
// at the version the placement pins (the keys and the token; none while unpinned), the
// aliases it carries, and its policy, by role.
type SecretSpec struct {
	ID      string
	Name    string
	Aliased bool
	Aliases map[string]string
	Policy  []org.Binding
}

// Secrets are the four secrets: the three keys, then the token.
func (d *Desired) Secrets() []SecretSpec {
	created := d.p.CreatedHostingOrganizations()
	policyAdmins := make([]string, 0, len(created)+1)
	for _, h := range created {
		policyAdmins = append(policyAdmins, h.BootIdentityMember())
	}
	policyAdmins = append(policyAdmins, member(d.Creator()))
	runners := []string{member(d.Applier()), member(d.Planner())}
	var specs []SecretSpec
	for _, key := range org.Keys {
		spec := SecretSpec{ID: d.p.SecretContainer(key), Aliased: true, Aliases: map[string]string{}}
		spec.Name = "projects/" + d.Project() + "/secrets/" + spec.ID
		if version := d.p.KeyVersion(key); version != "" {
			spec.Aliases[org.KeyAlias] = version
		}
		spec.Policy = bindings(map[string][]string{
			d.SecretPolicyAdminID(): policyAdmins,
			roleSecretAccessor:      slices.Concat(runners, d.p.KeyOperators),
			roleSecretVersionAdder:  d.p.KeyOperators,
		})
		specs = append(specs, spec)
	}
	token := SecretSpec{ID: d.p.TokenSecretID(), Aliased: true, Aliases: map[string]string{}}
	token.Name = "projects/" + d.Project() + "/secrets/" + token.ID
	if version := d.p.KeyVersion(org.Token); version != "" {
		token.Aliases[org.KeyAlias] = version
	}
	token.Policy = bindings(map[string][]string{
		d.SecretPolicyAdminID(): policyAdmins,
		roleSecretAccessor:      {d.ServiceAgent()},
	})

	return append(specs, token)
}

// bindings turns roles to members into sorted bindings, roles without members left out.
func bindings(roles map[string][]string) []org.Binding {
	var out []org.Binding
	for role, members := range roles {
		if len(members) == 0 {
			continue
		}
		members = slices.Clone(members)
		sort.Strings(members)
		out = append(out, org.Binding{Role: role, Members: slices.Compact(members)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Role < out[j].Role })

	return out
}

// ZoneSpec is one zone and its record sets.
type ZoneSpec struct {
	Zone    Zone
	Domain  string
	Records []RecordSet
}

// Zones are the zones of the domains delegated to the organization, in domain order,
// each with one NS record set per label handed onward.
func (d *Desired) Zones() []ZoneSpec {
	domains := make([]string, 0, len(d.p.Domains))
	for domain := range d.p.Domains {
		domains = append(domains, domain)
	}
	sort.Strings(domains)
	specs := make([]ZoneSpec, 0, len(domains))
	for _, domain := range domains {
		spec := ZoneSpec{Domain: domain, Zone: Zone{
			Name: d.p.ZoneName(domain), DNSName: domain + ".", Labels: d.Labels(), DNSSEC: true,
			Description: "Public zone for " + domain + ", delegated to this organization; its labels are handed onward to the hosting organizations' zones",
		}}
		labels := make([]string, 0, len(d.p.Domains[domain].Labels))
		for label := range d.p.Domains[domain].Labels {
			labels = append(labels, label)
		}
		sort.Strings(labels)
		for _, label := range labels {
			spec.Records = append(spec.Records, RecordSet{Name: label + "." + domain + ".", Type: "NS", TTL: 3600, Data: d.p.Domains[domain].Labels[label].NameServers})
		}
		specs = append(specs, spec)
	}

	return specs
}

// Connection is the Cloud Build connection the placement records: the Cloud Build GitHub
// App's installation and the token secret version.
func (d *Desired) Connection() Connection {
	return Connection{InstallationID: d.p.CloudBuildInstallationID, TokenSecretVersion: d.p.CloudBuildTokenSecretVersion}
}

// Connected reports whether the placement records the connection's two values.
func (d *Desired) Connected() bool {
	return d.p.CloudBuildInstallationID != "" && d.p.CloudBuildTokenSecretVersion != ""
}

// RemoteURI is the repository's clone URL, which the repository link names.
func (d *Desired) RemoteURI() string {
	return "https://github.com/" + d.p.GithubOrganization + "/" + d.p.SourceRepo + ".git"
}

// RepositoryLinkResource is the repository link's full resource name, which the
// triggers watch.
func (d *Desired) RepositoryLinkResource() string {
	return "projects/" + d.Project() + "/locations/" + d.Location() + "/connections/" + d.p.ConnectionName() + "/repositories/" + d.p.RepositoryLinkName()
}

// Triggers are the two triggers: the plan trigger on a pull request into the default
// branch, the apply trigger on a push to it.
func (d *Desired) Triggers() []*Trigger {
	branch := "^" + d.p.GithubDefaultBranch + "$"
	plan := &Trigger{
		Name: d.p.PlanTriggerName(), Description: "Says what a pull request's merge changes in the root organization, and posts it on the pull request.",
		ServiceAccount: "projects/" + d.Project() + "/serviceAccounts/" + d.Planner(), Substitutions: d.p.TriggerSubstitutions(),
		Repository: d.RepositoryLinkResource(), PullRequestBranch: branch,
		Steps: d.p.TriggerSteps(org.PlanCommand), TimeoutSeconds: d.p.TriggerBuildTimeoutSeconds(org.PlanCommand),
	}
	apply := &Trigger{
		Name: d.p.ApplyTriggerName(), Description: "Makes the root organization as placement.json says on a push to " + d.p.GithubDefaultBranch + ", and creates the hosting organizations recorded without a folder.",
		ServiceAccount: "projects/" + d.Project() + "/serviceAccounts/" + d.Applier(), Substitutions: d.p.TriggerSubstitutions(),
		Repository: d.RepositoryLinkResource(), PushBranch: branch,
		Steps: d.p.TriggerSteps(org.ApplyCommand), TimeoutSeconds: d.p.TriggerBuildTimeoutSeconds(org.ApplyCommand),
	}

	return []*Trigger{plan, apply}
}

// PlanCheckContext is the context Google's Cloud Build GitHub App reports the plan
// trigger's build under, which the ruleset requires: the trigger's name, then the
// project's id in parentheses.
func (d *Desired) PlanCheckContext() string {
	return d.p.PlanTriggerName() + " (" + d.Project() + ")"
}

// The words the ruleset's rules use.
const (
	rulesetName          = "default branch"
	ruleDeletion         = "deletion"
	ruleNonFastForward   = "non_fast_forward"
	rulePullRequest      = "pull_request"
	ruleRequiredChecks   = "required_status_checks"
	cloudBuildAppSlug    = "google-cloud-build"
	runnerGroupName      = "applications"
	runnerVariable       = "CI_LARGE_RUNNER"
	runnerImageID        = "2306"
	runnerImageSource    = "github"
	squashTitleFromPR    = "PR_TITLE"
	squashMessageFromPR  = "PR_BODY"
	infrastructureBypass = github.BypassIntegration
)

// Ruleset is the rule on the repository's default branch: changed by pull request alone,
// no force push, no deletion, the placement's approving reviews, squash the only merge,
// and the plan trigger's build passing on the latest commit with the branch up to date.
// The infrastructure app bypasses it: the apply pushes nothing here, but a hosting
// organization's creation pushes that organization's render after its 1-org rules the
// branch, and the same shape rules every infrastructure repository. cloudBuildAppID is
// Google's app's id, the check's reporter; infrastructureAppID the bypass actor's.
func (d *Desired) Ruleset(cloudBuildAppID, infrastructureAppID int64) *github.Ruleset {
	reviews := d.p.ApprovingReviewCount()
	rs := &github.Ruleset{
		Name: rulesetName, Target: github.RulesetTargetBranch, Enforcement: github.RulesetActive,
		BypassActors: []github.BypassActor{},
		Conditions:   &github.RulesetConditions{RefName: github.RefNameCondition{Include: []string{github.DefaultBranchPattern}, Exclude: []string{}}},
		Rules: []github.Rule{
			{Type: ruleDeletion},
			{Type: ruleNonFastForward},
			{Type: rulePullRequest, Parameters: map[string]any{
				"required_approving_review_count":   float64(reviews),
				"dismiss_stale_reviews_on_push":     reviews > 0,
				"require_code_owner_review":         false,
				"require_last_push_approval":        reviews > 0,
				"required_review_thread_resolution": false,
				"allowed_merge_methods":             []any{"squash"},
			}},
			{Type: ruleRequiredChecks, Parameters: map[string]any{
				"strict_required_status_checks_policy": true,
				"do_not_enforce_on_create":             false,
				"required_status_checks":               []any{map[string]any{"context": d.PlanCheckContext(), "integration_id": float64(cloudBuildAppID)}},
			}},
		},
	}
	if infrastructureAppID != 0 {
		rs.BypassActors = append(rs.BypassActors, github.BypassActor{ActorID: infrastructureAppID, ActorType: infrastructureBypass, BypassMode: github.BypassAlways})
	}

	return rs
}

// RepositorySettings are the repository's merge settings.
func (d *Desired) RepositorySettings() *github.RepositorySettings {
	f, t := false, true
	title, message := squashTitleFromPR, squashMessageFromPR

	return &github.RepositorySettings{
		AllowMergeCommit: &f, AllowRebaseMerge: &f, AllowSquashMerge: &t,
		SquashMergeCommitTitle: &title, SquashMergeCommitMessage: &message,
		DeleteBranchOnMerge: &t, AllowUpdateBranch: &t,
	}
}

// RunnerVariable is the organization's CI_LARGE_RUNNER variable, nil when the placement
// names no larger runner.
func (d *Desired) RunnerVariable() *github.Variable {
	if d.p.CiLargeRunner == "" {
		return nil
	}

	return &github.Variable{Name: runnerVariable, Value: d.p.CiLargeRunner, Visibility: github.VisibilityAll}
}

// HostedRunner is the larger runner the placement sizes, nil when it sizes none: its
// name is the variable's value.
func (d *Desired) HostedRunner() *github.HostedRunnerRequest {
	if d.p.CiLargeRunnerSize == "" {
		return nil
	}

	return &github.HostedRunnerRequest{Name: d.p.CiLargeRunner, Image: github.RunnerImage{ID: runnerImageID, Source: runnerImageSource}, Size: d.p.CiLargeRunnerSize, MaximumRunners: d.p.CiLargeRunnerMaximum}
}

// joined spells a list in a sentence.
func joined(words []string) string {
	return strings.Join(words, ", ")
}
