// preflight.go is the check before the seed: it asks Google which of the permissions the
// seed and the first applies of 0-bootstrap and 1-org need the caller holds, on the
// organization and on the billing account, and names each role that grants what is
// missing. Those three runs are the bootstrap administrator's, from their own computer,
// before any layer identity exists, so the roles they need are a person's to hold; the
// table below is the list the READMEs name them from.

package org

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"

	"cloud.google.com/go/iam/apiv1/iampb"
	resourcemanager "cloud.google.com/go/resourcemanager/apiv3"
	"github.com/go-playground/errors/v5"
	"google.golang.org/api/cloudbilling/v1"
)

// Scope is where a bootstrap role is granted and its permissions are tested.
type Scope string

// The two places the bootstrap administrator holds roles.
const (
	// OnOrganization is the organization node: the roles there reach every folder and
	// project under it.
	OnOrganization Scope = "organization"
	// OnBillingAccount is the billing account the projects are linked to.
	OnBillingAccount Scope = "billing account"
)

// The two verdicts on a role, as each line says it.
const (
	verdictHolds   = "holds"
	verdictMissing = "missing"
)

// BootstrapRole is one role the bootstrap administrator holds for the seed and the first
// applies of 0-bootstrap and 1-org: its title as the console names it, its id, where it
// is granted, the permissions tested for it, and what needs them.
type BootstrapRole struct {
	Title       string
	Role        string
	On          Scope
	Permissions []string
	Needs       string
}

// BootstrapRoles are the roles the seed and the first applies of 0-bootstrap and 1-org
// need, each with the permissions that stand for it. Everything else those runs do
// happens inside a project or a folder the person just created, where Google makes the
// creator Owner (a project) or Folder Admin and Folder Editor (a folder). Essential
// Contacts are left out: 1-org registers none until essential_contact_emails is set, and
// a later apply runs as the org identity, which holds the role.
var BootstrapRoles = []BootstrapRole{
	{
		Title:       "Folder Creator",
		Role:        "roles/resourcemanager.folderCreator",
		On:          OnOrganization,
		Permissions: []string{"resourcemanager.folders.create"},
		Needs:       "the seed's terraform folder and 1-org's folders, at the organization root",
	},
	{
		Title:       "Project Creator",
		Role:        "roles/resourcemanager.projectCreator",
		On:          OnOrganization,
		Permissions: []string{"resourcemanager.projects.create"},
		Needs:       "the seed's boot project and 1-org's projects",
	},
	{
		Title:       "Organization Administrator",
		Role:        "roles/resourcemanager.organizationAdmin",
		On:          OnOrganization,
		Permissions: []string{"resourcemanager.organizations.get", "resourcemanager.organizations.getIamPolicy", "resourcemanager.organizations.setIamPolicy"},
		Needs:       "the organization roles the seed grants the boot identity, and those 0-bootstrap and 1-org grant the layer identities",
	},
	{
		Title:       "Organization Policy Administrator",
		Role:        "roles/orgpolicy.policyAdmin",
		On:          OnOrganization,
		Permissions: []string{"orgpolicy.policy.set"},
		Needs:       "1-org's organization policies on its folders",
	},
	{
		Title:       "Organization Role Administrator",
		Role:        "roles/iam.organizationRoleAdmin",
		On:          OnOrganization,
		Permissions: []string{"iam.roles.create", "iam.roles.update"},
		Needs:       "the custom roles 0-bootstrap and 1-org define at the organization",
	},
	{
		Title:       "Tag Administrator",
		Role:        "roles/resourcemanager.tagAdmin",
		On:          OnOrganization,
		Permissions: []string{"resourcemanager.tagKeys.create", "resourcemanager.tagValues.create", "resourcemanager.tagValues.setIamPolicy"},
		Needs:       "1-org's public-invoker tag, its value and the grants on the value",
	},
	{
		Title:       "Billing Account User",
		Role:        "roles/billing.user",
		On:          OnBillingAccount,
		Permissions: []string{"billing.resourceAssociations.create"},
		Needs:       "linking the boot project and 1-org's projects to the billing account",
	},
}

