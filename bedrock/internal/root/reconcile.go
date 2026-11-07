// reconcile.go is the engine: it reads what stands, compares it with what the placement
// says, and either lists the differences (bedrock org check, and the plan trigger's
// comment) or makes them true in order (the apply trigger, and the bootstrap). Each
// section reads before it makes, so a run that stopped halfway finishes on the next.

package root

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"

	"google.golang.org/api/googleapi"

	"github.com/go-playground/errors/v5"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/cccteam/ccc/bedrock/internal/budgets"
	"github.com/cccteam/ccc/bedrock/internal/create"
	"github.com/cccteam/ccc/bedrock/internal/github"
	"github.com/cccteam/ccc/bedrock/internal/org"
)

// Mode is what a run does with the differences it finds.
type Mode int

// The three modes: check and plan list the differences; apply makes them true.
const (
	Check Mode = iota
	Plan
	Apply
)

// Change is one difference between the placement and what stands: what the apply does
// about it, in a sentence, and the call that does it.
type Change struct {
	Section string
	What    string
	apply   func(ctx context.Context) error
}

// Report is what a run found and did.
type Report struct {
	// Changes are the differences found, in the order the apply makes them true; Applied
	// says how many it did.
	Changes []Change
	Applied int
	// Findings are what the run cannot make true and a person must see: an app that is
	// not as declared, a zone whose domain the placement no longer lists.
	Findings []string
	// Notes are information: what was not checked and why.
	Notes []string
	// Created are the hosting organizations the apply created, by prefix, with their
	// hand-off pull requests.
	Created []Creation
}

// Creation is one hosting organization the apply created, or found created.
type Creation struct {
	Prefix      string
	PullRequest string
	Already     bool
}

// Clean reports whether nothing differs and nothing was found.
func (r *Report) Clean() bool {
	return len(r.Changes) == 0 && len(r.Findings) == 0
}

// Request is one run: the placement and its repository, the mode, the commit the run
// builds (for the marker and the creations) and the GitHub token the run acts with (the
// infrastructure app's installation token, or a person's; empty leaves GitHub unread).
type Request struct {
	Placement   *org.Placement
	Dir         string
	Mode        Mode
	Commit      string
	GitHubToken string
	// Bootstrap marks the bootstrap's run: the organization-node and billing grants and
	// the bucket's grant are made (an apply trigger's run holds no right to them and
	// reports them instead), and no hosting organization is created.
	Bootstrap bool
}

// engine is one run.
type engine struct {
	clients *Clients
	req     *Request
	p       *org.Placement
	d       *Desired
	out     io.Writer
	report  *Report
	google  create.Google
	// policies is the Org Policy client; created are the hosting organizations this run
	// created, by prefix, with their placements as recorded, for the policies section,
	// and createdFolders their own folders, which the section compares before the
	// hand-off records them.
	policies       OrgPolicies
	created        map[string]*org.Placement
	createdFolders map[string]string
	secrets        Secrets
	dns            DNS
	builds         Builds
	iam            IAM
	budgets        budgets.Client
	datasets       Datasets
	gh             *github.Client
}

// Run runs the engine in the request's mode and answers the report. In Apply mode a
// change that fails stops the run with the changes before it made.
func Run(ctx context.Context, clients *Clients, req *Request, out io.Writer) (*Report, error) {
	if !req.Placement.IsRoot() {
		return nil, errors.Newf("the %s organization is a hosting organization: the engine runs a root organization", req.Placement.Prefix)
	}
	if req.Placement.Projects[org.RootProjectKey] == "" {
		return nil, errors.New("placement.json records no root project (projects.root): bedrock org bootstrap records it")
	}
	e := &engine{clients: clients, req: req, p: req.Placement, d: NewDesired(req.Placement), out: out, report: &Report{}}
	if err := e.open(ctx); err != nil {
		return nil, err
	}
	defer e.close()
	if e.d.ProjectNumber == "" {
		number, err := e.google.ProjectNumber(ctx, e.d.Project())
		if err != nil {
			return nil, err
		}
		e.d.ProjectNumber = number
	}
	sections := []func(ctx context.Context) error{
		e.services, e.identities, e.customRole, e.secretsSection, e.grants, e.zones, e.verification, e.cloudBuild, e.githubSection, e.apps,
	}
	if req.Mode == Apply && !req.Bootstrap {
		sections = append(sections, e.creations)
	}
	sections = append(sections, e.orgPolicies, e.budgetsSection, e.billingExport)
	if req.Bootstrap {
		sections = append(sections, e.bootstrapWithdrawal)
	}
	for _, section := range sections {
		if err := section(ctx); err != nil {
			return e.report, err
		}
	}

	return e.report, nil
}

func (e *engine) open(ctx context.Context) error {
	var err error
	if e.google, err = e.clients.Google(ctx, e.p.Projects[org.RootProjectKey]); err != nil {
		return err
	}
	if e.clients.OrgPolicies == nil {
		return errors.New("no Org Policy client is wired: the root organization sets the organization policies of its hosting organizations")
	}
	if e.policies, err = e.clients.OrgPolicies(ctx, e.d.Project()); err != nil {
		return err
	}
	if e.secrets, err = e.clients.Secrets(ctx); err != nil {
		return err
	}
	if e.req.Bootstrap && e.clients.DNSAs != nil {
		// The bootstrap makes the zones as the applier, the identity Google verifies as
		// the domains' owner, with the Token Creator it grants itself for the run.
		e.dns, err = e.clients.DNSAs(ctx, e.d.Applier())
	} else {
		e.dns, err = e.clients.DNS(ctx)
	}
	if err != nil {
		return err
	}
	if e.builds, err = e.clients.Builds(ctx); err != nil {
		return err
	}
	if e.iam, err = e.clients.IAM(ctx); err != nil {
		return err
	}
	if e.clients.Budgets == nil || e.clients.Datasets == nil {
		return errors.New("no Budgets or BigQuery client is wired: the root organization keeps the spend budgets and the billing export's dataset")
	}
	if e.req.Mode == Apply && !e.req.Bootstrap && e.clients.BudgetsAs != nil {
		// An apply trigger's run holds nothing on the billing account itself: it keeps
		// the budgets as the creating identity, a Billing Account Administrator, which it
		// impersonates for the creations too. The bootstrap is the break-glass account,
		// an administrator of the account in its own right.
		e.budgets, err = e.clients.BudgetsAs(ctx, e.d.Creator())
	} else {
		e.budgets, err = e.clients.Budgets(ctx, e.d.Project())
	}
	if err != nil {
		return err
	}
	if e.datasets, err = e.clients.Datasets(ctx); err != nil {
		return err
	}
	if e.req.GitHubToken != "" {
		e.gh = e.clients.GitHub(e.req.GitHubToken)
	}

	return nil
}

