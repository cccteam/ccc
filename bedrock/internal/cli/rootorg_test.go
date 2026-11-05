package cli

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cccteam/ccc/bedrock/internal/deploy"
	"github.com/cccteam/ccc/bedrock/internal/domain"
	"github.com/cccteam/ccc/bedrock/internal/github"
	"github.com/cccteam/ccc/bedrock/internal/github/githubtest"
	"github.com/cccteam/ccc/bedrock/internal/org"
	rootorg "github.com/cccteam/ccc/bedrock/internal/root"
	"github.com/cccteam/ccc/bedrock/internal/root/roottest"
)

// rootFixture is the zones fixture organization: imr, in the boot project
// imr-root-gbl-core-c3d4, holding zones for clientbay.example (harbor handed to imp,
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

// TestRootOrgCommands: org new refuses a root organization's placement and names the
// bootstrap; render writes the root files and nothing of a layer; check is clean after a
// render.
func TestRootOrgCommands(t *testing.T) {
	t.Parallel()

	t.Run("new refuses a root organization and names the bootstrap", func(t *testing.T) {
		t.Parallel()

		dir := rootRepo(t)
		_, err := execute(orgDeps(""), "", "org", "new", dir)
		want := "is a root organization's placement (kind), and a root organization is made by bedrock org bootstrap, which writes its record and renders its files; org new renders a hosting organization's foundation"
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("org new error = %v, want %q", err, want)
		}
	})

	tests := []struct {
		name     string
		steps    func(t *testing.T, dir string) (code int, out string)
		wantCode int
		wantOut  []string
		absent   []string
	}{
		{
			name: "render writes the README, the journal and the ignore rules, and no layer",
			steps: func(t *testing.T, dir string) (int, string) {
				t.Helper()
				code, out := runOrg(t, "org", "render", "--dir", dir)
				for _, want := range []string{"README.md", "JOURNAL.md", ".gitignore"} {
					if _, err := os.Stat(filepath.Join(dir, want)); err != nil {
						t.Errorf("org render did not write %s: %v", want, err)
					}
				}
				for _, layer := range org.HostingLayers {
					if _, err := os.Stat(filepath.Join(dir, layer)); !os.IsNotExist(err) {
						t.Errorf("org render wrote %s, a layer: %v", layer, err)
					}
				}

				return code, out
			},
			wantOut: []string{"1 owned file(s) written"},
			absent:  []string{"0-bootstrap", "1-dns"},
		},
		{
			name: "check is clean after a render",
			steps: func(t *testing.T, dir string) (int, string) {
				t.Helper()
				if code, out := runOrg(t, "org", "render", "--dir", dir); code != 0 {
					t.Fatalf("org render: %d %s", code, out)
				}

				return runOrg(t, "org", "check", "--dir", dir)
			},
			wantOut: []string{"1 owned file(s) match the placement"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := rootRepo(t)
			code, out := tt.steps(t, dir)
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

// TestRootHostingOnly: the org commands that act on applications or environments refuse
// a root organization with its kind named, before anything is read or changed.
func TestRootHostingOnly(t *testing.T) {
	t.Parallel()

	dir := rootRepo(t)
	if code, out := runOrg(t, "org", "render", "--dir", dir); code != 0 {
		t.Fatalf("org render: %d %s", code, out)
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

			want := fmt.Sprintf("the imr organization is a root organization (kind in placement.json), which holds the GitHub Apps' keys, the zones of the domains delegated to it and the hosting organizations' records, no applications and no environments: bedrock org %s has nothing to act on here; the names under its domains are handed onward in placement.json (domains), and bedrock domain check reads them", tt.command)
			_, err := execute(orgDeps(dir), "", tt.args...)
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("%v error = %v, want %q", tt.args, err, want)
			}
		})
	}
}

