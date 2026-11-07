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
		{name: "the organization's domain, delegated whole, with the records its apex needs", mutate: func(p *Placement) {
			p.Domains = map[string]Domain{"imp.example": {Records: []Record{{Name: "@", Type: "TXT", Data: []string{"google-site-verification=abc", "v=spf1 -all"}}, {Name: "@", Type: "MX", TTL: 3600, Data: []string{"1 smtp.google.com."}}, {Name: "_dmarc", Type: "TXT", Data: []string{"v=DMARC1; p=reject"}}}}}
		}},
		{name: "a domain above the organization's domain", mutate: func(p *Placement) {
			p.OrganizationDomain = "corp.imp.example"
			p.Domains = map[string]Domain{"imp.example": {}}
		}},
		{name: "two regions", mutate: func(p *Placement) { p.Regions = append(p.Regions, Region{Name: "us-east1", Code: "ue1"}) }, wantErr: "regions has 2 entries; a root organization runs in one, where its connection, its triggers and its state bucket are"},
		{name: "no Bot Account", mutate: func(p *Placement) { p.GithubBotAccount = "" }, wantErr: "githubBotAccount is empty"},
		{name: "no organization domain", mutate: func(p *Placement) { p.OrganizationDomain = "" }, wantErr: "organizationDomain is empty"},
		{name: "no default branch", mutate: func(p *Placement) { p.GithubDefaultBranch = "" }, wantErr: "githubDefaultBranch is empty"},
		{name: "an apps domain", mutate: func(p *Placement) { p.AppsDomain = "apps.imp.example" }, wantErr: "appsDomain is set, and a root organization has no use for it: it holds no application and no environment"},
		{name: "a Spanner configuration", mutate: func(p *Placement) { p.Spanner.Config = "nam6" }, wantErr: "spanner.config is set, and a root organization has no use for it"},
		{name: "the release app, its slug and its key's version", mutate: func(p *Placement) {
			p.GithubReleaseAppID, p.GithubReleaseAppSlug, p.GithubReleaseKeyVersion = "5080645", "imp-release", "3"
		}},
		{name: "the root of another organization", mutate: func(p *Placement) { p.Root = &RootPlace{Organization: "imq", Project: "imq-boot-gbl-core-0a0b"} }, wantErr: "root is set, and a root organization holds the GitHub Apps' keys itself"},
		{name: "a key operator that is not a member", mutate: func(p *Placement) { p.KeyOperators = []string{"ops@imp.example"} }, wantErr: `keyOperators entry "ops@imp.example" is not an IAM member (user:, group:, serviceAccount:)`},
		{name: "the token's alias named with no version pinned", mutate: func(p *Placement) { p.GithubTokenVersion = "" }, wantErr: "cloudBuildTokenSecretVersion names the alias current of the token secret, and githubTokenVersion pins no version for it: the bootstrap records both; pin one (bedrock secret pin github-token <version>)"},
		{name: "a token version of latest", mutate: func(p *Placement) { p.GithubTokenVersion = "latest" }, wantErr: `githubTokenVersion "latest" is not a secret version's number (digits, never latest)`},
		{name: "a hosting organization twice", mutate: func(p *Placement) {
			p.HostingOrganizations = append(p.HostingOrganizations, HostingOrganization{Prefix: "imp", BootProject: "imp-boot-gbl-core-a1b2"})
		}, wantErr: `hostingOrganizations names "imp" twice`},
		{name: "a hosting organization with this prefix", mutate: func(p *Placement) {
			p.HostingOrganizations = []HostingOrganization{{Prefix: "imr", BootProject: "imr-root-gbl-core-c3d4"}}
		}, wantErr: `hostingOrganizations names "imr", this organization's own prefix`},
		{name: "a hosting organization without a project", mutate: func(p *Placement) {
			p.HostingOrganizations = []HostingOrganization{{Prefix: "rgb", BootProject: "RGB"}}
		}, wantErr: `hostingOrganizations entry "rgb": bootProject "RGB" is not a project id`},
		{name: "an infrastructure team", mutate: func(p *Placement) { p.GithubInfrastructureTeam = "platform" }, wantErr: "githubInfrastructureTeam is set, and a root organization has no use for it"},
		{name: "a larger runner the organization already has", mutate: func(p *Placement) { p.CiLargeRunnerSize, p.CiLargeRunnerMaximum = "", 0 }},
		{name: "no larger runner", mutate: func(p *Placement) { p.CiLargeRunner, p.CiLargeRunnerSize, p.CiLargeRunnerMaximum = "", "", 0 }},
		{name: "a created runner with GitHub's default cap", mutate: func(p *Placement) { p.CiLargeRunnerMaximum = 0 }},
		{name: "an arm size", mutate: func(p *Placement) { p.CiLargeRunnerSize = "arm-16-core" }},
		{name: "a size without the runner's name", mutate: func(p *Placement) { p.CiLargeRunner = "" }, wantErr: "ciLargeRunnerSize 8-core names the runner the root organization's apply creates, and ciLargeRunner, its name, is empty"},
		{name: "a size that is not GitHub's", mutate: func(p *Placement) { p.CiLargeRunnerSize = "large" }, wantErr: `ciLargeRunnerSize "large" is not a GitHub machine size`},
		{name: "a size in cores without the word", mutate: func(p *Placement) { p.CiLargeRunnerSize = "8" }, wantErr: `ciLargeRunnerSize "8" is not a GitHub machine size`},
		{name: "a name a hosted runner cannot carry", mutate: func(p *Placement) { p.CiLargeRunner = "ci large" }, wantErr: `ciLargeRunner "ci large" cannot name a GitHub-hosted runner`},
		{name: "a cap without a size", mutate: func(p *Placement) { p.CiLargeRunnerSize = "" }, wantErr: "ciLargeRunnerMaximum 10 caps the runner the root organization's apply creates, and ciLargeRunnerSize is empty"},
		{name: "a negative cap", mutate: func(p *Placement) { p.CiLargeRunnerMaximum = -1 }, wantErr: "ciLargeRunnerMaximum -1 is negative"},
		{name: "a created hosting organization", mutate: func(p *Placement) {
			p.HostingOrganizations = []HostingOrganization{{Prefix: "rgb", BootProject: "rgb-boot-gbl-core-e5f6", Folder: "987654321098765"}}
		}},
		{name: "a hosting organization with a folder that is not an id", mutate: func(p *Placement) {
			p.HostingOrganizations = []HostingOrganization{{Prefix: "rgb", BootProject: "rgb-boot-gbl-core-e5f6", Folder: "folders/1"}}
		}, wantErr: `hostingOrganizations entry "rgb": folder "folders/1" is not a folder's numeric id`},
		{name: "an application", mutate: func(p *Placement) { p.Applications = []Application{{Code: "quill", Repository: "quill"}} }, wantErr: "applications is set, and a root organization has no use for it"},
		{name: "team groups", mutate: func(p *Placement) { p.TeamGroups = map[string]string{"tst": "team-tst@imp.example"} }, wantErr: "teamGroups is set, and a root organization has no use for it"},
		{name: "vanity domains", mutate: func(p *Placement) { p.VanityDomains = VanityDomains{"harborlights.example": {}} }, wantErr: "vanityDomains is set, and a root organization has no use for it"},
		{name: "entitlement durations", mutate: func(p *Placement) { p.EntitlementDurations = map[string]string{"secretOperator": "30m"} }, wantErr: "entitlementDurations is set, and a root organization has no use for it"},
		{name: "a teardown", mutate: func(p *Placement) { p.Teardown = true }, wantErr: "teardown is set, and a root organization has no use for it"},
		{name: "recorded hostnames", mutate: func(p *Placement) {
			p.Hostnames = map[string]map[string]map[string]string{"harbor": {"tst": {"harbor-tst.apps.imp.example": "projects/p/global/backendServices/b"}}}
		}, wantErr: "hostnames is set, and a root organization has no use for it"},
		{name: "a project of the hosting model", mutate: func(p *Placement) { p.Projects["tst"] = "imr-tst-gbl-core-c3d4" }, wantErr: `projects names "tst", which is not one of root`},
		{name: "a project number of the hosting model", mutate: func(p *Placement) { p.ProjectNumbers["net"] = "100000000012" }, wantErr: `projectNumbers names "net", which is not one of root`},
		{name: "a record with an NS type", mutate: func(p *Placement) {
			p.Domains["imp.example"] = Domain{Records: []Record{{Name: "mail", Type: "NS", Data: []string{"ns1.example."}}}}
		}, wantErr: `domains.imp.example.records[0]: type "NS" is not one of A, AAAA, CAA, CNAME, MX, SRV, TXT (NS records are the labels handed onward, and the zone's own)`},
		{name: "a record named for a label handed onward", mutate: func(p *Placement) {
			p.Domains["clientbay.example"] = Domain{Labels: p.Domains["clientbay.example"].Labels, Records: []Record{{Name: "www.harbor", Type: "A", Data: []string{"192.0.2.1"}}}}
		}, wantErr: `domains.clientbay.example.records[0]: name "www.harbor" is handed onward (labels.harbor): the hosting organization's zone answers for it and everything under it`},
		{name: "a record with a bad name", mutate: func(p *Placement) {
			p.Domains["imp.example"] = Domain{Records: []Record{{Name: "Mail_", Type: "A", Data: []string{"192.0.2.1"}}}}
		}, wantErr: `domains.imp.example.records[0]: name "Mail_" is not "@" for the apex or a DNS label`},
		{name: "a record with no value", mutate: func(p *Placement) { p.Domains["imp.example"] = Domain{Records: []Record{{Name: "@", Type: "TXT"}}} }, wantErr: `domains.imp.example.records[0]: data is empty; a record set carries at least one value`},
		{name: "a CNAME at the apex", mutate: func(p *Placement) {
			p.Domains["imp.example"] = Domain{Records: []Record{{Name: "@", Type: "CNAME", Data: []string{"www.imp.example."}}}}
		}, wantErr: `domains.imp.example.records[0]: a CNAME at the apex is not allowed by DNS`},
		{name: "a record declared twice", mutate: func(p *Placement) {
			p.Domains["imp.example"] = Domain{Records: []Record{{Name: "@", Type: "TXT", Data: []string{"a"}}, {Name: "@", Type: "TXT", Data: []string{"b"}}}}
		}, wantErr: `domains.imp.example.records[1]: @ TXT is declared twice; several values go in one record's data`},
		{name: "a domain in capitals", mutate: func(p *Placement) { p.Domains["ClientBay.example"] = Domain{} }, wantErr: `domains names "ClientBay.example", which is not a bare lowercase domain such as example.com`},
		{name: "a domain with a trailing dot", mutate: func(p *Placement) { p.Domains["clientbay.example."] = Domain{} }, wantErr: `domains names "clientbay.example.", which is not a bare lowercase domain`},
		{name: "a domain under another listed", mutate: func(p *Placement) { p.Domains["west.clientbay.example"] = Domain{} }, wantErr: `domains lists "clientbay.example" and "west.clientbay.example", one under the other: the zone of a domain serves everything under it, so the one above is enough`},
		{name: "a domain above another listed", mutate: func(p *Placement) {
			p.Domains["imp.example.net"] = Domain{}
			p.Domains["zones.imp.example.net"] = Domain{}
		}, wantErr: `domains lists "imp.example.net" and "zones.imp.example.net", one under the other`},
		{name: "a domain of 46 characters, the most the zone's name fits", mutate: func(p *Placement) { p.Domains[strings.Repeat("a", 38)+".example"] = Domain{} }},
		{name: "a domain of 47 characters", mutate: func(p *Placement) { p.Domains[strings.Repeat("a", 39)+".example"] = Domain{} }, wantErr: `whose zone imr-root-gbl-dns-` + strings.Repeat("a", 39) + `-example would be 64 characters; Cloud DNS takes at most 63 in a zone's name, so a domain here is at most 46 characters`},
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

