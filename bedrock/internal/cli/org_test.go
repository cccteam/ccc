package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/deploy"
	"github.com/cccteam/ccc/bedrock/internal/derive"
	"github.com/cccteam/ccc/bedrock/internal/domain"
	"github.com/cccteam/ccc/bedrock/internal/github"
	"github.com/cccteam/ccc/bedrock/internal/github/githubtest"
	"github.com/cccteam/ccc/bedrock/internal/org"
	"github.com/cccteam/ccc/bedrock/internal/release"
)

const orgFixture = "../org/testdata/imp"

// orgOrganization is the fixture's GitHub organization, which an application's module
// path must name for register to read its repository from it.
const orgOrganization = "imp-example"

// appCheckout makes an application checkout for register: a directory whose go.mod
// names the module github.com/<organization>/<repository>, or no go.mod for an empty
// module path.
func appCheckout(t *testing.T, module string) string {
	t.Helper()

	dir := t.TempDir()
	if module == "" {
		return dir
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module "+module+"\n\ngo 1.26\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	return dir
}

// orgRepo copies the fixture organization's placement into a temporary directory,
// without its bedrock pin: the commands refuse a placement pinned to another bedrock
// than the one running, and the tests run as the head commit.
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
	editPlacement(t, dir, func(p *org.Placement) {
		p.BedrockVersion, p.BedrockSHA256 = "", ""
	})

	return dir
}

