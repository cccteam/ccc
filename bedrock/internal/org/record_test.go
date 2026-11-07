package org

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// hostingRecord is a hosting organization as a person records it for creation: what is
// the organization's own to decide, and nothing the root organization supplies.
func hostingRecord() *Placement {
	return &Placement{
		Prefix: "rgb", AppsDomain: "apps.rgb.example", Spanner: Spanner{Config: "nam10"}, MonthlyBudget: 500,
		Regions:        []Region{{Name: "us-central1", Code: "uc1"}, {Name: "us-west3", Code: "uw3"}},
		TeamGroups:     map[string]string{"tst": "team-tst@imp.example", "stg": "team-stg@imp.example", "prd": "team-prd@imp.example"},
		ContactDomains: []string{"@imp.example"},
		Labels:         map[string]string{"bedrock-lab": "true"},
	}
}

// TestFill: the root organization fills a record with what it supplies, chooses the boot
// project's suffix and derives the state bucket, keeps what the person wrote, and refuses
// a record that is the root organization's own prefix, a root kind, another primary
// region than the root organization's regional token, a boot project of another name,
// or one short of what is the organization's own to decide.
func TestFill(t *testing.T) {
	t.Parallel()

	root := rootPlacement(t)
	t.Run("filled", func(t *testing.T) {
		t.Parallel()

		h := hostingRecord()
		if err := Fill(root, h); err != nil {
			t.Fatalf("Fill() error = %v", err)
		}
		if !strings.HasPrefix(h.Projects["boot"], "rgb-boot-gbl-core-") || len(h.Projects["boot"]) != len("rgb-boot-gbl-core-")+4 {
			t.Errorf("projects.boot = %q, want rgb-boot-gbl-core-<four hex>", h.Projects["boot"])
		}
		if want := "rgb-boot-gbl-state-" + strings.TrimPrefix(h.Projects["boot"], "rgb-boot-gbl-core-"); h.StateBucket != want {
			t.Errorf("stateBucket = %q, want %q", h.StateBucket, want)
		}
		wants := map[string]string{
			"organizationDomain": h.OrganizationDomain, "organizationId": h.OrganizationID, "billingAccount": h.BillingAccount,
			"githubOrganization": h.GithubOrganization, "githubBotAccount": h.GithubBotAccount, "githubDefaultBranch": h.GithubDefaultBranch,
			"githubInfrastructureAppId": h.GithubInfrastructureAppID, "githubDeployerAppId": h.GithubDeployerAppID, "githubReleaseAppId": h.GithubReleaseAppID,
			"cloudBuildInstallationId": h.CloudBuildInstallationID, "cloudBuildTokenSecretVersion": h.CloudBuildTokenSecretVersion,
			"breakGlassAccount": h.BreakGlassAccount, "bedrockVersion": h.BedrockVersion, "bedrockSha256": h.BedrockSHA256,
		}
		roots := map[string]string{
			"organizationDomain": root.OrganizationDomain, "organizationId": root.OrganizationID, "billingAccount": root.BillingAccount,
			"githubOrganization": root.GithubOrganization, "githubBotAccount": root.GithubBotAccount, "githubDefaultBranch": root.GithubDefaultBranch,
			"githubInfrastructureAppId": root.GithubInfrastructureAppID, "githubDeployerAppId": root.GithubDeployerAppID, "githubReleaseAppId": root.GithubReleaseAppID,
			"cloudBuildInstallationId": root.CloudBuildInstallationID, "cloudBuildTokenSecretVersion": root.CloudBuildTokenSecretVersion,
			"breakGlassAccount": root.BreakGlassAccount, "bedrockVersion": root.BedrockVersion, "bedrockSha256": root.BedrockSHA256,
		}
		for name, got := range wants {
			if got != roots[name] || got == "" {
				t.Errorf("%s = %q, want the root organization's %q", name, got, roots[name])
			}
		}
		if h.Root == nil || h.Root.Organization != "imr" || h.Root.Project != "imr-root-gbl-core-c3d4" {
			t.Errorf("root = %+v", h.Root)
		}
		if h.SourceRepo != "rgb-infrastructure" || h.GithubReleaseAppSlug != "imp-example-release" {
			t.Errorf("sourceRepo = %q, githubReleaseAppSlug = %q", h.SourceRepo, h.GithubReleaseAppSlug)
		}
		if h.AppsDomain != "apps.rgb.example" || h.TeamGroups["prd"] != "team-prd@imp.example" || h.Labels["bedrock-lab"] != "true" {
			t.Errorf("the person's values changed: %+v", h)
		}
		// Filled twice, the suffix stays.
		boot := h.Projects["boot"]
		if err := Fill(root, h); err != nil || h.Projects["boot"] != boot {
			t.Errorf("Fill() again: error %v, projects.boot %q (was %q)", err, h.Projects["boot"], boot)
		}
	})

	tests := []struct {
		name    string
		mutate  func(h *Placement)
		wantErr string
	}{
		{name: "the root organization's own prefix", mutate: func(h *Placement) { h.Prefix = "imr" }, wantErr: `prefix "imr" is the root organization's own`},
		{name: "a root kind", mutate: func(h *Placement) { h.Kind = RootKind }, wantErr: "kind is root, and a record is a hosting organization's placement"},
		{name: "a boot project of another name", mutate: func(h *Placement) { h.Projects = map[string]string{"boot": "other-boot-gbl-core-a1b2"} }, wantErr: `projects.boot "other-boot-gbl-core-a1b2" is not rgb-boot-gbl-core-<suffix>`},
		{name: "no apps domain", mutate: func(h *Placement) { h.AppsDomain = "" }, wantErr: "organizations/rgb/placement.json needs what is the organization's own to decide"},
		{name: "one region", mutate: func(h *Placement) { h.Regions = h.Regions[:1] }, wantErr: "needs what is the organization's own to decide"},
		{name: "no team group", mutate: func(h *Placement) { h.TeamGroups = nil }, wantErr: "needs what is the organization's own to decide"},
		{name: "a billing account of its own kept", mutate: func(h *Placement) { h.BillingAccount = "ABCDEF-012345-6789AB" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := hostingRecord()
			tt.mutate(h)
			err := Fill(root, h)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Fill() error = %v", err)
				}
				if h.BillingAccount != "ABCDEF-012345-6789AB" {
					t.Errorf("billingAccount = %q, want the record's own kept", h.BillingAccount)
				}

				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Fill() error = %v, want %q", err, tt.wantErr)
			}
		})
	}

	t.Run("another primary region than the regional token's", func(t *testing.T) {
		t.Parallel()

		regional := *root
		regional.CloudBuildTokenSecretVersion = "projects/imr-root-gbl-core-c3d4/locations/us-central1/secrets/imr-root-uc1-github-token/versions/1"
		h := hostingRecord()
		h.Regions = []Region{{Name: "us-west3", Code: "uw3"}, {Name: "us-central1", Code: "uc1"}}
		err := Fill(&regional, h)
		if want := "regions names us-west3 first, and the Cloud Build token the root organization recorded is a regional secret in us-central1"; err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Fill() error = %v, want %q", err, want)
		}
	})

	t.Run("a hosting organization creates nothing", func(t *testing.T) {
		t.Parallel()

		err := Fill(testPlacement(t), hostingRecord())
		if err == nil || !strings.Contains(err.Error(), "only a root organization creates another organization") {
			t.Errorf("Fill() error = %v", err)
		}
	})
}