func (e *engine) close() {
	for _, c := range []io.Closer{e.datasets, e.budgets, e.iam, e.builds, e.dns, e.secrets, e.policies, e.google} {
		if c != nil {
			_ = c.Close()
		}
	}
}

// change records one difference and, in Apply mode, makes it true at once.
func (e *engine) change(ctx context.Context, section, what string, apply func(ctx context.Context) error) error {
	e.report.Changes = append(e.report.Changes, Change{Section: section, What: what, apply: apply})
	if e.req.Mode != Apply {
		return nil
	}
	fmt.Fprintf(e.out, "%s: %s\n", section, what)
	if err := e.settled(ctx, func() error { return apply(ctx) }); err != nil {
		return errors.Wrapf(err, "%s: %s", section, what)
	}
	e.report.Applied++

	return nil
}

// A resource made moments ago, an identity above all, is not seen by every API at once:
// a policy that names it is refused as InvalidArgument, "does not exist" (Secret Manager
// in the lab, 2026-11-05, seconds after the identity was created). settled runs the call
// again while that is the answer, settleWait apart, up to settleTries.
const (
	settleWait  = 10 * time.Second
	settleTries = 12
)

func (e *engine) settled(ctx context.Context, call func() error) error {
	for try := 1; ; try++ {
		err := call()
		if err == nil || !notSeenYet(err) || try == settleTries {
			return err
		}
		fmt.Fprintf(e.out, "  (what this names was made moments ago and is not seen yet; trying again in %s)\n", settleWait)
		if werr := e.sleep(ctx, settleWait); werr != nil {
			return werr
		}
	}
}

func (e *engine) sleep(ctx context.Context, d time.Duration) error {
	if e.clients.Sleep != nil {
		return e.clients.Sleep(ctx, d)
	}

	return sleep(ctx, d)
}

// notSeenYet reports whether the error is an API's answer that a resource named in the
// request does not exist, though it was just made.
func notSeenYet(err error) bool {
	st, ok := status.FromError(errors.Cause(err))

	return ok && st.Code() == codes.InvalidArgument && strings.Contains(st.Message(), "does not exist")
}

func (e *engine) finding(f string) {
	e.report.Findings = append(e.report.Findings, f)
}

func (e *engine) note(n string) {
	e.report.Notes = append(e.report.Notes, n)
}

// The sections, as the report names them.
const (
	sectionServices     = "APIs"
	sectionIdentities   = "identities"
	sectionRoles        = "roles"
	sectionSecrets      = "secrets"
	sectionGrants       = "grants"
	sectionZones        = "zones"
	sectionVerification = "verification"
	sectionCloudBuild   = "Cloud Build"
	sectionGitHub       = "GitHub"
	sectionApps         = "GitHub Apps"
	sectionCreations    = "creations"
	sectionPolicies     = "organization policies"
)

// services enables the APIs the root project lacks.
func (e *engine) services(ctx context.Context) error {
	enabled, err := e.iam.EnabledServices(ctx, e.d.Project())
	if err != nil {
		return err
	}
	var missing []string
	for _, service := range Services {
		if !slices.Contains(enabled, service) {
			missing = append(missing, service)
		}
	}
	if len(missing) == 0 {
		return nil
	}

	return e.change(ctx, sectionServices, "enables "+joined(missing)+" in "+e.d.Project(), func(ctx context.Context) error {
		return e.google.EnableServices(ctx, e.d.Project(), missing)
	})
}

// identities makes the three identities that are missing.
func (e *engine) identities(ctx context.Context) error {
	for _, id := range e.d.Identities() {
		exists, err := e.google.ServiceAccountExists(ctx, id.Email)
		if err != nil {
			return err
		}
		if exists {
			continue
		}
		if err := e.change(ctx, sectionIdentities, "creates the identity "+id.Email, func(ctx context.Context) error {
			return e.google.CreateServiceAccount(ctx, e.d.Project(), id.AccountID, id.DisplayName, id.Description)
		}); err != nil {
			return err
		}
	}

	return nil
}

// customRole makes the secret policy administrator role, or brings it to its permissions.
func (e *engine) customRole(ctx context.Context) error {
	want := e.d.SecretPolicyAdmin()
	got, err := e.iam.Role(ctx, e.d.Project(), org.SecretPolicyAdminRole)
	if err != nil {
		return err
	}
	switch {
	case got == nil:
		return e.change(ctx, sectionRoles, "creates the custom role "+org.SecretPolicyAdminRole+" in "+e.d.Project(), func(ctx context.Context) error {
			return e.iam.CreateRole(ctx, e.d.Project(), org.SecretPolicyAdminRole, want)
		})
	case !slices.Equal(got.Permissions, want.Permissions) || got.Title != want.Title || got.Description != want.Description:
		return e.change(ctx, sectionRoles, "brings the custom role "+org.SecretPolicyAdminRole+" to its permissions ("+joined(want.Permissions)+")", func(ctx context.Context) error {
			return e.iam.UpdateRole(ctx, e.d.Project(), org.SecretPolicyAdminRole, want)
		})
	}

	return nil
}

// secretsSection makes the four secrets, moves the keys' aliases to the pinned versions
// and sets each secret's policy for the roles the engine manages.
func (e *engine) secretsSection(ctx context.Context) error {
	for _, spec := range e.d.Secrets() {
		got, err := e.secrets.Secret(ctx, spec.Name)
		if err != nil {
			return err
		}
		if got == nil {
			if err := e.change(ctx, sectionSecrets, "creates the secret "+spec.ID+" in "+e.d.Project(), func(ctx context.Context) error {
				return e.secrets.CreateSecret(ctx, e.d.Project(), spec.ID, e.d.Labels())
			}); err != nil {
				return err
			}
			got = &Secret{Aliases: map[string]string{}}
		}
		if spec.Aliased {
			if err := e.aliases(ctx, spec, got); err != nil {
				return err
			}
		}
		if err := e.secretPolicy(ctx, spec, got != nil); err != nil {
			return err
		}
	}

	return nil
}