// editPlacement changes the organization's placement in the directory.
func editPlacement(t *testing.T, dir string, edit func(p *org.Placement)) {
	t.Helper()

	file := filepath.Join(dir, "placement.json")
	p, err := org.ReadPlacement(file)
	if err != nil {
		t.Fatal(err)
	}
	edit(p)
	if err := p.Write(file); err != nil {
		t.Fatal(err)
	}
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

// unauthorizeConnection clears the two Cloud Build values the fixture placement
// records, as a placement stands before the browser step in the boot project.
func unauthorizeConnection(t *testing.T, dir string) {
	t.Helper()

	editPlacement(t, dir, func(p *org.Placement) {
		p.CloudBuildInstallationID, p.CloudBuildTokenSecretVersion = "", ""
	})
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
				"tst (imp-tst-gbl-core-1a2b): user:seed@imp.example holds roles/owner, the creator's grant from the first apply of 1-org by hand; it is temporary, removed once the apply trigger applies the layers (1-org/README.md, Applying).",
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
// bedrock org apply's to restrict; any other key is a person's), names none when every
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
				`prd (imp-prd-gbl-core-5e6f): the API key "Browser key (auto created by Firebase)" (projects/imp-prd-gbl-core-5e6f/locations/global/keys/0f1e) carries no API restriction, so it answers every API in the project that accepts an API key; bedrock org apply restricts it to identitytoolkit.googleapis.com and securetoken.googleapis.com after each apply of 2-env: start the apply trigger for that environment (gcloud builds triggers run imp-boot-uc1-layers-apply --region=us-central1 --project=imp-boot-gbl-core-a1b2 --branch=master --substitutions=_LAYER=2-env,_ENVIRONMENT=prd) to restrict it now (2-env/README.md, Identity Platform).`,
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
	unauthorizeConnection(t, dir)
	_, err := execute(orgDeps(dir), "", "org", "register", "quill", "--dir", dir, appCheckout(t, "github.com/"+orgOrganization+"/quill"))
	want := `placement.json leaves cloudBuildInstallationId and cloudBuildTokenSecretVersion unset: the Cloud Build GitHub App's browser authorization in the boot project comes before the first application (0-bootstrap/README.md, "The Cloud Build GitHub authorization"); record both and register again`
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
				"By hand, before the Cloud Build triggers can run", "0. In the Workspace Admin console: one team group per environment, named in\n     placement.json (teamGroups",
				"its role groups (<group prefix><role>@imp.example), each created with\n     \"Who can view members\" set so that its members can",
				"On GitHub, as an organization owner: the organization, its machine account\n     (imp-machine, an owner of the organization, named in placement.json as\n     githubMachineAccount)",
				"has made their three containers in the boot project: bedrock secret add\n     github-infrastructure-key and bedrock secret pin github-infrastructure-key\n     <version>; the same with github-deployer-key and github-release-key. No key reaches\n     GitHub",
				"In a terminal, in this repository, the apps' keys, never by hand, once 0-bootstrap\n     has made their three containers in the boot project: bedrock secret add\n     github-infrastructure-key and bedrock secret pin github-infrastructure-key\n     <version>; the same with github-deployer-key and github-release-key. No key reaches\n     GitHub: the release trigger of each application's stack mints the release app's\n     token from the pinned version in Cloud Build.",
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
				"3. In the boot project's Cloud Build console, as seed@imp.example (the bootstrap\n     administrator, Owner on the boot project from the seed), signed in to GitHub as the\n     organization's machine account (imp-machine): the Cloud Build GitHub App's authorization\n     (0-bootstrap/README.md, \"The Cloud Build GitHub authorization\"), a connection named\n     imp-boot-uc1-github in us-central1; its installation id and the token secret's version go\n     into placement.json (cloudBuildInstallationId, cloudBuildTokenSecretVersion). Then,\n     in a terminal, in this repository: bedrock upgrade, which pins the bedrock the\n     triggers run (bedrockVersion) and renders. bedrock org register refuses the first\n     application until the two values are set.",
				"4. In a terminal, in 0-bootstrap: the apply on local state, the connection imported\n     into it first, then the migration of its state into the bucket.",
				"5. In a terminal, as a billing administrator of 012345-6789AB-CDEF01: roles/billing.user on\n     it for the two identities. In the Billing console, as the same administrator: the\n     spend budget on the account (0-bootstrap/README.md, step 5)",
				"6. In a terminal, in 1-org: the apply, with GITHUB_TOKEN", "a creator's grants,\n     temporary, removed by hand once the apply trigger applies the layers",
				"7. In each environment project's Google Cloud console, before the first application's\n     OAuth client is made there: the consent screen (APIs & Services, OAuth consent\n     screen), with the audience Internal",
				"From then on the two Cloud Build triggers in the boot project (imp-boot-uc1-layers-plan and imp-boot-uc1-layers-apply)\napply every layer, these two included: a pull request plans the layers it touches as their\nplan identities and posts the plans on it, the merge applies them as their apply\nidentities, in layer order.",
				"Pinned bedrock " + headVersion + " in ",
				"\nAfter the first apply of 2-net: bedrock domain check prints what the apps domain, and each vanity domain, still needs, and where.\n",
			},
		},
		{
			name: "check is clean after new, the triggers' file among the owned files and no workflow",
			steps: func(t *testing.T, dir string) (int, string) {
				t.Helper()
				if code, out := runOrg(t, "org", "new", dir); code != 0 {
					t.Fatalf("org new: %d %s", code, out)
				}
				if _, err := os.Stat(filepath.Join(dir, "0-bootstrap", "cloud-build.tf")); err != nil {
					t.Errorf("org new wrote no triggers: %v", err)
				}
				if _, err := os.Stat(filepath.Join(dir, ".github")); !os.IsNotExist(err) {
					t.Errorf("org new wrote a .github directory: %v", err)
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
				code, out := runOrg(t, "org", "register", "quill", "--dir", dir, appCheckout(t, "github.com/"+orgOrganization+"/quill-service"))
				placement, err := os.ReadFile(filepath.Join(dir, "placement.json"))
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(placement), "{\n      \"code\": \"quill\",\n      \"repository\": \"quill-service\"\n    }") {
					t.Errorf("placement.json lacks quill with its repository:\n%s", placement)
				}
				repositories, err := os.ReadFile(filepath.Join(dir, "1-org", "applications.auto.tfvars"))
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(repositories), "\n  quill  = \"quill-service\"\n}") {
					t.Errorf("1-org/applications.auto.tfvars lacks quill's repository:\n%s", repositories)
				}
				hosts, err := os.ReadFile(filepath.Join(dir, "2-net", "applications.auto.tfvars"))
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(hosts), "quill") {
					t.Errorf("2-net/applications.auto.tfvars carries quill's hosts at its identities:\n%s", hosts)
				}
				if !strings.Contains(string(placement), "\"registration\": {\n    \"quill\": \"identities\"\n  }") {
					t.Errorf("placement.json does not record quill at its identities:\n%s", placement)
				}
				if code, out := runOrg(t, "org", "check", "--dir", dir); code != 0 {
					t.Errorf("org check after register: %d %s", code, out)
				}

				return code, out
			},
			wantOut: []string{
				"Registered quill-service under the code quill in", "at its identities; rendered", "1-org/public-invokers.auto.tfvars among them", "Register through the Cloud Build triggers, as pull requests into master",
				"1. This render, quill-service at its identities: its repository",
				"2. The apply trigger by hand with 2-env, in a terminal:\n     gcloud builds triggers run imp-boot-uc1-layers-apply --region=us-central1 --project=imp-boot-gbl-core-a1b2 --branch=master --substitutions=_LAYER=2-env\n     tst and stg grant the next environment's deploy identity read on their records\n     bucket, from state the first pass did not have.",
				"3. bedrock org register quill --next, then the pull request it renders: the public-invoker\n     tag (1-org), the registry grants (2-shr) and the database admins (2-spn)",
				"5. bedrock org register quill --next once more, then the pull request it renders: the\n     hostnames (2-net)",
				"\n  6. After 2-net's apply: bedrock domain check, which passes when the apps domain and each vanity domain resolve to their zones.\n",
			},
		},
		{
			name: "render says what the triggers still lack",
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
			wantOut: []string{"69 owned file(s) written", "The triggers cannot run every layer yet: record projects.boot in placement.json and run bedrock org render."},
		},
		{
			name: "render says what the triggers' runs still lack: the pin and the authorization",
			steps: func(t *testing.T, dir string) (int, string) {
				t.Helper()
				if code, out := runOrg(t, "org", "new", dir); code != 0 {
					t.Fatalf("org new: %d %s", code, out)
				}
				editPlacement(t, dir, func(p *org.Placement) {
					p.BedrockVersion, p.BedrockSHA256 = "", ""
					p.CloudBuildInstallationID, p.CloudBuildTokenSecretVersion = "", ""
				})

				return runOrg(t, "org", "render", "--dir", dir)
			},
			wantOut: []string{
				"placement.json pins no bedrock (bedrockVersion), and the triggers' runs fetch the pinned one: run bedrock upgrade in this repository, which records the pin and renders.\n",
				"0-bootstrap renders the Cloud Build connection and the triggers once placement.json records cloudBuildInstallationId and cloudBuildTokenSecretVersion, from the GitHub authorization in the boot project (0-bootstrap/README.md, \"The Cloud Build GitHub authorization\").\n",
			},
		},
		{
			name: "new says when the running bedrock cannot be pinned",
			steps: func(t *testing.T, dir string) (int, string) {
				t.Helper()
				d := orgDeps(dir)
				d.version = func() build {
					return build{version: develVersion, kind: buildDevel}
				}
				out, err := execute(d, "", "org", "new", dir)
				if err != nil {
					t.Fatalf("org new error = %v; output:\n%s", err, out)
				}

				return 0, out
			},
			wantOut: []string{"This bedrock is (devel), which no placement can pin: ", "placement.json pins no bedrock yet, and the triggers' runs need one; bedrock upgrade in the repository records a release or an installed commit and renders with it.\n"},
		},
		{
			name: "render refuses a placement pinned to another bedrock",
			steps: func(t *testing.T, dir string) (int, string) {
				t.Helper()
				if code, out := runOrg(t, "org", "new", dir); code != 0 {
					t.Fatalf("org new: %d %s", code, out)
				}
				editPlacement(t, dir, func(p *org.Placement) {
					p.BedrockVersion, p.BedrockSHA256 = "v0.5.0", strings.Repeat("0", 64)
				})
				_, err := execute(orgDeps(dir), "", "org", "render", "--dir", dir)
				if err == nil {
					t.Fatal("org render ran on a placement pinned to another bedrock")
				}

				return 0, err.Error()
			},
			wantOut: []string{"placement.json pins bedrock v0.5.0 and this is bedrock " + headVersion + ": install the pinned one (https://github.com/cccteam/ccc/releases/tag/bedrock/v0.5.0) or move the pin (bedrock upgrade)"},
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
			wantOut:  []string{"2-shr/registry.tf: line 1 differs", "1 finding(s) against the 69 owned file(s) the placement renders"},
		},
		{
			name: "check reports a .tf file added by hand",
			steps: func(t *testing.T, dir string) (int, string) {
				t.Helper()
				if code, out := runOrg(t, "org", "new", dir); code != 0 {
					t.Fatalf("org new: %d %s", code, out)
				}
				if err := os.WriteFile(filepath.Join(dir, "2-env", "extra.tf"), []byte("# by hand\n"), 0o600); err != nil {
					t.Fatal(err)
				}

				return runOrg(t, "org", "check", "--dir", dir)
			},
			wantCode: 1,
			wantOut: []string{
				"2-env/extra.tf: not rendered by bedrock: every .tf file of a layer comes from the placement, and a file added by hand would run as the layer's identity; move what it declares into the placement, or remove it\n",
				"1 finding(s) against the 69 owned file(s) the placement renders\n",
			},
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
// organization's placement, the running bedrock and the checkout's module path, writing
// it into the checkout, and reads back through derive as render reads it; it refuses,
// changing nothing, a missing checkout, one whose module is not a repository of the
// organization, one that already holds a placement, an organization whose projects are
// not all recorded, and a bedrock no placement can pin.
func TestOrgRegisterPlacement(t *testing.T) {
	t.Parallel()

	src, sum := releaseServer(t)
	tests := []struct {
		name string
		// running is the bedrock running register; edit changes the organization's
		// placement before it; checkout is the second argument: "none" for none, "new"
		// for a checkout whose module names the organization's repository, "held" for one
		// already holding a placement, "absent" for a path that does not exist, "vanity"
		// for a module outside the organization, "nomod" for a checkout without a go.mod.
		running  build
		edit     func(p *org.Placement)
		checkout string
		wantPin  [2]string
		wantOut  []string
		wantErr  string
	}{
		{
			name:     "written into the checkout, pinned to an installed commit",
			running:  build{version: headVersion, kind: buildInstalled},
			checkout: "new",
			wantPin:  [2]string{headVersion, ""},
			wantOut:  []string{"Wrote ", "quill-service's first placement, pinned to bedrock " + headVersion + ": commit it in the application's repository"},
		},
		{
			name:     "written into the checkout, pinned to a release with its checksum",
			running:  build{version: "v0.4.0", kind: buildStamped},
			checkout: "new",
			wantPin:  [2]string{"v0.4.0", sum},
			wantOut:  []string{"Wrote ", "quill-service's first placement, pinned to bedrock v0.4.0: commit it in the application's repository"},
		},
		{
			name:     "no checkout",
			running:  build{version: headVersion, kind: buildInstalled},
			checkout: "none",
			wantErr:  "register quill needs the application's checkout as its second argument: its go.mod names the repository, and its first placement is written there",
		},
		{
			name:     "a checkout that holds a placement already",
			running:  build{version: headVersion, kind: buildInstalled},
			checkout: "held",
			wantErr:  "exists: register writes an application's first placement and never overwrites one",
		},
		{
			name:     "a checkout that is not there",
			running:  build{version: headVersion, kind: buildInstalled},
			checkout: "absent",
			wantErr:  "the second argument is the application's checkout, whose go.mod names its repository and where register writes its first placement (infrastructure/placement.json)",
		},
		{
			name:     "a checkout whose module is not a repository of the organization",
			running:  build{version: headVersion, kind: buildInstalled},
			checkout: "vanity",
			wantErr:  `the module path "example.com/acme/quill" (`,
		},
		{
			name:     "a checkout without a go.mod",
			running:  build{version: headVersion, kind: buildInstalled},
			checkout: "nomod",
			wantErr:  "no go.mod at",
		},
		{
			name:     "the environments' project numbers not recorded",
			running:  build{version: headVersion, kind: buildInstalled},
			checkout: "new",
			edit: func(p *org.Placement) {
				p.ProjectNumbers = map[string]string{"boot": "100000000001", "tst": "100000000002"}
			},
			wantErr: "placement.json records no project id and number for stg and prd yet: record 1-org's project_ids and project_numbers outputs there (projects, projectNumbers)",
		},
		{
			name:     "no state bucket recorded",
			running:  build{version: headVersion, kind: buildInstalled},
			checkout: "new",
			edit: func(p *org.Placement) {
				p.StateBucket = ""
			},
			wantErr: "placement.json records no stateBucket yet",
		},
		{
			name:     "a build from a checkout",
			running:  build{version: headVersion + "+dirty", kind: buildCheckout},
			checkout: "new",
			wantErr:  "this bedrock is " + headVersion + "+dirty, which no placement can pin, and an application's first placement pins the bedrock that writes it: register with a released bedrock",
		},
		{
			name:     "a devel build",
			running:  build{version: develVersion, kind: buildDevel},
			checkout: "new",
			wantErr:  "this bedrock is (devel), which no placement can pin",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := orgRepo(t)
			if code, out := runOrg(t, "org", "new", dir); code != 0 {
				t.Fatalf("org new: %d %s", code, out)
			}
			editPlacement(t, dir, func(p *org.Placement) {
				p.BedrockVersion, p.BedrockSHA256 = "", ""
			})
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
				checkout = appCheckout(t, "github.com/"+orgOrganization+"/quill-service")
				args = append(args, checkout)
			case "vanity":
				checkout = appCheckout(t, "example.com/acme/quill")
				args = append(args, checkout)
			case "nomod":
				checkout = appCheckout(t, "")
				args = append(args, checkout)
			case "absent":
				checkout = filepath.Join(t.TempDir(), "quill")
				args = append(args, checkout)
			}
			if tt.checkout == "vanity" {
				tt.wantErr += filepath.Join(checkout, "go.mod") + ") is not a repository of the " + orgOrganization + " GitHub organization (github.com/" + orgOrganization + "/<repository>): a vanity import path is not supported"
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
			p, err := derive.ReadPlacement(filepath.Join(checkout, "infrastructure", "placement.json"))
			if err != nil {
				t.Fatalf("derive.ReadPlacement() error = %v", err)
			}
			want := &derive.Placement{
				Prefix: "imp", Environments: []string{"tst", "stg", "prd"},
				Regions:    []derive.Region{{Name: "us-central1", Code: "uc1"}, {Name: "us-west3", Code: "uw3"}},
				AppsDomain: "apps.imp.example", HostedDomain: "imp.example", StateBucket: "imp-boot-gbl-state-a1b2",
				PlaceholderImage: derive.PlaceholderImage, DefaultBranch: "master", Code: "quill", Repository: "quill-service", ReleaseApp: "imp-release",
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
			orgFile := filepath.Join(dir, "placement.json")
			p, err := org.ReadPlacement(orgFile)
			if err != nil {
				t.Fatal(err)
			}
			// The fixtures' modules are github.com/impulseframework/<app>, which register
			// reads the repository from.
			p.GithubOrganization = "impulseframework"
			p.Applications = nil
			for _, other := range tt.others {
				p.Applications = append(p.Applications, org.Application{Code: other, Repository: other})
			}
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
		{name: "a code of the wrong shape", app: "Harbor", wantErr: `application code "Harbor" is not 1 to 6 lowercase letters and digits starting with a letter`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := orgRepo(t)
			d := deps{domains: noCloudDomains, secrets: noSecretManager, projects: noProjects, cwd: dir, interactive: never}
			checkout := appCheckout(t, "github.com/"+orgOrganization+"/harbor-again")
			if _, err := execute(d, "", "org", "register", tt.app, "--dir", dir, checkout); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Execute() error = %v, wantErr %q", err, tt.wantErr)
			}
			placement, err := os.ReadFile(filepath.Join(dir, "placement.json"))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Count(string(placement), `"code": "harbor"`) != 1 || strings.Contains(string(placement), "harbor-again") || strings.Contains(string(placement), "Harbor") {
				t.Errorf("placement.json changed on a refusal:\n%s", placement)
			}
		})
	}
}

// TestOrgRegisterNext: register --next moves a registration on once the state the step
// needs is in the bucket, renders what the step may apply, and refuses before: the grants
// wait for 2-env's state to hold the identities in every environment, the completion for
// the stack's state in every environment, and a registration in full has no step left.
func TestOrgRegisterNext(t *testing.T) {
	t.Parallel()

	envState := func(apps ...string) string {
		entries := make([]string, 0, len(apps))
		for _, app := range apps {
			entries = append(entries, fmt.Sprintf(`%q: {"apply_identity_email": "imp-tst-gbl-%s-tofu@imp-tst-gbl-core-1a2b.iam.gserviceaccount.com"}`, app, app))
		}

		return `{"outputs": {"applications": {"value": {` + strings.Join(entries, ", ") + `}}}}`
	}
	// stackState is the stack's state in an environment: the hostnames it serves (the
	// completion records them) and its net_hosts entries.
	stackState := func(env string) string {
		host := "quill-" + env + ".apps.imp.example"
		if env == "prd" {
			host = "quill.apps.imp.example"
		}

		stem := "projects/imp-" + env + "-gbl-core-0000/global/backendServices/imp-" + env + "-gbl-quill"

		return fmt.Sprintf(`{"outputs": {"hostnames": {"value": [%q]}, "net_hosts": {"value": {%q: %q, %q: %q}}}}`, host, host, stem+"-backend", org.NextHostname(host), stem+"-next-backend")
	}
	const (
		bucket  = "imp-boot-gbl-state-a1b2"
		host    = `"quill-tst.apps.imp.example"`
		invoker = `"serviceAccount:imp-tst-gbl-quill-tofu@imp-tst-gbl-core-1a2b.iam.gserviceaccount.com",`
	)
	type step struct {
		// objects are added to the store before the run.
		objects map[string]string
		args    []string
		// wantCode and wantOut are the run's; wantFiles and wantAbsent are lines the
		// rendered files must and must not carry after it.
		wantCode   int
		wantOut    []string
		wantFiles  map[string][]string
		wantAbsent map[string][]string
	}
	// stackTriggers are the commands the grants step prints while GitHub lists no
	// operations workflow: the stack trigger 2-env made in each environment, started as
	// the workflow would start it.
	const stackTriggers = "  gcloud builds triggers run imp-tst-uc1-quill-stack --branch=master --region=us-central1 --project=imp-tst-gbl-core-1a2b\n" +
		"  gcloud builds triggers run imp-stg-uc1-quill-stack --branch=master --region=us-central1 --project=imp-stg-gbl-core-3c4d\n" +
		"  gcloud builds triggers run imp-prd-uc1-quill-stack --branch=master --region=us-central1 --project=imp-prd-gbl-core-5e6f\n"
	tests := []struct {
		name string
		// workflows are the paths GitHub lists for the repository; pushed gives it a
		// default branch, so it is not empty; noRepo leaves the repository off the
		// stand-in, so the list is not found.
		workflows []string
		pushed    bool
		noRepo    bool
		steps     []step
	}{
		{
			name:      "the grants wait for the identities, the completion for the stacks",
			workflows: []string{".github/workflows/ci.yml", ".github/workflows/operations.yml"},
			pushed:    true,
			steps: []step{
				{
					args: []string{"org", "register", "quill", "--next"}, wantCode: 1,
					wantOut: []string{"quill-portal's identities are not in 2-env tst's state yet: merge the registration's pull request and start the apply trigger with 2-env (the sequence's step 2) before moving on"},
				},
				{
					objects: map[string]string{"2-env/tst/default.tfstate": envState("harbor", "quill"), "2-env/stg/default.tfstate": envState("quill")},
					args:    []string{"org", "register", "quill", "--next"}, wantCode: 1,
					wantOut: []string{"quill-portal's identities are not in 2-env prd's state yet"},
				},
				{
					objects: map[string]string{"2-env/prd/default.tfstate": envState("quill")},
					args:    []string{"org", "register", "quill", "--next"},
					wantOut: []string{
						"Moved quill-portal's registration to its grants in", "The pull request now carries 1-org/public-invokers.auto.tfvars, 2-shr/applications.auto.tfvars\nand 2-spn/applications.auto.tfvars",
						"After its merge and apply: quill-portal's own stack,\napplied for the first time in each environment through its operations workflow, which GitHub lists: Actions,\noperations, Run workflow, with the action apply and the environment, tst, stg, prd in that order (or\ngh workflow run operations.yml --repo imp-example/quill-portal -f action=apply -f environment=<env>; its\ninfrastructure/README.md, Applying). Then\nbedrock org register quill --next.\n",
					},
					wantFiles:  map[string][]string{"1-org/public-invokers.auto.tfvars": {invoker}, "placement.json": {"\"quill\": \"grants\""}},
					wantAbsent: map[string][]string{"2-net/applications.auto.tfvars": {host}},
				},
				{
					args: []string{"org", "register", "quill", "--next"}, wantCode: 1,
					wantOut: []string{"quill-portal's stack has no state in tst yet (3-app/quill/tst): its first apply there comes first (the sequence's step 4, as the grants step's output said) before moving on"},
				},
				{
					objects: map[string]string{"3-app/quill/tst/default.tfstate": stackState("tst"), "3-app/quill/stg/default.tfstate": stackState("stg"), "3-app/quill/prd/default.tfstate": `{"outputs": {}}`},
					args:    []string{"org", "register", "quill", "--next"}, wantCode: 1,
					wantOut: []string{"quill-portal's stack has no state in prd yet"},
				},
				{
					// A stack whose state carries no net_hosts output cannot complete: the
					// organization records what is served and invents nothing.
					objects: map[string]string{"3-app/quill/prd/default.tfstate": `{"outputs": {"hostnames": {"value": ["quill.apps.imp.example"]}}}`},
					args:    []string{"org", "register", "quill", "--next"}, wantCode: 1,
					wantOut: []string{"quill-portal's stack state in prd (3-app/quill/prd) has no net_hosts output: apply the stack there (the operations workflow's apply action) before recording its hostnames"},
				},
				{
					objects:    map[string]string{"3-app/quill/prd/default.tfstate": stackState("prd")},
					args:       []string{"org", "register", "quill", "--next"},
					wantOut:    []string{"Completed quill-portal's registration in", "its hostnames recorded from its stack's state (tst: quill-tst.apps.imp.example; stg: quill-stg.apps.imp.example; prd: quill.apps.imp.example)", "The pull request now carries 2-net/applications.auto.tfvars and 1-org/applications.auto.tfvars:\nthe hostnames"},
					wantFiles:  map[string][]string{"2-net/applications.auto.tfvars": {host, `"quill-next.apps.imp.example"`}, "1-org/public-invokers.auto.tfvars": {invoker}, "placement.json": {`"hostnames": {`, `"quill-stg.apps.imp.example"`}},
					wantAbsent: map[string][]string{"placement.json": {"registration"}},
				},
				{
					args: []string{"org", "register", "quill", "--next"}, wantCode: 1,
					wantOut: []string{`application "quill" is registered in full: there is no step left`},
				},
			},
		},
		{
			name:      "the stack trigger by hand while GitHub lists no operations workflow",
			workflows: []string{".github/workflows/ci.yml"},
			pushed:    true,
			steps: []step{{
				objects: map[string]string{"2-env/tst/default.tfstate": envState("quill"), "2-env/stg/default.tfstate": envState("quill"), "2-env/prd/default.tfstate": envState("quill")},
				args:    []string{"org", "register", "quill", "--next"},
				wantOut: []string{
					"Moved quill-portal's registration to its grants in",
					"After its merge and apply: quill-portal's own stack,\napplied for the first time in each environment. GitHub does not list its operations workflow yet: a\nworkflow is listed once a push of at most twenty commits adds or changes its file on the\ndefault branch, or once it runs, and the repository's first push carried more, so the\nworkflow's apply action cannot start the stack trigger for this apply. Start the trigger as\nthe workflow would, in a terminal, as the operator, tst, stg, prd in that order; in stg and prd, whose\nbuilds wait for an approval, approve the build in Cloud Build:\n" + stackTriggers +
						"A pull request that changes .github/workflows/operations.yml lists the workflow for every apply after this one. Then\nbedrock org register quill --next.\n",
				},
				wantFiles: map[string][]string{"placement.json": {"\"quill\": \"grants\""}},
			}},
		},
		{
			name: "the first push comes first while the repository is empty",
			steps: []step{{
				objects: map[string]string{"2-env/tst/default.tfstate": envState("quill"), "2-env/stg/default.tfstate": envState("quill"), "2-env/prd/default.tfstate": envState("quill")},
				args:    []string{"org", "register", "quill", "--next"},
				wantOut: []string{
					"Moved quill-portal's registration to its grants in",
					"After its merge and apply: quill-portal's own stack,\napplied for the first time in each environment. The repository is empty: the application's first push comes first, to master, the default\nbranch the stack trigger builds, with its stack rendered there (bedrock render) from its\nfirst placement.json. A first push of at most twenty commits lists the operations workflow\n(GitHub lists a workflow once a push of at most twenty commits adds or changes its file on\nthe default branch, or once it runs), and the stack's first apply then starts through the\nworkflow's apply action: Actions,\noperations, Run workflow, with the action apply and the environment, tst, stg, prd in that order (or\ngh workflow run operations.yml --repo imp-example/quill-portal -f action=apply -f environment=<env>; its\ninfrastructure/README.md, Applying). A first push of more lists no workflow until a pull request\nchanges its file, so the workflow's apply action cannot start the stack trigger for this\napply: start the trigger as the workflow would, in a terminal, as the operator,\ntst, stg, prd in that order; in stg and prd, whose\nbuilds wait for an approval, approve the build in Cloud Build:\n" + stackTriggers +
						"A pull request that changes .github/workflows/operations.yml lists the workflow for every apply after this one. Then\nbedrock org register quill --next.\n",
				},
				wantFiles: map[string][]string{"placement.json": {"\"quill\": \"grants\""}},
			}},
		},
		{
			name:   "a workflow list that cannot be read moves nothing",
			noRepo: true,
			steps: []step{{
				objects: map[string]string{"2-env/tst/default.tfstate": envState("quill"), "2-env/stg/default.tfstate": envState("quill"), "2-env/prd/default.tfstate": envState("quill")},
				args:    []string{"org", "register", "quill", "--next"}, wantCode: 1,
				wantOut:   []string{"reading imp-example/quill-portal's workflows, which decide how the stack's first apply starts; nothing moved", "answered 404: Not Found"},
				wantFiles: map[string][]string{"placement.json": {"\"quill\": \"identities\""}},
			}},
		},
		{
			name: "a checkout with --next is refused",
			steps: []step{{
				args: []string{"org", "register", "quill", "../quill", "--next"}, wantCode: 1,
				wantOut: []string{"--next moves a registration on and writes no first placement: leave the checkout out"},
			}},
		},
		{
			name: "an application not registered is refused",
			steps: []step{{
				args: []string{"org", "register", "nobody", "--next"}, wantCode: 1,
				wantOut: []string{`application "nobody" is not registered (bedrock org register nobody)`},
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := orgRepo(t)
			if code, out := runOrg(t, "org", "new", dir); code != 0 {
				t.Fatalf("org new: %d %s", code, out)
			}
			if code, out := runOrg(t, "org", "register", "quill", "--dir", dir, appCheckout(t, "github.com/"+orgOrganization+"/quill-portal")); code != 0 {
				t.Fatalf("org register: %d %s", code, out)
			}
			store := &memoryStore{objects: map[string]string{}}
			server := githubtest.New(t)
			if !tt.noRepo {
				repo := &githubtest.Repo{Workflows: tt.workflows}
				if tt.pushed {
					repo.Refs = map[string]github.Object{"refs/heads/master": {Type: "commit", SHA: "c1"}}
				}
				server.AddRepo(orgOrganization, "quill-portal", repo)
			}
			d := orgDeps(dir)
			d.deploy = &deploy.Clients{Storage: store.open}
			d.github = func(context.Context) (*github.Client, error) {
				return server.Client(), nil
			}
			for i, st := range tt.steps {
				for object, data := range st.objects {
					store.objects["gs://"+bucket+"/"+object] = data
				}
				args := append(append([]string{}, st.args...), "--dir", dir)
				// A refusal is the command's error, which the root prints and exits 1 on.
				out, err := execute(d, "", args...)
				code := 0
				if err != nil {
					code = 1
					out += err.Error()
				}
				if code != st.wantCode {
					t.Fatalf("step %d: code = %d, want %d; output:\n%s", i, code, st.wantCode, out)
				}
				for _, want := range st.wantOut {
					if !strings.Contains(out, want) {
						t.Errorf("step %d: output lacks %q:\n%s", i, want, out)
					}
				}
				for path, lines := range st.wantFiles {
					data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(path)))
					if err != nil {
						t.Fatal(err)
					}
					for _, line := range lines {
						if !strings.Contains(string(data), line) {
							t.Errorf("step %d: %s lacks %q:\n%s", i, path, line, data)
						}
					}
				}
				for path, lines := range st.wantAbsent {
					data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(path)))
					if err != nil {
						t.Fatal(err)
					}
					for _, line := range lines {
						if strings.Contains(string(data), line) {
							t.Errorf("step %d: %s carries %q", i, path, line)
						}
					}
				}
				if st.wantCode == 0 {
					if code, out := runOrg(t, "org", "check", "--dir", dir); code != 0 {
						t.Errorf("step %d: org check: %d %s", i, code, out)
					}
				}
			}
		})
	}
}

