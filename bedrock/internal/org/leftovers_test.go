package org

import (
	"context"
	"strings"
	"testing"

	"github.com/go-playground/errors/v5"
	"github.com/google/go-cmp/cmp"
)

// fakePolicies answers each project's and folder's IAM policy from a map; a project or
// folder it lacks is an error, as the API answers for one the caller cannot read. The
// organization's folder is found when folder names it, with its children.
type fakePolicies struct {
	byProject map[string][]Binding
	byFolder  map[string][]Binding
	folder    string
	children  []Folder
}

func (f *fakePolicies) ProjectPolicy(_ context.Context, project string) ([]Binding, error) {
	bindings, ok := f.byProject[project]
	if !ok {
		return nil, errors.Newf("GetIamPolicy(): %s: permission denied", project)
	}

	return bindings, nil
}

func (f *fakePolicies) OrganizationFolder(context.Context, string, string) (string, error) {
	return f.folder, nil
}

func (f *fakePolicies) ChildFolders(context.Context, string) ([]Folder, error) {
	return f.children, nil
}

func (f *fakePolicies) FolderPolicy(_ context.Context, folder string) ([]Binding, error) {
	bindings, ok := f.byFolder[folder]
	if !ok {
		return nil, errors.Newf("GetIamPolicy(): folders/%s: permission denied", folder)
	}

	return bindings, nil
}

func (*fakePolicies) Close() error {
	return nil
}