// aliases moves the secret's alias to the pinned version, when one is pinned and enabled.
func (e *engine) aliases(ctx context.Context, spec SecretSpec, got *Secret) error {
	want := spec.Aliases[org.KeyAlias]
	if want == "" {
		if _, has := got.Aliases[org.KeyAlias]; has {
			e.finding(fmt.Sprintf("the alias %s of %s points at version %s, and placement.json pins no version for it: pin one (bedrock secret pin), or the readers keep that version", org.KeyAlias, spec.ID, got.Aliases[org.KeyAlias]))
		}

		return nil
	}
	if got.Aliases[org.KeyAlias] == want {
		return nil
	}
	if e.req.Mode == Apply {
		versions, err := e.secrets.SecretVersions(ctx, spec.Name)
		if err != nil {
			return err
		}
		i := slices.IndexFunc(versions, func(v SecretVersion) bool { return v.Version == want })
		if i < 0 || versions[i].State != "ENABLED" {
			return errors.Newf("placement.json pins version %s of %s, which is not an enabled version of the secret: pin one that is (bedrock org secrets check says which)", want, spec.ID)
		}
	}
	from := "no version"
	if got.Aliases[org.KeyAlias] != "" {
		from = "version " + got.Aliases[org.KeyAlias]
	}

	return e.change(ctx, sectionSecrets, fmt.Sprintf("moves the alias %s of %s from %s to version %s", org.KeyAlias, spec.ID, from, want), func(ctx context.Context) error {
		return e.secrets.SetSecretAliases(ctx, spec.Name, spec.Aliases)
	})
}

// secretPolicy sets the secret's bindings for the roles the engine manages (the policy
// administrator role, accessor, version adder), leaving every other role as it is.
func (e *engine) secretPolicy(ctx context.Context, spec SecretSpec, exists bool) error {
	var current []org.Binding
	if exists {
		var err error
		if current, err = e.secrets.SecretPolicy(ctx, spec.Name); err != nil {
			return err
		}
	}
	managed := []string{e.d.SecretPolicyAdminID(), roleSecretAccessor, roleSecretVersionAdder}
	want := slices.Clone(spec.Policy)
	for _, b := range current {
		if !slices.Contains(managed, b.Role) {
			want = append(want, b)
		}
	}
	// On the managed roles the root organization owns its own members alone. A hosting
	// organization's layers grant their identities and their Cloud Build service agent
	// the policy administration and reads of the keys and the token, through the policy
	// administration the creation gave its boot identity (0-bootstrap, 1-org, 2-env);
	// those members are theirs to hold, and stay as they stand.
	want = keepTheirs(want, current, managed, e.hostingMembers())
	sort.Slice(want, func(i, j int) bool { return want[i].Role < want[j].Role })
	if diff := cmp.Diff(current, want, cmp.Transformer("sorted", sortedBindings)); diff == "" {
		return nil
	}

	return e.change(ctx, sectionSecrets, "sets the policy of "+spec.ID+": "+describeBindings(want, managed), func(ctx context.Context) error {
		return e.secrets.SetSecretPolicy(ctx, spec.Name, want)
	})
}

// hostingMembers answers whether a member of a shared secret's policy is a hosting
// organization's: a service account in one of its projects, which bedrock names after
// the organization's prefix (<prefix>-<key>-gbl-core-<suffix>; the organization's record
// lists only the projects recorded so far, and its 1-org grants its environments'
// identities before the creation records their projects), or any Cloud Build service
// agent, which only an organization's connection reads the token through.
func (e *engine) hostingMembers() func(member string) bool {
	prefixes := make([]string, 0, len(e.p.HostingOrganizations))
	for _, record := range e.p.HostingOrganizations {
		prefixes = append(prefixes, record.Prefix+"-")
	}

	return func(member string) bool {
		account, ok := strings.CutPrefix(member, "serviceAccount:")
		if !ok {
			return false
		}
		if strings.HasPrefix(account, "service-") && strings.HasSuffix(account, "@gcp-sa-cloudbuild.iam.gserviceaccount.com") {
			return true
		}
		_, domain, ok := strings.Cut(account, "@")
		project, isAccount := strings.CutSuffix(domain, ".iam.gserviceaccount.com")

		return ok && isAccount && slices.ContainsFunc(prefixes, func(prefix string) bool { return strings.HasPrefix(project, prefix) })
	}
}

// keepTheirs adds to each managed role's binding in want the members of current the
// hosting organizations hold there (theirs), so that the root organization's apply
// leaves them as they stand.
func keepTheirs(want, current []org.Binding, managed []string, theirs func(member string) bool) []org.Binding {
	for _, b := range current {
		if !slices.Contains(managed, b.Role) {
			continue
		}
		var kept []string
		for _, m := range b.Members {
			if theirs(m) {
				kept = append(kept, m)
			}
		}
		if len(kept) == 0 {
			continue
		}
		i := slices.IndexFunc(want, func(w org.Binding) bool { return w.Role == b.Role })
		if i < 0 {
			want = append(want, org.Binding{Role: b.Role})
			i = len(want) - 1
		}
		members := slices.Clone(want[i].Members)
		for _, m := range kept {
			if !slices.Contains(members, m) {
				members = append(members, m)
			}
		}
		want[i].Members = members
	}

	return want
}

// sortedBindings orders bindings and their members, for a comparison.
func sortedBindings(bs []org.Binding) []org.Binding {
	out := make([]org.Binding, 0, len(bs))
	for _, b := range bs {
		members := slices.Clone(b.Members)
		sort.Strings(members)
		out = append(out, org.Binding{Role: b.Role, Members: members})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Role < out[j].Role })

	return out
}

// describeBindings spells the managed roles' members.
func describeBindings(bs []org.Binding, managed []string) string {
	var parts []string
	for _, b := range bs {
		if slices.Contains(managed, b.Role) {
			parts = append(parts, b.Role[strings.LastIndex(b.Role, "/")+1:]+" to "+joined(b.Members))
		}
	}

	return joined(parts)
}

// bootstrapGrants makes the break-glass account's Token Creator on the applier and the
// creating identity in the bootstrap's run, which takes them off at its end
// (bootstrapWithdrawal); any other run takes off what a bootstrap that stopped left.
func (e *engine) bootstrapGrants(ctx context.Context) error {
	if !e.req.Bootstrap {
		return e.bootstrapWithdrawal(ctx)
	}
	for _, g := range e.d.BootstrapGrants() {
		bindings, err := e.iam.ServiceAccountPolicy(ctx, g.Resource)
		if err != nil {
			return err
		}
		if holds(bindings, g.Role, g.Member) {
			continue
		}
		if err := e.change(ctx, sectionGrants, fmt.Sprintf("grants %s %s on the identity %s, for the bootstrap's run", g.Member, g.Role, g.Resource), func(ctx context.Context) error {
			return e.iam.GrantServiceAccount(ctx, g.Resource, g.Role, g.Member)
		}); err != nil {
			return err
		}
	}

	return nil
}

