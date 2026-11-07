package org

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/go-playground/errors/v5"
)

// fakeTester answers which permissions the caller holds from what it is told the caller
// lacks, or refuses as an API does; it records what it was asked about.
type fakeTester struct {
	lacks      []string
	orgErr     error
	billingErr error
	asked      *[]string
}

func (f *fakeTester) held(permissions []string) []string {
	var held []string
	for _, p := range permissions {
		if !slices.Contains(f.lacks, p) {
			held = append(held, p)
		}
	}

	return held
}

func (f *fakeTester) OrganizationPermissions(_ context.Context, organizationID string, permissions []string) ([]string, error) {
	*f.asked = append(*f.asked, "organizations/"+organizationID)
	if f.orgErr != nil {
		return nil, f.orgErr
	}

	return f.held(permissions), nil
}

func (f *fakeTester) BillingAccountPermissions(_ context.Context, billingAccount string, permissions []string) ([]string, error) {
	*f.asked = append(*f.asked, "billingAccounts/"+billingAccount)
	if f.billingErr != nil {
		return nil, f.billingErr
	}

	return f.held(permissions), nil
}

func (*fakeTester) Close() error {
	return nil
}

// TestPreflight: one line per bootstrap role, holds or missing with the permissions
// tested; a missing role fails the check with who grants it where; an API that refuses
// and a run without credentials say the permissions were not checked, and fail it; a
// hosting organization has nothing to check.
func TestPreflight(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// tester is the fake opened; noCredentials makes the open fail instead; hosting
		// checks a hosting organization's placement.
		tester        fakeTester
		noCredentials bool
		hosting       bool
		wantOK        bool
		wantAsked     []string
		wantOut       []string
		wantAbsent    []string
	}{
		{
			name:      "every permission held",
			wantOK:    true,
			wantAsked: []string{"organizations/123456789012", "billingAccounts/012345-6789AB-CDEF01"},
			wantOut: []string{
				"Organization Administrator (roles/resourcemanager.organizationAdmin), on organization 123456789012: holds resourcemanager.organizations.getIamPolicy, resourcemanager.organizations.setIamPolicy\n",
				"Folder Creator (roles/resourcemanager.folderCreator), on organization 123456789012: holds resourcemanager.folders.create\n",
				"Billing Account Administrator (roles/billing.admin), on billing account 012345-6789AB-CDEF01: holds billing.accounts.getIamPolicy, billing.accounts.setIamPolicy, billing.budgets.create, billing.budgets.list, billing.resourceAssociations.create\n",
				"These credentials hold every permission bedrock org bootstrap needs at the organization node and on the billing account (README.md, \"Bootstrapped by one command\").\n",
			},
			wantAbsent: []string{"missing"},
		},
		{
			name:      "one permission missing names its role and who grants it",
			tester:    fakeTester{lacks: []string{"billing.accounts.setIamPolicy"}},
			wantAsked: []string{"organizations/123456789012", "billingAccounts/012345-6789AB-CDEF01"},
			wantOut: []string{
				"Folder Creator (roles/resourcemanager.folderCreator), on organization 123456789012: holds resourcemanager.folders.create\n",
				"Billing Account Administrator (roles/billing.admin), on billing account 012345-6789AB-CDEF01: missing billing.accounts.setIamPolicy\n",
				"1 of 3 role(s) missing: the break-glass account (admin@example.com) is granted Billing Account Administrator on billing account 012345-6789AB-CDEF01, by a Billing Account Administrator of it; then run the command again with that account's credentials (gcloud auth application-default login).\n",
			},
			wantAbsent: []string{"These credentials hold every permission", "by someone who administers the organization"},
		},
		{
			name:      "roles missing at both scopes name both grants",
			tester:    fakeTester{lacks: []string{"resourcemanager.folders.create", "billing.resourceAssociations.create"}},
			wantAsked: []string{"organizations/123456789012", "billingAccounts/012345-6789AB-CDEF01"},
			wantOut: []string{
				"Organization Administrator (roles/resourcemanager.organizationAdmin), on organization 123456789012: holds resourcemanager.organizations.getIamPolicy, resourcemanager.organizations.setIamPolicy\n",
				"Folder Creator (roles/resourcemanager.folderCreator), on organization 123456789012: missing resourcemanager.folders.create\n",
				"Billing Account Administrator (roles/billing.admin), on billing account 012345-6789AB-CDEF01: missing billing.resourceAssociations.create\n",
				"2 of 3 role(s) missing: the break-glass account (admin@example.com) is granted Folder Creator at organization 123456789012, by someone who administers the organization, and Billing Account Administrator on billing account 012345-6789AB-CDEF01, by a Billing Account Administrator of it; then run the command again",
			},
		},
		{
			name:      "the organization's API refusing: not checked",
			tester:    fakeTester{orgErr: errors.New("rpc error: code = PermissionDenied desc = Cloud Resource Manager API has not been used in project 0")},
			wantAsked: []string{"organizations/123456789012"},
			wantOut: []string{
				"Permissions not checked (rpc error: code = PermissionDenied desc = Cloud Resource Manager API has not been used in project 0): the preflight asks Google, with the run's Application Default Credentials (gcloud auth application-default login, as the break-glass account), which of the permissions bedrock org bootstrap needs at the organization node and on the billing account they hold.\n",
			},
			wantAbsent: []string{"holds", "missing"},
		},
		{
			name:      "the billing account's API refusing: not checked",
			tester:    fakeTester{billingErr: errors.New("googleapi: Error 403: The caller does not have permission, forbidden")},
			wantAsked: []string{"organizations/123456789012", "billingAccounts/012345-6789AB-CDEF01"},
			wantOut:   []string{"Permissions not checked (googleapi: Error 403: The caller does not have permission, forbidden): the preflight asks Google"},
		},
		{
			name:          "no credentials: not checked, and says how to get them",
			noCredentials: true,
			wantOut: []string{
				"Permissions not checked (google: could not find default credentials): the preflight asks Google, with the run's Application Default Credentials (gcloud auth application-default login, as the break-glass account)",
			},
		},
		{
			name:    "a hosting organization has nothing to check: its root organization creates it",
			hosting: true,
			wantOut: []string{
				"Nothing to check: the imp organization is a hosting organization, which its root organization creates from its record as the creating identity; the bootstrap roles are a root organization's break-glass account's to hold.\n",
			},
			wantAbsent: []string{"holds", "missing", "Permissions not checked"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := &Placement{Prefix: "imr", Kind: RootKind, OrganizationID: "123456789012", BillingAccount: "012345-6789AB-CDEF01", BreakGlassAccount: "admin@example.com"}
			if tt.hosting {
				p.Prefix, p.Kind = "imp", ""
			}
			var asked []string
			tester := tt.tester
			tester.asked = &asked
			open := func(context.Context) (PermissionTester, error) {
				if tt.noCredentials {
					return nil, errors.Wrap(errors.New("google: could not find default credentials"), "resourcemanager.NewOrganizationsClient()")
				}

				return &tester, nil
			}
			var out bytes.Buffer
			if got := Preflight(context.Background(), p, open, &out); got != tt.wantOK {
				t.Errorf("Preflight() = %t, want %t; output:\n%s", got, tt.wantOK, out.String())
			}
			if !slices.Equal(asked, tt.wantAsked) {
				t.Errorf("asked about %q, want %q", asked, tt.wantAsked)
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out.String(), want) {
					t.Errorf("output lacks %q:\n%s", want, out.String())
				}
			}
			for _, absent := range tt.wantAbsent {
				if strings.Contains(out.String(), absent) {
					t.Errorf("output shows %q:\n%s", absent, out.String())
				}
			}
		})
	}
}
