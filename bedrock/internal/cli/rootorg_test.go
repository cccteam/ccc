package cli

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cccteam/ccc/bedrock/internal/deploy"
	"github.com/cccteam/ccc/bedrock/internal/domain"
	"github.com/cccteam/ccc/bedrock/internal/org"
)

// rootFixture is the zones fixture organization: imr, in the boot project
// imr-boot-gbl-core-c3d4, holding zones for clientbay.example (harbor handed to imp,
// beacon to rgb), impapps.example (a handed to imp) and zones.imp.example (nothing handed
// yet).
const rootFixture = "../org/testdata/imr"

// The name servers the zones fixture's labels are handed to, as Cloud DNS spells them.
var (
	harborServers = []string{"ns-cloud-c1.googledomains.com.", "ns-cloud-c2.googledomains.com.", "ns-cloud-c3.googledomains.com.", "ns-cloud-c4.googledomains.com."}
	beaconServers = []string{"ns-cloud-d1.googledomains.com.", "ns-cloud-d2.googledomains.com.", "ns-cloud-d3.googledomains.com.", "ns-cloud-d4.googledomains.com."}
)

// rootRepo copies the zones fixture's placement into a temporary directory, without its
// bedrock pin.
func rootRepo(t *testing.T) string {
	t.Helper()

	return fixtureRepo(t, rootFixture)
}