// bootstrapWithdrawal takes the bootstrap's grants off: at the end of the bootstrap's
// run, once the zones are made and the identities verified, and in any other run that
// finds them.
func (e *engine) bootstrapWithdrawal(ctx context.Context) error {
	for _, g := range e.d.BootstrapGrants() {
		bindings, err := e.iam.ServiceAccountPolicy(ctx, g.Resource)
		if err != nil {
			return err
		}
		if !holds(bindings, g.Role, g.Member) {
			continue
		}
		if err := e.change(ctx, sectionGrants, fmt.Sprintf("takes %s's %s off the identity %s, the bootstrap's grant for its run", g.Member, g.Role, g.Resource), func(ctx context.Context) error {
			return e.iam.RevokeServiceAccount(ctx, g.Resource, g.Role, g.Member)
		}); err != nil {
			return err
		}
	}

	return nil
}

// grants brings each identity's roles on the project and the folder to the desired
// list, and makes the bindings on the identities themselves; at the bootstrap, the
// creating identity's roles at the organization node and on the billing account and the
// applier's grant on the bucket too, which a trigger's run reports when they are missing.
func (e *engine) grants(ctx context.Context) error {
	if err := e.memberRoles(ctx, "project "+e.d.Project(), e.d.ProjectRoles(), e.iam.ProjectPolicy, e.d.Project(), e.google.GrantProject, e.google.RevokeProject); err != nil {
		return err
	}
	if err := e.operatorGrants(ctx); err != nil {
		return err
	}
	folder, err := e.folder(ctx)
	if err != nil {
		return err
	}
	if folder != "" {
		if err := e.memberRoles(ctx, "folder "+folder, e.d.FolderRoles(), e.iam.FolderPolicy, folder, e.google.GrantFolder, e.google.RevokeFolder); err != nil {
			return err
		}
	}
	for _, g := range e.d.ServiceAccountGrants() {
		bindings, err := e.iam.ServiceAccountPolicy(ctx, g.Resource)
		if err != nil {
			return err
		}
		if holds(bindings, g.Role, g.Member) {
			continue
		}
		if err := e.change(ctx, sectionGrants, fmt.Sprintf("grants %s %s on the identity %s", g.Member, g.Role, g.Resource), func(ctx context.Context) error {
			return e.iam.GrantServiceAccount(ctx, g.Resource, g.Role, g.Member)
		}); err != nil {
			return err
		}
	}
	if err := e.bootstrapGrants(ctx); err != nil {
		return err
	}

	return e.outerGrants(ctx)
}

// operatorGrants keep the break-glass account's roles on the root project
// (Desired.OperatorGrants): BigQuery Admin, with which it points the billing export at
// the dataset, and Service Usage Consumer, for the quota of its checks at a terminal.
// The account's other roles on the project are not this run's to touch, so each grant is
// made when missing and nothing is taken off.
func (e *engine) operatorGrants(ctx context.Context) error {
	grants := e.d.OperatorGrants()
	if len(grants) == 0 {
		return nil
	}
	bindings, err := e.iam.ProjectPolicy(ctx, grants[0].Resource)
	if err != nil {
		return err
	}
	for _, g := range grants {
		if holds(bindings, g.Role, g.Member) {
			continue
		}
		why := "for pointing the billing export at its dataset"
		if g.Role == org.ServiceUsageConsumerRole {
			why = "for the quota its checks at a terminal bill to the project"
		}
		if err := e.change(ctx, sectionGrants, fmt.Sprintf("grants %s %s on project %s, %s", g.Member, g.Role, g.Resource, why), func(ctx context.Context) error {
			return e.google.GrantProject(ctx, g.Resource, g.Role, g.Member)
		}); err != nil {
			return err
		}
	}

	return nil
}

// outerGrants are the grants above the root project: the creating identity's at the
// organization node and on the billing account, and the applier's on the bucket. The
// bootstrap makes them as the break-glass account, and a run that reads those policies
// reports one missing as a finding. A trigger's run reads neither the organization
// node's policy nor the billing account's: the runners hold their roles under the
// organization's folder and nowhere higher, by design (README.md, "Several
// organizations"), so the run notes what it did not compare and goes on. The bucket is
// the root project's, which both runners read.
func (e *engine) outerGrants(ctx context.Context) error {
	organization, read, err := e.outerPolicy(ctx, "the grants at the organization node (the creating identity's Folder Creator, the applier's Organization Policy Administrator, the planner's Organization Policy Viewer) were", "the organization node's", "an Organization Administrator", func(ctx context.Context) ([]org.Binding, error) {
		return e.iam.OrganizationPolicy(ctx, e.p.OrganizationID)
	})
	if err != nil {
		return err
	}
	if read {
		for _, g := range e.d.OrganizationGrants() {
			if !holds(organization, g.Role, g.Member) {
				if err := e.outerGrant(ctx, fmt.Sprintf("grants %s %s at the organization node", g.Member, g.Role), "an Organization Administrator", func(ctx context.Context) error {
					return e.iam.GrantOrganization(ctx, e.p.OrganizationID, g.Role, g.Member)
				}); err != nil {
					return err
				}
			}
		}
	}
	billing, read, err := e.outerPolicy(ctx, "the creating identity's Billing Account Administrator on billing account "+e.p.BillingAccount+" was", "the billing account's", "a Billing Account Administrator of it", func(ctx context.Context) ([]org.Binding, error) {
		return e.iam.BillingPolicy(ctx, e.p.BillingAccount)
	})
	if err != nil {
		return err
	}
	if read {
		for _, g := range e.d.BillingGrants() {
			if !holds(billing, g.Role, g.Member) {
				if err := e.outerGrant(ctx, fmt.Sprintf("grants %s %s on billing account %s", g.Member, g.Role, e.p.BillingAccount), "a Billing Account Administrator of it", func(ctx context.Context) error {
					return e.iam.GrantBilling(ctx, e.p.BillingAccount, g.Role, g.Member)
				}); err != nil {
					return err
				}
			}
		}
	}
	bucket, err := e.iam.BucketBindings(ctx, e.p.StateBucket)
	if err != nil {
		return err
	}
	condition := e.d.MarkerCondition()
	i := slices.IndexFunc(bucket, func(b ConditionedBinding) bool {
		return b.Role == roleObjectUser && b.Condition != nil && b.Condition.Title == condition.Title && slices.Contains(b.Members, member(e.d.Applier()))
	})
	if i < 0 {
		return e.outerGrant(ctx, fmt.Sprintf("grants %s %s on the markers and the lock in gs://%s", member(e.d.Applier()), roleObjectUser, e.p.StateBucket), "an administrator of the bucket", func(ctx context.Context) error {
			return e.iam.GrantBucket(ctx, e.p.StateBucket, roleObjectUser, member(e.d.Applier()), condition)
		})
	}

	return nil
}

