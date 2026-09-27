package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/deploy"
	"github.com/cccteam/ccc/bedrock/internal/domain"
	"github.com/cccteam/ccc/bedrock/internal/github"
	"github.com/cccteam/ccc/bedrock/internal/secret"
	"github.com/cccteam/ccc/bedrock/internal/where"
)

const (
	fixtureApp   = "../derive/testdata/harbor"
	placement    = "../derive/testdata/placement.json"
	fixtureInfra = "../domain/testdata/repo"
	// fixtureFlat is a repository with the layers at its root and one application,
	// quill; fixtureNested one with the layers under infrastructure/ and two.
	fixtureFlat   = "../where/testdata/flat"
	fixtureNested = "../where/testdata/nested"
	// fixtureAppRepo is an application repository carrying its own stack under
	// infrastructure/, the application named by its go.mod.
	fixtureAppRepo = "../where/testdata/apprepo"
)

// run executes the command tree with the arguments and returns the exit code and the
// standard output.
func run(t *testing.T, args ...string) (code int, out string) {
	t.Helper()

	var buf bytes.Buffer
	root := newRoot()
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		var exit exitError
		if !asExit(err, &exit) {
			t.Fatalf("Execute(%v) error = %v", args, err)
		}
		code = exit.code
	}

	return code, buf.String()
}

func TestRenderThenCheck(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		afterFunc func(t *testing.T, dir string)
		wantCode  int
		wantOut   []string
	}{
		{
			name:      "a fresh render checks clean",
			afterFunc: func(*testing.T, string) {},
			wantCode:  0,
			wantOut:   []string{"13 owned file(s) match the code"},
		},
		{
			name: "an edited owned file fails the check",
			afterFunc: func(t *testing.T, dir string) {
				t.Helper()

				if err := os.WriteFile(filepath.Join(dir, "outputs.tf"), []byte("# nothing\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			wantCode: 1,
			wantOut:  []string{"differs  outputs.tf:1"},
		},
		{
			name: "an edited seeded file is left alone",
			afterFunc: func(t *testing.T, dir string) {
				t.Helper()

				if err := os.WriteFile(filepath.Join(dir, "terraform.tfvars"), []byte("state_bucket = \"mine\"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			wantCode: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := filepath.Join(t.TempDir(), "stack")
			app := copyRepo(t, fixtureApp)
			code, out := run(t, "render", "--app", app, "--out", dir, "--placement", placement)
			if code != 0 || !strings.Contains(out, "Rendered the harbor stack") || !strings.Contains(out, "Seeded terraform.tfvars") || !strings.Contains(out, "Rendered the pipeline into "+app+": cloudbuild-sweep.yaml, cloudbuild.yaml.") {
				t.Fatalf("render exit = %d, output:\n%s", code, out)
			}
			tt.afterFunc(t, dir)
			code, out = run(t, "check", "--app", app, "--dir", dir, "--placement", placement)
			if code != tt.wantCode {
				t.Errorf("check exit = %d, want %d; output:\n%s", code, tt.wantCode, out)
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out, want) {
					t.Errorf("check output lacks %q:\n%s", want, out)
				}
			}
		})
	}
}

// appRepo builds an application-layout repository in a temporary directory: the harbor
// fixture as the application at its root, marked a repository, with the placement in its
// infrastructure directory and nothing else there yet, as before the first render.
func appRepo(t *testing.T) string {
	t.Helper()

	dir := copyRepo(t, fixtureApp)
	infra := filepath.Join(dir, "infrastructure")
	if err := os.Mkdir(infra, 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(placement)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(infra, placementFile), data, 0o600); err != nil {
		t.Fatal(err)
	}

	return dir
}

// bareApp is the harbor fixture marked a repository, with no infrastructure directory.
func bareApp(t *testing.T) string {
	t.Helper()

	return copyRepo(t, fixtureApp)
}

// flatRepo is the organization layout with one application layer, quill.
func flatRepo(t *testing.T) string {
	t.Helper()

	return copyRepo(t, fixtureFlat)
}

// nestedRepo is the organization layout under infrastructure/ with two applications.
func nestedRepo(t *testing.T) string {
	t.Helper()

	return copyRepo(t, fixtureNested)
}

// noRepo is a directory outside every repository.
func noRepo(t *testing.T) string {
	t.Helper()

	return t.TempDir()
}

func TestStackFromLayout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// repo builds the repository and cwd is where the commands run, inside it.
		repo func(t *testing.T) string
		cwd  string
		// withApp names the harbor fixture with --app and its placement with
		// --placement; outside puts the stack in a directory outside the repository
		// with --out and --dir, and names the placement.
		withApp bool
		outside bool
		// wantStack is where the stack lands, relative to the repository (ignored with
		// outside).
		wantStack string
		wantErr   string
	}{
		{name: "the application repository, from inside it", repo: appRepo, cwd: filepath.Join("pkg", "config"), wantStack: "infrastructure"},
		{name: "the application repository, from its root", repo: appRepo, cwd: ".", wantStack: "infrastructure"},
		{name: "--out overrides the stack directory, the application still from the repository", repo: appRepo, cwd: "pkg", outside: true},
		{name: "the organization layout, the application named", repo: flatRepo, cwd: filepath.Join("3-app", "quill"), withApp: true, wantStack: filepath.Join("3-app", "quill")},
		{name: "several application layers are refused", repo: nestedRepo, cwd: ".", withApp: true, wantErr: "several application layers under"},
		{name: "an application repository with no stack directory yet is refused", repo: bareApp, cwd: ".", wantErr: "is not the application's stack (a terraform.tfvars or a placement.json, and no layers): pass --dir"},
		{name: "a working directory outside every repository is refused", repo: noRepo, cwd: ".", wantErr: "no repository above"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := tt.repo(t)
			d := deps{domains: noCloudDomains, secrets: noSecretManager, projects: noProjects, cwd: filepath.Join(repo, tt.cwd), interactive: never}
			stack := filepath.Join(repo, tt.wantStack)
			renderArgs, checkArgs := []string{"render"}, []string{"check"}
			if tt.withApp {
				app := copyRepo(t, fixtureApp)
				renderArgs = append(renderArgs, "--app", app, "--placement", placement)
				checkArgs = append(checkArgs, "--app", app, "--placement", placement)
			}
			if tt.outside {
				stack = filepath.Join(t.TempDir(), "stack")
				renderArgs = append(renderArgs, "--out", stack, "--placement", placement)
				checkArgs = append(checkArgs, "--dir", stack, "--placement", placement)
			}
			out, err := execute(d, "", renderArgs...)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("render error = %v, wantErr %q; output:\n%s", err, tt.wantErr, out)
				}

				return
			}
			if err != nil {
				t.Fatalf("render error = %v; output:\n%s", err, out)
			}
			if want := "Rendered the harbor stack into " + stack + ":"; !strings.Contains(out, want) {
				t.Errorf("render output lacks %q:\n%s", want, out)
			}
			if _, err := os.Stat(filepath.Join(stack, "README.md")); err != nil {
				t.Errorf("the stack's README is not there: %v", err)
			}
			out, err = execute(d, "", checkArgs...)
			if err != nil {
				t.Fatalf("check error = %v; output:\n%s", err, out)
			}
			if want := "13 owned file(s) match the code"; !strings.Contains(out, want) {
				t.Errorf("check output lacks %q:\n%s", want, out)
			}
		})
	}
}