// TestLeftovers: on every project the placement records, in its order, each person on
// roles/owner and each other role of the break-glass account; on the organization's
// folder each role of the account but Folder Admin, and each grant to a principal
// outside the organization (another organization's identity, a group, another person;
// not the organization's own identities, not Google's service agents); on the folders
// under it each person's Folder Admin or Folder Editor and each other role of the
// account. Service accounts and groups on those roles, and people on other roles, are not
// listed there; an environment without a project is named, not read (a root organization
// has none to name); no organization folder is reported, not an error.
func TestLeftovers(t *testing.T) {
	t.Parallel()

	const account = "user:bedrock@imp.example"
	policies := map[string][]Binding{
		"imp-boot-gbl-core-7a8b": {
			{Role: "roles/owner", Members: []string{account}},
			{Role: "roles/iam.serviceAccountTokenCreator", Members: []string{account, "serviceAccount:imp-org-gbl-applier@imp-boot-gbl-core-7a8b.iam.gserviceaccount.com"}},
		},
		"imp-root-gbl-core-7a8b": {
			{Role: "roles/owner", Members: []string{account}},
			{Role: "roles/iam.serviceAccountTokenCreator", Members: []string{account, "serviceAccount:imp-root-gbl-applier@imp-root-gbl-core-7a8b.iam.gserviceaccount.com"}},
		},
		"imp-tst-gbl-core-1a2b": {
			{Role: "roles/owner", Members: []string{"user:seed@imp.example", "serviceAccount:imp-tst-gbl-tofu@imp-tst-gbl-core-1a2b.iam.gserviceaccount.com"}},
			{Role: "roles/editor", Members: []string{"user:someone@imp.example"}},
		},
		"imp-stg-gbl-core-3c4d": {
			{Role: "roles/owner", Members: []string{"group:team-stg@imp.example"}},
		},
		"imp-prd-gbl-core-5e6f": {
			{Role: "roles/owner", Members: []string{"user:seed@imp.example", "user:other@imp.example"}},
		},
	}
	folders := map[string][]Binding{
		"100": {
			{Role: FolderAdminRole, Members: []string{account, "serviceAccount:imp-boot-gbl-tofu@imp-boot-gbl-core-7a8b.iam.gserviceaccount.com"}},
			{Role: FolderEditorRole, Members: []string{account}},
			{Role: "roles/resourcemanager.projectCreator", Members: []string{account, "serviceAccount:imp-org-gbl-tofu@imp-boot-gbl-core-7a8b.iam.gserviceaccount.com", "serviceAccount:imr-create-gbl-tofu@imr-root-gbl-core-c3d4.iam.gserviceaccount.com"}},
			{Role: "roles/browser", Members: []string{"serviceAccount:imp-boot-gbl-plan@imp-boot-gbl-core-7a8b.iam.gserviceaccount.com", "serviceAccount:service-100000000001@gcp-sa-pam.iam.gserviceaccount.com"}},
			{Role: "roles/resourcemanager.folderViewer", Members: []string{"group:auditors@imp.example", "user:someone@imp.example"}},
		},
		"101": {
			{Role: FolderAdminRole, Members: []string{"user:seed@imp.example"}},
			{Role: FolderEditorRole, Members: []string{"user:seed@imp.example", "group:team-tst@imp.example"}},
		},
		"102": {
			{Role: "roles/resourcemanager.folderViewer", Members: []string{account}},
		},
	}
	children := []Folder{{ID: "101", Name: "terraform"}, {ID: "102", Name: "tst"}}
	tests := []struct {
		name           string
		kind           string
		projects       map[string]string
		folder         string
		wantLeftovers  []Leftover
		wantPlace      string
		wantUnrecorded string
		wantFound      bool
		wantErr        string
	}{
		{
			name:      "every project and the folders: the grants, projects first in the placement's order, then the folders",
			projects:  map[string]string{"boot": "imp-boot-gbl-core-7a8b", "tst": "imp-tst-gbl-core-1a2b", "stg": "imp-stg-gbl-core-3c4d", "prd": "imp-prd-gbl-core-5e6f"},
			folder:    "100",
			wantPlace: "project boot (imp-boot-gbl-core-7a8b)",
			wantLeftovers: []Leftover{
				{On: OnProject, Key: "boot", ID: "imp-boot-gbl-core-7a8b", Member: account, Role: "roles/owner"},
				{On: OnProject, Key: "boot", ID: "imp-boot-gbl-core-7a8b", Member: account, Role: "roles/iam.serviceAccountTokenCreator"},
				{On: OnProject, Key: "tst", ID: "imp-tst-gbl-core-1a2b", Member: "user:seed@imp.example", Role: "roles/owner"},
				{On: OnProject, Key: "prd", ID: "imp-prd-gbl-core-5e6f", Member: "user:seed@imp.example", Role: "roles/owner"},
				{On: OnProject, Key: "prd", ID: "imp-prd-gbl-core-5e6f", Member: "user:other@imp.example", Role: "roles/owner"},
				{On: OnFolder, Key: "imp", ID: "100", Member: account, Role: FolderEditorRole},
				{On: OnFolder, Key: "imp", ID: "100", Member: account, Role: "roles/resourcemanager.projectCreator"},
				{On: OnFolder, Key: "imp", ID: "100", Member: "serviceAccount:imr-create-gbl-tofu@imr-root-gbl-core-c3d4.iam.gserviceaccount.com", Role: "roles/resourcemanager.projectCreator", Outside: true},
				{On: OnFolder, Key: "imp", ID: "100", Member: "group:auditors@imp.example", Role: "roles/resourcemanager.folderViewer", Outside: true},
				{On: OnFolder, Key: "imp", ID: "100", Member: "user:someone@imp.example", Role: "roles/resourcemanager.folderViewer", Outside: true},
				{On: OnFolder, Key: "terraform", ID: "101", Member: "user:seed@imp.example", Role: FolderAdminRole},
				{On: OnFolder, Key: "terraform", ID: "101", Member: "user:seed@imp.example", Role: FolderEditorRole},
				{On: OnFolder, Key: "tst", ID: "102", Member: account, Role: "roles/resourcemanager.folderViewer"},
			},
			wantFound: true,
		},
		{
			name:           "an environment without a project is named and not read; no folder yet is reported, not an error",
			projects:       map[string]string{"stg": "imp-stg-gbl-core-3c4d"},
			wantUnrecorded: "tst,prd",
		},
		{
			name:      "a root organization: its root project and its folder, no environment to name; a hosting organization's identities on the folder are outside it",
			kind:      RootKind,
			projects:  map[string]string{"root": "imp-root-gbl-core-7a8b"},
			folder:    "100",
			wantPlace: "project root (imp-root-gbl-core-7a8b)",
			wantLeftovers: []Leftover{
				{On: OnProject, Key: "root", ID: "imp-root-gbl-core-7a8b", Member: account, Role: "roles/owner"},
				{On: OnProject, Key: "root", ID: "imp-root-gbl-core-7a8b", Member: account, Role: "roles/iam.serviceAccountTokenCreator"},
				{On: OnFolder, Key: "imp", ID: "100", Member: "serviceAccount:imp-boot-gbl-tofu@imp-boot-gbl-core-7a8b.iam.gserviceaccount.com", Role: "roles/resourcemanager.folderAdmin", Outside: true},
				{On: OnFolder, Key: "imp", ID: "100", Member: account, Role: FolderEditorRole},
				{On: OnFolder, Key: "imp", ID: "100", Member: account, Role: "roles/resourcemanager.projectCreator"},
				{On: OnFolder, Key: "imp", ID: "100", Member: "serviceAccount:imp-org-gbl-tofu@imp-boot-gbl-core-7a8b.iam.gserviceaccount.com", Role: "roles/resourcemanager.projectCreator", Outside: true},
				{On: OnFolder, Key: "imp", ID: "100", Member: "serviceAccount:imr-create-gbl-tofu@imr-root-gbl-core-c3d4.iam.gserviceaccount.com", Role: "roles/resourcemanager.projectCreator", Outside: true},
				{On: OnFolder, Key: "imp", ID: "100", Member: "serviceAccount:imp-boot-gbl-plan@imp-boot-gbl-core-7a8b.iam.gserviceaccount.com", Role: "roles/browser", Outside: true},
				{On: OnFolder, Key: "imp", ID: "100", Member: "group:auditors@imp.example", Role: "roles/resourcemanager.folderViewer", Outside: true},
				{On: OnFolder, Key: "imp", ID: "100", Member: "user:someone@imp.example", Role: "roles/resourcemanager.folderViewer", Outside: true},
				{On: OnFolder, Key: "terraform", ID: "101", Member: "user:seed@imp.example", Role: FolderAdminRole},
				{On: OnFolder, Key: "terraform", ID: "101", Member: "user:seed@imp.example", Role: FolderEditorRole},
				{On: OnFolder, Key: "tst", ID: "102", Member: account, Role: "roles/resourcemanager.folderViewer"},
			},
			wantFound: true,
		},
		{
			name:     "a policy that cannot be read fails the listing",
			projects: map[string]string{"tst": "imp-qa-gbl-core-0000"},
			wantErr:  "GetIamPolicy(): imp-qa-gbl-core-0000: permission denied",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := &Placement{Prefix: "imp", Kind: tt.kind, OrganizationID: "123456789012", BreakGlassAccount: "bedrock@imp.example", Projects: tt.projects}
			leftovers, unrecorded, found, err := Leftovers(context.Background(), p, &fakePolicies{byProject: policies, byFolder: folders, folder: tt.folder, children: children})
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Leftovers() error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("Leftovers() error = %v", err)
			}
			if diff := cmp.Diff(tt.wantLeftovers, leftovers); diff != "" {
				t.Errorf("leftovers mismatch (-want +got):\n%s", diff)
			}
			if got := strings.Join(unrecorded, ","); got != tt.wantUnrecorded {
				t.Errorf("unrecorded = %q, want %q", got, tt.wantUnrecorded)
			}
			if found != tt.wantFound {
				t.Errorf("folder found = %v, want %v", found, tt.wantFound)
			}
			if len(leftovers) > 0 {
				if got, want := leftovers[0].Place(), tt.wantPlace; got != want {
					t.Errorf("Place() = %q, want %q", got, want)
				}
			}
		})
	}
}