// outerPolicy reads an IAM policy above the root project (what names the grant compared
// against it, whose the policy, who holds the read). A run refused the read, other than
// the bootstrap's, notes the grant it did not compare and answers read false; the
// bootstrap's refusal and every other error are returned.
func (e *engine) outerPolicy(ctx context.Context, what, whose, who string, read func(ctx context.Context) ([]org.Binding, error)) (bindings []org.Binding, ok bool, err error) {
	bindings, err = read(ctx)
	if err == nil {
		return bindings, true, nil
	}
	if e.req.Bootstrap || status.Code(errors.Cause(err)) != codes.PermissionDenied {
		return nil, false, err
	}
	e.note(what + " not compared: this run holds no read of " + whose + " IAM policy, as the triggers' runners hold their roles under the organization's folder alone; the bootstrap made the grants and compares them, and so does bedrock org check run by " + who)

	return nil, false, nil
}

// outerGrant makes a grant above the root project at the bootstrap, and reports it as a
// finding otherwise, since a trigger's run holds no right to it.
func (e *engine) outerGrant(ctx context.Context, what, who string, apply func(ctx context.Context) error) error {
	if e.req.Bootstrap {
		return e.change(ctx, sectionGrants, what, apply)
	}
	e.finding("the root organization lacks a grant the bootstrap makes: " + what + "; " + who + " makes it, or bedrock org bootstrap run again does")

	return nil
}

// memberRoles brings each managed member's roles on the resource to the desired list:
// grants the missing, revokes the extra.
func (e *engine) memberRoles(ctx context.Context, where string, want map[string][]string, read func(ctx context.Context, resource string) ([]org.Binding, error), resource string, grant func(ctx context.Context, resource, role, member string) error, revoke func(ctx context.Context, resource, member string, roles []string) error) error {
	bindings, err := read(ctx, resource)
	if err != nil {
		return err
	}
	members := make([]string, 0, len(want))
	for m := range want {
		members = append(members, m)
	}
	sort.Strings(members)
	for _, m := range members {
		var held []string
		for _, b := range bindings {
			if slices.Contains(b.Members, m) {
				held = append(held, b.Role)
			}
		}
		for _, role := range want[m] {
			if slices.Contains(held, role) {
				continue
			}
			if err := e.change(ctx, sectionGrants, fmt.Sprintf("grants %s %s on %s", m, role, where), func(ctx context.Context) error {
				return grant(ctx, resource, role, m)
			}); err != nil {
				return err
			}
		}
		var extra []string
		for _, role := range held {
			if !slices.Contains(want[m], role) {
				extra = append(extra, role)
			}
		}
		if len(extra) > 0 {
			if err := e.change(ctx, sectionGrants, fmt.Sprintf("takes %s off %s on %s", joined(extra), m, where), func(ctx context.Context) error {
				return revoke(ctx, resource, m, extra)
			}); err != nil {
				return err
			}
		}
	}

	return nil
}

// holds reports whether the bindings give the member the role.
func holds(bindings []org.Binding, role, member string) bool {
	for _, b := range bindings {
		if b.Role == role && slices.Contains(b.Members, member) {
			return true
		}
	}

	return false
}

// folder finds the organization's folder; empty, with a note, when there is none yet.
func (e *engine) folder(ctx context.Context) (string, error) {
	folder, err := e.google.FindFolder(ctx, "organizations/"+e.p.OrganizationID, e.p.Prefix)
	if err != nil {
		return "", err
	}
	if folder == "" {
		e.note("no folder named " + e.p.Prefix + " at the organization node: the bootstrap makes it, and the runners' reads on it wait")
	}

	return folder, nil
}

// zones makes the zone of each domain the placement lists and brings its record sets to
// the labels handed onward; a zone whose domain the placement no longer lists is
// reported, never deleted.
func (e *engine) zones(ctx context.Context) error {
	var existing []Zone
	if err := e.patient(ctx, func() error {
		var err error
		existing, err = e.dns.Zones(ctx, e.d.Project())

		return err
	}); err != nil {
		return err
	}
	wanted := map[string]bool{}
	specs := e.d.Zones()
	for i := range specs {
		spec := &specs[i]
		wanted[spec.Zone.Name] = true
		i := slices.IndexFunc(existing, func(z Zone) bool { return z.Name == spec.Zone.Name })
		switch {
		case i < 0:
			what := "creates the zone " + spec.Zone.Name + " for " + spec.Domain
			if e.req.Mode != Apply {
				e.report.Changes = append(e.report.Changes, Change{Section: sectionZones, What: what})
				for _, r := range spec.Records {
					e.report.Changes = append(e.report.Changes, Change{Section: sectionZones, What: "hands " + strings.TrimSuffix(r.Name, ".") + " onward to " + joined(r.Data)})
				}

				continue
			}
			made, err := e.makeZone(ctx, *spec, what)
			if err != nil {
				return err
			}
			if !made {
				continue
			}
		case existing[i].DNSName != spec.Zone.DNSName:
			e.finding(fmt.Sprintf("the zone %s serves %s, not %s: a zone's domain cannot change; delete the zone by hand and let the apply make it again", spec.Zone.Name, existing[i].DNSName, spec.Zone.DNSName))

			continue
		}
		if err := e.records(ctx, *spec); err != nil {
			return err
		}
	}
	for _, z := range existing {
		if !wanted[z.Name] && strings.HasPrefix(z.Name, e.p.Prefix+"-"+org.RootProjectKey+"-gbl-dns-") {
			e.finding(fmt.Sprintf("the zone %s serves %s, which placement.json no longer lists (domains): the apply deletes no zone; once nothing delegates the domain to it, delete it by hand (gcloud dns managed-zones delete %s --project %s)", z.Name, strings.TrimSuffix(z.DNSName, "."), z.Name, e.d.Project()))
		}
	}

	return nil
}

