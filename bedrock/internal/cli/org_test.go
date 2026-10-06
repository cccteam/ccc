package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/derive"
	"github.com/cccteam/ccc/bedrock/internal/domain"
	"github.com/cccteam/ccc/bedrock/internal/org"
	"github.com/cccteam/ccc/bedrock/internal/release"
)

const orgFixture = "../org/testdata/imp"

// orgRepo copies the fixture organization's placement into a temporary directory.
func orgRepo(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	data, err := os.ReadFile(filepath.Join(orgFixture, "placement.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "placement.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	return dir
}

// orgDeps are the org commands' seams in tests: no cloud client answers, the owner and
// API key reports run without credentials, and the running bedrock is a commit installed
// with go install, which an application's first placement can pin.
func orgDeps(dir string) deps {
	return deps{domains: noCloudDomains, secrets: noSecretManager, projects: noProjects, org: &orgClients{policies: noPolicies, keys: noKeys, registrations: noRegistrations, permissions: noPermissions}, cwd: dir, interactive: never, version: installedHead}
}

// installedHead is the running bedrock as go install builds the head commit.
func installedHead() build {
	return build{version: headVersion, kind: buildInstalled}
}

// noPolicies refuses to open an IAM policy reader, as a run without Google credentials.
func noPolicies(context.Context) (org.PolicyReader, error) {
	return nil, errors.New("no Google credentials in this test")
}

// noKeys refuses to open an API key lister, as a run without Google credentials.
func noKeys(context.Context) (org.KeyLister, error) {
	return nil, errors.New("no Google credentials in this test")
}

// noRegistrations refuses to open a Cloud Domains registration reader, as a run without
// Google credentials.
func noRegistrations(context.Context, string) (domain.RegistrationReader, error) {
	return nil, errors.New("no Google credentials in this test")
}

// noPermissions refuses to open a permission tester, as a run without Google credentials.
func noPermissions(context.Context) (org.PermissionTester, error) {
	return nil, errors.New("no Google credentials in this test")
}

// runOrg runs an org command over the test seams and returns its exit code and output.
func runOrg(t *testing.T, args ...string) (code int, out string) {
	t.Helper()

	out, err := execute(orgDeps(""), "", args...)
	if err != nil {
		var exit exitError
		if !asExit(err, &exit) {
			t.Fatalf("Execute(%v) error = %v", args, err)
		}
		code = exit.code
	}

	return code, out
}

// authorizeConnection sets the two connection values in 2-env's placement, as the
// browser step's reader does before the first application is registered.
func authorizeConnection(t *testing.T, dir string) {
	t.Helper()

	path := filepath.Join(dir, "2-env", "terraform.tfvars")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	values := "github_app_installation_id        = 12345678\ngithub_oauth_token_secret_version = \"projects/imp-tst-gbl-core-1a2b/locations/us-central1/secrets/github-github-oauthtoken-abcdef/versions/1\"\n"
	if err := os.WriteFile(path, append(data, []byte(values)...), 0o600); err != nil {
		t.Fatal(err)
	}
}

// fakePolicyReader answers every project's IAM policy from one map; a project it lacks
// has no bindings.
type fakePolicyReader struct {
	byProject map[string][]org.Binding
}

func (f *fakePolicyReader) ProjectPolicy(_ context.Context, project string) ([]org.Binding, error) {
	return f.byProject[project], nil
}

func (*fakePolicyReader) Close() error {
	return nil
}

// TestOrgCheckOwners: org check lists each person holding roles/owner on an environment
// project, with the project, and says what it does when it has no credentials; neither
// fails the check.
func TestOrgCheckOwners(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		policies org.PolicyReaderFunc
		wantOut  []string
	}{
		{
			name: "a person on roles/owner is reported with the project; a service account is not",
			policies: func(context.Context) (org.PolicyReader, error) {
				return &fakePolicyReader{byProject: map[string][]org.Binding{
					"imp-tst-gbl-core-1a2b": {{Role: "roles/owner", Members: []string{"user:seed@imp.example", "serviceAccount:imp-tst-gbl-tofu@imp-tst-gbl-core-1a2b.iam.gserviceaccount.com"}}},
				}}, nil
			},
			wantOut: []string{
				"tst (imp-tst-gbl-core-1a2b): user:seed@imp.example holds roles/owner, the creator's grant from the first apply of 1-org by hand; it is temporary, removed once the layers workflow applies the layers (1-org/README.md, Applying).",
				"owned file(s) match the placement",
			},
		},
		{
			name: "nobody on roles/owner",
			policies: func(context.Context) (org.PolicyReader, error) {
				return &fakePolicyReader{}, nil
			},
			wantOut: []string{"No person holds roles/owner on an environment project.", "owned file(s) match the placement"},
		},
		{
			name:     "without credentials the check says what it would do",
			policies: noPolicies,
			wantOut: []string{
				"Owners not checked (no Google credentials in this test): org check lists each person (a user: member) holding roles/owner on an environment project, the creator's temporary grant, when it runs with Google credentials that read the projects' IAM policies (gcloud auth application-default login).",
				"owned file(s) match the placement",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := orgRepo(t)
			if code, out := runOrg(t, "org", "new", dir); code != 0 {
				t.Fatalf("org new: %d %s", code, out)
			}
			d := orgDeps(dir)
			d.org = &orgClients{policies: tt.policies, keys: noKeys, permissions: noPermissions}
			out, err := execute(d, "", "org", "check", "--dir", dir)
			if err != nil {
				t.Fatalf("Execute() error = %v; output:\n%s", err, out)
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out, want) {
					t.Errorf("output lacks %q:\n%s", want, out)
				}
			}
		})
	}
}