// TestRootOrgCommands runs the org commands over the zones fixture: new renders the two
// layers and prints a root organization's hand steps (the five roles its seed needs, no
// team group, no consent screen, the two apps' keys, the domains last), check is clean
// after it with no hosting layer rendered, and render says what the triggers lack before
// the boot project is recorded.
func TestRootOrgCommands(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		steps   func(t *testing.T, dir string) (code int, out string)
		wantOut []string
		absent  []string
	}{
		{
			name: "new renders the two layers and prints the hand steps",
			steps: func(t *testing.T, dir string) (int, string) {
				t.Helper()

				return runOrg(t, "org", "new", dir)
			},
			wantOut: []string{
				"Rendered the imr foundation", "18 owned file(s) in 0-bootstrap, 1-dns; seeded .gitignore, 0-bootstrap/terraform.tfvars, 1-dns/terraform.tfvars, JOURNAL.md.",
				"By hand, before the Cloud Build triggers can run",
				"0. On GitHub, as an organization owner: the organization, its machine account (imp-machine, an\n     owner of the organization, named in placement.json as githubMachineAccount), this\n     infrastructure repository (default branch master; 1-dns rules that branch and nothing\n     else of it), and the deployer and infrastructure apps",
				"A GitHub organization that holds a hosting organization\n     has the account and the apps already: record the same, and the release app's App ID\n     too (githubReleaseAppId), since this organization holds its key for them.",
				"In a terminal, in this repository, the three apps' keys, never by hand, once\n     0-bootstrap has made their secrets in the boot project, one per app for every\n     organization in the Google Cloud organization: bedrock secret add\n     github-infrastructure-key, bedrock org secrets check github-infrastructure-key\n     <version>, which proves the version with GitHub, and bedrock secret pin\n     github-infrastructure-key <version>, which pins the version the alias current points\n     at; the same with github-deployer-key and github-release-key. The pull request's\n     apply moves the alias, which every reader in every organization names\n     (0-bootstrap/README.md, \"Rotating a GitHub App's key\").",
				"1. In a terminal, as seed@imp.example, after gcloud auth application-default login:\n     bedrock org preflight, which must find each of these roles held:\n" +
					"       - Folder Creator (roles/resourcemanager.folderCreator), on the organization\n" +
					"       - Billing Account User (roles/billing.user), on the billing account\n" +
					"     then the seed: the organization's folder at the organization root (123456789012), which makes\n" +
					"     the account its administrator, the terraform folder under it, the boot project\n" +
					"     imr-boot-gbl-core-<suffix>, the boot identity imr-boot-gbl-tofu with its roles on the\n" +
					"     organization's folder, and the state bucket imr-boot-gbl-state-<suffix>. Nothing of the\n" +
					"     organization's sits at the organization node.",
				"2. In this repository, the seed's values: boot_project_id and terraform_folder_id in\n     0-bootstrap/terraform.tfvars, boot_project_id in 1-dns, and in placement.json the\n     bucket (stateBucket) and the boot project's id and number (projects.boot,\n     projectNumbers.boot)",
				"a connection named\n     imr-boot-uc1-github in us-central1",
				"4. In a terminal, in 0-bootstrap: the apply on local state, the connection imported\n     into it first, then the migration of its state into the bucket.",
				"5. In a terminal, as a billing administrator of 012345-6789AB-CDEF01: roles/billing.user on\n     it for the boot identity.",
				"6. In a terminal, in 1-dns: the apply, with GITHUB_TOKEN set to an organization\n     owner's token (1-dns/README.md, \"Applying\"). It leaves nothing behind: the layer\n     creates no project and no identity.",
				"From then on the two Cloud Build triggers in the boot project (imr-boot-uc1-layers-plan and imr-boot-uc1-layers-apply)\napply both layers: a pull request plans the layers it touches as their plan identities and\nposts the plans on it, the merge applies them as their apply identities, in layer order, and\nfounds what placement.json lists without a folder yet.",
				"Then the domains: each domain delegated to this organization goes into placement.json\n(domains), bedrock org render and the pull request whose apply makes its zone; by the\ncustomer, where the domain's DNS is set, the delegation to the zone's name servers, as\nbedrock domain check prints them with the place (1-dns/README.md, \"Delegating a domain to\nthis organization\"). Each application's name under a domain follows at the application's\nregistration, in two pull requests (1-dns/README.md, \"Handing a label onward\").",
				"7. In a terminal, once 0-bootstrap has applied and made the founding identity\n     imr-found-gbl-tofu: an administrator of the Google Cloud organization grants it Folder\n     Creator (roles/resourcemanager.folderCreator) at the organization node, and a billing\n     administrator Billing Account User (roles/billing.user) on 012345-6789AB-CDEF01",
				"Pinned bedrock " + headVersion + " in ",
			},
			absent: []string{"Organization Policy Administrator", "Tag Administrator", "team group", "consent screen", "2-net", "1-org", "8. "},
		},
		{
			name: "check is clean after new, the two layers rendered and no other",
			steps: func(t *testing.T, dir string) (int, string) {
				t.Helper()
				if code, out := runOrg(t, "org", "new", dir); code != 0 {
					t.Fatalf("org new: %d %s", code, out)
				}
				for _, want := range []string{"0-bootstrap/cloud-build.tf", "1-dns/dns.tf", "1-dns/domains.auto.tfvars", "1-dns/github.tf", "1-dns/terraform.tfvars"} {
					if _, err := os.Stat(filepath.Join(dir, want)); err != nil {
						t.Errorf("org new did not write %s: %v", want, err)
					}
				}
				for _, layer := range org.HostingLayers[1:] {
					if _, err := os.Stat(filepath.Join(dir, layer)); !os.IsNotExist(err) {
						t.Errorf("org new wrote %s, a hosting layer: %v", layer, err)
					}
				}

				return runOrg(t, "org", "check", "--dir", dir)
			},
			wantOut: []string{"18 owned file(s) match the placement"},
		},
		{
			name: "render says what the triggers still lack before the boot project is recorded",
			steps: func(t *testing.T, dir string) (int, string) {
				t.Helper()
				if code, out := runOrg(t, "org", "new", dir); code != 0 {
					t.Fatalf("org new: %d %s", code, out)
				}
				editPlacement(t, dir, func(p *org.Placement) {
					p.Projects, p.ProjectNumbers = nil, nil
				})

				return runOrg(t, "org", "render", "--dir", dir)
			},
			wantOut: []string{"18 owned file(s) written", "The triggers cannot run every layer yet: record projects.boot in placement.json and run bedrock org render."},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := rootRepo(t)
			code, out := tt.steps(t, dir)
			if code != 0 {
				t.Errorf("exit code = %d, want 0:\n%s", code, out)
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out, want) {
					t.Errorf("output lacks %q:\n%s", want, out)
				}
			}
			for _, absent := range tt.absent {
				if strings.Contains(out, absent) {
					t.Errorf("output carries %q:\n%s", absent, out)
				}
			}
		})
	}
}