func TestPlacementDefault(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		place    bool
		wantCode int
		wantErr  string
	}{
		{name: "placement.json beside the stack", place: true, wantCode: 0},
		{name: "no placement anywhere", wantErr: "no placement at"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := filepath.Join(t.TempDir(), "stack")
			if tt.place {
				data, err := os.ReadFile(placement)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, placementFile), data, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			root := newRoot()
			root.SetOut(&bytes.Buffer{})
			root.SetArgs([]string{"render", "--app", copyRepo(t, fixtureApp), "--out", dir})
			err := root.Execute()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Execute() error = %v", err)
				}

				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Execute() error = %v, wantErr %q", err, tt.wantErr)
			}
		})
	}
}

// fixedParameters answers every question with the same parameters.
type fixedParameters struct {
	params *domain.RegisterParameters
}

func (f fixedParameters) RetrieveRegisterParameters(_ context.Context, name string) (*domain.RegisterParameters, error) {
	p := *f.params
	p.DomainName = name

	return &p, nil
}

// fixedClient opens fixedParameters for every project.
func fixedClient(params *domain.RegisterParameters) domain.ClientFunc {
	return func(context.Context, string) (domain.ParametersClient, error) {
		return fixedParameters{params: params}, nil
	}
}

// noCloudDomains refuses to open a Cloud Domains client, for tests that never register.
func noCloudDomains(context.Context, string) (domain.ParametersClient, error) {
	return nil, errors.New("no Cloud Domains in this test")
}

// noSecretManager refuses to open a Secret Manager client, for tests that never pin.
func noSecretManager(context.Context, string) (secret.Client, error) {
	return nil, errors.New("no Secret Manager in this test")
}

// noProjects refuses to open a Cloud Resource Manager client, for tests that never
// search.
func noProjects(context.Context) (where.ProjectClient, error) {
	return nil, errors.New("no Cloud Resource Manager in this test")
}

// never says no person is at the terminal; always says one is.
func never() bool {
	return false
}

func always() bool {
	return true
}