// fakeKeyLister answers every project's API keys from one map; a project it lacks has
// none.
type fakeKeyLister struct {
	byProject map[string][]org.APIKey
}

func (f *fakeKeyLister) ProjectKeys(_ context.Context, project string) ([]org.APIKey, error) {
	return f.byProject[project], nil
}

func (*fakeKeyLister) Close() error {
	return nil
}

// TestOrgCheckAPIKeys: org check lists each API key in an environment project that
// carries no API restriction, with the project and what to do (Firebase's browser key is
// the layers workflow's to restrict; any other key is a person's), names none when every
// key is restricted or there are none, and says what it does when it has no credentials;
// none of it fails the check.
func TestOrgCheckAPIKeys(t *testing.T) {
	t.Parallel()

	const (
		tst = "imp-tst-gbl-core-1a2b"
		prd = "imp-prd-gbl-core-5e6f"
	)
	lister := func(byProject map[string][]org.APIKey) org.KeyListerFunc {
		return func(context.Context) (org.KeyLister, error) {
			return &fakeKeyLister{byProject: byProject}, nil
		}
	}
	tests := []struct {
		name    string
		keys    org.KeyListerFunc
		wantOut []string
		absent  []string
	}{
		{
			name: "the browser key restricted to the sign-in APIs",
			keys: lister(map[string][]org.APIKey{tst: {{Name: "projects/" + tst + "/locations/global/keys/0f1e", DisplayName: org.FirebaseBrowserKey, APITargets: org.SignInAPIs}}}),
			wantOut: []string{
				"Every API key in the environment projects carries an API restriction.",
				"owned file(s) match the placement",
			},
			absent: []string{"carries no API restriction, so"},
		},
		{
			name: "the browser key unrestricted",
			keys: lister(map[string][]org.APIKey{prd: {{Name: "projects/" + prd + "/locations/global/keys/0f1e", DisplayName: org.FirebaseBrowserKey}}}),
			wantOut: []string{
				`prd (imp-prd-gbl-core-5e6f): the API key "Browser key (auto created by Firebase)" (projects/imp-prd-gbl-core-5e6f/locations/global/keys/0f1e) carries no API restriction, so it answers every API in the project that accepts an API key; the layers workflow restricts it to identitytoolkit.googleapis.com and securetoken.googleapis.com after each apply of 2-env: run the workflow for 2-env (Run workflow, on the Actions tab) to restrict it now (2-env/README.md, Identity Platform).`,
				"owned file(s) match the placement",
			},
			absent: []string{"Every API key in the environment projects carries an API restriction."},
		},
		{
			name: "another key unrestricted",
			keys: lister(map[string][]org.APIKey{tst: {{Name: "projects/" + tst + "/locations/global/keys/9a8b", DisplayName: "a key made by hand"}}}),
			wantOut: []string{
				`tst (imp-tst-gbl-core-1a2b): the API key "a key made by hand" (projects/imp-tst-gbl-core-1a2b/locations/global/keys/9a8b) carries no API restriction, so it answers every API in the project that accepts an API key; no layer declares it: restrict it to the APIs it is for, or delete it.`,
				"owned file(s) match the placement",
			},
		},
		{
			name:    "no keys",
			keys:    lister(nil),
			wantOut: []string{"Every API key in the environment projects carries an API restriction.", "owned file(s) match the placement"},
		},
		{
			name: "without credentials the check says what it would do",
			keys: noKeys,
			wantOut: []string{
				"API keys not checked (no Google credentials in this test): org check lists each API key in an environment project that carries no API restriction, when it runs with Google credentials that list the projects' API keys (gcloud auth application-default login).",
				"owned file(s) match the placement",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := orgRepo(t)
			if code, out := runOrg(t, "org", "new", dir); code != 0 {
				t.Fatalf("org new: %d %s", code, out)
			}
			d := orgDeps(dir)
			d.org = &orgClients{policies: noPolicies, keys: tt.keys, permissions: noPermissions}
			out, err := execute(d, "", "org", "check", "--dir", dir)
			if err != nil {
				t.Fatalf("Execute() error = %v; output:\n%s", err, out)
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out, want) {
					t.Errorf("output lacks %q:\n%s", want, out)
				}
			}
			for _, a := range tt.absent {
				if strings.Contains(out, a) {
					t.Errorf("output carries %q:\n%s", a, out)
				}
			}
		})
	}
}