// rootWorld is the zones fixture's root organization standing as its bootstrap left it,
// before the engine's first apply, in a repository rendered from the fixture, with the
// engine's stand-ins wired into the deps: what org check, plan, apply and destroy run
// against in a root organization's repository.
func rootWorld(t *testing.T) (deps, *roottest.Fake, *githubtest.Server, string) {
	t.Helper()

	dir := rootRepo(t)
	if code, out := runOrg(t, "org", "render", "--dir", dir); code != 0 {
		t.Fatalf("org render: %d %s", code, out)
	}
	p, err := org.ReadPlacement(filepath.Join(dir, "placement.json"))
	if err != nil {
		t.Fatal(err)
	}
	srv := githubtest.New(t)
	fake := roottest.Standing(t, p, srv)
	clients := &rootorg.Clients{GitHub: func(token string) *github.Client { return github.New(srv.URL, token) }, Sleep: noWait}
	fake.Clients(clients)
	d := orgDeps(dir)
	d.org.root = clients
	d.githubToken = func(context.Context) (string, error) { return "test-token", nil }
	d.deploy = &deploy.Clients{Secrets: func(context.Context) (deploy.Secrets, error) { return fake, nil }}

	return d, fake, srv, dir
}

// TestRootOrgCheckEngine: org check in a root organization's repository compares it with
// Google Cloud and GitHub through the engine, after the render check, as information: what
// the apply would make is listed, and the files still match.
func TestRootOrgCheckEngine(t *testing.T) {
	t.Parallel()

	d, _, _, dir := rootWorld(t)
	out, err := execute(d, "", "org", "check", "--dir", dir)
	if err != nil {
		t.Fatalf("org check = %v; printed:\n%s", err, out)
	}
	for _, want := range []string{
		"identities: the apply creates the identity imr-root-gbl-planner@imr-root-gbl-core-c3d4.iam.gserviceaccount.com",
		"zones: the apply creates the zone imr-root-gbl-dns-clientbay-example for clientbay.example",
		"Cloud Build: the apply creates the trigger imr-root-uc1-apply",
		"change(s) the apply would make;",
		dir + ": ",
		"owned file(s) match the placement",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("printed:\n%s\nwant it to contain %q", out, want)
		}
	}
}

// TestRootOrgPlanApply: in a root organization's repository, org plan is the plan
// trigger's run through the engine, which posts what the merge changes on the pull
// request, and org apply the apply trigger's, which makes it and leaves the marker.
func TestRootOrgPlanApply(t *testing.T) {
	// No t.Parallel(): the commands read the build's id, project and location from the
	// process environment, as the trigger's step passes them.
	d, fake, srv, dir := rootWorld(t)
	p, err := org.ReadPlacement(filepath.Join(dir, "placement.json"))
	if err != nil {
		t.Fatal(err)
	}
	// The organization stands as the bootstrap's apply leaves it; the fixture's pending
	// creation (rgb) is the create package's to test.
	editPlacement(t, dir, func(p *org.Placement) {
		p.HostingOrganizations = slices.DeleteFunc(p.HostingOrganizations, func(h org.HostingOrganization) bool { return !h.Created() })
	})
	p.HostingOrganizations = slices.DeleteFunc(p.HostingOrganizations, func(h org.HostingOrganization) bool { return !h.Created() })
	if _, err := rootorg.Run(context.Background(), d.org.root, &rootorg.Request{Placement: p, Dir: dir, Mode: rootorg.Apply, Commit: "abc1234", GitHubToken: "test-token", Bootstrap: true}, io.Discard); err != nil {
		t.Fatal(err)
	}
	// The pull request adds a domain.
	editPlacement(t, dir, func(p *org.Placement) { p.Domains["harbor.example"] = org.Domain{} })
	build := fmt.Sprintf(`{"id":"b-1","substitutions":{"_BEDROCK_STEPS":%q,"_PR_NUMBER":"7","COMMIT_SHA":"0123456789abcdef0123456789abcdef01234567"}}`, org.StepsShape(p))
	d.deploy.Builds = (&fakeBuilds{build: build}).open
	t.Setenv("BUILD_ID", "b-1")
	t.Setenv("PROJECT_ID", "imr-root-gbl-core-c3d4")
	t.Setenv("LOCATION", "us-central1")
	out, err := execute(d, "", "org", "plan", "--dir", dir)
	if err != nil {
		t.Fatalf("org plan = %v; printed:\n%s", err, out)
	}
	for _, want := range []string{
		"zones: the apply creates the zone imr-root-gbl-dns-harbor-example for harbor.example",
		"Posted the report on pull request #7 as the deployer app.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("org plan printed:\n%s\nwant it to contain %q", out, want)
		}
	}
	if comments := srv.Repos["imp-example/imr-root-infrastructure"].Comments[7]; len(comments) != 1 || !strings.Contains(comments[0].Body, "- zones: creates the zone imr-root-gbl-dns-harbor-example for harbor.example") {
		t.Errorf("the pull request's comments: %+v", comments)
	}
	out, err = execute(d, "", "org", "apply", "--dir", dir)
	if err != nil {
		t.Fatalf("org apply = %v; printed:\n%s", err, out)
	}
	if !strings.Contains(out, "zones: creates the zone imr-root-gbl-dns-harbor-example for harbor.example") || !strings.Contains(out, "Applied 1 change(s)") {
		t.Errorf("org apply printed:\n%s", out)
	}
	if _, ok := fake.ZoneMap["imr-root-gbl-core-c3d4"]["imr-root-gbl-dns-harbor-example"]; !ok {
		t.Errorf("the zone was not made: %v", fake.ZoneMap)
	}
	if marker, ok := fake.Objects["imr-root-gbl-state-c3d4/"+rootorg.MarkerObject]; !ok || !strings.Contains(string(marker), "0123456789abcdef") {
		t.Errorf("the marker: %q", marker)
	}
}