// TestRecordAndPending: Record lists a filled organization in the root organization's
// placement once, keeps a recorded folder, and Pending names the ones not created yet.
func TestRecordAndPending(t *testing.T) {
	t.Parallel()

	root := rootPlacement(t)
	if got := root.Pending(); len(got) != 1 || got[0].Prefix != "rgb" {
		t.Fatalf("Pending() = %v, want rgb alone (imp has its folder)", got)
	}
	h := hostingRecord()
	if err := Fill(root, h); err != nil {
		t.Fatal(err)
	}
	if changed := root.Record(h); !changed {
		t.Error("Record() changed nothing, want the boot project updated")
	}
	if got := root.HostingOrganization("rgb"); got == nil || got.BootProject != h.Projects["boot"] || got.Created() {
		t.Errorf("rgb = %+v", got)
	}
	if changed := root.Record(h); changed {
		t.Error("Record() twice changed the placement")
	}
	imp := root.HostingOrganization("imp")
	if imp == nil || imp.Folder != "123456789012345" || !imp.Created() {
		t.Errorf("imp = %+v", imp)
	}
	if got := root.CreatedHostingOrganizations(); len(got) != 1 || got[0].Prefix != "imp" {
		t.Errorf("CreatedHostingOrganizations() = %v", got)
	}
	n := len(root.HostingOrganizations)
	h.Prefix, h.Projects["boot"] = "xyz", "xyz-boot-gbl-core-0a0b"
	if !root.Record(h) || len(root.HostingOrganizations) != n+1 || root.HostingOrganization("xyz") == nil {
		t.Errorf("Record() of a new organization: %v", root.HostingOrganizations)
	}
}