// TestRootHostingOnly: the org commands that act on applications or environments refuse
// a root organization with its kind named, before anything is read or changed.
func TestRootHostingOnly(t *testing.T) {
	t.Parallel()

	dir := rootRepo(t)
	if code, out := runOrg(t, "org", "new", dir); code != 0 {
		t.Fatalf("org new: %d %s", code, out)
	}
	tests := []struct {
		name    string
		command string
		args    []string
	}{
		{name: "register", command: "register", args: []string{"org", "register", "quill", "--dir", dir, appCheckout(t, "github.com/"+orgOrganization+"/quill-service")}},
		{name: "place", command: "place", args: []string{"org", "place", "quill", "tst", "--dir", dir}},
		{name: "teardown of every application", command: "teardown", args: []string{"org", "teardown", "--dir", dir}},
		{name: "teardown of one application", command: "teardown", args: []string{"org", "teardown", "quill", "--dir", dir}},
		{name: "serve of every application", command: "serve", args: []string{"org", "serve", "--dir", dir}},
		{name: "serve of one application", command: "serve", args: []string{"org", "serve", "quill", "--dir", dir}},
		{name: "hostnames", command: "hostnames", args: []string{"org", "hostnames", "quill", "--dir", dir}},
		{name: "retire", command: "retire", args: []string{"org", "retire", "quill", "--dir", dir}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			want := fmt.Sprintf("the imr organization is a root organization (kind in placement.json), which holds DNS zones alone, no applications and no environments: bedrock org %s has nothing to act on here; the names under its domains are handed onward in placement.json (domains), and bedrock domain check reads them", tt.command)
			_, err := execute(orgDeps(dir), "", tt.args...)
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("%v error = %v, want %q", tt.args, err, want)
			}
		})
	}
}

// rootDestroyTofu is tofu in the zones destroy's tests: 1-dns's state holds this
// repository and its ruleset, a zone and a record set; 0-bootstrap's the boot project.
type rootDestroyTofu struct {
	ran []string
}

func (f *rootDestroyTofu) Run(_ context.Context, c deploy.Command, _ io.Writer) error {
	f.ran = append(f.ran, filepath.Base(c.Dir)+": "+c.String())

	return nil
}

func (f *rootDestroyTofu) Output(_ context.Context, c deploy.Command, _ io.Writer) ([]byte, error) {
	f.ran = append(f.ran, filepath.Base(c.Dir)+": "+c.String())
	switch {
	case c.Args[1] == "pull":
		return []byte(`{"version": 4}`), nil
	case filepath.Base(c.Dir) == "1-dns":
		return []byte("github_repository.infrastructure\ngithub_repository_ruleset.infrastructure\ngoogle_dns_managed_zone.domain[\"clientbay.example\"]\ngoogle_dns_record_set.delegation[\"harbor.clientbay.example\"]\n"), nil
	default:
		return []byte("module.boot_project.module.project-factory.google_project.main\n"), nil
	}
}

// rootDestroyRepo is the zones fixture's placement with the two layers' files the destroy
// reads: 0-bootstrap's variables declaring destroy and its backend, 1-dns's variables
// declaring none.
func rootDestroyRepo(t *testing.T) string {
	t.Helper()

	dir := rootRepo(t)
	files := map[string]string{
		"0-bootstrap/variables.tf":  "variable \"destroy\" {\n  type    = bool\n  default = false\n}\n",
		"0-bootstrap/initialize.tf": "terraform {\n  backend \"gcs\" {}\n}\n",
		"1-dns/variables.tf":        "variable \"boot_project_id\" {\n  type = string\n}\n",
		"JOURNAL.md":                "# Journal\n",
	}
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	return dir
}