// TestRootOrgDestroy: org destroy on a root organization refuses while a hosting
// organization is recorded as created; otherwise it says what goes (the root project with
// the zones, the folder, the creating identity's grants), asks for the prefix, takes the
// grants off, deletes the project and the folder by API, and writes the journal.
func TestRootOrgDestroy(t *testing.T) {
	t.Parallel()

	t.Run("refuses while a hosting organization is recorded as created", func(t *testing.T) {
		t.Parallel()

		d, fake, _, dir := rootWorld(t)
		_, err := execute(d, "", "org", "destroy", "--dir", dir, "--yes")
		if want := "placement.json records imp as created hosting organizations (hostingOrganizations with a folder): destroy each in its own repository first (bedrock org destroy there), then take its entry out and destroy the root organization"; err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("org destroy = %v, want %q", err, want)
		}
		if !fake.Projects["imr-root-gbl-core-c3d4"] {
			t.Errorf("the project went: %v", fake.Projects)
		}
	})
	const warning = "bedrock org destroy deletes the root project of the imr root organization, and with it everything the bootstrap and the applies made:"
	tests := []struct {
		name        string
		interactive func() bool
		in          string
		args        []string
		wantErr     string
		wantOut     []string
		absent      []string
		deleted     bool
		wantJournal string
	}{
		{
			name:        "refuses without a terminal and without --yes, after saying what goes",
			interactive: never,
			wantErr:     "asks for the prefix typed back at the terminal, and this run has none: --yes stands in for it",
			wantOut: []string{
				warning,
				"imr-root-gbl-core-c3d4 (root): the runner identities and the creating identity, the state bucket with the hosting organizations' creation records, the Cloud Build connection and triggers, the GitHub Apps' keys and the Bot Account's token, and the zones with their record sets: every domain delegated to this organization then points at servers that answer nothing",
				"Also gone: the folder, and the creating identity's grants at the organization node and on the billing account. The repository stays on GitHub with its code, pull requests, settings and ruleset, and the three GitHub Apps stay installed.",
			},
			absent: []string{"Spanner", "registrations", "environment", "layers"},
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
				"Took the creating identity's grants off the organization node and billing account 012345-6789AB-CDEF01.",
				"Deleted the project imr-root-gbl-core-c3d4, with the secrets, the zones, the connection, the triggers, the identities and the state bucket: Google holds it for 30 days, and its id never returns.",
				"Deleted the folder imr (100).",
				"Deleted the imr root organization's root project and folder; a line went into JOURNAL.md. Google holds the project for 30 days (its id never returns); the GitHub repository is as it was, with its settings and ruleset, and the three GitHub Apps stay installed, their keys in nobody's hands until a bootstrap stores them again.",
				"To start over: bedrock org bootstrap <dir> again, in an empty directory, with the apps brought (--infrastructure-app and --infrastructure-key, and the others) or made anew; every domain delegated here is then delegated again, to the new zones' name servers.",
			},
			deleted:     true,
			wantJournal: "bedrock org destroy deleted the root organization's root project (imr-root-gbl-core-c3d4) and folder, with its zones: Google holds the project for 30 days, its id never returns. The GitHub repository and the apps stay.\n",
		},
		{
			name:        "destroys with --yes and no terminal",
			interactive: never,
			args:        []string{"--yes"},
			wantOut:     []string{"Deleted the project imr-root-gbl-core-c3d4", "Deleted the folder imr (100)."},
			deleted:     true,
			wantJournal: "and folder, with its zones: Google holds the project for 30 days, its id never returns. The GitHub repository and the apps stay.\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			d, fake, _, dir := rootWorld(t)
			editPlacement(t, dir, func(p *org.Placement) { p.HostingOrganizations = nil })
			d.interactive = tt.interactive
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
					t.Errorf("printed:\n%s\nwant nothing about %q", out, absent)
				}
			}
			if fake.Projects["imr-root-gbl-core-c3d4"] == tt.deleted {
				t.Errorf("the project is there: %v, want deleted %v", fake.Projects["imr-root-gbl-core-c3d4"], tt.deleted)
			}
			if tt.deleted && len(fake.Folders) != 0 {
				t.Errorf("the folder is there: %v", fake.Folders)
			}
			// The render seeds the journal; the destroy adds its line.
			journal, err := os.ReadFile(filepath.Join(dir, "JOURNAL.md"))
			if tt.wantJournal == "" {
				if strings.Contains(string(journal), "bedrock org destroy") {
					t.Errorf("a journal line was written:\n%s", journal)
				}

				return
			}
			if err != nil || !strings.Contains(string(journal), tt.wantJournal) {
				t.Errorf("the journal = %q, %v; want it to contain %q", journal, err, tt.wantJournal)
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
// given, with the root project recorded or not.
func rootCheckRepo(t *testing.T, recorded bool, domains map[string]org.Domain) string {
	t.Helper()

	dir := t.TempDir()
	p, err := org.ReadPlacement(filepath.Join(rootFixture, "placement.json"))
	if err != nil {
		t.Fatal(err)
	}
	p.Domains = domains
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
	zones := zonesByName{"imr-root-gbl-dns-clientbay-example": clientbay, "imr-root-gbl-dns-zones-imp-example": zonesImp}
	tests := []struct {
		name     string
		recorded bool
		domains  map[string]org.Domain
		world    labelWorld
		wantCode int
		wantOut  []string
		absent   []string
		wantErr  string
	}{
		{
			name:     "a delegated domain with every label handed passes",
			recorded: true,
			domains:  handed,
			world:    labelWorld{world: delegated, sets: map[string][]string{"harbor." + clientbay: harborServers, "beacon." + clientbay: beaconServers}},
			wantOut: []string{
				"clientbay.example is delegated to the zone imr-root-gbl-dns-clientbay-example in imr-root-gbl-core-c3d4: it answers the zone's name servers (" + checkServer + ").",
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
				"clientbay.example is delegated to the zone imr-root-gbl-dns-clientbay-example in imr-root-gbl-core-c3d4",
				"\n\nzones.imp.example is delegated to the zone imr-root-gbl-dns-zones-imp-example in imr-root-gbl-core-c3d4: it answers the zone's name servers (" + checkServer + ").\nNo name under zones.imp.example is handed onward yet (domains in placement.json).\n",
			},
		},
		{
			name:     "a domain whose zone 1-dns has not made stops the check",
			recorded: true,
			domains:  map[string]org.Domain{"impapps.example": {}},
			world:    labelWorld{world: delegated},
			wantErr:  "no zone imr-root-gbl-dns-impapps-example in imr-root-gbl-core-c3d4: the root organization's apply makes it from domains in placement.json; merge the pull request that lists the domain, then check again",
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

			dir := rootCheckRepo(t, tt.recorded, tt.domains)
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