// fakeRegistrationReader answers every registration from one map; a domain it lacks is not
// registered.
type fakeRegistrationReader struct {
	byDomain map[string]domain.RegistrationStatus
}

func (f *fakeRegistrationReader) Registration(_ context.Context, _, name string) (*domain.RegistrationStatus, error) {
	s, ok := f.byDomain[name]
	if !ok {
		return &domain.RegistrationStatus{Domain: name}, nil
	}
	s.Domain, s.Found = name, true

	return &s, nil
}

func (*fakeRegistrationReader) Close() error {
	return nil
}

// TestOrgCheckRegistrations: org check reports, before anything else, each domain 2-net
// registers as Cloud Domains holds it in the network project, a registrant mailbox waiting
// on its verification first; it says so when 2-net registers none, and says what it does
// when it cannot reach Cloud Domains; none of it fails the check.
func TestOrgCheckRegistrations(t *testing.T) {
	t.Parallel()

	const registrations = "registrant_contact = {\n  email = \"hostmaster@imp.example\"\n}\n\nregistrations = {\n  \"imp.app\" = {\n    yearly_price_usd = 14\n    notices          = [\"HSTS_PRELOADED\"]\n  }\n  \"imp.dev\" = {\n    yearly_price_usd = 12\n    notices          = [\"HSTS_PRELOADED\"]\n  }\n}\n"
	created := time.Now().Add(-48 * time.Hour)
	expires := time.Now().Add(365 * 24 * time.Hour)
	reader := func(byDomain map[string]domain.RegistrationStatus) domain.RegistrationReaderFunc {
		return func(_ context.Context, project string) (domain.RegistrationReader, error) {
			if project != "imp-net-gbl-core-9c0d" {
				return nil, errors.Newf("opened for %s, not the network project", project)
			}

			return &fakeRegistrationReader{byDomain: byDomain}, nil
		}
	}
	tests := []struct {
		name          string
		registrations string
		reader        domain.RegistrationReaderFunc
		// wantFirst is the output's first line; wantOut are lines it holds anywhere.
		wantFirst string
		wantOut   []string
	}{
		{
			name:          "an unverified registrant mailbox comes before anything else",
			registrations: registrations,
			reader: reader(map[string]domain.RegistrationStatus{
				"imp.app": {State: "ACTIVE", Created: created, Expires: expires},
				"imp.dev": {State: "ACTIVE", Created: created, Expires: expires, Issues: []string{domain.IssueUnverifiedEmail}},
			}),
			wantFirst: "In the registrant's mailbox (hostmaster@imp.example, registrant_contact.email in 2-net/terraform.tfvars): the registration of imp.dev waits on the registrar's verification mail; follow its link by " + created.Add(15*24*time.Hour).Format("2006-01-02") + ", fifteen days after the registration on " + created.Format("2006-01-02") + ", or the domain is suspended.",
			wantOut: []string{
				"imp.app: ACTIVE, expires on " + expires.Format("2006-01-02") + ".",
				"imp.dev: ACTIVE, expires on " + expires.Format("2006-01-02") + ".",
				"owned file(s) match the placement",
			},
		},
		{
			name:      "2-net registers none",
			reader:    noRegistrations,
			wantFirst: "No domain is registered through 2-net (registrations in 2-net/terraform.tfvars lists none).",
			wantOut:   []string{"owned file(s) match the placement"},
		},
		{
			name:          "Cloud Domains out of reach: the check says what it would do",
			registrations: registrations,
			reader:        noRegistrations,
			wantFirst:     "Registrations not checked (no Google credentials in this test): org check reports each domain 2-net registers (registrations in 2-net/terraform.tfvars), its state and expiry date, a registrant mailbox waiting on its verification first, when it runs with Google credentials that read the network project's Cloud Domains registrations (roles/domains.viewer; gcloud auth application-default login).",
			wantOut:       []string{"owned file(s) match the placement"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := orgRepo(t)
			if code, out := runOrg(t, "org", "new", dir); code != 0 {
				t.Fatalf("org new: %d %s", code, out)
			}
			if tt.registrations != "" {
				path := filepath.Join(dir, "2-net", "terraform.tfvars")
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				data = bytes.Replace(data, []byte("registrations = {}\n"), []byte(tt.registrations), 1)
				if err := os.WriteFile(path, data, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			d := orgDeps(dir)
			d.org = &orgClients{policies: noPolicies, keys: noKeys, registrations: tt.reader, permissions: noPermissions}
			out, err := execute(d, "", "org", "check", "--dir", dir)
			if err != nil {
				t.Fatalf("Execute() error = %v; output:\n%s", err, out)
			}
			if first, _, _ := strings.Cut(out, "\n"); first != tt.wantFirst {
				t.Errorf("first line = %q, want %q", first, tt.wantFirst)
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out, want) {
					t.Errorf("output lacks %q:\n%s", want, out)
				}
			}
		})
	}
}

// TestOrgCheckZoneReplacement: while zoneReplacement is set in the placement, org check
// names it and what is left to do, as information; the layers rendered with it match, so
// the check stays clean; without it nothing is said.
func TestOrgCheckZoneReplacement(t *testing.T) {
	t.Parallel()

	const notice = "zoneReplacement is set in placement.json: 2-net's apps zone and parked zones are rendered without prevent_destroy, so a plan may destroy them and create them again on other name servers. Make the change that recreates the zone (a new appsDomain, then bedrock org render), point what pointed at the old name servers at the new ones as bedrock domain check prints, then clear zoneReplacement and run bedrock org render to write the rule back; bedrock domain check says when the domain points at the zone (2-net/README.md, \"Making a zone again\").\n"
	tests := []struct {
		name            string
		zoneReplacement bool
		wantNotice      bool
	}{
		{name: "set: named, with what is left to do", zoneReplacement: true, wantNotice: true},
		{name: "not set: nothing said"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := orgRepo(t)
			path := filepath.Join(dir, "placement.json")
			p, err := org.ReadPlacement(path)
			if err != nil {
				t.Fatal(err)
			}
			p.ZoneReplacement = tt.zoneReplacement
			if err := p.Write(path); err != nil {
				t.Fatal(err)
			}
			if code, out := runOrg(t, "org", "new", dir); code != 0 {
				t.Fatalf("org new: %d %s", code, out)
			}
			code, out := runOrg(t, "org", "check", "--dir", dir)
			if code != 0 {
				t.Fatalf("org check: exit %d, want 0:\n%s", code, out)
			}
			if strings.Contains(out, notice) != tt.wantNotice {
				t.Errorf("output carries the notice: %v, want %v:\n%s", !tt.wantNotice, tt.wantNotice, out)
			}
			if !strings.Contains(out, "owned file(s) match the placement") {
				t.Errorf("output lacks the match line:\n%s", out)
			}
		})
	}
}