// TestRootOrgDestroy: org destroy on a root organization says what goes (the boot
// project with the zones, the folders and the roles, the ruleset), asks for the prefix,
// destroys 1-dns with this repository and the zones taken out of its state and no destroy
// variable set on it, then 0-bootstrap, and writes the journal.
func TestRootOrgDestroy(t *testing.T) {
	t.Parallel()

	const warning = "bedrock org destroy deletes the boot project of the imr root organization, and with it everything the layers made:"
	tests := []struct {
		name        string
		interactive func() bool
		in          string
		args        []string
		wantErr     string
		wantOut     []string
		absent      []string
		wantRan     []string
		wantJournal string
	}{
		{
			name:        "refuses without a terminal and without --yes, after saying what goes",
			interactive: never,
			wantErr:     "asks for the prefix typed back at the terminal, and this run has none: --yes stands in for it",
			wantOut: []string{
				warning,
				"imr-boot-gbl-core-c3d4 (boot): the layer identities, the state bucket with both layers' states, the Cloud Build connection and triggers, the GitHub Apps' keys, and the zones with their record sets: every domain delegated to this organization then points at servers that answer nothing",
				"Also gone: the folders and the custom roles, and on GitHub this repository's ruleset. The repository stays, with its code and pull requests.",
			},
			absent: []string{"Spanner", "registrations", "tst"},
		},
		{
			name:        "stops when something else than the prefix is typed",
			interactive: always,
			in:          "imp\n",
			wantOut:     []string{"Type the prefix (imr) to go on; anything else stops:", "Stopped: nothing was deleted."},
		},
		{
			name:        "destroys when the prefix is typed, and writes the journal",
			interactive: always,
			in:          "imr\n",
			wantOut: []string{
				warning,
				"Took github_repository.infrastructure out of 1-dns's state: the repository stays.",
				`Took google_dns_managed_zone.domain["clientbay.example"] out of 1-dns's state: it goes with the boot project.`,
				`Took google_dns_record_set.delegation["harbor.clientbay.example"] out of 1-dns's state: it goes with the boot project.`,
				"Destroyed 1-dns.", "Destroyed 0-bootstrap.",
				"Deleted the imr root organization's boot project; a line went into JOURNAL.md. Google holds it for 30 days (its id never returns); the GitHub repository is as it was, without the ruleset 1-dns made.",
				"every domain delegated here is then delegated again, to the new zones' name servers.",
			},
			wantRan: []string{
				"1-dns: tofu init -input=false -no-color -reconfigure",
				"1-dns: tofu state list",
				"1-dns: tofu state rm github_repository.infrastructure",
				`1-dns: tofu state rm google_dns_managed_zone.domain["clientbay.example"]`,
				`1-dns: tofu state rm google_dns_record_set.delegation["harbor.clientbay.example"]`,
				"1-dns: tofu destroy -input=false -no-color -auto-approve",
				"0-bootstrap: tofu init -input=false -no-color -reconfigure",
				"0-bootstrap: tofu state pull",
				"0-bootstrap: tofu init -input=false -no-color -reconfigure",
				"0-bootstrap: tofu state list",
				"0-bootstrap: tofu apply -input=false -no-color -auto-approve -var destroy=true -target=module.boot_project.module.project-factory.google_project.main",
				"0-bootstrap: tofu destroy -input=false -no-color -auto-approve -var destroy=true",
			},
			wantJournal: "bedrock org destroy deleted the root organization's boot project (imr-boot-gbl-core-c3d4), with its zones: Google holds it for 30 days, its id never returns. The GitHub repository stays.\n",
		},
		{
			name:        "destroys with --yes and no terminal",
			interactive: never,
			args:        []string{"--yes"},
			wantOut:     []string{"Destroyed 1-dns.", "Destroyed 0-bootstrap."},
			wantJournal: "with its zones: Google holds it for 30 days, its id never returns. The GitHub repository stays.\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := rootDestroyRepo(t)
			tofu := &rootDestroyTofu{}
			d := orgDeps(dir)
			d.interactive = tt.interactive
			d.deploy = &deploy.Clients{Exec: tofu}
			d.githubToken = func(context.Context) (string, error) { return "ghs_token", nil }
			out, err := execute(d, tt.in, append([]string{"org", "destroy", "--dir", dir}, tt.args...)...)
			switch {
			case tt.wantErr == "" && err != nil:
				t.Fatalf("org destroy = %v, want no error; printed:\n%s", err, out)
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Fatalf("org destroy = %v, want an error containing %q", err, tt.wantErr)
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out, want) {
					t.Errorf("printed:\n%s\nwant it to contain %q", out, want)
				}
			}
			for _, absent := range tt.absent {
				if strings.Contains(out, absent) {
					t.Errorf("printed:\n%s\nwant it without %q", out, absent)
				}
			}
			if tt.wantRan != nil && strings.Join(tofu.ran, "\n") != strings.Join(tt.wantRan, "\n") {
				t.Errorf("tofu ran:\n%s\nwant:\n%s", strings.Join(tofu.ran, "\n"), strings.Join(tt.wantRan, "\n"))
			}
			journal, err := os.ReadFile(filepath.Join(dir, "JOURNAL.md"))
			if err != nil {
				t.Fatal(err)
			}
			if tt.wantJournal == "" && string(journal) != "# Journal\n" {
				t.Errorf("the journal changed:\n%s", journal)
			}
			if tt.wantJournal != "" && !strings.HasSuffix(string(journal), tt.wantJournal) {
				t.Errorf("the journal is:\n%s\nwant it to end with %q", journal, tt.wantJournal)
			}
		})
	}
}

