// preflight.go is the check before the bootstrap: it asks Google which of the
// permissions bedrock org bootstrap needs the caller holds at the organization node and
// on the billing account, and names each role that grants what is missing. The bootstrap
// is the break-glass account's one run from a person's computer, before any identity of
// the organization exists, so the roles it needs are that account's to hold: the table
// below is the list the README names them from. Everything else the bootstrap does
// happens under the organization's folder, which it makes, where Google makes a
// folder's creator its administrator; the bootstrap grants itself the rest there and
// takes it off at the end.

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

// The two places the break-glass account holds roles above the organization's folder.
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

// BootstrapRole is one role the break-glass account holds for the bootstrap: its title
// as the console names it, its id, where it is granted, the permissions tested for it,
// and what needs them.
type BootstrapRole struct {
	Title       string
	Role        string
	On          Scope
	Permissions []string
	Needs       string
}

// BootstrapRoles are the roles bedrock org bootstrap needs the break-glass account to
// hold, each with the permissions that stand for it: administering the organization's
// IAM policy, which grants the creating identity Folder Creator at the node and the
// bootstrap its own working grants; creating the organization's folder at the node; and
// administering the billing account, which links the root project and grants the
// creating identity the account's administration. Everything else happens under the
// folder, where the bootstrap is the creator, and every working grant it makes itself
// comes off at the end; org check lists what is left.
var BootstrapRoles = []BootstrapRole{
	{
		Title:       "Organization Administrator",
		Role:        "roles/resourcemanager.organizationAdmin",
		On:          OnOrganization,
		Permissions: []string{"resourcemanager.organizations.getIamPolicy", "resourcemanager.organizations.setIamPolicy"},
		Needs:       "granting the creating identity Folder Creator at the organization node, and the bootstrap its own working grants, which it takes off at the end",
	},
	{
		Title:       "Folder Creator",
		Role:        "roles/resourcemanager.folderCreator",
		On:          OnOrganization,
		Permissions: []string{"resourcemanager.folders.create"},
		Needs:       "the organization's folder, at the organization root; the root project is made under it as its administrator",
	},
	{
		Title:       "Billing Account Administrator",
		Role:        "roles/billing.admin",
		On:          OnBillingAccount,
		Permissions: []string{"billing.accounts.getIamPolicy", "billing.accounts.setIamPolicy", "billing.budgets.create", "billing.budgets.list", "billing.resourceAssociations.create"},
		Needs:       "linking the root project to the billing account, making the account's spend budget, and granting the creating identity the account's administration, with which each hosting organization's identities get their billing grants and its budget is made",
	},
}

// BootstrapRoles are the roles the bootstrap needs, by the organization's kind: the table
// for a root organization; none for a hosting organization, which its root organization
// creates.
func (p *Placement) BootstrapRoles() []BootstrapRole {
	if !p.IsRoot() {
		return nil
	}

	return BootstrapRoles
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
// and break-glass account, and each role's answer in the table's order.
type PreflightResult struct {
	OrganizationID    string
	BillingAccount    string
	BreakGlassAccount string
	Checks            []RoleCheck
}

// CheckPermissions asks, once per scope, which of the bootstrap roles' permissions the
// caller holds, and answers each role with the permissions it lacks. An API that refuses
// to answer fails the check.
func CheckPermissions(ctx context.Context, p *Placement, tester PermissionTester) (*PreflightResult, error) {
	held := map[Scope][]string{}
	roles := p.BootstrapRoles()
	for _, scope := range []Scope{OnOrganization, OnBillingAccount} {
		permissions := scopePermissions(roles, scope)
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
	r := &PreflightResult{OrganizationID: p.OrganizationID, BillingAccount: p.BillingAccount, BreakGlassAccount: p.BreakGlassAccount}
	for _, role := range roles {
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
func scopePermissions(roles []BootstrapRole, scope Scope) []string {
	var permissions []string
	for _, role := range roles {
		if role.On == scope {
			permissions = append(permissions, role.Permissions...)
		}
	}

	return permissions
}

// Missing are the roles whose permissions the caller does not all hold.
func (r *PreflightResult) Missing() []RoleCheck {
	var missing []RoleCheck
	for i := range r.Checks {
		if !r.Checks[i].Held() {
			missing = append(missing, r.Checks[i])
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
	for i := range r.Checks {
		c := &r.Checks[i]
		verdict, permissions := verdictHolds, c.Permissions
		if !c.Held() {
			verdict, permissions = verdictMissing, c.Missing
		}
		fmt.Fprintf(w, "%s (%s), on %s: %s %s\n", c.Title, c.Role, r.where(c.On), verdict, strings.Join(permissions, ", "))
	}
	missing := r.Missing()
	if len(missing) == 0 {
		fmt.Fprintln(w, "These credentials hold every permission bedrock org bootstrap needs at the organization node and on the billing account (README.md, \"Bootstrapped by one command\").")

		return
	}
	var atOrg, atBilling []string
	for i := range missing {
		c := &missing[i]
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
	fmt.Fprintf(w, "%d of %d role(s) missing: the break-glass account (%s) is granted %s; then run the command again with that account's credentials (gcloud auth application-default login).\n",
		len(missing), len(r.Checks), r.BreakGlassAccount, strings.Join(grants, ", and "))
}

// preflightDoes says what the check does, for the line that says it did not run.
const preflightDoes = "the preflight asks Google, with the run's Application Default Credentials (gcloud auth application-default login, as the break-glass account), which of the permissions bedrock org bootstrap needs at the organization node and on the billing account they hold"

// Preflight opens the tester, runs the check and writes its answer. It reports whether
// the caller holds every role; without credentials, or when an API refuses to answer, it
// says the permissions were not checked and why, and reports false, since a check that
// did not run passes nothing. A hosting organization has no bootstrap roles to check,
// since its root organization creates it: the check says so and reports false.
func Preflight(ctx context.Context, p *Placement, open PermissionTesterFunc, w io.Writer) bool {
	if !p.IsRoot() {
		fmt.Fprintf(w, "Nothing to check: the %s organization is a hosting organization, which its root organization creates from its record as the creating identity; the bootstrap roles are a root organization's break-glass account's to hold.\n", p.Prefix)

		return false
	}
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
