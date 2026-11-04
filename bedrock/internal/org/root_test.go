package org

import (
	"slices"
	"strings"
	"testing"
)

// rootRenderedFile is one file of the zones fixture organization's render.
func rootRenderedFile(t *testing.T, path string) string {
	t.Helper()

	files, err := Render(rootPlacement(t))
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	for _, f := range files {
		if f.Path == path {
			return string(f.Content)
		}
	}
	t.Fatalf("%s is not rendered", path)

	return ""
}

// TestRootPlacementValidate holds a root organization's placement to its rules: the
// fields every organization has, one region, none of the hosting model's fields, and the
// domains with the labels handed onward (a label a DNS label or labels joined with dots,
// none above or below another, handed to another organization's prefix on four distinct
// name servers as Cloud DNS spells them).
func TestRootPlacementValidate(t *testing.T) {
	t.Parallel()

	fourServers := []string{"ns-cloud-f1.googledomains.com.", "ns-cloud-f2.googledomains.com.", "ns-cloud-f3.googledomains.com.", "ns-cloud-f4.googledomains.com."}
	handed := func(p *Placement, label string, to Delegation) {
		p.Domains["clientbay.example"].Labels[label] = to
	}
	tests := []struct {
		name    string
		mutate  func(p *Placement)
		wantErr string
	}{
		{name: "the fixture", mutate: func(*Placement) {}},
		{name: "no domain yet", mutate: func(p *Placement) { p.Domains = nil }},
		{name: "a domain with no label", mutate: func(p *Placement) { p.Domains = map[string]Domain{"clientbay.example": {}} }},
		{name: "a label of the organization's domain", mutate: func(p *Placement) { p.Domains = map[string]Domain{"apps.imp.example": {}} }},
		{name: "two regions", mutate: func(p *Placement) { p.Regions = append(p.Regions, Region{Name: "us-east1", Code: "ue1"}) }, wantErr: "regions has 2 entries; a root organization runs in one, where its connection, its triggers and its state bucket are"},
		{name: "no machine account", mutate: func(p *Placement) { p.GithubMachineAccount = "" }, wantErr: "githubMachineAccount is empty"},
		{name: "no organization domain", mutate: func(p *Placement) { p.OrganizationDomain = "" }, wantErr: "organizationDomain is empty"},
		{name: "no default branch", mutate: func(p *Placement) { p.GithubDefaultBranch = "" }, wantErr: "githubDefaultBranch is empty"},
		{name: "an apps domain", mutate: func(p *Placement) { p.AppsDomain = "apps.imp.example" }, wantErr: "appsDomain is set, and a root organization has no use for it: it holds DNS zones alone, no applications and no environments"},
		{name: "a Spanner configuration", mutate: func(p *Placement) { p.Spanner.Config = "nam6" }, wantErr: "spanner.config is set, and a root organization has no use for it"},
		{name: "a release app's slug", mutate: func(p *Placement) { p.GithubReleaseAppSlug = "imp-release" }, wantErr: "githubReleaseAppSlug is set, and a root organization has no use for it"},
		{name: "the release app and its key's version", mutate: func(p *Placement) { p.GithubReleaseAppID, p.GithubReleaseKeyVersion = "5080645", "3" }},
		{name: "the root of another organization", mutate: func(p *Placement) { p.Root = &RootPlace{Organization: "imq", Project: "imq-boot-gbl-core-0a0b"} }, wantErr: "root is set, and a root organization holds the GitHub Apps' keys itself"},
		{name: "a key operator that is not a member", mutate: func(p *Placement) { p.KeyOperators = []string{"ops@imp.example"} }, wantErr: `keyOperators entry "ops@imp.example" is not an IAM member (user:, group:, serviceAccount:)`},
		{name: "a hosting organization twice", mutate: func(p *Placement) {
			p.HostingOrganizations = append(p.HostingOrganizations, HostingOrganization{Prefix: "imp", BootProject: "imp-boot-gbl-core-a1b2"})
		}, wantErr: `hostingOrganizations names "imp" twice`},
		{name: "a hosting organization with this prefix", mutate: func(p *Placement) {
			p.HostingOrganizations = []HostingOrganization{{Prefix: "imr", BootProject: "imr-boot-gbl-core-c3d4"}}
		}, wantErr: `hostingOrganizations names "imr", this organization's own prefix`},
		{name: "a hosting organization without a project", mutate: func(p *Placement) {
			p.HostingOrganizations = []HostingOrganization{{Prefix: "rgb", BootProject: "RGB"}}
		}, wantErr: `hostingOrganizations entry "rgb": bootProject "RGB" is not a project id`},
		{name: "an infrastructure team", mutate: func(p *Placement) { p.GithubInfrastructureTeam = "platform" }, wantErr: "githubInfrastructureTeam is set, and a root organization has no use for it"},
		{name: "a larger runner the organization already has", mutate: func(p *Placement) { p.CiLargeRunnerSize, p.CiLargeRunnerMaximum = "", 0 }},
		{name: "no larger runner", mutate: func(p *Placement) { p.CiLargeRunner, p.CiLargeRunnerSize, p.CiLargeRunnerMaximum = "", "", 0 }},
		{name: "a created runner with GitHub's default cap", mutate: func(p *Placement) { p.CiLargeRunnerMaximum = 0 }},
		{name: "an arm size", mutate: func(p *Placement) { p.CiLargeRunnerSize = "arm-16-core" }},
		{name: "a size without the runner's name", mutate: func(p *Placement) { p.CiLargeRunner = "" }, wantErr: "ciLargeRunnerSize 8-core names the runner 1-dns creates, and ciLargeRunner, its name, is empty"},
		{name: "a size that is not GitHub's", mutate: func(p *Placement) { p.CiLargeRunnerSize = "large" }, wantErr: `ciLargeRunnerSize "large" is not a GitHub machine size`},
		{name: "a size in cores without the word", mutate: func(p *Placement) { p.CiLargeRunnerSize = "8" }, wantErr: `ciLargeRunnerSize "8" is not a GitHub machine size`},
		{name: "a name a hosted runner cannot carry", mutate: func(p *Placement) { p.CiLargeRunner = "ci large" }, wantErr: `ciLargeRunner "ci large" cannot name a GitHub-hosted runner`},
		{name: "a cap without a size", mutate: func(p *Placement) { p.CiLargeRunnerSize = "" }, wantErr: "ciLargeRunnerMaximum 10 caps the runner 1-dns creates, and ciLargeRunnerSize is empty"},
		{name: "a negative cap", mutate: func(p *Placement) { p.CiLargeRunnerMaximum = -1 }, wantErr: "ciLargeRunnerMaximum -1 is negative"},
		{name: "a founded hosting organization", mutate: func(p *Placement) {
			p.HostingOrganizations = []HostingOrganization{{Prefix: "rgb", BootProject: "rgb-boot-gbl-core-e5f6", Folder: "987654321098765"}}
		}},
		{name: "a hosting organization with a folder that is not an id", mutate: func(p *Placement) {
			p.HostingOrganizations = []HostingOrganization{{Prefix: "rgb", BootProject: "rgb-boot-gbl-core-e5f6", Folder: "folders/1"}}
		}, wantErr: `hostingOrganizations entry "rgb": folder "folders/1" is not a folder's numeric id`},
		{name: "an application", mutate: func(p *Placement) { p.Applications = []Application{{Code: "quill", Repository: "quill"}} }, wantErr: "applications is set, and a root organization has no use for it"},
		{name: "team groups", mutate: func(p *Placement) { p.TeamGroups = map[string]string{"tst": "team-tst@imp.example"} }, wantErr: "teamGroups is set, and a root organization has no use for it"},
		{name: "vanity domains", mutate: func(p *Placement) { p.VanityDomains = []string{"harborlights.example"} }, wantErr: "vanityDomains is set, and a root organization has no use for it"},
		{name: "entitlement durations", mutate: func(p *Placement) { p.EntitlementDurations = map[string]string{"secretOperator": "30m"} }, wantErr: "entitlementDurations is set, and a root organization has no use for it"},
		{name: "a teardown", mutate: func(p *Placement) { p.Teardown = true }, wantErr: "teardown is set, and a root organization has no use for it"},
		{name: "recorded hostnames", mutate: func(p *Placement) {
			p.Hostnames = map[string]map[string]map[string]string{"harbor": {"tst": {"harbor-tst.apps.imp.example": "projects/p/global/backendServices/b"}}}
		}, wantErr: "hostnames is set, and a root organization has no use for it"},
		{name: "a project of the hosting model", mutate: func(p *Placement) { p.Projects["tst"] = "imr-tst-gbl-core-c3d4" }, wantErr: `projects names "tst", which is not one of boot`},
		{name: "a project number of the hosting model", mutate: func(p *Placement) { p.ProjectNumbers["net"] = "100000000012" }, wantErr: `projectNumbers names "net", which is not one of boot`},
		{name: "the organization's domain", mutate: func(p *Placement) { p.Domains["imp.example"] = Domain{} }, wantErr: `domains names "imp.example", which is the organization's domain, whose mail is on it: a root organization holds a label of it (apps.imp.example) or another domain, and the identity domain stays at its registrar's DNS`},
		{name: "a domain in capitals", mutate: func(p *Placement) { p.Domains["ClientBay.example"] = Domain{} }, wantErr: `domains names "ClientBay.example", which is not a bare lowercase domain such as example.com`},
		{name: "a domain with a trailing dot", mutate: func(p *Placement) { p.Domains["clientbay.example."] = Domain{} }, wantErr: `domains names "clientbay.example.", which is not a bare lowercase domain`},
		{name: "a domain under another listed", mutate: func(p *Placement) { p.Domains["west.clientbay.example"] = Domain{} }, wantErr: `domains lists "clientbay.example" and "west.clientbay.example", one under the other: the zone of a domain serves everything under it, so the one above is enough`},
		{name: "a domain above another listed", mutate: func(p *Placement) {
			p.Domains["imp.example.net"] = Domain{}
			p.Domains["zones.imp.example.net"] = Domain{}
		}, wantErr: `domains lists "imp.example.net" and "zones.imp.example.net", one under the other`},
		{name: "a domain of 46 characters, the most the zone's name fits", mutate: func(p *Placement) { p.Domains[strings.Repeat("a", 38)+".example"] = Domain{} }},
		{name: "a domain of 47 characters", mutate: func(p *Placement) { p.Domains[strings.Repeat("a", 39)+".example"] = Domain{} }, wantErr: `whose zone imr-boot-gbl-dns-` + strings.Repeat("a", 39) + `-example would be 64 characters; Cloud DNS takes at most 63 in a zone's name, so a domain here is at most 46 characters`},
		{name: "a label of two labels", mutate: func(p *Placement) { handed(p, "east.quill", Delegation{Organization: "rgb", NameServers: fourServers}) }},
		{name: "a label in capitals", mutate: func(p *Placement) { handed(p, "Quill", Delegation{Organization: "rgb", NameServers: fourServers}) }, wantErr: `domains.clientbay.example.labels.Quill: label "Quill" is not a DNS label of at most 54 lowercase letters, digits and hyphens, starting with a letter and ending with a letter or digit (joined with dots for a name more than one label below the domain)`},
		{name: "a label ending with a hyphen", mutate: func(p *Placement) { handed(p, "quill-", Delegation{Organization: "rgb", NameServers: fourServers}) }, wantErr: `domains.clientbay.example.labels.quill-: label "quill-" is not a DNS label`},
		{name: "a label with an empty part", mutate: func(p *Placement) {
			handed(p, "east..quill", Delegation{Organization: "rgb", NameServers: fourServers})
		}, wantErr: `label "east..quill" is not a DNS label`},
		{name: "a label under another", mutate: func(p *Placement) {
			handed(p, "west.harbor", Delegation{Organization: "rgb", NameServers: fourServers})
		}, wantErr: `domains.clientbay.example lists the labels "harbor" and "west.harbor", one under the other: a label handed onward takes everything under it with it, so the one above is enough`},
		{name: "a label handed to no prefix", mutate: func(p *Placement) { handed(p, "quill", Delegation{Organization: "Impulse", NameServers: fourServers}) }, wantErr: `domains.clientbay.example.labels.quill names organization "Impulse", which is not a bedrock organization's prefix (2 to 4 lowercase alphanumeric characters starting with a letter): the hosting organization whose zone serves the name`},
		{name: "a label handed to this organization", mutate: func(p *Placement) { handed(p, "quill", Delegation{Organization: "imr", NameServers: fourServers}) }, wantErr: "domains.clientbay.example.labels.quill names organization imr, which is this organization: a root organization hosts no application, and a label is handed to the hosting organization whose zone serves the name"},
		{name: "a label on three name servers", mutate: func(p *Placement) { handed(p, "quill", Delegation{Organization: "rgb", NameServers: fourServers[:3]}) }, wantErr: "domains.clientbay.example.labels.quill names 3 name servers, not 4: the four name servers Cloud DNS assigned the application's zone in the hosting organization, as bedrock domain check prints them there"},
		{name: "a label on no name server", mutate: func(p *Placement) { handed(p, "quill", Delegation{Organization: "rgb"}) }, wantErr: "domains.clientbay.example.labels.quill names 0 name servers, not 4"},
		{name: "a name server without the trailing dot", mutate: func(p *Placement) {
			handed(p, "quill", Delegation{Organization: "rgb", NameServers: []string{"ns-cloud-f1.googledomains.com", fourServers[1], fourServers[2], fourServers[3]}})
		}, wantErr: `domains.clientbay.example.labels.quill names the name server "ns-cloud-f1.googledomains.com", which is not a lowercase host name with the trailing dot Cloud DNS writes (ns-cloud-a1.googledomains.com.)`},
		{name: "a name server in capitals", mutate: func(p *Placement) {
			handed(p, "quill", Delegation{Organization: "rgb", NameServers: []string{"NS-CLOUD-F1.googledomains.com.", fourServers[1], fourServers[2], fourServers[3]}})
		}, wantErr: `names the name server "NS-CLOUD-F1.googledomains.com.", which is not a lowercase host name`},
		{name: "a name server twice", mutate: func(p *Placement) {
			handed(p, "quill", Delegation{Organization: "rgb", NameServers: []string{fourServers[0], fourServers[0], fourServers[2], fourServers[3]}})
		}, wantErr: "domains.clientbay.example.labels.quill names the name server ns-cloud-f1.googledomains.com. twice"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := rootPlacement(t)
			tt.mutate(p)
			err := p.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v", err)
				}

				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate() error = %v, wantErr %q", err, tt.wantErr)
			}
		})
	}
}