// labelWorld is the world of the zones check: name servers from sets of several for the
// labels handed onward, SERVFAIL for a name in lame (servers that do not answer for it),
// and the one-server answers of world for the rest.
type labelWorld struct {
	world
	sets map[string][]string
	lame map[string]bool
}

func (w labelWorld) LookupNS(ctx context.Context, name string) ([]*net.NS, error) {
	if hosts, ok := w.sets[name]; ok {
		ns := make([]*net.NS, 0, len(hosts))
		for _, h := range hosts {
			ns = append(ns, &net.NS{Host: h})
		}

		return ns, nil
	}
	if w.lame[name] {
		return nil, &net.DNSError{Err: "server misbehaving", Name: name, IsTemporary: true}
	}

	return w.world.LookupNS(ctx, name)
}

// rootCheckRepo is a root organization repository whose placement holds the domains
// given, with the boot project recorded or not and zoneReplacement set or not.
func rootCheckRepo(t *testing.T, recorded, zoneReplacement bool, domains map[string]org.Domain) string {
	t.Helper()

	dir := t.TempDir()
	p, err := org.ReadPlacement(filepath.Join(rootFixture, "placement.json"))
	if err != nil {
		t.Fatal(err)
	}
	p.Domains = domains
	p.ZoneReplacement = zoneReplacement
	if !recorded {
		p.Projects, p.ProjectNumbers = nil, nil
	}
	if err := p.Write(filepath.Join(dir, "placement.json")); err != nil {
		t.Fatal(err)
	}

	return dir
}