// PermissionTester asks Google which of the permissions the caller holds: on an
// organization (Cloud Resource Manager's organizations.testIamPermissions) and on a
// billing account (Cloud Billing's billingAccounts.testIamPermissions). Each answers the
// subset held. NewPermissionTester is the real one; tests pass a fake.
type PermissionTester interface {
	// OrganizationPermissions are the permissions, of those asked, the caller holds on
	// the organization (its numeric id).
	OrganizationPermissions(ctx context.Context, organizationID string, permissions []string) ([]string, error)
	// BillingAccountPermissions are the permissions, of those asked, the caller holds on
	// the billing account (its id, 012345-6789AB-CDEF01).
	BillingAccountPermissions(ctx context.Context, billingAccount string, permissions []string) ([]string, error)
	// Close releases the connections.
	Close() error
}

// PermissionTesterFunc opens a PermissionTester.
type PermissionTesterFunc func(ctx context.Context) (PermissionTester, error)

// RoleCheck is one bootstrap role and the permissions tested for it that the caller
// lacks; none when the caller holds them all.
type RoleCheck struct {
	BootstrapRole
	Missing []string
}

// Held reports whether the caller holds every permission tested for the role.
func (c RoleCheck) Held() bool {
	return len(c.Missing) == 0
}

// PreflightResult is what the check found: the placement's organization, billing account
// and bootstrap administrator, and each role's answer in the table's order.
type PreflightResult struct {
	OrganizationID string
	BillingAccount string
	Operator       string
	Checks         []RoleCheck
}

// CheckPermissions asks, once per scope, which of the bootstrap roles' permissions the
// caller holds, and answers each role with the permissions it lacks. An API that refuses
// to answer fails the check.
func CheckPermissions(ctx context.Context, p *Placement, tester PermissionTester) (*PreflightResult, error) {
	held := map[Scope][]string{}
	for _, scope := range []Scope{OnOrganization, OnBillingAccount} {
		permissions := scopePermissions(scope)
		var (
			answer []string
			err    error
		)
		switch scope {
		case OnOrganization:
			answer, err = tester.OrganizationPermissions(ctx, p.OrganizationID, permissions)
		case OnBillingAccount:
			answer, err = tester.BillingAccountPermissions(ctx, p.BillingAccount, permissions)
		}
		if err != nil {
			return nil, err
		}
		held[scope] = answer
	}
	r := &PreflightResult{OrganizationID: p.OrganizationID, BillingAccount: p.BillingAccount, Operator: p.Operator}
	for _, role := range BootstrapRoles {
		check := RoleCheck{BootstrapRole: role}
		for _, permission := range role.Permissions {
			if !slices.Contains(held[role.On], permission) {
				check.Missing = append(check.Missing, permission)
			}
		}
		r.Checks = append(r.Checks, check)
	}

	return r, nil
}

// scopePermissions lists every permission the bootstrap roles granted at the scope stand
// for, in the table's order.
func scopePermissions(scope Scope) []string {
	var permissions []string
	for _, role := range BootstrapRoles {
		if role.On == scope {
			permissions = append(permissions, role.Permissions...)
		}
	}

	return permissions
}

// Missing are the roles whose permissions the caller does not all hold.
func (r *PreflightResult) Missing() []RoleCheck {
	var missing []RoleCheck
	for _, c := range r.Checks {
		if !c.Held() {
			missing = append(missing, c)
		}
	}

	return missing
}

// where names the scope's resource in a sentence.
func (r *PreflightResult) where(scope Scope) string {
	if scope == OnBillingAccount {
		return "billing account " + r.BillingAccount
	}

	return "organization " + r.OrganizationID
}