// grantedTester holds every permission asked about except those it lacks.
type grantedTester struct {
	lacks []string
}

func (g grantedTester) held(permissions []string) []string {
	var held []string
	for _, p := range permissions {
		if !slices.Contains(g.lacks, p) {
			held = append(held, p)
		}
	}

	return held
}

func (g grantedTester) OrganizationPermissions(_ context.Context, _ string, permissions []string) ([]string, error) {
	return g.held(permissions), nil
}

func (g grantedTester) BillingAccountPermissions(_ context.Context, _ string, permissions []string) ([]string, error) {
	return g.held(permissions), nil
}

func (grantedTester) Close() error {
	return nil
}

// TestOrgPreflight: org preflight reads the organization and the billing account from
// the placement at the repository root, prints one line per role and exits 1 when a role
// is missing or the permissions could not be checked.
func TestOrgPreflight(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		permissions org.PermissionTesterFunc
		wantCode    int
		wantOut     []string
	}{
		{
			name: "every role held",
			permissions: func(context.Context) (org.PermissionTester, error) {
				return grantedTester{}, nil
			},
			wantOut: []string{
				"Folder Creator (roles/resourcemanager.folderCreator), on organization 123456789012: holds resourcemanager.folders.create",
				"Billing Account User (roles/billing.user), on billing account 012345-6789AB-CDEF01: holds billing.resourceAssociations.create",
				"These credentials hold every permission",
			},
		},
		{
			name: "a role missing exits 1",
			permissions: func(context.Context) (org.PermissionTester, error) {
				return grantedTester{lacks: []string{"resourcemanager.tagValues.setIamPolicy"}}, nil
			},
			wantCode: 1,
			wantOut: []string{
				"Tag Administrator (roles/resourcemanager.tagAdmin), on organization 123456789012: missing resourcemanager.tagValues.setIamPolicy",
				"1 of 7 role(s) missing",
			},
		},
		{
			name:        "no credentials exits 1 and says so",
			permissions: noPermissions,
			wantCode:    1,
			wantOut:     []string{"Permissions not checked (no Google credentials in this test): org preflight asks Google"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := orgRepo(t)
			d := orgDeps(dir)
			d.org = &orgClients{policies: noPolicies, keys: noKeys, permissions: tt.permissions}
			out, err := execute(d, "", "org", "preflight", "--dir", dir)
			code := 0
			if err != nil {
				var exit exitError
				if !asExit(err, &exit) {
					t.Fatalf("Execute() error = %v; output:\n%s", err, out)
				}
				code = exit.code
			}
			if code != tt.wantCode {
				t.Errorf("exit = %d, want %d; output:\n%s", code, tt.wantCode, out)
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out, want) {
					t.Errorf("output lacks %q:\n%s", want, out)
				}
			}
		})
	}
}

// TestOrgRegisterNeedsAuthorization: the first application is registered after the Cloud
// Build GitHub App's browser authorization, so register refuses while 2-env's placement
// leaves either connection value unset, and changes nothing.
func TestOrgRegisterNeedsAuthorization(t *testing.T) {
	t.Parallel()

	dir := orgRepo(t)
	if code, out := runOrg(t, "org", "new", dir); code != 0 {
		t.Fatalf("org new: %d %s", code, out)
	}
	_, err := execute(orgDeps(dir), "", "org", "register", "quill", "--dir", dir)
	want := `2-env/terraform.tfvars leaves github_app_installation_id and github_oauth_token_secret_version unset: the Cloud Build GitHub App's browser authorization comes before the first application (2-env/README.md, "The GitHub authorization, before the first application"); set both and register again`
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("Execute() error = %v, want %q", err, want)
	}
	placement, err := os.ReadFile(filepath.Join(dir, "placement.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(placement), `"quill"`) {
		t.Errorf("placement.json changed on a refusal:\n%s", placement)
	}
}