// records brings the zone's record sets to the placement: one NS record set per label
// handed onward, and the records the domain declares (records), each written as declared
// and rewritten when the zone holds it differently, the apex TXT set keeping the owners'
// verification tokens beside the declared values. The zone's own NS and SOA records are
// its, and stay; a label taken out of the placement has its NS record set taken back,
// and a declared record taken out stays until a person deletes it, as the apply deletes
// no zone.
func (e *engine) records(ctx context.Context, spec ZoneSpec) error {
	current, err := e.dns.RecordSets(ctx, e.d.Project(), spec.Zone.Name)
	if err != nil {
		return err
	}
	var additions, deletions []RecordSet
	for _, want := range spec.Records {
		i := slices.IndexFunc(current, func(r RecordSet) bool { return r.Name == want.Name && r.Type == want.Type })
		if i >= 0 && want.Type == recordTypeTXT && want.Name == spec.Zone.DNSName {
			want.Data = withTokens(want.Data, current[i].Data)
		}
		if i < 0 {
			additions = append(additions, want)

			continue
		}
		if current[i].TTL != want.TTL || !slices.Equal(current[i].Data, want.Data) {
			deletions = append(deletions, current[i])
			additions = append(additions, want)
		}
	}
	for _, r := range current {
		if r.Type != recordTypeNS || r.Name == spec.Zone.DNSName {
			continue
		}
		if !slices.ContainsFunc(spec.Records, func(w RecordSet) bool { return w.Name == r.Name && w.Type == recordTypeNS }) {
			deletions = append(deletions, r)
		}
	}
	if len(additions) == 0 && len(deletions) == 0 {
		return nil
	}
	var what []string
	for _, r := range additions {
		if r.Type == recordTypeNS {
			what = append(what, "hands "+strings.TrimSuffix(r.Name, ".")+" onward to "+joined(r.Data))

			continue
		}
		what = append(what, "writes "+strings.TrimSuffix(r.Name, ".")+" "+r.Type+" "+joined(r.Data))
	}
	for _, r := range deletions {
		if r.Type == recordTypeNS && !slices.ContainsFunc(additions, func(a RecordSet) bool { return a.Name == r.Name && a.Type == recordTypeNS }) {
			what = append(what, "takes back "+strings.TrimSuffix(r.Name, "."))
		}
	}

	return e.change(ctx, sectionZones, "in the zone "+spec.Zone.Name+": "+strings.Join(what, "; "), func(ctx context.Context) error {
		return e.dns.ChangeRecordSets(ctx, e.d.Project(), spec.Zone.Name, additions, deletions)
	})
}

// withTokens keeps the owners' verification tokens the apex TXT set holds beside the
// declared values, so a declared record never takes a token out (verification puts a
// missing one in); anything else the set held is rewritten away.
func withTokens(declared, current []string) []string {
	data := slices.Clone(declared)
	for _, v := range current {
		if strings.HasPrefix(v, `"`+verificationTokenPrefix) && !slices.Contains(data, v) {
			data = append(data, v)
		}
	}

	return data
}

// makeZone makes the zone in an apply, and meets Cloud DNS's ownership check when the
// domain is one Google holds as verified (registered through Google, or well known): the
// applier, as which the zone is made, must own the domain in Google's webmaster
// verification, and before the zone exists its token can only be where the domain's DNS
// is served today, by a person. The run says which record, and the zone waits for the
// next apply.
func (e *engine) makeZone(ctx context.Context, spec ZoneSpec, what string) (made bool, err error) {
	fmt.Fprintf(e.out, "%s: %s\n", sectionZones, what)
	err = e.patient(ctx, func() error { return e.dns.CreateZone(ctx, e.d.Project(), spec.Zone) })
	if err != nil && strings.Contains(err.Error(), create.OwnershipCode) {
		verified, verr := e.applierVerified(ctx, spec)
		if verr != nil {
			return false, verr
		}
		if !verified {
			return false, nil
		}
		err = e.dns.CreateZone(ctx, e.d.Project(), spec.Zone)
	}
	if err != nil {
		return false, errors.Wrapf(err, "%s: %s", sectionZones, what)
	}
	e.report.Changes = append(e.report.Changes, Change{Section: sectionZones, What: what})
	e.report.Applied++

	return true, nil
}

// applierVerified verifies the applier as an owner of the domain through the token a
// person put where the domain's DNS is served today, or says which record to put there,
// as a finding; false while Google does not see it.
func (e *engine) applierVerified(ctx context.Context, spec ZoneSpec) (bool, error) {
	applier := e.d.Applier()
	if e.clients.Verifier == nil {
		return false, errors.Newf("Cloud DNS asks the maker of the zone %s to own %s in Google's webmaster verification, and this run cannot act as %s to verify it", spec.Zone.Name, spec.Domain, applier)
	}
	v, err := e.clients.Verifier(ctx, applier)
	if err != nil {
		return false, err
	}
	defer v.Close()
	token, err := e.token(ctx, v, spec.Domain)
	if err != nil {
		return false, err
	}
	seen, err := v.Verify(ctx, spec.Domain)
	if err != nil {
		return false, err
	}
	if seen {
		fmt.Fprintf(e.out, "  %s owns %s in Google's webmaster verification now, by the record a person put where the domain's DNS is served; the zone can be made.\n", applier, spec.Domain)

		return true, nil
	}
	e.finding(fmt.Sprintf("Cloud DNS asks the maker of the zone %s to own %s in Google's webmaster verification, and the zone waits: where %s's DNS is served today (the registrar's, until the delegation), a person adds a TXT record at the apex with the value %s, the token of %s; the next apply verifies it, makes the zone and names its name servers for the registrar (README.md, \"Delegating a domain to this organization\")", spec.Zone.Name, spec.Domain, spec.Domain, token, applier))

	return false, nil
}

// verification keeps the applier and the creating identity verified owners of each
// domain whose zone stands: their tokens as TXT records at the domain's apex in the
// zone, beside whatever else the apex carries, and the verification itself where one is
// not an owner yet, which Google answers once the registration points at the zone and
// the record resolves; until then the run says the verification waits. The plan's
// runner and a person's check cannot act as the identities and say the ownership is not
// compared.
func (e *engine) verification(ctx context.Context) error {
	specs := e.d.Zones()
	if len(specs) == 0 {
		return nil
	}
	if e.clients.Verifier == nil || e.req.Mode != Apply {
		e.note("the ownership of the domains in Google's webmaster verification by " + e.d.Applier() + " and " + e.d.Creator() + " was not compared: this run cannot act as them (the apply's runner does, and the bootstrap); the apply keeps their tokens in the zones, verifies them and says so")

		return nil
	}
	existing, err := e.dns.Zones(ctx, e.d.Project())
	if err != nil {
		return err
	}
	for _, identity := range []string{e.d.Applier(), e.d.Creator()} {
		if err := e.verifyAs(ctx, identity, specs, existing); err != nil {
			return err
		}
	}

	return nil
}

// verifyAs keeps the identity's token in each standing zone and verifies the identity
// where it is not an owner yet.
func (e *engine) verifyAs(ctx context.Context, identity string, specs []ZoneSpec, existing []Zone) error {
	v, err := e.clients.Verifier(ctx, identity)
	if err != nil {
		return err
	}
	defer v.Close()
	for i := range specs {
		spec := &specs[i]
		if !slices.ContainsFunc(existing, func(z Zone) bool { return z.Name == spec.Zone.Name }) {
			continue
		}
		token, err := e.token(ctx, v, spec.Domain)
		if err != nil {
			return err
		}
		if err := e.tokenRecord(ctx, *spec, identity, token); err != nil {
			return err
		}
		owned, err := v.Owned(ctx, spec.Domain)
		if err != nil {
			return err
		}
		if owned {
			continue
		}
		what := "verifies " + identity + " as an owner of " + spec.Domain + " in Google's webmaster verification"
		fmt.Fprintf(e.out, "%s: %s\n", sectionVerification, what)
		seen, err := v.Verify(ctx, spec.Domain)
		if err != nil {
			return errors.Wrapf(err, "%s: %s", sectionVerification, what)
		}
		if !seen {
			e.note("the verification of " + identity + " as an owner of " + spec.Domain + " waits: Google does not see the token at the domain's apex yet; the registration must point at the zone's name servers (README.md, \"Delegating a domain to this organization\"), and a record just written takes a while to resolve; the next apply asks again")

			continue
		}
		e.report.Changes = append(e.report.Changes, Change{Section: sectionVerification, What: what})
		e.report.Applied++
	}

	return nil
}

