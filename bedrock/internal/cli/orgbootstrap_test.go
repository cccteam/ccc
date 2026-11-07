package cli

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cccteam/ccc/bedrock/internal/deploy"
	"github.com/cccteam/ccc/bedrock/internal/github"
	"github.com/cccteam/ccc/bedrock/internal/github/githubtest"
	"github.com/cccteam/ccc/bedrock/internal/org"
	rootorg "github.com/cccteam/ccc/bedrock/internal/root"
	"github.com/cccteam/ccc/bedrock/internal/root/roottest"
)

// fakeDiscovery is what the bootstrap's credentials see in tests.
type fakeDiscovery struct {
	account string
	orgs    []rootorg.Organization
	billing []rootorg.BillingAccount
}

func (f *fakeDiscovery) open(context.Context) (rootorg.Discovery, error) {
	return f, nil
}

func (f *fakeDiscovery) Account(context.Context) (string, error) {
	return f.account, nil
}

func (f *fakeDiscovery) Organizations(context.Context) ([]rootorg.Organization, error) {
	return f.orgs, nil
}

func (f *fakeDiscovery) BillingAccounts(context.Context) ([]rootorg.BillingAccount, error) {
	return f.billing, nil
}

func (*fakeDiscovery) Close() error {
	return nil
}

// allPermissions answers every permission asked about, as the break-glass account holds
// them.
type allPermissions struct{}

func (allPermissions) OrganizationPermissions(_ context.Context, _ string, permissions []string) ([]string, error) {
	return permissions, nil
}

func (allPermissions) BillingAccountPermissions(_ context.Context, _ string, permissions []string) ([]string, error) {
	return permissions, nil
}

func (allPermissions) Close() error {
	return nil
}

// noWait is the clients' waiting in tests: none.
func noWait(context.Context, time.Duration) error {
	return nil
}

// bootstrapWorld is the lab before the bootstrap: nothing on Google Cloud, GitHub's
// stand-in with the Bot Account an owner of imp-example and the Cloud Build app installed,
// one organization and one billing account in the credentials' sight, and the three apps
// the organization brings, their keys in files. It answers the deps, the stand-ins and the
// flags that bring the apps.
func bootstrapWorld(t *testing.T) (deps, *roottest.Fake, *githubtest.Server, []string) {
	t.Helper()

	fake := roottest.New()
	fake.Landed = rootorg.ExportTables("012345-6789AB-CDEF01")
	srv := githubtest.New(t)
	srv.Login = "imp-bot"
	srv.Memberships["imp-example"] = "admin"
	srv.PublicApps["google-cloud-build"] = github.App{ID: 10529, Slug: "google-cloud-build", Name: "Google Cloud Build"}
	srv.OrgInstallations["imp-example"] = []github.OrgInstallation{{ID: 55, AppID: 10529, AppSlug: "google-cloud-build", RepositorySelection: github.AllRepositories}}
	disc := &fakeDiscovery{
		account: "bedrock@imp.example",
		orgs:    []rootorg.Organization{{ID: "123456789012", Domain: "imp.example", DirectoryCustomerID: "C0a1b2c3d"}},
		billing: []rootorg.BillingAccount{{ID: "012345-6789AB-CDEF01", DisplayName: "Imp Billing", Open: true}},
	}
	clients := &rootorg.Clients{
		GitHub: func(token string) *github.Client { return github.New(srv.URL, token) },
		Permissions: func(context.Context) (org.PermissionTester, error) {
			return allPermissions{}, nil
		},
		Discovery: disc.open,
		Sleep:     noWait,
	}
	fake.Clients(clients)
	d := orgDeps("")
	d.org.root = clients
	d.deploy = &deploy.Clients{Secrets: func(context.Context) (deploy.Secrets, error) { return fake, nil }}
	keys := t.TempDir()
	var flags []string
	for i, m := range (&org.Placement{GithubOrganization: "imp-example"}).AppManifests() {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatal(err)
		}
		keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
		id := int64(5000 + i)
		srv.Apps[fmt.Sprint(id)] = &githubtest.App{
			Key:           &key.PublicKey,
			Record:        github.App{ID: id, Slug: m.Name, Name: m.Name, Owner: github.Account{Login: "imp-example", Type: "Organization"}, Permissions: m.Permissions, PEM: string(keyPEM)},
			Installations: []github.Installation{{ID: id + 1, AppID: id, Account: github.Account{Login: "imp-example", Type: "Organization"}, RepositorySelection: github.AllRepositories}},
		}
		file := filepath.Join(keys, string(m.Role)+".pem")
		if err := os.WriteFile(file, keyPEM, 0o600); err != nil {
			t.Fatal(err)
		}
		flags = append(flags, "--"+string(m.Role)+"-app", fmt.Sprint(id), "--"+string(m.Role)+"-key", file)
	}

	return d, fake, srv, flags
}