func TestOrgCommands(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		steps    func(t *testing.T, dir string) (code int, out string)
		wantCode int
		wantOut  []string
	}{
		{
			name: "new renders the layers and prints the hand steps",
			steps: func(t *testing.T, dir string) (int, string) {
				t.Helper()

				return runOrg(t, "org", "new", dir)
			},
			wantOut: []string{
				"Rendered the imp foundation", "69 owned file(s)", "seeded .gitignore, 0-bootstrap/terraform.tfvars",
				"By hand, before the layers workflow can run", "0. In the Workspace Admin console: one team group per environment, named in\n     placement.json (teamGroups",
				"its role groups (<group prefix><role>@imp.example), each created with\n     \"Who can view members\" set so that its members can",
				"On GitHub, as an organization owner: the organization, its machine account\n     (imp-machine, an owner of the organization, named in placement.json as\n     githubMachineAccount)",
				"the release app's two organization secrets\n     RELEASE_APP_ID and RELEASE_APP_PRIVATE_KEY, set once by an owner for all\n     repositories",
				"In a terminal, in this repository, the apps' keys, never by hand: bedrock secret\n     add github-infrastructure-key and bedrock secret pin github-infrastructure-key\n     <version> once 0-bootstrap has made its container; bedrock secret add\n     github-deployer-key <env> and bedrock secret pin github-deployer-key <env>\n     <version> per environment once 2-env has made its container there.",
				"1. In a terminal, as seed@imp.example, after gcloud auth application-default login:\n     bedrock org preflight, which must find each of these roles held:\n" +
					"       - Folder Creator (roles/resourcemanager.folderCreator), on the organization\n" +
					"       - Project Creator (roles/resourcemanager.projectCreator), on the organization\n" +
					"       - Organization Administrator (roles/resourcemanager.organizationAdmin), on the organization\n" +
					"       - Organization Policy Administrator (roles/orgpolicy.policyAdmin), on the organization\n" +
					"       - Organization Role Administrator (roles/iam.organizationRoleAdmin), on the organization\n" +
					"       - Tag Administrator (roles/resourcemanager.tagAdmin), on the organization\n" +
					"       - Billing Account User (roles/billing.user), on the billing account\n" +
					"     then the seed: the terraform folder at the organization root (123456789012)",
				"imp-boot-gbl-tofu", "projects.boot, projectNumbers.boot",
				"4. In a terminal, as a billing administrator of 012345-6789AB-CDEF01: roles/billing.user on\n     it for the two identities. In the Billing console, as the same administrator: the\n     spend budget on the account",
				"5. In a terminal, in 1-org: the apply, with GITHUB_TOKEN", "a creator's grants,\n     temporary, removed by hand once the workflow applies the layers",
				"7. In each environment project's Google Cloud console, before the first application's\n     OAuth client is made there: the consent screen (APIs & Services, OAuth consent\n     screen), with the audience Internal",
				"6. In the tst project's Cloud Build console, signed in to GitHub as the organization's\n     machine account (imp-machine): the Cloud Build GitHub App's authorization", "bedrock org register refuses the first application until both are set.",
				"the layers workflow (.github/workflows/layers.yml) applies every layer",
				"\nAfter the first apply of 2-net: bedrock domain check prints what the apps domain still needs, and where.\n",
			},
		},
		{
			name: "check is clean after new, the workflow among the owned files",
			steps: func(t *testing.T, dir string) (int, string) {
				t.Helper()
				if code, out := runOrg(t, "org", "new", dir); code != 0 {
					t.Fatalf("org new: %d %s", code, out)
				}
				if _, err := os.Stat(filepath.Join(dir, ".github", "workflows", "layers.yml")); err != nil {
					t.Errorf("org new wrote no workflow: %v", err)
				}

				return runOrg(t, "org", "check", "--dir", dir)
			},
			wantOut: []string{"69 owned file(s) match the placement"},
		},
		{
			name: "register adds an application, renders the values and prints the sequence",
			steps: func(t *testing.T, dir string) (int, string) {
				t.Helper()
				if code, out := runOrg(t, "org", "new", dir); code != 0 {
					t.Fatalf("org new: %d %s", code, out)
				}
				authorizeConnection(t, dir)
				code, out := runOrg(t, "org", "register", "quill", "--dir", dir)
				placement, err := os.ReadFile(filepath.Join(dir, "placement.json"))
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(placement), `"quill"`) {
					t.Errorf("placement.json lacks quill:\n%s", placement)
				}
				hosts, err := os.ReadFile(filepath.Join(dir, "2-net", "applications.auto.tfvars"))
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(hosts), `"quill-tst.apps.imp.example"       = "projects/imp-tst-gbl-core-1a2b/global/backendServices/imp-tst-gbl-quill-backend"`) {
					t.Errorf("2-net/applications.auto.tfvars lacks quill's host:\n%s", hosts)
				}
				if code, out := runOrg(t, "org", "check", "--dir", dir); code != 0 {
					t.Errorf("org check after register: %d %s", code, out)
				}

				return code, out
			},
			wantOut: []string{
				"Registered quill in", "1-org/public-invokers.auto.tfvars among them", "Register through the layers workflow, as pull requests into master",
				"1. placement.json, 1-org/applications.auto.tfvars and 2-env/applications.auto.tfvars", "2. Run workflow (the Actions tab) with 2-env",
				"3. 1-org/public-invokers.auto.tfvars, 2-shr/applications.auto.tfvars and", "5. 2-net/applications.auto.tfvars: the hostnames",
				"\n  6. After 2-net's apply: bedrock domain check, which passes when the apps domain resolves to its zone.\n",
			},
		},
		{
			name: "render says what the workflow still lacks",
			steps: func(t *testing.T, dir string) (int, string) {
				t.Helper()
				if code, out := runOrg(t, "org", "new", dir); code != 0 {
					t.Fatalf("org new: %d %s", code, out)
				}
				path := filepath.Join(dir, "placement.json")
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				edited := strings.Replace(string(data), `"projectNumbers": {"boot": "100000000001", "tst": "100000000002", "stg": "100000000003", "prd": "100000000004"},`, "", 1)
				edited = strings.Replace(edited, `    "boot": "imp-boot-gbl-core-a1b2",`, "", 1)
				if err := os.WriteFile(path, []byte(edited), 0o600); err != nil {
					t.Fatal(err)
				}

				return runOrg(t, "org", "render", "--dir", dir)
			},
			wantOut: []string{"69 owned file(s) written", "The layers workflow cannot run every layer yet: record projectNumbers.boot, projects.boot in placement.json and run bedrock org render."},
		},
		{
			name: "check reports a hand edit",
			steps: func(t *testing.T, dir string) (int, string) {
				t.Helper()
				if code, out := runOrg(t, "org", "new", dir); code != 0 {
					t.Fatalf("org new: %d %s", code, out)
				}
				path := filepath.Join(dir, "2-shr", "registry.tf")
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, append([]byte("# edited by hand\n"), data...), 0o600); err != nil {
					t.Fatal(err)
				}

				return runOrg(t, "org", "check", "--dir", dir)
			},
			wantCode: 1,
			wantOut:  []string{"2-shr/registry.tf: line 1 differs", "1 of 69 owned file(s) differ"},
		},
		{
			name: "check reports a hand edit to the workflow",
			steps: func(t *testing.T, dir string) (int, string) {
				t.Helper()
				if code, out := runOrg(t, "org", "new", dir); code != 0 {
					t.Fatalf("org new: %d %s", code, out)
				}
				path := filepath.Join(dir, ".github", "workflows", "layers.yml")
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				edited := strings.Replace(string(data), "      fail-fast: true\n", "      fail-fast: false\n", 1)
				if err := os.WriteFile(path, []byte(edited), 0o600); err != nil {
					t.Fatal(err)
				}

				return runOrg(t, "org", "check", "--dir", dir)
			},
			wantCode: 1,
			wantOut:  []string{".github/workflows/layers.yml: line ", "  want:       fail-fast: true", "  got:        fail-fast: false", "1 of 69 owned file(s) differ"},
		},
		{
			name: "render keeps the seeded values",
			steps: func(t *testing.T, dir string) (int, string) {
				t.Helper()
				if code, out := runOrg(t, "org", "new", dir); code != 0 {
					t.Fatalf("org new: %d %s", code, out)
				}
				values := filepath.Join(dir, "2-spn", "terraform.tfvars")
				if err := os.WriteFile(values, []byte("processing_units = 200\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				code, out := runOrg(t, "org", "render", "--dir", dir)
				data, err := os.ReadFile(values)
				if err != nil {
					t.Fatal(err)
				}
				if string(data) != "processing_units = 200\n" {
					t.Errorf("render rewrote the seeded values: %q", data)
				}

				return code, out
			},
			wantOut: []string{"69 owned file(s) written"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := orgRepo(t)
			code, out := tt.steps(t, dir)
			if code != tt.wantCode {
				t.Errorf("exit code = %d, want %d:\n%s", code, tt.wantCode, out)
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out, want) {
					t.Errorf("output lacks %q:\n%s", want, out)
				}
			}
		})
	}
}