// TestRootBootstrapRoles: a root organization's bootstrap needs three roles of the
// break-glass account, each named in the README with its id; a hosting organization,
// which its root organization creates, needs none.
func TestRootBootstrapRoles(t *testing.T) {
	t.Parallel()

	if got := testPlacement(t).BootstrapRoles(); len(got) != 0 {
		t.Errorf("a hosting organization's BootstrapRoles() = %d roles, want none: its root organization creates it", len(got))
	}
	roles := rootPlacement(t).BootstrapRoles()
	var titles []string
	for _, r := range roles {
		titles = append(titles, r.Title)
	}
	want := []string{"Organization Administrator", "Folder Creator", "Billing Account Administrator"}
	if !slices.Equal(titles, want) {
		t.Errorf("a root organization's BootstrapRoles() = %v, want %v", titles, want)
	}
	got := rootRenderedFile(t, "README.md")
	for _, r := range roles {
		if !strings.Contains(got, r.Title+" (`"+r.Role+"`)") {
			t.Errorf("README.md lacks %s", r.Title)
		}
	}
}

// TestRootRender reads the root fixture's render: the README, the journal and the ignore
// rules alone, since a root organization has no layer, with the zone's name the README
// gives a domain.
func TestRootRender(t *testing.T) {
	t.Parallel()

	files, err := Render(rootPlacement(t))
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	var paths []string
	for _, f := range files {
		paths = append(paths, f.Path)
		if strings.Contains(f.Path, "/") {
			t.Errorf("the render carries %s, a layer's file; a root organization has no layer", f.Path)
		}
	}
	if want := []string{".gitignore", "JOURNAL.md", "README.md"}; !slices.Equal(paths, want) {
		t.Errorf("paths = %v, want %v", paths, want)
	}
	readme := rootRenderedFile(t, "README.md")
	for _, want := range []string{"| a domain's zone | `imr-root-gbl-dns-mydomain-com` |"} {
		if !strings.Contains(readme, want) {
			t.Errorf("README.md lacks %q", want)
		}
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