// destroyTofu is tofu in org destroy's tests: it records every command by the layer it
// ran in and answers each layer's state list with one project, the repository in 1-org.
type destroyTofu struct {
	ran []string
}

func (f *destroyTofu) Run(_ context.Context, c deploy.Command, _ io.Writer) error {
	f.ran = append(f.ran, filepath.Base(c.Dir)+": "+c.String())

	return nil
}

func (f *destroyTofu) Output(_ context.Context, c deploy.Command, _ io.Writer) ([]byte, error) {
	f.ran = append(f.ran, filepath.Base(c.Dir)+": "+c.String())
	switch {
	case c.Args[1] == "pull":
		return []byte(`{"version": 4}`), nil
	case filepath.Base(c.Dir) == "1-org":
		return []byte("github_repository.infrastructure\nmodule.project[\"tst\"].module.project-factory.google_project.main\n"), nil
	default:
		return []byte("module.boot_project.module.project-factory.google_project.main\n"), nil
	}
}

// destroyRepo is the fixture organization's placement with the two outer layers' files
// the destroy reads (the variables declaring destroy, the backend) and, when asked, a
// network placement registering a domain.
func destroyRepo(t *testing.T, registered bool) string {
	t.Helper()

	dir := orgRepo(t)
	files := map[string]string{
		"0-bootstrap/variables.tf":  "variable \"destroy\" {\n  type    = bool\n  default = false\n}\n",
		"0-bootstrap/initialize.tf": "terraform {\n  backend \"gcs\" {}\n}\n",
		"1-org/variables.tf":        "variable \"destroy\" {\n  type    = bool\n  default = false\n}\n",
		"JOURNAL.md":                "# Journal\n",
	}
	if registered {
		files["2-net/terraform.tfvars"] = "registrations = {\n  \"example.dev\" = {\n    yearly_price_usd = 12\n  }\n}\n"
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

func TestOrgDestroy(t *testing.T) {
	t.Parallel()

	const warning = "bedrock org destroy deletes every project of the imp organization"
	tests := []struct {
		name        string
		registered  bool
		interactive func() bool
		in          string
		args        []string
		wantErr     string
		wantOut     []string
		wantRan     int
		wantJournal string
	}{
		{
			name:        "refuses without a terminal and without --yes",
			interactive: never,
			wantErr:     "asks for the prefix typed back at the terminal, and this run has none: --yes stands in for it",
			wantOut:     []string{warning, "imp-tst-gbl-core-1a2b (tst): every application's tst services, jobs, secrets, records and files, and the tst Spanner instance"},
		},
		{
			name:        "stops when something else than the prefix is typed",
			interactive: always,
			in:          "no\n",
			wantOut:     []string{"Type the prefix (imp) to go on; anything else stops:", "Stopped: nothing was deleted."},
		},
		{
			name:        "destroys when the prefix is typed, and writes the journal",
			interactive: always,
			in:          "imp\n",
			wantOut:     []string{warning, "Took github_repository.infrastructure out of 1-org's state", "Destroyed 1-org.", "Destroyed 0-bootstrap.", "Deleted the imp organization's projects; a line went into JOURNAL.md.", "To start over: a new seed"},
			wantRan:     11,
			wantJournal: "bedrock org destroy deleted the organization's projects (imp-boot-gbl-core-a1b2, imp-shr-gbl-core-7a8b, imp-net-gbl-core-9c0d, imp-spn-gbl-core-2e3f, imp-tst-gbl-core-1a2b, imp-stg-gbl-core-3c4d, imp-prd-gbl-core-5e6f): Google holds them for 30 days, their ids never return. The GitHub repositories stay.\n",
		},
		{
			name:        "keeps the network project when the layer registers a domain, with --yes",
			registered:  true,
			interactive: never,
			args:        []string{"--yes"},
			wantOut:     []string{"Kept: the network project imp-net-gbl-core-9c0d, with its folder and the organization's, since 2-net registers example.dev there", "The network project imp-net-gbl-core-9c0d stays with its registrations, for a person."},
			wantRan:     11,
			wantJournal: "(imp-boot-gbl-core-a1b2, imp-shr-gbl-core-7a8b, imp-spn-gbl-core-2e3f, imp-tst-gbl-core-1a2b, imp-stg-gbl-core-3c4d, imp-prd-gbl-core-5e6f): Google holds them for 30 days, their ids never return. The GitHub repositories stay. The network project imp-net-gbl-core-9c0d was kept with its registrations, for a person.\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := destroyRepo(t, tt.registered)
			tofu := &destroyTofu{}
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
			if len(tofu.ran) != tt.wantRan {
				t.Errorf("%d tofu commands ran, want %d:\n%s", len(tofu.ran), tt.wantRan, strings.Join(tofu.ran, "\n"))
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

// TestOrgRegisterCapacity: register refuses the application the placement has no room
// for on the shared Spanner instance (the registered applications, one database each in
// stg and prd, against ten databases for every hundred processing units 2-spn declares;
// 100 when 2-spn's terraform.tfvars does not set them), naming the setting that grows it
// and the other way out, with the placement unchanged; a larger instance declared there
// has the room.
func TestOrgRegisterCapacity(t *testing.T) {
	t.Parallel()

	const refused = "register sixth: the placement declares 12 databases on the shared Spanner instance (6 applications, one database each in stg and prd), and the instance holds 10 at 100 processing units (ten for every hundred below a node): raise processing_units in 2-spn/terraform.tfvars (100 or 200; beyond 200 is a change to the variable's validation, the decision of 2026-09-25 in JOURNAL.md), or bedrock destroy an application the organization no longer needs, which frees its databases"
	tests := []struct {
		name string
		// spn is 2-spn/terraform.tfvars, none when empty (the variable's default).
		spn     string
		wantErr string
	}{
		{name: "the sixth application on the smallest instance is refused", wantErr: refused},
		{name: "two hundred processing units declared in 2-spn hold it", spn: "processing_units = 200\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := orgRepo(t)
			editPlacement(t, dir, func(p *org.Placement) {
				for i := len(p.Applications); i < 5; i++ {
					code := "app" + strconv.Itoa(i)
					p.Applications = append(p.Applications, org.Application{Code: code, Repository: code})
				}
			})
			if tt.spn != "" {
				if err := os.MkdirAll(filepath.Join(dir, "2-spn"), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "2-spn", "terraform.tfvars"), []byte(tt.spn), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			d := orgDeps(dir)
			checkout := appCheckout(t, "github.com/"+orgOrganization+"/sixth")
			_, err := execute(d, "", "org", "register", "sixth", "--dir", dir, checkout)
			placement, readErr := os.ReadFile(filepath.Join(dir, "placement.json"))
			if readErr != nil {
				t.Fatal(readErr)
			}
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Execute() error = %v, wantErr %q", err, tt.wantErr)
				}
				if strings.Contains(string(placement), `"sixth"`) {
					t.Errorf("placement.json changed on a refusal:\n%s", placement)
				}

				return
			}
			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if !strings.Contains(string(placement), `"code": "sixth"`) {
				t.Errorf("placement.json does not carry the sixth application:\n%s", placement)
			}
		})
	}
}

// TestOrgCheckCapacity: org check says, without failing, when the placement declares
// more databases on the shared Spanner instance than it holds, and nothing while it fits.
func TestOrgCheckCapacity(t *testing.T) {
	t.Parallel()

	const notice = "Spanner capacity: the placement declares 12 databases on the shared Spanner instance (6 applications, one database each in stg and prd), and the instance holds 10 at 100 processing units"
	tests := []struct {
		name         string
		applications int
		wantNotice   bool
	}{
		{name: "past the instance: said", applications: 6, wantNotice: true},
		{name: "within it: nothing said", applications: 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := orgRepo(t)
			editPlacement(t, dir, func(p *org.Placement) {
				for i := len(p.Applications); i < tt.applications; i++ {
					code := "app" + strconv.Itoa(i)
					p.Applications = append(p.Applications, org.Application{Code: code, Repository: code})
				}
			})
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
		})
	}
}