// copyRepo copies the fixture repository into a temporary directory and marks it a
// repository with an empty .git directory (a committed one would make the fixture a
// repository of its own).
func copyRepo(t *testing.T, fixture string) string {
	t.Helper()

	dir := filepath.Join(t.TempDir(), path.Base(fixture))
	if err := os.CopyFS(dir, os.DirFS(fixture)); err != nil {
		t.Fatalf("os.CopyFS() error = %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}

	return dir
}

// execute runs the command tree over the seams with the input on standard input, and
// returns what it printed and the error it ended with.
func execute(d deps, in string, args ...string) (out string, err error) {
	var buf bytes.Buffer
	root := newRootWith(d)
	root.SetIn(strings.NewReader(in))
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs(args)
	err = root.Execute()

	return buf.String(), err
}

func TestDomainAdd(t *testing.T) {
	t.Parallel()

	available := &domain.RegisterParameters{Availability: domain.Available, Currency: "USD", YearlyPrice: 12, Notices: []string{"HSTS_PRELOADED"}, SupportedPrivacy: []string{"REDACTED_CONTACT_DATA"}}
	placed := []string{
		"example.dev is available at 12 USD per year (asked through project lab-boot-1234).",
		"Notices to acknowledge: HSTS_PRELOADED (HTTPS only; the TLD is on the HSTS preload list).",
		"Added example.dev to",
		"The registrant contact in that file (registrant_contact) must name a mailbox a person reads",
		"Next: commit the change to the values file and open the pull request; the plan on it shows the purchase. The apply registers the domain and points it at the zone.",
	}
	placedFile := []string{`"example.dev" = {`, "yearly_price_usd = 12", `notices          = ["HSTS_PRELOADED"]`}
	tests := []struct {
		name string
		// args are the arguments after "domain add"; withDir adds --dir, else the
		// repository is found from cwd, a directory inside the copied fixture.
		args        []string
		withDir     bool
		cwd         string
		interactive func() bool
		in          string
		params      *domain.RegisterParameters
		mutate      func(t *testing.T, dir string)
		wantErr     string
		wantOut     []string
		wantFile    []string
	}{
		{
			name:     "an available domain is placed",
			args:     []string{"example.dev"},
			withDir:  true,
			params:   available,
			wantOut:  placed,
			wantFile: placedFile,
		},
		{
			name:     "the infrastructure root is found from inside the repository",
			args:     []string{"example.dev"},
			cwd:      "2-net",
			params:   available,
			wantOut:  placed,
			wantFile: placedFile,
		},
		{
			name:        "the domain is asked for at the terminal",
			withDir:     true,
			interactive: always,
			in:          "example.dev\n",
			params:      available,
			wantOut:     append([]string{"Which domain? (a bare lowercase name, such as example.com)", "> "}, placed...),
			wantFile:    placedFile,
		},
		{
			name:    "a domain left out without a terminal is refused",
			withDir: true,
			params:  available,
			wantErr: "no domain given and no terminal to ask on: pass a bare lowercase domain, such as example.com",
		},
		{
			name:    "two domains are refused",
			args:    []string{"example.dev", "example.app"},
			withDir: true,
			params:  available,
			wantErr: "accepts at most 1 arg(s), received 2",
		},
		{
			name:    "an unavailable domain is refused",
			args:    []string{"example.dev"},
			withDir: true,
			params:  &domain.RegisterParameters{Availability: "UNAVAILABLE", Currency: "USD"},
			wantErr: "example.dev is not available for registration",
		},
		{
			name:    "a price not in US dollars is refused",
			args:    []string{"example.dev"},
			withDir: true,
			params:  &domain.RegisterParameters{Availability: domain.Available, Currency: "EUR", YearlyPrice: 10},
			wantErr: "example.dev is priced in EUR, not USD",
		},
		{
			name:    "an empty registrant mailbox is refused",
			args:    []string{"example.dev"},
			withDir: true,
			params:  available,
			mutate: func(t *testing.T, dir string) {
				t.Helper()

				file := filepath.Join(dir, "2-net", "terraform.tfvars")
				data, err := os.ReadFile(file)
				if err != nil {
					t.Fatal(err)
				}
				data = bytes.Replace(data, []byte(`"hostmaster@example.com"`), []byte(`""`), 1)
				if err := os.WriteFile(file, data, 0o600); err != nil {
					t.Fatal(err)
				}
			},
			wantErr: "registrant_contact.email in",
		},
		{
			name:    "a name that is not a bare domain is refused",
			args:    []string{"Example.dev"},
			withDir: true,
			params:  available,
			wantErr: "a bare lowercase domain",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// The domain fixture has the network and environment layers; the two that
			// mark an infrastructure root are added so it can be found from inside.
			dir := copyRepo(t, fixtureInfra)
			for _, layer := range []string{"1-org", "3-app"} {
				if err := os.Mkdir(filepath.Join(dir, layer), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			if tt.mutate != nil {
				tt.mutate(t, dir)
			}
			interactive := tt.interactive
			if interactive == nil {
				interactive = never
			}
			d := deps{domains: fixedClient(tt.params), secrets: noSecretManager, projects: noProjects, cwd: filepath.Join(dir, tt.cwd), interactive: interactive}
			args := append([]string{"domain", "add"}, tt.args...)
			if tt.withDir {
				args = append(args, "--dir", dir)
			}
			out, err := execute(d, tt.in, args...)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Execute() error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out, want) {
					t.Errorf("output lacks %q:\n%s", want, out)
				}
			}
			data, err := os.ReadFile(filepath.Join(dir, "2-net", "terraform.tfvars"))
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range tt.wantFile {
				if !strings.Contains(string(data), want) {
					t.Errorf("the placement lacks %q:\n%s", want, data)
				}
			}
		})
	}
}

// labProjects answers every search with the same projects.
type labProjects struct {
	projects []where.Project
}

func (l labProjects) SearchProjects(context.Context, string) ([]where.Project, error) {
	return l.projects, nil
}

// projectsOf opens labProjects holding the projects.
func projectsOf(projects ...where.Project) where.ProjectClientFunc {
	return func(context.Context) (where.ProjectClient, error) {
		return labProjects{projects: projects}, nil
	}
}

// lab is a project of the lab organization in the environment, labeled as 1-org labels
// the projects it creates.
func lab(id, env string) where.Project {
	return where.Project{ID: id, DisplayName: "Lab " + env, State: where.Active, Labels: map[string]string{"environment": env, "terraform_source_path": "1-org"}}
}

// labSecrets is a Secret Manager holding, by container name, the state of each version
// (the first is version 1). Every container carries the layer's label, and every
// project holds the same containers.
type labSecrets struct {
	layer      string
	containers map[string][]string
	// created records the labels each container created through the fake was given,
	// and added the last value added to each container, when the test made the maps.
	created map[string]map[string]string
	added   map[string]string
}

// open is the fake's secret.ClientFunc.
func (l labSecrets) open(context.Context, string) (secret.Client, error) {
	return l, nil
}

func (l labSecrets) GetSecretVersion(_ context.Context, name string) (*secret.Version, error) {
	container, version := path.Base(path.Dir(path.Dir(name))), path.Base(name)
	states, ok := l.containers[container]
	if !ok {
		return nil, errors.Newf("rpc error: code = NotFound desc = Secret [%s] not found", container)
	}
	if version == secret.Latest && len(states) > 0 {
		version = strconv.Itoa(len(states))
	}
	n, err := strconv.Atoi(version)
	if err != nil || n < 1 || n > len(states) {
		return nil, errors.Newf("rpc error: code = NotFound desc = Secret Version [%s] not found", name)
	}

	return &secret.Version{Name: path.Dir(name) + "/" + version, State: states[n-1]}, nil
}

func (l labSecrets) ListSecrets(_ context.Context, _, filter string) ([]string, error) {
	if filter != "labels.terraform_source_path="+l.layer {
		return nil, nil
	}
	names := make([]string, 0, len(l.containers))
	for name := range l.containers {
		names = append(names, name)
	}
	slices.Sort(names)

	return names, nil
}

func (l labSecrets) ListSecretVersions(_ context.Context, project, container string) ([]secret.Version, error) {
	states, ok := l.containers[container]
	if !ok {
		return nil, errors.Newf("rpc error: code = NotFound desc = Secret [%s] not found", container)
	}
	versions := make([]secret.Version, 0, len(states))
	for i, state := range states {
		versions = append(versions, secret.Version{Name: "projects/" + project + "/secrets/" + container + "/versions/" + strconv.Itoa(i+1), State: state})
	}

	return versions, nil
}

func (l labSecrets) GetSecret(_ context.Context, name string) (labels map[string]string, exists bool, err error) {
	_, ok := l.containers[path.Base(name)]

	return nil, ok, nil
}

func (l labSecrets) CreateSecret(_ context.Context, _, id string, labels map[string]string) error {
	l.containers[id] = []string{}
	if l.created != nil {
		l.created[id] = labels
	}

	return nil
}

func (l labSecrets) AddSecretVersion(_ context.Context, name string, payload []byte) (string, error) {
	id := path.Base(name)
	states, ok := l.containers[id]
	if !ok {
		return "", errors.Newf("rpc error: code = NotFound desc = Secret [%s] not found", id)
	}
	l.containers[id] = append(states, secret.Enabled)
	if l.added != nil {
		l.added[id] = string(payload)
	}

	return strconv.Itoa(len(states) + 1), nil
}

// pinArgs is the secret pin command line with the arguments.
func pinArgs(args ...string) []string {
	return append([]string{secretCommand, "pin"}, args...)
}

// addArgs is the secret add command line with the arguments.
func addArgs(args ...string) []string {
	return append([]string{secretCommand, "add"}, args...)
}

func TestSecretAdd(t *testing.T) {
	t.Parallel()

	labs := projectsOf(lab("lab-tst-1", "tst"), lab("lab-stg-1", "stg"), lab("lab-prd-1", "prd"))
	const mailKey = "imp-tst-gbl-quill-mail-api-key"
	placement := `{"prefix": "imp", "environments": ["tst", "stg", "prd"], "regions": [{"name": "us-central1", "code": "uc1"}],
		"appsDomain": "impulseframework.dev", "hostedDomain": "impulseframework.com", "stateBucket": "imp-boot-gbl-state-0000",
		"placeholderImage": "us-docker.pkg.dev/cloudrun/container/hello", "defaultBranch": "master", "repository": "quill",
		"releaseApp": "impulseframework-release", "bedrockImage": "us-central1-docker.pkg.dev/lab-shr-1/lab-shr-uc1-tools/bedrock@sha256:0000000000000000000000000000000000000000000000000000000000000000",
		"labels": {"bedrock-lab": "true"}}`
	added3 := []string{
		"Added version 3 of imp-tst-gbl-quill-mail-api-key in project lab-tst-1, for APP_MAIL_API_KEY of quill in tst.",
		"Pin it: bedrock secret pin tst APP_MAIL_API_KEY 3",
	}
	tests := []struct {
		name string
		// args are the arguments after "secret add"; --dir is added. placement, when
		// set, is written beside the layer. value is the file --from-file names when
		// fromFile is set, else standard input; hidden is what the terminal answers.
		args        []string
		placement   string
		tfvars      string
		fromFile    bool
		value       string
		interactive func() bool
		hidden      string
		projects    where.ProjectClientFunc
		secrets     labSecrets
		wantErr     string
		wantOut     []string
		wantAbsent  []string
		wantCreated map[string]string
		wantValue   string
	}{
		{
			name:     "a build secret the placement declares for the environment is accepted, no prefix needed",
			args:     []string{"tst", "UI_LICENSE", "--project", "lab-tst-1", "--container", "imp-tst-gbl-quill-ui-license"},
			value:    "lic-2",
			projects: noProjects,
			secrets:  labSecrets{layer: "3-app-quill", containers: map[string][]string{"imp-tst-gbl-quill-ui-license": {secret.Enabled}}},
			wantOut:  []string{"Added version 2 of imp-tst-gbl-quill-ui-license in project lab-tst-1, for UI_LICENSE of quill in tst.", "Pin it: bedrock secret pin tst UI_LICENSE 2"},
		},
		{
			name:     "a build secret the placement declares for another environment is refused, naming what this one declares",
			args:     []string{"tst", "UI_LICENSE", "--container", "x"},
			tfvars:   "state_bucket = \"b\"\nsecret_versions = {\n  tst = {}\n  stg = {}\n  prd = {}\n}\nbuild_secrets = {\n  tst = {}\n  stg = {\n    UI_LICENSE = \"1\"\n  }\n  prd = {}\n}\n",
			value:    "k-1",
			projects: noProjects,
			wantErr:  "the code declares no secret variable UI_LICENSE (it declares APP_COOKIE_KEY, APP_MAIL_API_KEY): a container is added only for a secret the application reads, or for a build secret the placement declares for tst (it declares none)",
		},
		{
			name:     "a name without the prefix that the placement does not declare as a build secret is refused with what it declares",
			args:     []string{"tst", "KENDO_UI_LICENSE", "--container", "x"},
			value:    "k-1",
			projects: noProjects,
			wantErr:  "the code declares no secret variable KENDO_UI_LICENSE (it declares APP_COOKIE_KEY, APP_MAIL_API_KEY): a container is added only for a secret the application reads, or for a build secret the placement declares for tst (it declares UI_LICENSE)",
		},
		{
			name:      "everything typed, the value from a file",
			args:      []string{"tst", "APP_MAIL_API_KEY", "--project", "lab-tst-1", "--container", mailKey},
			fromFile:  true,
			value:     "k-3\n",
			projects:  noProjects,
			wantOut:   added3,
			wantValue: "k-3\n",
		},
		{
			name:      "the project is found by its labels and the container named from the placement; the value from standard input",
			args:      []string{"tst", "APP_MAIL_API_KEY"},
			placement: placement,
			value:     "k-3",
			wantOut:   added3,
			wantValue: "k-3",
		},
		{
			name:      "a container the project lacks is created with the stack's labels",
			args:      []string{"tst", "APP_MAIL_API_KEY"},
			placement: placement,
			value:     "k-1",
			secrets:   labSecrets{layer: "3-app-quill", containers: map[string][]string{"imp-tst-gbl-quill-cookie-key": {secret.Enabled}}},
			wantOut: []string{
				"Created the container imp-tst-gbl-quill-mail-api-key in project lab-tst-1; the next apply of the quill stack in tst adopts it.",
				"Added version 1 of imp-tst-gbl-quill-mail-api-key in project lab-tst-1, for APP_MAIL_API_KEY of quill in tst.",
				"Pin it: bedrock secret pin tst APP_MAIL_API_KEY 1",
			},
			wantCreated: map[string]string{
				"terraform": "true", "terraform_source_path": "3-app-quill", "source_repo": "quill",
				"environment": "tst", "application": "quill", "variable": "app_mail_api_key", "bedrock-lab": "true",
			},
			wantValue: "k-1",
		},
		{
			name:      "without a placement, the container is found by its labels",
			args:      []string{"tst", "APP_MAIL_API_KEY"},
			value:     "k-3",
			wantOut:   added3,
			wantValue: "k-3",
		},
		{
			name:    "without a placement and without the container, refused",
			args:    []string{"tst", "APP_MAIL_API_KEY"},
			value:   "k-1",
			secrets: labSecrets{layer: "3-app-quill", containers: map[string][]string{"imp-tst-gbl-quill-cookie-key": {secret.Enabled}}},
			wantErr: "no secret container in project lab-tst-1 carrying the labels terraform_source_path=3-app-quill ends with -mail-api-key (found imp-tst-gbl-quill-cookie-key): pass --container",
		},
		{
			name:        "asked at the terminal without echo",
			args:        []string{"tst", "APP_MAIL_API_KEY", "--container", mailKey},
			interactive: always,
			hidden:      "k-3",
			wantOut:     added3,
			wantAbsent:  []string{"k-3"},
			wantValue:   "k-3",
		},
		{
			name:     "a variable the code does not declare is refused before anything is looked up",
			args:     []string{"tst", "APP_OTHER_KEY", "--container", "x"},
			value:    "k-1",
			projects: noProjects,
			wantErr:  "the code declares no secret variable APP_OTHER_KEY (it declares APP_COOKIE_KEY, APP_MAIL_API_KEY): a container is added only for a secret the application reads",
		},
		{
			name:    "an empty value is refused",
			args:    []string{"tst", "APP_MAIL_API_KEY", "--container", mailKey},
			value:   "",
			wantErr: "the value of APP_MAIL_API_KEY is empty: nothing to add",
		},
		{
			name:     "an environment of the wrong shape is refused before anything is looked up",
			args:     []string{"dev", "APP_MAIL_API_KEY"},
			value:    "k-1",
			projects: noProjects,
			wantErr:  `environment "dev": one of tst, stg, prd`,
		},
		{
			name:    "the variable left out is refused with the declared ones listed",
			args:    []string{"tst"},
			value:   "k-1",
			wantErr: "no variable given and no terminal to ask on: pass one of APP_COOKIE_KEY, APP_MAIL_API_KEY",
		},
		{
			name:    "three arguments are refused",
			args:    []string{"tst", "APP_MAIL_API_KEY", "3"},
			value:   "k-1",
			wantErr: "accepts at most 2 arg(s), received 3",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := copyRepo(t, fixtureFlat)
			layer := filepath.Join(dir, "3-app", "quill")
			if tt.placement != "" {
				if err := os.WriteFile(filepath.Join(layer, "placement.json"), []byte(tt.placement), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if tt.tfvars != "" {
				if err := os.WriteFile(filepath.Join(layer, "terraform.tfvars"), []byte(tt.tfvars), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			secrets := tt.secrets
			if secrets.containers == nil {
				secrets = quillSecrets()
			}
			secrets.created, secrets.added = map[string]map[string]string{}, map[string]string{}
			d := deps{domains: noCloudDomains, secrets: secrets.open, projects: labs, cwd: dir, interactive: never}
			if tt.projects != nil {
				d.projects = tt.projects
			}
			if tt.interactive != nil {
				d.interactive = tt.interactive
			}
			d.readSecret = func(w io.Writer, question string) ([]byte, error) {
				fmt.Fprintln(w, question)

				return []byte(tt.hidden), nil
			}
			args := append(addArgs(tt.args...), "--dir", dir)
			in := tt.value
			if tt.fromFile {
				file := filepath.Join(t.TempDir(), "value")
				if err := os.WriteFile(file, []byte(tt.value), 0o600); err != nil {
					t.Fatal(err)
				}
				args = append(args, "--from-file", file)
				in = ""
			}
			out, err := execute(d, in, args...)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Execute() error = %v, wantErr %q; output:\n%s", err, tt.wantErr, out)
				}
				if len(secrets.added) != 0 {
					t.Errorf("Execute() added %v, want nothing", secrets.added)
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
			for _, absent := range tt.wantAbsent {
				if strings.Contains(out, absent) {
					t.Errorf("output shows %q:\n%s", absent, out)
				}
			}
			if tt.wantCreated != nil && !maps.Equal(secrets.created[mailKey], tt.wantCreated) {
				t.Errorf("created with labels %v, want %v", secrets.created[mailKey], tt.wantCreated)
			}
			if tt.wantCreated == nil && len(secrets.created) != 0 {
				t.Errorf("created %v, want nothing created", secrets.created)
			}
			if got := secrets.added[mailKey]; got != tt.wantValue {
				t.Errorf("stored %q, want %q", got, tt.wantValue)
			}
		})
	}
}

// quillSecrets are the lab's containers for quill: the cookie key at three versions,
// the second disabled, and the mail key at two, the first disabled.
func quillSecrets() labSecrets {
	return labSecrets{layer: "3-app-quill", containers: map[string][]string{
		"imp-tst-gbl-quill-cookie-key":   {secret.Enabled, "DISABLED", secret.Enabled},
		"imp-tst-gbl-quill-mail-api-key": {"DISABLED", secret.Enabled},
	}}
}

func TestSecretPin(t *testing.T) {
	t.Parallel()

	labs := projectsOf(lab("lab-tst-1", "tst"), lab("lab-stg-1", "stg"), lab("lab-prd-1", "prd"))
	pinned3 := []string{
		"Pinned APP_COOKIE_KEY to version 3 for quill in tst: secret_versions.tst in",
		"Secret Manager confirms version 3 of imp-tst-gbl-quill-cookie-key in project lab-tst-1 is enabled.",
		"Next: commit the change to the values file and open the pull request; the plan for tst shows the change to the service's configuration (Cloud Run's revision template) and nothing in the other environments. After the merge, the run of that commit in tst applies it, builds the release, deploys it and moves traffic.",
	}
	file3 := []string{`APP_COOKIE_KEY = "3"`, "stg = {}", "prd = {}"}
	tests := []struct {
		name string
		// fixture is the repository copied; args the arguments after "secret pin".
		// withDir adds --dir <infrastructure root>; else the repository is found from
		// cwd, a directory inside the copy.
		fixture     string
		args        []string
		withDir     bool
		cwd         string
		interactive func() bool
		in          string
		projects    where.ProjectClientFunc
		secrets     labSecrets
		wantErr     string
		wantOut     []string
		wantFile    []string
	}{
		{
			name:     "everything typed",
			args:     []string{"tst", "APP_COOKIE_KEY", "3", "--project", "lab-tst-1", "--container", "imp-tst-gbl-quill-cookie-key"},
			withDir:  true,
			projects: noProjects,
			wantOut:  pinned3,
			wantFile: file3,
		},
		{
			name:     "a build secret is pinned under build_secrets",
			args:     []string{"tst", "UI_LICENSE", "2", "--project", "lab-tst-1", "--container", "imp-tst-gbl-quill-ui-license"},
			withDir:  true,
			projects: noProjects,
			secrets:  labSecrets{layer: "3-app-quill", containers: map[string][]string{"imp-tst-gbl-quill-ui-license": {secret.Enabled, secret.Enabled}}},
			wantOut: []string{
				"Pinned UI_LICENSE to version 2 for quill in tst: build_secrets.tst in",
				"Secret Manager confirms version 2 of imp-tst-gbl-quill-ui-license in project lab-tst-1 is enabled.",
				"the plan for tst shows the change to the triggers' substitutions (the image build reads the new version)",
			},
			wantFile: []string{`UI_LICENSE = "2"`, `APP_COOKIE_KEY = "2"`},
		},
		{
			name:     "the project and the container are found by their labels",
			args:     []string{"tst", "APP_COOKIE_KEY", "3"},
			withDir:  true,
			wantOut:  pinned3,
			wantFile: file3,
		},
		{
			name:        "nothing typed, run from inside the repository, the answers given at the terminal",
			cwd:         filepath.Join("3-app", "quill"),
			interactive: always,
			in:          "1\n1\n3\n",
			wantOut: append([]string{
				"Which environment?", "  1) tst", "  2) stg", "  3) prd",
				"Which variable?", "  1) APP_COOKIE_KEY", "  2) APP_MAIL_API_KEY",
				"Which version of imp-tst-gbl-quill-cookie-key?", "  3  ENABLED", "  2  DISABLED", "  1  ENABLED",
			}, pinned3...),
			wantFile: file3,
		},
		{
			name:     "the application repository's own stack, run from its root",
			fixture:  fixtureAppRepo,
			cwd:      ".",
			args:     []string{"tst", "APP_COOKIE_KEY", "3"},
			wantOut:  pinned3,
			wantFile: []string{`APP_COOKIE_KEY = "3"`, "stg = {}"},
		},
		{
			name:        "the values themselves answer too",
			cwd:         ".",
			interactive: always,
			in:          "stg\nAPP_MAIL_API_KEY\n2\n",
			wantOut:     []string{"Pinned APP_MAIL_API_KEY to version 2 for quill in stg", "Secret Manager confirms version 2 of imp-tst-gbl-quill-mail-api-key in project lab-stg-1 is enabled."},
			wantFile:    []string{`APP_MAIL_API_KEY = "2"`, `APP_COOKIE_KEY = "2"`},
		},
		{
			name:        "a disabled version chosen at the terminal is refused",
			withDir:     true,
			interactive: always,
			in:          "tst\nAPP_COOKIE_KEY\n2\n",
			wantErr:     "version 2 of imp-tst-gbl-quill-cookie-key in project lab-tst-1 is DISABLED, not ENABLED: the environment could not mount it; choose an enabled version",
		},
		{
			name:    "a disabled version typed is refused by the verification",
			args:    []string{"tst", "APP_MAIL_API_KEY", "1"},
			withDir: true,
			wantErr: "version 1 of imp-tst-gbl-quill-mail-api-key in project lab-tst-1 is DISABLED, not ENABLED: the environment could not mount it",
		},
		{
			name:    "a variable left out without a terminal is refused with the choices",
			args:    []string{"tst"},
			withDir: true,
			wantErr: "no variable given and no terminal to ask on: pass one of APP_COOKIE_KEY, APP_MAIL_API_KEY",
		},
		{
			name:    "a version left out without a terminal is refused with the choices and their states",
			args:    []string{"tst", "APP_COOKIE_KEY"},
			withDir: true,
			wantErr: "no version given and no terminal to ask on: pass one of 3 (ENABLED), 2 (DISABLED), 1 (ENABLED)",
		},
		{
			name:    "an environment left out without a terminal is refused with the choices",
			withDir: true,
			wantErr: "no environment given and no terminal to ask on: pass one of tst, stg, prd",
		},
		{
			name:    "several applications without --app are refused",
			fixture: fixtureNested,
			args:    []string{"tst", "APP_COOKIE_KEY", "3"},
			withDir: true,
			wantErr: "several application layers under",
		},
		{
			name:     "the application chosen among several, the layers found under infrastructure",
			fixture:  fixtureNested,
			args:     []string{"tst", "APP_COOKIE_KEY", "3", "--app", "quill"},
			cwd:      ".",
			wantOut:  pinned3,
			wantFile: file3,
		},
		{
			name:     "no project found",
			args:     []string{"tst", "APP_COOKIE_KEY", "3"},
			withDir:  true,
			projects: projectsOf(),
			wantErr:  "no active project carries the labels environment=tst, terraform_source_path=1-org: pass --project",
		},
		{
			name:     "several projects found",
			args:     []string{"tst", "APP_COOKIE_KEY", "3"},
			withDir:  true,
			projects: projectsOf(lab("lab-tst-1", "tst"), lab("lab-tst-2", "tst")),
			wantErr:  "several active projects carry the labels environment=tst, terraform_source_path=1-org: lab-tst-1 (Lab tst), lab-tst-2 (Lab tst); pass --project",
		},
		{
			name:    "several containers found",
			args:    []string{"tst", "APP_COOKIE_KEY", "3"},
			withDir: true,
			secrets: labSecrets{layer: "3-app-quill", containers: map[string][]string{
				"imp-tst-gbl-quill-cookie-key":     {secret.Enabled, secret.Enabled, secret.Enabled},
				"imp-tst-gbl-quill-old-cookie-key": {secret.Enabled},
			}},
			wantErr: "several secret containers in project lab-tst-1 carrying the labels terraform_source_path=3-app-quill end with -cookie-key: imp-tst-gbl-quill-cookie-key, imp-tst-gbl-quill-old-cookie-key; pass --container",
		},
		{
			name:    "no container found",
			args:    []string{"tst", "APP_COOKIE_KEY", "3"},
			withDir: true,
			secrets: labSecrets{layer: "3-app-other", containers: map[string][]string{"imp-tst-gbl-other-cookie-key": {secret.Enabled}}},
			wantErr: "no secret container in project lab-tst-1 carries the labels terraform_source_path=3-app-quill: pass --container",
		},
		{
			name:     "the older --secret flag still names the container",
			args:     []string{"tst", "APP_COOKIE_KEY", "3", "--project", "lab-tst-1", "--secret", "custom-cookie"},
			withDir:  true,
			projects: noProjects,
			secrets:  labSecrets{layer: "3-app-quill", containers: map[string][]string{"custom-cookie": {secret.Enabled, secret.Enabled, secret.Enabled}}},
			wantOut:  []string{"Secret Manager confirms version 3 of custom-cookie in project lab-tst-1 is enabled."},
			wantFile: []string{`APP_COOKIE_KEY = "3"`},
		},
		{
			name:    "a pin already in place is reported",
			args:    []string{"tst", "APP_COOKIE_KEY", "2"},
			withDir: true,
			wantOut: []string{"APP_COOKIE_KEY is already pinned to version 2 for quill in tst"},
		},
		{
			name:     "an environment of the wrong shape is refused before anything is looked up",
			args:     []string{"dev", "APP_COOKIE_KEY", "3"},
			withDir:  true,
			projects: noProjects,
			wantErr:  `environment "dev": one of tst, stg, prd`,
		},
		{
			name:     "a variable of the wrong shape is refused before anything is looked up",
			args:     []string{"tst", "cookie_key", "3"},
			withDir:  true,
			projects: noProjects,
			wantErr:  `variable "cookie_key": an environment variable in upper snake case under the APP_ prefix`,
		},
		{
			name:    "a version of the wrong shape is refused",
			args:    []string{"tst", "APP_COOKIE_KEY", "0"},
			withDir: true,
			wantErr: `version "0": a positive integer or the word latest`,
		},
		{
			name:    "four arguments are refused",
			args:    []string{"quill", "tst", "APP_COOKIE_KEY", "3"},
			withDir: true,
			wantErr: "accepts at most 3 arg(s), received 4",
		},
		{
			name:    "a working directory outside every repository is refused",
			args:    []string{"tst", "APP_COOKIE_KEY", "3"},
			cwd:     "..",
			wantErr: "no repository above",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fixture := tt.fixture
			if fixture == "" {
				fixture = fixtureFlat
			}
			dir := copyRepo(t, fixture)
			infra := dir
			if fixture == fixtureNested || fixture == fixtureAppRepo {
				infra = filepath.Join(dir, "infrastructure")
			}
			placement := filepath.Join(infra, "3-app", "quill", "terraform.tfvars")
			if fixture == fixtureAppRepo {
				placement = filepath.Join(infra, "terraform.tfvars")
			}
			d := deps{domains: noCloudDomains, secrets: quillSecrets().open, projects: labs, cwd: filepath.Join(dir, tt.cwd), interactive: never}
			if tt.projects != nil {
				d.projects = tt.projects
			}
			if tt.secrets.containers != nil {
				d.secrets = tt.secrets.open
			}
			if tt.interactive != nil {
				d.interactive = tt.interactive
			}
			args := pinArgs(tt.args...)
			if tt.withDir {
				args = append(args, "--dir", infra)
			}
			out, err := execute(d, tt.in, args...)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Execute() error = %v, wantErr %q; output:\n%s", err, tt.wantErr, out)
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
			data, err := os.ReadFile(placement)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range tt.wantFile {
				if !strings.Contains(string(data), want) {
					t.Errorf("the placement lacks %q:\n%s", want, data)
				}
			}
		})
	}
}

func TestSecretPinCompletion(t *testing.T) {
	t.Parallel()

	labs := projectsOf(lab("lab-tst-1", "tst"))
	tests := []struct {
		name     string
		fixture  string
		args     []string
		projects where.ProjectClientFunc
		want     []string
	}{
		{name: "the environments", args: []string{""}, want: []string{"tst", "stg", "prd"}},
		{name: "the variables", args: []string{"tst", ""}, want: []string{"APP_COOKIE_KEY", "APP_MAIL_API_KEY", "UI_LICENSE"}},
		{name: "the versions with their states", args: []string{"tst", "APP_COOKIE_KEY", ""}, want: []string{"3\tENABLED", "2\tDISABLED", "1\tENABLED"}},
		{name: "the versions of the container named", args: []string{"tst", "APP_COOKIE_KEY", "--project", "lab-tst-1", "--container", "imp-tst-gbl-quill-mail-api-key", ""}, projects: noProjects, want: []string{"2\tENABLED", "1\tDISABLED"}},
		{name: "nothing after the third argument", args: []string{"tst", "APP_COOKIE_KEY", "3", ""}},
		{name: "nothing when the project cannot be found", args: []string{"tst", "APP_COOKIE_KEY", ""}, projects: projectsOf()},
		{name: "nothing when the search fails", args: []string{"tst", "APP_COOKIE_KEY", ""}, projects: noProjects},
		{name: "nothing when the application cannot be told", fixture: fixtureNested, args: []string{""}},
		{name: "the environments of the application named", fixture: fixtureNested, args: []string{"--app", "harbor", ""}, want: []string{"tst", "prd"}},
		{name: "the applications for --app", fixture: fixtureNested, args: []string{"--app", ""}, want: []string{"harbor", "quill"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fixture := tt.fixture
			if fixture == "" {
				fixture = fixtureFlat
			}
			infra := copyRepo(t, fixture)
			if fixture == fixtureNested {
				infra = filepath.Join(infra, "infrastructure")
			}
			d := deps{domains: noCloudDomains, secrets: quillSecrets().open, projects: labs, interactive: never}
			if tt.projects != nil {
				d.projects = tt.projects
			}
			args := append([]string{"__complete"}, pinArgs(append([]string{"--dir", infra}, tt.args...)...)...)
			out, err := execute(d, "", args...)
			if err != nil {
				t.Fatalf("Execute() error = %v; output:\n%s", err, out)
			}
			// cobra prints one completion per line, then the directive (:4, no file
			// completion) on a line of its own, then a note about it on standard error.
			lines := strings.Split(out, "\n")
			end := slices.Index(lines, ":4")
			if end < 0 {
				t.Fatalf("no :4 directive (no file completion) in the output:\n%s", out)
			}
			got := lines[:end]
			if len(got) == 0 {
				got = nil
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("completions = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCompletionCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		shell string
		want  string
	}{
		{name: "bash", shell: "bash", want: "bash completion V2 for bedrock"},
		{name: "zsh", shell: "zsh", want: "zsh completion for bedrock"},
		{name: "fish", shell: "fish", want: "fish completion for bedrock"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			code, out := run(t, "completion", tt.shell)
			if code != 0 || !strings.Contains(out, tt.want) {
				t.Errorf("completion %s exit = %d, output lacks %q:\n%.200s", tt.shell, code, tt.want, out)
			}
		})
	}
}

// memoryStore is the deploy steps' Store in tests: what was written, by gs:// path.
type memoryStore struct {
	objects map[string]string
}

func (m *memoryStore) open(context.Context) (deploy.Store, error) {
	return m, nil
}

func (m *memoryStore) Write(_ context.Context, bucket, object string, data []byte) error {
	m.objects["gs://"+bucket+"/"+object] = string(data)

	return nil
}

func (*memoryStore) Close() error {
	return nil
}

func TestDeployRecord(t *testing.T) {
	t.Parallel()

	environment := "export SERVICES=\"us-central1=harbor-app\"\nexport SHIFT_TRAFFIC=\"true\"\nexport VERSION=\"v1.2.3\"\nexport RELEASE=\"v1.2.3\"\nexport IMAGE=\"reg/harbor\"\nexport SKIP_DEPLOY=\"\"\nexport IMAGE_DIGEST=\"sha256:abc\"\n"
	build := `{"id": "b-1", "substitutions": {"_APP": "harbor", "_ENV": "tst", "_RECORDS_BUCKET": "records", "COMMIT_SHA": "deadbeef"}}`
	tests := []struct {
		name       string
		files      map[string]string
		wantOut    []string
		wantStored string
		wantErr    string
	}{
		{
			name:       "the record is written from the workspace",
			files:      map[string]string{"environment.sh": environment, "build.json": build, "revisions.txt": "us-central1,harbor-app,harbor-app-00007-abc\n"},
			wantOut:    []string{`"status": "live"`, `"commit": "deadbeef"`, "Recorded live deployment of v1.2.3 in tst: gs://records/harbor/tst/v1.2.3/b-1.json"},
			wantStored: "gs://records/harbor/tst/v1.2.3/b-1.json",
		},
		{
			name:    "a torn-down environment records nothing",
			files:   map[string]string{"environment.sh": "export SKIP_DEPLOY=\"true\"\n"},
			wantOut: []string{"The pull request's environment was torn down: nothing to record."},
		},
		{
			name:    "a workspace without the facts is refused",
			files:   map[string]string{"build.json": build},
			wantErr: "environment.sh",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			workspace := t.TempDir()
			for name, content := range tt.files {
				if err := os.WriteFile(filepath.Join(workspace, name), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			store := &memoryStore{objects: map[string]string{}}
			d := deps{domains: noCloudDomains, secrets: noSecretManager, projects: noProjects, deploy: &deploy.Clients{Storage: store.open}, interactive: never}
			out, err := execute(d, "", "deploy", "record", "--workspace", workspace)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Execute() error = %v, wantErr %q; output:\n%s", err, tt.wantErr, out)
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
			if tt.wantStored == "" && len(store.objects) != 0 {
				t.Errorf("stored %v, want nothing", store.objects)
			}
			if tt.wantStored != "" {
				if _, ok := store.objects[tt.wantStored]; !ok {
					t.Errorf("stored %v, want %s", store.objects, tt.wantStored)
				}
			}
		})
	}
}

// fakeBuilds is the deploy steps' Builds in tests: one build, one token.
type fakeBuilds struct {
	build string
	token string
}

func (b *fakeBuilds) open(context.Context) (deploy.Builds, error) {
	return b, nil
}

func (b *fakeBuilds) Get(context.Context, string, string, string) ([]byte, error) {
	return []byte(b.build), nil
}

func (b *fakeBuilds) ReadToken(context.Context, string) (string, error) {
	return b.token, nil
}

// noComments is the comment read for a build that must not need one.
func noComments(context.Context, string, string, int) ([]github.Comment, error) {
	return nil, errors.New("no comments in this test")
}

func TestDeployResolve(t *testing.T) {
	// No t.Parallel(): the command reads the build's id, project and location from the
	// process environment, as the pipeline passes them to the step.
	build := `{"id": "b-1", "substitutions": {"TAG_NAME": "v1.2.3", "_ENV": "tst", "_SERVICES": "us-central1=quill-app", "_MIGRATE_JOB": "us-central1=quill-migrate", "_REPO_CONNECTION_NAME": "CONNECTION_NOT_AUTHORIZED_IN_2-ENV", "_REPO_NAME": "quill", "_REGISTRY": "reg", "_APP": "quill", "COMMIT_SHA": "deadbeefcafe", "SHORT_SHA": "deadbee", "_THEME": "dusk"}}`
	tests := []struct {
		name    string
		env     map[string]string
		wantOut []string
		// wantFiles names what each workspace file must contain.
		wantFiles map[string]string
		wantErr   string
	}{
		{
			name:    "the facts are worked out from the build and written to the workspace",
			env:     map[string]string{"BUILD_ID": "b-1", "PROJECT_ID": "tst-project", "LOCATION": "us-central1"},
			wantOut: []string{"Triggered by tag v1.2.3", "IMAGE=reg/quill IMAGE_TAG=v1.2.3-tst VERSION=v1.2.3 RELEASE=v1.2.3", "Declared substitutions for the hooks and the image build: _THEME"},
			wantFiles: map[string]string{
				"environment.sh": "export IMAGE_TAG=\"v1.2.3-tst\"\n",
				"build-args.sh":  "BUILD_ARGS+=(--build-arg '_THEME=dusk')\n",
				"build.json":     `"id": "b-1"`,
			},
		},
		{
			name:    "without the build's id the step refuses",
			env:     map[string]string{"PROJECT_ID": "tst-project", "LOCATION": "us-central1"},
			wantErr: "BUILD_ID is not set",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, name := range []string{"BUILD_ID", "PROJECT_ID", "LOCATION"} {
				t.Setenv(name, tt.env[name])
			}
			workspace := t.TempDir()
			builds := &fakeBuilds{build: build, token: "tok"}
			d := deps{domains: noCloudDomains, secrets: noSecretManager, projects: noProjects, deploy: &deploy.Clients{Builds: builds.open, Comments: noComments}, interactive: never}
			out, err := execute(d, "", "deploy", "resolve", "--workspace", workspace)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Execute() error = %v, wantErr %q; output:\n%s", err, tt.wantErr, out)
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
			for name, want := range tt.wantFiles {
				data, err := os.ReadFile(filepath.Join(workspace, name))
				if err != nil {
					t.Fatalf("%s: %v", name, err)
				}
				if !strings.Contains(string(data), want) {
					t.Errorf("%s lacks %q:\n%s", name, want, data)
				}
			}
		})
	}
}