// TestOrgRegisterPlacement: register makes the application's first placement from the
// organization's placement and the running bedrock, writing it into the checkout given
// or printing it, and reads back through derive as render reads it; it refuses, changing
// nothing, a checkout that already holds one, an organization whose projects are not all
// recorded, and a bedrock no placement can pin.
func TestOrgRegisterPlacement(t *testing.T) {
	t.Parallel()

	src, sum := releaseServer(t)
	tests := []struct {
		name string
		// running is the bedrock running register; edit changes the organization's
		// placement before it; checkout is the second argument: "" for none, "new" for an
		// empty directory, "held" for one already holding a placement, "absent" for a
		// path that does not exist.
		running  build
		edit     func(p *org.Placement)
		checkout string
		wantPin  [2]string
		wantOut  []string
		wantErr  string
	}{
		{
			name:    "printed, pinned to an installed commit",
			running: build{version: headVersion, kind: buildInstalled},
			wantPin: [2]string{headVersion, ""},
			wantOut: []string{"quill's first placement, pinned to bedrock " + headVersion + ": commit it in the application's repository as infrastructure/placement.json"},
		},
		{
			name:     "written into the checkout, pinned to a release with its checksum",
			running:  build{version: "v0.4.0", kind: buildStamped},
			checkout: "new",
			wantPin:  [2]string{"v0.4.0", sum},
			wantOut:  []string{"Wrote ", "quill's first placement, pinned to bedrock v0.4.0: commit it in the application's repository"},
		},
		{
			name:     "a checkout that holds a placement already",
			running:  build{version: headVersion, kind: buildInstalled},
			checkout: "held",
			wantErr:  "exists: register writes an application's first placement and never overwrites one; leave the checkout out to print the file instead",
		},
		{
			name:     "a checkout that is not there",
			running:  build{version: headVersion, kind: buildInstalled},
			checkout: "absent",
			wantErr:  "the second argument is the application's checkout, where register writes its first placement (infrastructure/placement.json)",
		},
		{
			name:    "the environments' project numbers not recorded",
			running: build{version: headVersion, kind: buildInstalled},
			edit: func(p *org.Placement) {
				p.ProjectNumbers = map[string]string{"boot": "100000000001", "tst": "100000000002"}
			},
			wantErr: "placement.json records no project id and number for stg and prd yet: record 1-org's project_ids and project_numbers outputs there (projects, projectNumbers)",
		},
		{
			name:    "no state bucket recorded",
			running: build{version: headVersion, kind: buildInstalled},
			edit: func(p *org.Placement) {
				p.StateBucket = ""
			},
			wantErr: "placement.json records no stateBucket yet",
		},
		{
			name:    "a build from a checkout",
			running: build{version: headVersion + "+dirty", kind: buildCheckout},
			wantErr: "this bedrock is " + headVersion + "+dirty, which no placement can pin, and an application's first placement pins the bedrock that writes it: register with a released bedrock",
		},
		{
			name:    "a devel build",
			running: build{version: develVersion, kind: buildDevel},
			wantErr: "this bedrock is (devel), which no placement can pin",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := orgRepo(t)
			if code, out := runOrg(t, "org", "new", dir); code != 0 {
				t.Fatalf("org new: %d %s", code, out)
			}
			authorizeConnection(t, dir)
			orgFile := filepath.Join(dir, "placement.json")
			if tt.edit != nil {
				p, err := org.ReadPlacement(orgFile)
				if err != nil {
					t.Fatal(err)
				}
				tt.edit(p)
				if err := p.Write(orgFile); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.ReadFile(orgFile)
			if err != nil {
				t.Fatal(err)
			}
			args := []string{"org", "register", "quill", "--dir", dir}
			var checkout string
			switch tt.checkout {
			case "new", "held":
				checkout = t.TempDir()
				args = append(args, checkout)
			case "absent":
				checkout = filepath.Join(t.TempDir(), "quill")
				args = append(args, checkout)
			}
			held := []byte("{\"prefix\": \"mine\"}\n")
			if tt.checkout == "held" {
				if err := os.MkdirAll(filepath.Join(checkout, "infrastructure"), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(checkout, "infrastructure", "placement.json"), held, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			d := orgDeps(dir)
			d.version = func() build {
				return tt.running
			}
			d.releases = func() *release.Source {
				return src
			}
			out, err := execute(d, "", args...)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Execute() error = %v, wantErr %q; output:\n%s", err, tt.wantErr, out)
				}
				after, err := os.ReadFile(orgFile)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(before, after) {
					t.Errorf("placement.json changed on a refusal:\n%s", after)
				}
				if tt.checkout == "held" {
					if got, err := os.ReadFile(filepath.Join(checkout, "infrastructure", "placement.json")); err != nil || !bytes.Equal(got, held) {
						t.Errorf("the checkout's placement changed on a refusal: %s %v", got, err)
					}
				}

				return
			}
			if err != nil {
				t.Fatalf("Execute() error = %v; output:\n%s", err, out)
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out, want) {
					t.Errorf("output lacks %q:\n%s", want, out)
				}
			}
			file := filepath.Join(t.TempDir(), "placement.json")
			if checkout != "" {
				file = filepath.Join(checkout, "infrastructure", "placement.json")
			} else {
				start := strings.Index(out, "\n{\n")
				if start < 0 || !strings.HasSuffix(out, "}\n") {
					t.Fatalf("output ends with no placement:\n%s", out)
				}
				if err := os.WriteFile(file, []byte(out[start+1:]), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			p, err := derive.ReadPlacement(file)
			if err != nil {
				t.Fatalf("derive.ReadPlacement() error = %v", err)
			}
			want := &derive.Placement{
				Prefix: "imp", Environments: []string{"tst", "stg", "prd"},
				Regions:    []derive.Region{{Name: "us-central1", Code: "uc1"}, {Name: "us-west3", Code: "uw3"}},
				AppsDomain: "apps.imp.example", HostedDomain: "imp.example", StateBucket: "imp-boot-gbl-state-a1b2",
				PlaceholderImage: derive.PlaceholderImage, DefaultBranch: "master", Repository: "quill", ReleaseApp: "imp-release",
				BedrockVersion: tt.wantPin[0], BedrockSHA256: tt.wantPin[1],
				Labels: map[string]string{"bedrock-lab": "true"}, Seed: []string{"tst"},
				Projects: map[string]derive.Project{
					"tst": {ID: "imp-tst-gbl-core-1a2b", Number: "100000000002"},
					"stg": {ID: "imp-stg-gbl-core-3c4d", Number: "100000000003"},
					"prd": {ID: "imp-prd-gbl-core-5e6f", Number: "100000000004"},
				},
			}
			if !reflect.DeepEqual(p, want) {
				t.Errorf("the first placement =\n%+v\nwant\n%+v", p, want)
			}
		})
	}
}

// TestOrgRegisterThenRender: an application registered into its checkout renders from the
// placement register wrote and passes bedrock check, with no file copied from another
// application: render seeds release-please's files, the manifest at 0.0.0.
func TestOrgRegisterThenRender(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// app is the application registered, a fixture under ../derive/testdata; others
		// are the applications the organization holds before it.
		app    string
		others []string
	}{
		{name: "harbor, a Google directory auth", app: "harbor", others: []string{"beacon"}},
		{name: "beacon, a password auth, the organization's first", app: "beacon"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := orgRepo(t)
			if code, out := runOrg(t, "org", "new", dir); code != 0 {
				t.Fatalf("org new: %d %s", code, out)
			}
			authorizeConnection(t, dir)
			orgFile := filepath.Join(dir, "placement.json")
			p, err := org.ReadPlacement(orgFile)
			if err != nil {
				t.Fatal(err)
			}
			p.Applications = tt.others
			if err := p.Write(orgFile); err != nil {
				t.Fatal(err)
			}
			app := copyRepo(t, filepath.Join("..", "derive", "testdata", tt.app))
			if out, err := execute(orgDeps(dir), "", "org", "register", tt.app, "--dir", dir, app); err != nil {
				t.Fatalf("org register error = %v; output:\n%s", err, out)
			}
			d := deps{interactive: never, version: installedHead, cwd: app}
			out, err := execute(d, "", renderCommand)
			if err != nil {
				t.Fatalf("render error = %v; output:\n%s", err, out)
			}
			for _, want := range []string{"Rendered the " + tt.app + " stack into ", "Seeded release-please-config.json into ", "Seeded .release-please-manifest.json into "} {
				if !strings.Contains(out, want) {
					t.Errorf("render output lacks %q:\n%s", want, out)
				}
			}
			if out, err := execute(d, "", checkCommand); err != nil || !strings.Contains(out, "owned file(s) match the code") {
				t.Errorf("check error = %v; output:\n%s", err, out)
			}
			manifest, err := os.ReadFile(filepath.Join(app, ".release-please-manifest.json"))
			if err != nil || string(manifest) != "{\n  \".\": \"0.0.0\"\n}\n" {
				t.Errorf("the manifest = %q (%v), want 0.0.0", manifest, err)
			}
		})
	}
}