// TestRootBootstrapRoles: a root organization's seed needs five of the seven bootstrap
// roles (no organization policy and no tag, which 1-org alone makes), each with what a
// root organization needs it for, and the two READMEs list those five and no other; a
// hosting organization's are the seven.
func TestRootBootstrapRoles(t *testing.T) {
	t.Parallel()

	if got := testPlacement(t).BootstrapRoles(); len(got) != len(BootstrapRoles) {
		t.Errorf("a hosting organization's BootstrapRoles() has %d roles, want every one of the %d", len(got), len(BootstrapRoles))
	}
	roles := rootPlacement(t).BootstrapRoles()
	var titles []string
	for _, r := range roles {
		titles = append(titles, r.Title)
		if r.Needs != r.RootNeeds {
			t.Errorf("%s: Needs = %q, want the root organization's %q", r.Title, r.Needs, r.RootNeeds)
		}
		if strings.Contains(r.Needs, "1-org") {
			t.Errorf("%s: Needs %q names 1-org, which a root organization has not", r.Title, r.Needs)
		}
	}
	want := []string{"Folder Creator", "Billing Account User"}
	if !slices.Equal(titles, want) {
		t.Errorf("a root organization's BootstrapRoles() = %v, want %v", titles, want)
	}
	for _, path := range []string{"0-bootstrap/README.md", "README.md"} {
		got := rootRenderedFile(t, path)
		for _, r := range roles {
			if !strings.Contains(got, r.Title+" (`"+r.Role+"`)") {
				t.Errorf("%s lacks %s", path, r.Title)
			}
		}
		for _, absent := range []string{"Organization Administrator", "Organization Policy Administrator", "Organization Role Administrator", "Tag Administrator"} {
			if strings.Contains(got, absent) {
				t.Errorf("%s names %s, which a root organization's seed does not need", path, absent)
			}
		}
	}
	table := rootRenderedFile(t, "0-bootstrap/README.md")
	for _, r := range roles {
		if !strings.Contains(table, " | "+r.Needs+" |") {
			t.Errorf("0-bootstrap/README.md's role table lacks %q for %s", r.Needs, r.Title)
		}
	}
}