// TestRootDomainCheck runs domain check over a root organization: each domain's zone in
// the boot project, the delegation as the world sees it, then each label handed onward,
// passing when every domain is delegated and every label answers its servers; exit 1
// with the registrar step and the labels waiting on an undelegated domain, or with a
// label whose servers do not answer for it; nothing to check without a domain; and a
// refusal before the boot project is recorded.
func TestRootDomainCheck(t *testing.T) {
	t.Parallel()

	const (
		clientbay = "clientbay.example"
		zonesImp  = "zones.imp.example"
	)
	handed := map[string]org.Domain{clientbay: {Labels: map[string]org.Delegation{
		"harbor": {Organization: "imp", NameServers: harborServers},
		"beacon": {Organization: "rgb", NameServers: beaconServers},
	}}}
	delegated := world{ns: map[string]string{clientbay: checkServer, zonesImp: checkServer}}
	zones := zonesByName{"imr-boot-gbl-dns-clientbay-example": clientbay, "imr-boot-gbl-dns-zones-imp-example": zonesImp}
	tests := []struct {
		name            string
		recorded        bool
		zoneReplacement bool
		domains         map[string]org.Domain
		world           labelWorld
		wantCode        int
		wantOut         []string
		absent          []string
		wantErr         string
	}{
		{
			name:     "a delegated domain with every label handed passes",
			recorded: true,
			domains:  handed,
			world:    labelWorld{world: delegated, sets: map[string][]string{"harbor." + clientbay: harborServers, "beacon." + clientbay: beaconServers}},
			wantOut: []string{
				"clientbay.example is delegated to the zone imr-boot-gbl-dns-clientbay-example in imr-boot-gbl-core-c3d4: it answers the zone's name servers (" + checkServer + ").",
				"harbor.clientbay.example is handed to imp: it answers the name servers of its zone there (" + strings.Join(harborServers, ", ") + ").",
				"beacon.clientbay.example is handed to rgb: it answers the name servers of its zone there (" + strings.Join(beaconServers, ", ") + ").",
			},
			absent: []string{"zoneReplacement", "certificate"},
		},
		{
			name:     "a label whose servers do not answer for it exits 1",
			recorded: true,
			domains:  handed,
			world:    labelWorld{world: delegated, sets: map[string][]string{"harbor." + clientbay: harborServers}, lame: map[string]bool{"beacon." + clientbay: true}},
			wantCode: 1,
			wantOut: []string{
				"harbor.clientbay.example is handed to imp: it answers the name servers of its zone there",
				"beacon.clientbay.example is handed to rgb, and the name servers it is handed to do not answer for it: the application's zone in rgb is not applied yet, or the servers recorded here (" + strings.Join(beaconServers, ", ") + ") are not that zone's; bedrock domain check in rgb's infrastructure repository prints them.",
			},
		},
		{
			name:     "an undelegated domain exits 1 with the registrar step, the labels waiting and no registration offered",
			recorded: true,
			domains:  handed,
			world:    labelWorld{},
			wantCode: 1,
			wantOut: []string{
				"clientbay.example does not answer the zone's name servers (it answers none).",
				"At the registrar where clientbay.example is registered: set its name servers to the zone's 1 below",
				"\n" + strings.TrimSuffix(checkServer, ".") + "\n",
				"The 2 name(s) handed onward under clientbay.example resolve once the domain is delegated here.",
			},
			absent: []string{"bedrock domain add", "is handed to"},
		},
		{
			name:     "two domains, each after a blank line, the one with nothing handed yet saying so",
			recorded: true,
			domains:  map[string]org.Domain{clientbay: handed[clientbay], zonesImp: {}},
			world:    labelWorld{world: delegated, sets: map[string][]string{"harbor." + clientbay: harborServers, "beacon." + clientbay: beaconServers}},
			wantOut: []string{
				"clientbay.example is delegated to the zone imr-boot-gbl-dns-clientbay-example in imr-boot-gbl-core-c3d4",
				"\n\nzones.imp.example is delegated to the zone imr-boot-gbl-dns-zones-imp-example in imr-boot-gbl-core-c3d4: it answers the zone's name servers (" + checkServer + ").\nNo name under zones.imp.example is handed onward yet (domains in placement.json).\n",
			},
		},
		{
			name:            "every domain passing with zoneReplacement set: the value can be cleared",
			recorded:        true,
			zoneReplacement: true,
			domains:         map[string]org.Domain{zonesImp: {}},
			world:           labelWorld{world: delegated},
			wantOut:         []string{"\nzoneReplacement is set in placement.json and every domain points at its zone: clear it and run bedrock org render.\n"},
		},
		{
			name:     "a domain whose zone 1-dns has not made stops the check",
			recorded: true,
			domains:  map[string]org.Domain{"impapps.example": {}},
			world:    labelWorld{world: delegated},
			wantErr:  "no zone imr-boot-gbl-dns-impapps-example in imr-boot-gbl-core-c3d4: the apply of 1-dns creates it; apply 1-dns, then check again",
		},
		{
			name:     "no domain: nothing to check yet",
			recorded: true,
			world:    labelWorld{},
			wantOut:  []string{"placement.json names no domain (domains): nothing to check until one is delegated to this organization.\n"},
		},
		{
			name:     "refused before the boot project is recorded",
			recorded: false,
			domains:  handed,
			world:    labelWorld{world: delegated},
			wantErr:  "placement.json records no boot project (projects.boot): record the seed's value there, then check again",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := rootCheckRepo(t, tt.recorded, tt.zoneReplacement, tt.domains)
			d := orgDeps(dir)
			d.lookups = &domain.Lookups{
				OpenZones: func(context.Context) (domain.ZoneReader, error) {
					return zones, nil
				},
				Resolver: tt.world,
			}
			out, err := execute(d, "", "domain", "check", "--dir", dir)
			code := 0
			if err != nil {
				var exit exitError
				if !asExit(err, &exit) {
					if tt.wantErr == "" || !strings.Contains(err.Error(), tt.wantErr) {
						t.Fatalf("Execute() error = %v, wantErr %q", err, tt.wantErr)
					}

					return
				}
				code = exit.code
			}
			if tt.wantErr != "" {
				t.Fatalf("Execute() error = %v, wantErr %q", err, tt.wantErr)
			}
			if code != tt.wantCode {
				t.Errorf("exit code = %d, want %d:\n%s", code, tt.wantCode, out)
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out, want) {
					t.Errorf("output lacks %q:\n%s", want, out)
				}
			}
			for _, absent := range tt.absent {
				if strings.Contains(out, absent) {
					t.Errorf("output carries %q:\n%s", absent, out)
				}
			}
		})
	}
}