func TestOrgNewRefusal(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "placement.json"), []byte(`{"prefix": "impulse"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	d := deps{domains: noCloudDomains, secrets: noSecretManager, projects: noProjects, cwd: dir, interactive: never}
	if _, err := execute(d, "", "org", "new", dir); err == nil || !strings.Contains(err.Error(), "prefix") {
		t.Errorf("org new error = %v, want a refusal naming the prefix", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "1-org")); !os.IsNotExist(err) {
		t.Errorf("org new wrote layers from a refused placement: %v", err)
	}
}

func TestOrgRegisterRefusal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		app     string
		wantErr string
	}{
		{name: "an application registered already", app: "harbor", wantErr: `application "harbor" is registered already`},
		{name: "a code of the wrong shape", app: "Harbor", wantErr: `application "Harbor" is not 1 to 6 lowercase alphanumeric characters`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := orgRepo(t)
			d := deps{domains: noCloudDomains, secrets: noSecretManager, projects: noProjects, cwd: dir, interactive: never}
			if _, err := execute(d, "", "org", "register", tt.app, "--dir", dir); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Execute() error = %v, wantErr %q", err, tt.wantErr)
			}
			placement, err := os.ReadFile(filepath.Join(dir, "placement.json"))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Count(string(placement), `"harbor"`) != 1 || strings.Contains(string(placement), "Harbor") {
				t.Errorf("placement.json changed on a refusal:\n%s", placement)
			}
		})
	}
}