// TestOrgBootstrap: with the flags and GITHUB_TOKEN set and no terminal, the bootstrap
// drafts the placement from the discovery and the flags, makes the root organization with
// the apps brought, renders and pushes the repository, and says what follows; a run again
// in the same directory finishes from the record and takes no naming flag.
func TestOrgBootstrap(t *testing.T) {
	// No t.Parallel(): the Bot Account's token is read from the process environment.
	t.Setenv(github.TokenEnv, "test-token")
	d, fake, srv, brought := bootstrapWorld(t)
	dir := filepath.Join(t.TempDir(), "imr-root-infrastructure")
	args := append([]string{"org", "bootstrap", dir, "--prefix", "imr", "--github-organization", "imp-example", "--monthly-budget", "1500", "--domain", "clientbay.example", "--key-operator", "group:key-operators@imp.example", "--label", "bedrock-lab=true", "--no-browser"}, brought...)
	out, err := execute(d, "", args...)
	if err != nil {
		t.Fatalf("org bootstrap = %v; printed:\n%s", err, out)
	}
	for _, want := range []string{
		"Drafted the imr root organization: Google Cloud organization imp.example (123456789012), billing account 012345-6789AB-CDEF01, GitHub organization imp-example, region us-central1 (uc1), bedrock " + headVersion + ", as bedrock@imp.example.",
		"The token is imp-bot's, an owner of imp-example: the Bot Account.",
		"Created the folder imr at the organization node: 900000000001.",
		"the infrastructure app: App 5000 (imp-example-infrastructure), brought, holds what bedrock declares and is installed on imp-example.",
		"Created the repository imp-example/imr-root-infrastructure, private.",
		"Renamed the branch main of imp-example/imr-root-infrastructure to master, the default branch placement.json names.",
		"zones: creates the zone imr-root-gbl-dns-clientbay-example for clientbay.example",
		"Bootstrapped the imr root organization as bedrock@imp.example",
		"Next, each where it happens:",
		"clone imp-example/imr-root-infrastructure (git clone https://github.com/imp-example/imr-root-infrastructure.git), which holds what " + dir + " holds, at its first commit.",
		dir + " holds the files pushed; clone the repository to go on from it: git clone https://github.com/imp-example/imr-root-infrastructure.git",
		"The break-glass account keeps Folder Admin on the folder imr alone",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("printed:\n%s\nwant it to contain %q", out, want)
		}
	}
	p, err := org.ReadPlacement(filepath.Join(dir, "placement.json"))
	if err != nil {
		t.Fatalf("the placement the bootstrap wrote: %v", err)
	}
	if !strings.HasPrefix(p.Projects[org.RootProjectKey], "imr-root-gbl-core-") || p.StateBucket == "" || p.GithubInfrastructureAppID != "5000" || p.GithubReleaseAppSlug != "imp-example-release" || p.BedrockVersion != headVersion || p.GithubBotAccount != "imp-bot" || p.Labels["bedrock-lab"] != "true" {
		t.Errorf("the placement: project %q, bucket %q, infrastructure app %q, release slug %q, bedrock %q, bot %q, labels %v", p.Projects[org.RootProjectKey], p.StateBucket, p.GithubInfrastructureAppID, p.GithubReleaseAppSlug, p.BedrockVersion, p.GithubBotAccount, p.Labels)
	}
	if !slices.Contains(srv.Created, "imp-example/imr-root-infrastructure") {
		t.Errorf("the repository was not made: %v", srv.Created)
	}
	if !fake.Projects[p.Projects[org.RootProjectKey]] {
		t.Errorf("the project is not there: %v", fake.Projects)
	}

	out, err = execute(d, "", "org", "bootstrap", dir, "--no-browser")
	if err != nil {
		t.Fatalf("org bootstrap again = %v; printed:\n%s", err, out)
	}
	for _, want := range []string{
		"records the imr root organization from an earlier run: the bootstrap finishes what that run left.",
		"The project " + p.Projects[org.RootProjectKey] + " exists.",
		"The root organization stands as placement.json says: nothing to change.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the run again printed:\n%s\nwant it to contain %q", out, want)
		}
	}
	_, err = execute(d, "", "org", "bootstrap", dir, "--prefix", "imr", "--monthly-budget", "1500", "--no-browser")
	if want := "records the imr root organization from an earlier run, and names everything: run again without --prefix"; err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("org bootstrap with a naming flag = %v, want %q", err, want)
	}
}

// TestOrgBootstrapPrompts: at a terminal, the prefix, the GitHub organization and the
// spend budget are asked for, and the token is typed without echo.
func TestOrgBootstrapPrompts(t *testing.T) {
	t.Setenv(github.TokenEnv, "")
	d, _, _, brought := bootstrapWorld(t)
	d.interactive = always
	d.readSecret = func(w io.Writer, question string) ([]byte, error) {
		fmt.Fprint(w, question)

		return []byte("test-token\n"), nil
	}
	dir := filepath.Join(t.TempDir(), "imr-root-infrastructure")
	out, err := execute(d, "imr\nimp-example\n1500\n", append([]string{"org", "bootstrap", dir, "--no-browser"}, brought...)...)
	if err != nil {
		t.Fatalf("org bootstrap = %v; printed:\n%s", err, out)
	}
	for _, want := range []string{
		"The prefix the root organization's names start with (2 to 4 lowercase characters, a letter first):",
		"The GitHub organization's login (the Bot Account is an owner of it):",
		"The spend budget on the whole billing account, in whole units of its currency a month",
		"The GitHub Bot Account's token (a classic personal access token; it goes into Secret Manager and nowhere else): ",
		"Bootstrapped the imr root organization as bedrock@imp.example",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("printed:\n%s\nwant it to contain %q", out, want)
		}
	}
}