// token is the identity's token for the domain, asked for again while the impersonation
// a grant moments ago allows is still refused.
func (e *engine) token(ctx context.Context, v Verifier, domain string) (string, error) {
	var token string
	err := e.patient(ctx, func() error {
		var err error
		token, err = v.Token(ctx, domain)

		return err
	})

	return token, err
}

// tokenRecord puts the token among the TXT values at the domain's apex, leaving the
// values there (a mail policy, another owner's token) as they are.
func (e *engine) tokenRecord(ctx context.Context, spec ZoneSpec, identity, token string) error {
	current, err := e.dns.RecordSets(ctx, e.d.Project(), spec.Zone.Name)
	if err != nil {
		return err
	}
	want := VerificationRecord(spec.Domain, token)
	var deletions []RecordSet
	if i := slices.IndexFunc(current, func(r RecordSet) bool { return r.Name == want.Name && r.Type == want.Type }); i >= 0 {
		if slices.Contains(current[i].Data, want.Data[0]) {
			return nil
		}
		deletions = []RecordSet{current[i]}
		want.TTL = current[i].TTL
		want.Data = append(slices.Clone(current[i].Data), want.Data...)
	}

	return e.change(ctx, sectionVerification, "in the zone "+spec.Zone.Name+": adds the token of "+identity+" for "+spec.Domain+" (a TXT record at the apex)", func(ctx context.Context) error {
		return e.dns.ChangeRecordSets(ctx, e.d.Project(), spec.Zone.Name, []RecordSet{want}, deletions)
	})
}

// A grant made moments ago is not seen by IAM at once: an impersonation it allows, or a
// call it permits, is refused with PermissionDenied for a while. patient runs the call
// again while that is the answer, grantPoll apart, up to grantSettle (the bootstrap's
// constants). Cloud DNS's ownership refusal is a 403 too, and is not waited on.
func (e *engine) patient(ctx context.Context, call func() error) error {
	deadline := time.Now().Add(grantSettle)
	for {
		err := call()
		if err == nil || !permissionNotSeen(err) || time.Now().After(deadline) {
			return err
		}
		fmt.Fprintln(e.out, "  (a grant made moments ago has not reached IAM yet; trying again in a moment)")
		if werr := e.sleep(ctx, grantPoll); werr != nil {
			return werr
		}
	}
}

// permissionNotSeen reports whether the error is a refusal for a permission: a gRPC
// PermissionDenied, an HTTP 403, or an impersonation refused with one; not Cloud DNS's
// ownership refusal, which no wait resolves.
func permissionNotSeen(err error) bool {
	if strings.Contains(err.Error(), create.OwnershipCode) {
		return false
	}
	if status.Code(errors.Cause(err)) == codes.PermissionDenied {
		return true
	}
	var gerr *googleapi.Error
	if errors.As(err, &gerr) && gerr.Code == http.StatusForbidden {
		return true
	}

	return strings.Contains(err.Error(), "status code 403")
}

// cloudBuild makes the connection, the repository link and the two triggers, and grants
// the runners the connection's read token.
func (e *engine) cloudBuild(ctx context.Context) error {
	if !e.d.Connected() {
		e.note("placement.json records no Cloud Build authorization (cloudBuildInstallationId, cloudBuildTokenSecretVersion): the bootstrap records it, and the connection and the triggers wait")

		return nil
	}
	project, location, name := e.d.Project(), e.d.Location(), e.p.ConnectionName()
	connection, err := e.builds.Connection(ctx, project, location, name)
	if err != nil {
		return err
	}
	want := e.d.Connection()
	switch {
	case connection == nil:
		if err := e.change(ctx, sectionCloudBuild, "creates the connection "+name+" in "+location+" from the recorded authorization", func(ctx context.Context) error {
			return e.builds.CreateConnection(ctx, project, location, name, want)
		}); err != nil {
			return err
		}
	case *connection != want:
		if err := e.change(ctx, sectionCloudBuild, "brings the connection "+name+" to the recorded authorization (installation "+want.InstallationID+", token "+want.TokenSecretVersion+")", func(ctx context.Context) error {
			return e.builds.UpdateConnection(ctx, project, location, name, want)
		}); err != nil {
			return err
		}
	}
	exists := connection != nil
	if exists || e.req.Mode == Apply {
		for _, runner := range []string{member(e.d.Planner()), member(e.d.Applier())} {
			bindings, err := e.builds.ConnectionPolicy(ctx, project, location, name)
			if err != nil {
				return err
			}
			if holds(bindings, roleReadTokenAccessor, runner) {
				continue
			}
			if err := e.change(ctx, sectionCloudBuild, "grants "+runner+" "+roleReadTokenAccessor+" on the connection", func(ctx context.Context) error {
				return e.builds.GrantConnection(ctx, project, location, name, roleReadTokenAccessor, runner)
			}); err != nil {
				return err
			}
		}
	} else {
		for _, runner := range []string{member(e.d.Planner()), member(e.d.Applier())} {
			e.report.Changes = append(e.report.Changes, Change{Section: sectionCloudBuild, What: "grants " + runner + " " + roleReadTokenAccessor + " on the connection"})
		}
	}
	linkName := e.p.RepositoryLinkName()
	remote := ""
	if exists {
		if remote, err = e.builds.RepositoryLink(ctx, project, location, name, linkName); err != nil {
			return err
		}
	}
	if remote == "" {
		if err := e.change(ctx, sectionCloudBuild, "links the repository "+e.d.RemoteURI()+" under the connection as "+linkName, func(ctx context.Context) error {
			return e.builds.CreateRepositoryLink(ctx, project, location, name, linkName, e.d.RemoteURI())
		}); err != nil {
			return err
		}
	} else if remote != e.d.RemoteURI() {
		e.finding(fmt.Sprintf("the repository link %s points at %s, not %s: a link's repository cannot change; delete the link by hand and let the apply make it again", linkName, remote, e.d.RemoteURI()))
	}
	for _, want := range e.d.Triggers() {
		got, err := e.builds.Trigger(ctx, project, location, want.Name)
		if err != nil {
			return err
		}
		switch {
		case got == nil:
			if err := e.change(ctx, sectionCloudBuild, "creates the trigger "+want.Name, func(ctx context.Context) error {
				return e.builds.CreateTrigger(ctx, project, location, want)
			}); err != nil {
				return err
			}
		case triggerDiff(got, want) != "":
			want.ID = got.ID
			if err := e.change(ctx, sectionCloudBuild, "brings the trigger "+want.Name+" to its steps: "+triggerDiff(got, want), func(ctx context.Context) error {
				return e.builds.UpdateTrigger(ctx, project, location, want)
			}); err != nil {
				return err
			}
		}
	}

	return nil
}