// TestLeftoversPassOverKeptProjectRoles: the roles the break-glass account keeps on a
// root organization's root project (BreakGlassProjectRoles) are not leftovers, and any
// other role it holds there still is.
func TestLeftoversPassOverKeptProjectRoles(t *testing.T) {
	t.Parallel()

	p := rootPlacement(t)
	account := "user:" + p.BreakGlassAccount
	project := p.Projects[RootProjectKey]
	policies := &fakePolicies{byProject: map[string][]Binding{project: {
		{Role: BigQueryAdminRole, Members: []string{account}},
		{Role: ServiceUsageConsumerRole, Members: []string{account}},
		{Role: "roles/dns.admin", Members: []string{account}},
	}}}
	leftovers, _, _, err := Leftovers(context.Background(), p, policies)
	if err != nil {
		t.Fatal(err)
	}
	want := []Leftover{{On: OnProject, Key: RootProjectKey, ID: project, Member: account, Role: "roles/dns.admin"}}
	if diff := cmp.Diff(want, leftovers); diff != "" {
		t.Errorf("Leftovers() (-want +got):\n%s", diff)
	}
	if got := (&Placement{}).BreakGlassProjectRoles(); got != nil {
		t.Errorf("a hosting organization's BreakGlassProjectRoles() = %v, want none", got)
	}
}