// TestReadRecord reads a record from the repository, and refuses a root kind or
// another prefix.
func TestReadRecord(t *testing.T) {
	t.Parallel()

	root := rootPlacement(t)
	write := func(t *testing.T, prefix string, h *Placement) string {
		t.Helper()
		dir := t.TempDir()
		path := RecordPath(dir, prefix)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := h.Write(path); err != nil {
			t.Fatal(err)
		}

		return dir
	}
	h := hostingRecord()
	if err := Fill(root, h); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadRecord(write(t, "rgb", h), "rgb"); err != nil || got.Prefix != "rgb" || got.Root.Organization != "imr" {
		t.Errorf("ReadRecord() = %+v, %v", got, err)
	}
	if _, err := ReadRecord(write(t, "xyz", h), "xyz"); err == nil || !strings.Contains(err.Error(), `has prefix "rgb", and the record under organizations/xyz is that organization's`) {
		t.Errorf("ReadRecord() of another prefix: %v", err)
	}
	if _, err := ReadRecord(write(t, "imr", root), "imr"); err == nil || !strings.Contains(err.Error(), "describes a root organization (kind), and a record is a hosting organization's placement") {
		t.Errorf("ReadRecord() of a root kind: %v", err)
	}
	if want := filepath.Join("r", "organizations", "rgb", "placement.json"); RecordPath("r", "rgb") != want {
		t.Errorf("RecordPath() = %q, want %q", RecordPath("r", "rgb"), want)
	}
}