// triggerDiff says in a few words what differs between the trigger that stands and the
// one wanted, empty for nothing.
func triggerDiff(got, want *Trigger) string {
	var parts []string
	if got.ServiceAccount != want.ServiceAccount {
		parts = append(parts, "service account")
	}
	if got.Repository != want.Repository || got.PullRequestBranch != want.PullRequestBranch || got.PushBranch != want.PushBranch {
		parts = append(parts, "repository event")
	}
	if !cmp.Equal(got.Substitutions, want.Substitutions, cmp.Transformer("empty", emptyMap)) {
		parts = append(parts, "substitutions")
	}
	if got.TimeoutSeconds != want.TimeoutSeconds || got.Description != want.Description {
		parts = append(parts, "timeout or description")
	}
	if !cmp.Equal(got.Steps, want.Steps, cmp.Transformer("empty", emptySlice)) {
		parts = append(parts, "steps")
	}

	return joined(parts)
}

func emptyMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}

	return m
}

func emptySlice(s []string) []string {
	if s == nil {
		return []string{}
	}

	return s
}

// apps holds each GitHub App to its declaration: a key readable by its alias signs as the
// app, and GitHub's record of the app and its installations is compared with the
// manifest. Without the key pinned, or without the right to read it, the app is noted
// as not checked.
func (e *engine) apps(ctx context.Context) error {
	if e.clients.GitHub == nil {
		e.note("the GitHub Apps were not checked: no GitHub client is wired")

		return nil
	}
	for _, m := range e.p.AppManifests() {
		key := m.Role.Key()
		appID := e.p.AppID(m.Role)
		if appID == "" || e.p.KeyVersion(key) == "" {
			e.note(fmt.Sprintf("%s is not checked: placement.json records no App ID or no pinned key version for it (%s); the bootstrap records both", m.Role.Title(), key))

			continue
		}
		pem, err := e.secrets.AccessSecretVersion(ctx, "projects/"+e.d.Project()+"/secrets/"+e.p.SecretContainer(key)+"/versions/"+org.KeyAlias)
		if err != nil {
			e.note(fmt.Sprintf("%s is not checked: its key could not be read (%v)", m.Role.Title(), errors.Cause(err)))

			continue
		}
		findings, err := VerifyApp(ctx, e.clients.GitHub, m, appID, pem, e.p.GithubOrganization)
		if err != nil {
			e.note(fmt.Sprintf("%s is not checked: %v", m.Role.Title(), errors.Cause(err)))

			continue
		}
		for _, f := range findings {
			e.finding(f)
		}
	}

	return nil
}

// creations creates each hosting organization the placement records without a folder,
// as the creating identity, and records the hand-off.
func (e *engine) creations(ctx context.Context) error {
	pending := e.p.Pending()
	if len(pending) == 0 {
		return nil
	}
	if e.clients.Create == nil {
		return errors.New("placement.json records a hosting organization to create, and no creation clients are wired")
	}
	for _, record := range pending {
		fmt.Fprintf(e.out, "%s: creates the hosting organization %s\n", sectionCreations, record.Prefix)
		seeded := func(ctx context.Context, folder string) error {
			return e.setPolicies(ctx, record.Prefix, OrganizationFolderPolicies(folder))
		}
		res, err := create.Run(ctx, e.clients.Create, &create.Request{Root: e.p, RootDir: e.req.Dir, Record: record, Token: e.req.GitHubToken, Commit: e.req.Commit, Seeded: seeded}, e.out)
		if err != nil {
			return errors.Wrapf(err, "the creation of %s", record.Prefix)
		}
		e.report.Changes = append(e.report.Changes, Change{Section: sectionCreations, What: "creates the hosting organization " + record.Prefix})
		e.report.Applied++
		e.report.Created = append(e.report.Created, Creation{Prefix: record.Prefix, PullRequest: res.PullRequest, Already: res.Already})
		if e.created == nil {
			e.created = map[string]*org.Placement{}
			e.createdFolders = map[string]string{}
		}
		e.created[record.Prefix] = res.Placement
		e.createdFolders[record.Prefix] = res.Marker.OrganizationFolder
	}

	return nil
}

// Marker is what the apply leaves in the state bucket: the commit it made the
// organization true from, and when.
type Marker struct {
	Commit    string `json:"commit"`
	AppliedAt string `json:"appliedAt"`
	Changes   int    `json:"changes"`
}

// WriteMarker records the apply in the state bucket.
func WriteMarker(ctx context.Context, clients *Clients, p *org.Placement, commit string, changes int) error {
	store, err := clients.Store(ctx)
	if err != nil {
		return err
	}
	defer store.Close()
	now := time.Now
	if clients.Now != nil {
		now = clients.Now
	}
	data, err := json.MarshalIndent(Marker{Commit: commit, AppliedAt: now().UTC().Format(time.RFC3339), Changes: changes}, "", "  ")
	if err != nil {
		return errors.Wrap(err, "json.MarshalIndent()")
	}

	return store.Write(ctx, p.StateBucket, MarkerObject, append(data, '\n'))
}

// Write prints the report: each change the apply would make (an apply printed each as it
// made it), each finding, each note, and the verdict.
func (r *Report) Write(w io.Writer, mode Mode) {
	if mode != Apply {
		for _, c := range r.Changes {
			fmt.Fprintf(w, "%s: the apply %s\n", c.Section, c.What)
		}
	}
	for _, f := range r.Findings {
		fmt.Fprintf(w, "finding: %s\n", f)
	}
	for _, n := range r.Notes {
		fmt.Fprintf(w, "note: %s\n", n)
	}
	switch {
	case mode == Apply:
		fmt.Fprintf(w, "Applied %d change(s); %d finding(s).\n", r.Applied, len(r.Findings))
	case len(r.Changes) == 0 && len(r.Findings) == 0:
		fmt.Fprintln(w, "The root organization stands as placement.json says: nothing to change.")
	default:
		fmt.Fprintf(w, "%d change(s) the apply would make; %d finding(s).\n", len(r.Changes), len(r.Findings))
	}
}
