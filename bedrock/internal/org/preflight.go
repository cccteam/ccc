// preflight.go is the check before the seed: it asks Google which of the permissions the
// seed and the first applies of 0-bootstrap and 1-org need the caller holds at the
// organization and on the billing account, and names each role that grants what is
// missing. Those runs are the break-glass account's, from a person's computer, before
// any layer identity exists, so the roles they need are that account's to hold; the table
// below is the list the READMEs name them from. Everything else the runs need is under
// the organization's folder, which the seed creates: Google makes a folder's creator its
// administrator, and the seed grants the account there what the first apply of 1-org
// needs (0-bootstrap/README.md, "First-time setup").

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

// BootstrapRole is one role the break-glass account holds for the seed and the first
// applies of 0-bootstrap and 1-org: its title as the console names it, its id, where it
// is granted, the permissions tested for it, and what needs them; ZonesNeeds is what a
// zones organization's seed and first applies need it for, empty for a role they do not
// need (Placement.BootstrapRoles).
type BootstrapRole struct {
	Title       string
	Role        string
	On          Scope
	Permissions []string
	Needs       string
	ZonesNeeds  string
}

// BootstrapRoles are the roles the seed and the first applies of 0-bootstrap and 1-org
// need above the organization's folder, each with the permissions that stand for it:
// creating the folder at the organization, and linking the projects to the billing
// account. Everything else those runs do happens under the folder, where the seed grants
// the break-glass account what 1-org's first apply needs (project creator, organization
// policy administrator and tag administrator, on the folder) and where Google makes the
// creator Owner (a project) or Folder Admin and Folder Editor (a folder); every one of
// those grants is removed once the apply trigger applies the layers, and org check lists
// what is left. Nothing of the organization's sits at the organization node afterward.
var BootstrapRoles = []BootstrapRole{
	{
		Title:       "Folder Creator",
		Role:        "roles/resourcemanager.folderCreator",
		On:          OnOrganization,
		Permissions: []string{"resourcemanager.folders.create"},
		Needs:       "the organization's folder, at the organization root; the seed's terraform folder and 1-org's folders are made under it as its administrator",
		ZonesNeeds:  "the organization's folder, at the organization root; the seed's terraform folder is made under it as its administrator",
	},
	{
		Title:       "Billing Account User",
		Role:        "roles/billing.user",
		On:          OnBillingAccount,
		Permissions: []string{"billing.resourceAssociations.create"},
		Needs:       "linking the boot project and 1-org's projects to the billing account",
		ZonesNeeds:  "linking the boot project to the billing account",
	},
}

// BootstrapRoles are the roles the organization's seed and first applies need, by its
// kind: the table for a hosting organization; for a zones organization the same roles,
// each with what needs it there, since 1-dns declares no folder, project, policy or tag.
func (p *Placement) BootstrapRoles() []BootstrapRole {
	if !p.Zones() {
		return BootstrapRoles
	}
	var roles []BootstrapRole
	for _, role := range BootstrapRoles {
		if role.ZonesNeeds == "" {
			continue
		}
		role.Needs = role.ZonesNeeds
		roles = append(roles, role)
	}

	return roles
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
// and break-glass account, the layer after the bootstrap whose first apply the roles
// serve, and each role's answer in the table's order.
type PreflightResult struct {
	OrganizationID    string
	BillingAccount    string
	BreakGlassAccount string
	SecondLayer       string
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
	r := &PreflightResult{OrganizationID: p.OrganizationID, BillingAccount: p.BillingAccount, BreakGlassAccount: p.BreakGlassAccount, SecondLayer: p.SecondLayer()}
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
		fmt.Fprintf(w, "These credentials hold every permission the seed and the first applies of 0-bootstrap and %s need above the organization's folder (0-bootstrap/README.md, First-time setup).\n", r.SecondLayer)

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
	fmt.Fprintf(w, "%d of %d role(s) missing: the break-glass account (%s) is granted %s; then run org preflight again with that account's credentials (gcloud auth application-default login).\n",
		len(missing), len(r.Checks), r.BreakGlassAccount, strings.Join(grants, ", and "))
}

// preflightDoes says what the check does, for the line that says it did not run.
const preflightDoes = "org preflight asks Google, with the run's Application Default Credentials (gcloud auth application-default login, as the break-glass account), which of the permissions the seed and the first applies of 0-bootstrap and 1-org need above the organization's folder they hold"

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