// Write prints one line per role, holds or missing with the permissions tested (all of
// them when held, the ones lacking when not), then what to do.
func (r *PreflightResult) Write(w io.Writer) {
	for _, c := range r.Checks {
		verdict, permissions := verdictHolds, c.Permissions
		if !c.Held() {
			verdict, permissions = verdictMissing, c.Missing
		}
		fmt.Fprintf(w, "%s (%s), on %s: %s %s\n", c.Title, c.Role, r.where(c.On), verdict, strings.Join(permissions, ", "))
	}
	missing := r.Missing()
	if len(missing) == 0 {
		fmt.Fprintf(w, "These credentials hold every permission the seed and the first applies of 0-bootstrap and 1-org need (0-bootstrap/README.md, First-time setup).\n")

		return
	}
	var atOrg, atBilling []string
	for _, c := range missing {
		if c.On == OnBillingAccount {
			atBilling = append(atBilling, c.Title)

			continue
		}
		atOrg = append(atOrg, c.Title)
	}
	var grants []string
	if len(atOrg) > 0 {
		grants = append(grants, fmt.Sprintf("%s at %s, by someone who administers the organization", prose(atOrg), r.where(OnOrganization)))
	}
	if len(atBilling) > 0 {
		grants = append(grants, fmt.Sprintf("%s on %s, by a Billing Account Administrator of it", prose(atBilling), r.where(OnBillingAccount)))
	}
	fmt.Fprintf(w, "%d of %d role(s) missing: the bootstrap administrator (%s) is granted %s; then run org preflight again with that administrator's credentials (gcloud auth application-default login).\n",
		len(missing), len(r.Checks), r.Operator, strings.Join(grants, ", and "))
}

// preflightDoes says what the check does, for the line that says it did not run.
const preflightDoes = "org preflight asks Google, with the run's Application Default Credentials (gcloud auth application-default login, as the bootstrap administrator), which of the permissions the seed and the first applies of 0-bootstrap and 1-org need they hold"

// Preflight opens the tester, runs the check and writes its answer. It reports whether
// the caller holds every role; without credentials, or when an API refuses to answer, it
// says the permissions were not checked and why, and reports false, since a check that
// did not run passes nothing.
func Preflight(ctx context.Context, p *Placement, open PermissionTesterFunc, w io.Writer) bool {
	tester, err := open(ctx)
	if err != nil {
		fmt.Fprintf(w, "Permissions not checked (%v): %s.\n", errors.Cause(err), preflightDoes)

		return false
	}
	defer tester.Close()
	r, err := CheckPermissions(ctx, p, tester)
	if err != nil {
		fmt.Fprintf(w, "Permissions not checked (%v): %s.\n", errors.Cause(err), preflightDoes)

		return false
	}
	r.Write(w)

	return len(r.Missing()) == 0
}

// permissionTester is the PermissionTester over Cloud Resource Manager and Cloud Billing.
type permissionTester struct {
	organizations *resourcemanager.OrganizationsClient
	billing       *cloudbilling.APIService
}

// NewPermissionTester opens the two clients with Application Default Credentials;
// without any, the open fails and says so.
func NewPermissionTester(ctx context.Context) (PermissionTester, error) {
	organizations, err := resourcemanager.NewOrganizationsClient(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "resourcemanager.NewOrganizationsClient()")
	}
	billing, err := cloudbilling.NewService(ctx)
	if err != nil {
		_ = organizations.Close()

		return nil, errors.Wrap(err, "cloudbilling.NewService()")
	}

	return &permissionTester{organizations: organizations, billing: billing}, nil
}

// OrganizationPermissions asks Cloud Resource Manager which of the permissions the
// caller holds on the organization.
func (t *permissionTester) OrganizationPermissions(ctx context.Context, organizationID string, permissions []string) ([]string, error) {
	resource := "organizations/" + organizationID
	resp, err := t.organizations.TestIamPermissions(ctx, &iampb.TestIamPermissionsRequest{Resource: resource, Permissions: permissions})
	if err != nil {
		return nil, errors.Wrapf(err, "resourcemanager.OrganizationsClient.TestIamPermissions(): %s", resource)
	}

	return resp.GetPermissions(), nil
}

// BillingAccountPermissions asks Cloud Billing which of the permissions the caller holds
// on the billing account.
func (t *permissionTester) BillingAccountPermissions(ctx context.Context, billingAccount string, permissions []string) ([]string, error) {
	resource := "billingAccounts/" + billingAccount
	resp, err := t.billing.BillingAccounts.TestIamPermissions(resource, &cloudbilling.TestIamPermissionsRequest{Permissions: permissions}).Context(ctx).Do()
	if err != nil {
		return nil, errors.Wrapf(err, "cloudbilling.BillingAccountsService.TestIamPermissions(): %s", resource)
	}

	return resp.Permissions, nil
}

// Close releases the Cloud Resource Manager connection; the Cloud Billing client holds
// none of its own.
func (t *permissionTester) Close() error {
	if err := t.organizations.Close(); err != nil {
		return errors.Wrap(err, "resourcemanager.OrganizationsClient.Close()")
	}

	return nil
}