// TestRootRender reads the zones fixture's render: the bootstrap and 1-dns alone, 1-dns
// with the domains and their labels as the placement has them, the zones kept and the
// delegations an hour's time to live, the bootstrap granting the dns identity the zones'
// administration in the boot project and nothing at the organization node, no hosting
// variable, layer or role anywhere in it; and nothing of the root kind in the hosting
// fixture's render.
func TestRootRender(t *testing.T) {
	t.Parallel()

	files, err := Render(rootPlacement(t))
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	// explaining are 1-dns's files, which name the hosting organizations' layers on
	// purpose: where a handed name is served, and what rules a hosting repository.
	explaining := []string{"1-dns/README.md", "1-dns/dns.tf", "1-dns/github.tf"}
	var paths []string
	for _, f := range files {
		paths = append(paths, f.Path)
		if layer, _, _ := strings.Cut(f.Path, "/"); slices.Contains(HostingLayers[1:], layer) {
			t.Errorf("the render carries %s, a hosting layer's file", f.Path)
		}
		if slices.Contains(explaining, f.Path) {
			continue
		}
		for _, absent := range []string{"1-org", "2-env", "2-shr", "2-spn", "org_layer_roles", "team group", "teamGroups", "Spanner"} {
			if strings.Contains(string(f.Content), absent) {
				t.Errorf("%s carries %q, which a root organization has not", f.Path, absent)
			}
		}
	}
	for _, want := range []string{"0-bootstrap/bootstrap.tf", "0-bootstrap/cloud-build.tf", "0-bootstrap/github.tf", "0-bootstrap/state-bucket.tf", "1-dns/dns.tf", "1-dns/domains.auto.tfvars", "1-dns/github.tf", "1-dns/locals.tf", "1-dns/README.md", "1-dns/terraform.tfvars", "README.md", "JOURNAL.md"} {
		if !slices.Contains(paths, want) {
			t.Errorf("the render lacks %s", want)
		}
	}
	tests := []struct {
		path   string
		want   []string
		absent []string
	}{
		{
			path: "1-dns/domains.auto.tfvars",
			want: []string{`domains = {
  "clientbay.example" = {
    labels = {
      "beacon" = { organization = "rgb", name_servers = ["ns-cloud-d1.googledomains.com.", "ns-cloud-d2.googledomains.com.", "ns-cloud-d3.googledomains.com.", "ns-cloud-d4.googledomains.com."] }
      "harbor" = { organization = "imp", name_servers = ["ns-cloud-c1.googledomains.com.", "ns-cloud-c2.googledomains.com.", "ns-cloud-c3.googledomains.com.", "ns-cloud-c4.googledomains.com."] }
    }
  }
  "impapps.example" = {
    labels = {
      "a" = { organization = "imp", name_servers = ["ns-cloud-e1.googledomains.com.", "ns-cloud-e2.googledomains.com.", "ns-cloud-e3.googledomains.com.", "ns-cloud-e4.googledomains.com."] }
    }
  }
  "zones.imp.example" = {
    labels = {}
  }
}
`},
		},
		{
			path: "1-dns/dns.tf",
			want: []string{
				"resource \"google_dns_managed_zone\" \"domain\" {\n  for_each = var.domains\n",
				"  name        = \"${local.name_prefix}-dns-${replace(each.key, \".\", \"-\")}\"\n  dns_name    = \"${each.key}.\"\n",
				"  dnssec_config {\n    state = \"on\"\n  }\n",
				"  lifecycle {\n    prevent_destroy = true\n  }\n",
				"resource \"google_dns_record_set\" \"delegation\" {\n  for_each = local.delegations\n",
				"  type         = \"NS\"\n  ttl          = 3600\n  rrdatas      = each.value.name_servers\n",
			},
			absent: []string{"google_dns_record_set\" \"apex", "certificate_manager", "google_dns_record_set\" \"wildcard"},
		},
		{
			path:   "1-dns/variables.tf",
			want:   []string{"length(to.name_servers) == 4", "endswith(ns, \".\")"},
			absent: []string{`variable "destroy"`},
		},
		{
			path: "1-dns/locals.tf",
			want: []string{"name_prefix = \"${var.prefix}-boot-gbl\"", "terraform_source_path = \"1-dns\"", "environment           = \"boot\"", "\"${label}.${domain}\""},
		},
		{
			path: "0-bootstrap/bootstrap.tf",
			want: []string{
				"resource \"google_project_iam_member\" \"org_tofu_dns\" {\n  project = module.boot_project.project_id\n  role    = \"roles/dns.admin\"\n",
				"resource \"google_project_iam_member\" \"dns_plan\" {\n  project = module.boot_project.project_id\n  role    = \"roles/dns.reader\"\n",
				"    dns  = { layer = \"1-dns\", reads = [\"1-dns\"] }\n",
				"    \"dns.googleapis.com\",",
				"project and holds no role at the organization node.\n",
				"their reads on the\n# organization's folder are this layer's to grant, like every binding on the\n# folder",
			},
			absent: []string{"privilegedaccessmanager", "org_tofu_project_updater", "google_organization_iam_member\" \"org_tofu\"", "entitlement"},
		},
		{
			path:   "0-bootstrap/state-bucket.tf",
			want:   []string{"dns  = { member = google_service_account.org_tofu.member, prefix = \"1-dns\" }"},
			absent: []string{"org_tofu_state_upstream", "2-env"},
		},
		{
			path: "0-bootstrap/github.tf",
			want: []string{
				"github_infrastructure_key", "github_deployer_key", "github_release_key", `boot_plan["dns"]`,
				"  labels          = local.labels\n  version_aliases = { current = \"1\" }\n",
				"  labels          = local.labels\n  version_aliases = { current = \"2\" }\n",
				"resource \"google_project_iam_custom_role\" \"secret_policy_admin\" {\n  project     = module.boot_project.project_id\n  role_id     = \"secretPolicyAdmin\"\n",
				"    imp = \"serviceAccount:imp-boot-gbl-tofu@imp-boot-gbl-core-a1b2.iam.gserviceaccount.com\"\n",
				"  key_operators = toset([\"group:key-operators@imp.example\"])\n",
				"resource \"google_secret_manager_secret_iam_member\" \"hosting_boot_tofu_keys\" {\n  for_each = local.hosting_key_grants\n\n  project   = module.boot_project.project_id\n  secret_id = local.key_secrets[each.value.key]\n  role      = google_project_iam_custom_role.secret_policy_admin.id\n  member    = local.hosting_boot_identities[each.value.organization]\n}\n",
				"resource \"google_secret_manager_secret_iam_member\" \"key_operators\" {\n  for_each = local.key_operator_grants\n",
			},
			absent: []string{"keys_policy_admin_role", "secret_container_admin"},
		},
		{
			path:   "0-bootstrap/cloud-build.tf",
			want:   []string{"--substitutions=_LAYER=1-dns", "a root organization has no environment layer", "(applied/<layer>), holding the commit"},
			absent: []string{"applied/2-env"},
		},
		{
			path: "README.md",
			want: []string{
				"| 0 | [`0-bootstrap/`](0-bootstrap/) |",
				"| 1 | [`1-dns/`](1-dns/) |",
				"## What this organization is",
				"(0-bootstrap, then 1-dns), one at a time",
				"The two layers share no output: `1-dns` reads nothing from `0-bootstrap`'s\nstate",
				"| a domain's zone | `imr-boot-gbl-dns-mydomain-com` |",
			},
			absent: []string{"| 2 |", "2-net", "secondary region", "Spanner"},
		},
		{
			path: "0-bootstrap/README.md",
			want: []string{
				"creates the dns layer identity, grants it the zones' administration in the\nboot project",
				"## The GitHub Apps' keys\n",
				"## Rotating a GitHub App's key\n",
				"- The key operators (`keyOperators`, IAM members: `user:`, `group:`) hold\n  Secret Version Adder and Secret Accessor on the three secrets",
				"has its boot identity granted `secretPolicyAdmin` on the three\n  secrets",
				"3. In the same terminal: `bedrock org secrets check <secret> <version>`, which\n   reads the version, signs as the app and asks GitHub for the app itself.",
				"gcloud builds triggers run imr-boot-uc1-layers-apply --region=us-central1 --project=<boot project> --branch=master --substitutions=_LAYER=1-dns",
				"a root organization has no other layer",
				"## The infrastructure GitHub App",
				"## Destroying the organization",
				"so a root organization is destroyed once no domain is\ndelegated to it",
			},
			absent: []string{"2-env", "1-org"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()

			got := rootRenderedFile(t, tt.path)
			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Errorf("%s lacks %q", tt.path, w)
				}
			}
			for _, a := range tt.absent {
				if strings.Contains(got, a) {
					t.Errorf("%s carries %q", tt.path, a)
				}
			}
		})
	}
	hosting, err := Render(testPlacement(t))
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	for _, f := range hosting {
		for _, absent := range []string{"1-dns", "dns layer", "handed onward"} {
			if strings.Contains(string(f.Content), absent) {
				t.Errorf("the hosting organization's %s carries %q, which is the root kind's", f.Path, absent)
			}
		}
	}
}