// TestOrgBootstrapRefusals: what the command refuses before anything is made, and why.
func TestOrgBootstrapRefusals(t *testing.T) {
	t.Setenv(github.TokenEnv, "test-token")
	tests := []struct {
		name    string
		prepare func(d *deps)
		env     string
		args    []string
		wantErr string
	}{
		{name: "no prefix and no terminal", args: []string{"--github-organization", "imp-example", "--monthly-budget", "1500"}, wantErr: "no --prefix given and no terminal to ask on"},
		{name: "no GitHub organization and no terminal", args: []string{"--prefix", "imr"}, wantErr: "no --github-organization given and no terminal to ask on"},
		{name: "no token and no terminal", env: "none", args: []string{"--prefix", "imr", "--github-organization", "imp-example", "--monthly-budget", "1500"}, wantErr: "no token of the GitHub Bot Account: set GITHUB_TOKEN to a classic personal access token of the Bot Account"},
		{
			name: "several organizations and no terminal",
			prepare: func(d *deps) {
				d.org.root.Discovery = (&fakeDiscovery{account: "bedrock@imp.example", orgs: []rootorg.Organization{{ID: "123456789012", Domain: "imp.example"}, {ID: "210987654321", Domain: "other.example"}}, billing: []rootorg.BillingAccount{{ID: "012345-6789AB-CDEF01", Open: true}}}).open
			},
			args:    []string{"--prefix", "imr", "--github-organization", "imp-example", "--monthly-budget", "1500"},
			wantErr: "these credentials see 2 Google Cloud organizations and no terminal is here to choose on: pass --organization, one of 123456789012 (imp.example), 210987654321 (other.example)",
		},
		{
			name: "an organization the credentials do not see",
			args: []string{"--prefix", "imr", "--github-organization", "imp-example", "--monthly-budget", "1500", "--organization", "other.example"}, wantErr: "--organization other.example is none these credentials see: 123456789012 (imp.example)",
		},
		{
			name: "no open billing account",
			prepare: func(d *deps) {
				d.org.root.Discovery = (&fakeDiscovery{account: "bedrock@imp.example", orgs: []rootorg.Organization{{ID: "123456789012", Domain: "imp.example"}}}).open
			},
			args:    []string{"--prefix", "imr", "--github-organization", "imp-example", "--monthly-budget", "1500"},
			wantErr: "these credentials see no open billing account: the break-glass account holds Billing Account Administrator on the one the organization's projects link to",
		},
		{name: "a prefix that breaks the rule", args: []string{"--prefix", "Imr", "--github-organization", "imp-example", "--monthly-budget", "1500"}, wantErr: `prefix "Imr" is not 2 to 4 lowercase alphanumeric characters starting with a letter`},
		{name: "a domain that is not bare", args: []string{"--prefix", "imr", "--github-organization", "imp-example", "--monthly-budget", "1500", "--domain", "ClientBay.example"}, wantErr: `domains names "ClientBay.example", which is not a bare lowercase domain`},
		{name: "a region of another shape", args: []string{"--prefix", "imr", "--github-organization", "imp-example", "--monthly-budget", "1500", "--region", "global"}, wantErr: `"global" is not a region name of the shape <area>-<direction><number> (us-central1)`},
		{name: "an app brought without its key", args: []string{"--prefix", "imr", "--github-organization", "imp-example", "--monthly-budget", "1500", "--deployer-app", "5001"}, wantErr: "--deployer-app and --deployer-key go together: the App ID of the deployer app and the file holding its private key"},
		{
			name:    "a bedrock built from a checkout",
			prepare: func(d *deps) { d.version = nil },
			args:    []string{"--prefix", "imr", "--github-organization", "imp-example", "--monthly-budget", "1500"},
			wantErr: "this bedrock is (devel), which no placement can pin, and the root organization's triggers fetch the bedrock placement.json pins",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.env == "none" {
				t.Setenv(github.TokenEnv, "")
			}
			d, fake, _, _ := bootstrapWorld(t)
			if tt.prepare != nil {
				tt.prepare(&d)
			}
			dir := filepath.Join(t.TempDir(), "imr-root-infrastructure")
			out, err := execute(d, "", append([]string{"org", "bootstrap", dir, "--no-browser"}, tt.args...)...)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("org bootstrap = %v, want %q; printed:\n%s", err, tt.wantErr, out)
			}
			if len(fake.Projects) > 0 || len(fake.Folders) > 0 {
				t.Errorf("something was made: %v %v", fake.Projects, fake.Folders)
			}
		})
	}
}