// TestSharedSecretsAndIdentity: a root organization's shared secrets are its three key
// secrets and its token secret, by resource name, and its creating identity lives in its
// boot project; the boot identity's roles are the ones 0-bootstrap's variables.tf lists.
func TestSharedSecretsAndIdentity(t *testing.T) {
	t.Parallel()

	root := rootPlacement(t)
	want := []string{
		"projects/imr-root-gbl-core-c3d4/secrets/imr-root-gbl-github-infrastructure-key",
		"projects/imr-root-gbl-core-c3d4/secrets/imr-root-gbl-github-deployer-key",
		"projects/imr-root-gbl-core-c3d4/secrets/imr-root-gbl-github-release-key",
		"projects/imr-root-gbl-core-c3d4/secrets/imr-root-gbl-github-token",
	}
	if got := root.SharedSecrets(); !slices.Equal(got, want) {
		t.Errorf("SharedSecrets() = %v, want %v", got, want)
	}
	root.CloudBuildTokenSecretVersion = ""
	if got := root.SharedSecrets(); len(got) != 3 {
		t.Errorf("SharedSecrets() without the token = %v", got)
	}
	if got := root.CreatingIdentity(); got != "imr-create-gbl-tofu@imr-root-gbl-core-c3d4.iam.gserviceaccount.com" {
		t.Errorf("CreatingIdentity() = %q", got)
	}
	variables := renderedFile(t, "0-bootstrap/variables.tf")
	for _, role := range BootLayerRoles {
		if !strings.Contains(variables, `"`+role+`",`) {
			t.Errorf("0-bootstrap/variables.tf lacks %s in boot_layer_roles, which BootLayerRoles names", role)
		}
	}
	if got := strings.Count(variables[strings.Index(variables, `variable "boot_layer_roles"`):strings.Index(variables, `variable "boot_plan_roles"`)], "roles/"); got != len(BootLayerRoles) {
		t.Errorf("boot_layer_roles lists %d roles, BootLayerRoles %d", got, len(BootLayerRoles))
	}
	for _, role := range BootProjectRoles {
		if !strings.Contains(variables, `"`+role+`",`) {
			t.Errorf("0-bootstrap/variables.tf lacks %s in boot_project_roles, which BootProjectRoles names", role)
		}
	}
	if got := strings.Count(variables[strings.Index(variables, `variable "boot_project_roles"`):strings.Index(variables, `variable "org_project_roles"`)], "roles/"); got != len(BootProjectRoles) {
		t.Errorf("boot_project_roles lists %d roles, BootProjectRoles %d", got, len(BootProjectRoles))
	}
	// The org layer identity's Custom Roles Administrator on the boot project is
	// 0-bootstrap's own grant, which the seed never makes.
	if got := strings.Count(variables[strings.Index(variables, `variable "org_project_roles"`):strings.Index(variables, `variable "github_default_branch"`)], "roles/"); got != 1 {
		t.Errorf("org_project_roles lists %d roles, want 1 (roles/iam.roleAdmin)", got)
	}
	p := testPlacement(t)
	if got := p.TokenSecretProject(); got != "imr-root-gbl-core-c3d4" {
		t.Errorf("TokenSecretProject() = %q", got)
	}
	if p.TokenSecretRegional() || p.TokenSecretLocation() != "" || p.TokenSecretName() != "projects/imr-root-gbl-core-c3d4/secrets/imr-root-gbl-github-token" {
		t.Errorf("token secret: regional %v, location %q, name %q", p.TokenSecretRegional(), p.TokenSecretLocation(), p.TokenSecretName())
	}
	regional := *p
	regional.CloudBuildTokenSecretVersion = "projects/imr-root-gbl-core-c3d4/locations/us-central1/secrets/imr-root-uc1-github-token/versions/1"
	if !regional.TokenSecretRegional() || regional.TokenSecretLocation() != "us-central1" || regional.TokenSecretName() != "projects/imr-root-gbl-core-c3d4/locations/us-central1/secrets/imr-root-uc1-github-token" {
		t.Errorf("regional token secret: regional %v, location %q, name %q", regional.TokenSecretRegional(), regional.TokenSecretLocation(), regional.TokenSecretName())
	}
}

// TestRenderSeeded: a creation's render carries the folders and the boot project in the
// seeded tfvars, where a hand seed's render carries REPLACEME.
func TestRenderSeeded(t *testing.T) {
	t.Parallel()

	root := rootPlacement(t)
	h := hostingRecord()
	h.Projects = map[string]string{"boot": "rgb-boot-gbl-core-e5f6"}
	if err := Fill(root, h); err != nil {
		t.Fatal(err)
	}
	files, err := RenderSeeded(h, &Seed{OrganizationFolder: "900000000001", TerraformFolder: "900000000002"})
	if err != nil {
		t.Fatalf("RenderSeeded() error = %v", err)
	}
	contents := map[string]string{}
	for _, f := range files {
		contents[f.Path] = string(f.Content)
	}
	wants := map[string][]string{
		"0-bootstrap/terraform.tfvars": {`boot_project_id        = "rgb-boot-gbl-core-e5f6"`, `organization_folder_id = "900000000001"`, `terraform_folder_id    = "900000000002"`},
		"1-org/terraform.tfvars":       {`boot_project_id    = "rgb-boot-gbl-core-e5f6"`},
		"2-env/terraform.tfvars":       {`boot_project_id = "rgb-boot-gbl-core-e5f6"`},
	}
	for path, lines := range wants {
		for _, line := range lines {
			if !strings.Contains(contents[path], line) {
				t.Errorf("%s lacks %q:\n%s", path, line, contents[path])
			}
		}
	}
	unseeded, err := Render(h)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range unseeded {
		if f.Path == "0-bootstrap/terraform.tfvars" && !strings.Contains(string(f.Content), `organization_folder_id = "REPLACEME"`) {
			t.Errorf("a render without a seed:\n%s", f.Content)
		}
	}
}
