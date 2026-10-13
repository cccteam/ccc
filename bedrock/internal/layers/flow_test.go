package layers

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/deploy"
	"github.com/cccteam/ccc/bedrock/internal/github"
	"github.com/cccteam/ccc/bedrock/internal/github/githubtest"
	"github.com/cccteam/ccc/bedrock/internal/org"
)

// The fixture organization the flows run over: its placement, its repository, the
// commits the stand-in knows, and the apps.
const (
	fixturePlacement = "../org/testdata/imp/placement.json"
	owner            = "imp-example"
	repoName         = "imp-impulse-infrastructure"
	baseSHA          = "base1"
	headSHA          = "head1"
	pushSHA          = "head2"
	deployerAppID    = "5080800"
	infraAppID       = "5080700"
	installation     = int64(77)
	bucket           = "imp-boot-gbl-state-a1b2"
	deployerVersion  = "projects/imp-boot-gbl-core-a1b2/secrets/imp-boot-gbl-github-deployer-key/versions/1"
	infraVersion     = "projects/imp-boot-gbl-core-a1b2/secrets/imp-boot-gbl-github-infrastructure-key/versions/1"
)

// appKey is the RSA key the apps' tokens are minted with in tests, made once.
var (
	appKeyOnce sync.Once
	appKey     *rsa.PrivateKey
	appKeyPEM  string
)

func testKey(t *testing.T) (key *rsa.PrivateKey, pemKey string) {
	t.Helper()

	appKeyOnce.Do(func() {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			panic(err)
		}
		appKey = key
		appKeyPEM = string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	})

	return appKey, appKeyPEM
}

// checkout is the fixture organization's placement in a temporary directory, changed by
// mutate, as the trigger's checkout.
func checkout(t *testing.T, mutate func(p *org.Placement)) string {
	t.Helper()

	p, err := org.ReadPlacement(fixturePlacement)
	if err != nil {
		t.Fatalf("ReadPlacement() error = %v", err)
	}
	mutate(p)
	dir := t.TempDir()
	if err := p.Write(filepath.Join(dir, org.PlacementFile)); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	return dir
}

// buildJSON is a build as Cloud Build describes it, with the substitutions.
func buildJSON(subs map[string]string) string {
	data, _ := json.Marshal(map[string]any{"id": "build-1", "substitutions": subs})

	return string(data)
}

// pullRequestSubs are the plan trigger's substitutions on a pull request; pushSubs the
// apply trigger's on a push.
func pullRequestSubs() map[string]string {
	return map[string]string{prNumberSub: "7", baseBranchSub: "master", headBranchSub: "feature", commitSub: headSHA, stepsSub: org.StepsVersion, layerSub: "", environmentSub: ""}
}

func pushSubs() map[string]string {
	return map[string]string{commitSub: pushSHA, stepsSub: org.StepsVersion, layerSub: "", environmentSub: ""}
}

// fakeBuilds answers one build and one read token.
type fakeBuilds struct {
	build string
	asked []string
}

func (b *fakeBuilds) open(context.Context) (deploy.Builds, error) {
	return b, nil
}

func (b *fakeBuilds) Get(context.Context, string, string, string) ([]byte, error) {
	return []byte(b.build), nil
}

func (b *fakeBuilds) ReadToken(_ context.Context, repository string) (string, error) {
	b.asked = append(b.asked, repository)

	return "test-token", nil
}

// fakeStore is the state bucket's objects.
type fakeStore struct {
	mu      sync.Mutex
	objects map[string]string
	written []string
	listed  []string // the prefixes listed: a listing is the bucket's own permission, which the applier lacks
}

func (s *fakeStore) open(context.Context) (deploy.Store, error) {
	return s, nil
}

func (s *fakeStore) Write(_ context.Context, bucket, object string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[object] = string(data)
	s.written = append(s.written, bucket+"/"+object+"="+string(data))

	return nil
}

func (s *fakeStore) List(_ context.Context, _, prefix string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listed = append(s.listed, prefix)
	var names []string
	for name := range s.objects {
		if strings.HasPrefix(name, prefix) {
			names = append(names, name)
		}
	}
	slices.Sort(names)

	return names, nil
}

func (s *fakeStore) Read(_ context.Context, _, object string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.objects[object]
	if !ok {
		return nil, fmt.Errorf("gs://%s: %w", object, os.ErrNotExist)
	}

	return []byte(data), nil
}

func (*fakeStore) Close() error {
	return nil
}

// fakeSecrets answers payloads by version name.
type fakeSecrets struct {
	payloads map[string]string
}

func (s *fakeSecrets) open(context.Context) (deploy.Secrets, error) {
	return s, nil
}

func (s *fakeSecrets) Access(_ context.Context, version string) ([]byte, error) {
	payload, ok := s.payloads[version]
	if !ok {
		return nil, errors.Newf("NotFound: %s", version)
	}

	return []byte(payload), nil
}

func (*fakeSecrets) State(context.Context, string) (string, error) {
	return "ENABLED", nil
}

func (*fakeSecrets) Close() error {
	return nil
}

// fakeKeys is the API Keys client as an identity: the keys of each project, and what
// was restricted, with the identity each open was made as.
type fakeKeys struct {
	mu         sync.Mutex
	keys       map[string][]org.APIKey
	identities []string
	restricted []string
}

func (k *fakeKeys) openAs(_ context.Context, identity string) (org.KeyClient, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.identities = append(k.identities, identity)

	return k, nil
}

func (k *fakeKeys) ProjectKeys(_ context.Context, project string) ([]org.APIKey, error) {
	return k.keys[project], nil
}

func (k *fakeKeys) RestrictKey(_ context.Context, name string, services []string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.restricted = append(k.restricted, name+" -> "+strings.Join(services, ","))

	return nil
}

func (*fakeKeys) Close() error {
	return nil
}

// tofuRun is what the fake tofu does for one run (a layer, or a layer and an
// environment): whether its init fails, whether its plan fails with the errors given
// and prints the text, the output changes its plan makes, and whether its apply fails.
type tofuRun struct {
	initFails   bool
	planFails   bool
	text        string
	diagnostics []diagnostic
	outputs     map[string][]string
	applyFails  bool
}

// fakeRunner is tofu in tests: it records every command with its environment, finds the
// run by the command's directory and the environment its TF_DATA_DIR names, writes the
// plan's JSON log where -json-into points, and answers tofu show -json with the run's
// output changes.
type fakeRunner struct {
	mu   sync.Mutex
	runs map[string]tofuRun
	ran  []deploy.Command
}

func (r *fakeRunner) key(c deploy.Command) string {
	key := filepath.Base(c.Dir)
	for _, e := range c.Env {
		if env, ok := strings.CutPrefix(e, dataDirEnv+"=.terraform."); ok {
			key += " " + env
		}
	}

	return key
}

func (r *fakeRunner) Run(_ context.Context, c deploy.Command, out io.Writer) error {
	r.mu.Lock()
	r.ran = append(r.ran, c)
	run := r.runs[r.key(c)]
	r.mu.Unlock()
	switch c.Args[0] {
	case "init":
		if run.initFails {
			_, _ = io.WriteString(out, "Error: Failed to get existing workspaces\n")

			return errors.New("tofu failed: exit status 1")
		}
		_, _ = io.WriteString(out, "OpenTofu has been successfully initialized!\n")
	case "plan":
		_, _ = io.WriteString(out, run.text)
		var log []string
		for _, d := range run.diagnostics {
			line, _ := json.Marshal(map[string]any{"type": "diagnostic", "diagnostic": map[string]string{"severity": "error", "summary": d.Summary, "detail": d.Detail}})
			log = append(log, string(line))
		}
		log = append(log, `{"type": "diagnostic", "diagnostic": {"severity": "warning", "summary": "a warning", "detail": ""}}`)
		for _, a := range c.Args {
			if path, ok := strings.CutPrefix(a, "-json-into="); ok {
				if err := os.WriteFile(path, []byte(strings.Join(log, "\n")+"\n"), 0o600); err != nil {
					return errors.Wrap(err, "os.WriteFile()")
				}
			}
		}
		if run.planFails {
			return errors.New("tofu failed: exit status 1")
		}
	case "apply":
		if run.applyFails {
			_, _ = io.WriteString(out, "Error: creating project: quota\n")

			return errors.New("tofu failed: exit status 1")
		}
		_, _ = io.WriteString(out, "Apply complete! Resources: 1 added, 0 changed, 0 destroyed.\n")
	}

	return nil
}

func (r *fakeRunner) Output(_ context.Context, c deploy.Command, _ io.Writer) ([]byte, error) {
	r.mu.Lock()
	r.ran = append(r.ran, c)
	run := r.runs[r.key(c)]
	r.mu.Unlock()
	changes := map[string]map[string][]string{}
	for name, actions := range run.outputs {
		changes[name] = map[string][]string{"actions": actions}
	}

	data, err := json.Marshal(map[string]any{"output_changes": changes})
	if err != nil {
		return nil, errors.Wrap(err, "json.Marshal()")
	}

	return data, nil
}

// lines are the commands run, as a log shows them, each with the run's key.
func (r *fakeRunner) lines() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	lines := make([]string, 0, len(r.ran))
	for _, c := range r.ran {
		lines = append(lines, r.key(c)+": "+c.String())
	}

	return lines
}

// flow is a run's seams over the fakes.
type flow struct {
	builds  *fakeBuilds
	store   *fakeStore
	secrets *fakeSecrets
	keys    *fakeKeys
	runner  *fakeRunner
	srv     *githubtest.Server
	repo    *githubtest.Repo
}

// newFlow sets the fakes up: the build, the stand-in GitHub with the repository (master
// at base1, the pull request's head1 and the push's head2 ahead of it, the files each
// changes) and the app registered, the keys, and the tofu runs.
func newFlow(t *testing.T, subs map[string]string, appID string, runs map[string]tofuRun) *flow {
	t.Helper()

	key, pemKey := testKey(t)
	srv := githubtest.New(t)
	srv.AppID, srv.AppKey, srv.AppInstallation = appID, &key.PublicKey, installation
	repo := srv.AddRepo(owner, repoName, &githubtest.Repo{
		Refs:     map[string]github.Object{"refs/heads/master": {Type: "commit", SHA: baseSHA}},
		Ancestry: map[string][]string{headSHA: {headSHA, baseSHA}, pushSHA: {pushSHA, baseSHA}, baseSHA: {baseSHA}},
		Changed: map[string][]string{
			headSHA: {"2-shr/registry.tf", "2-env/locals.tf", "README.md"},
			pushSHA: {"2-net/dns.tf", "2-env/identities.tf"},
		},
		Comments: map[int][]github.Comment{7: {{ID: 1, Body: "a person's comment"}, {ID: 2, Body: markerOf("2-shr") + "\n### plan of 2-shr\n\nan earlier push's plan\n"}}},
	})

	return &flow{
		builds:  &fakeBuilds{build: buildJSON(subs)},
		store:   &fakeStore{objects: map[string]string{}},
		secrets: &fakeSecrets{payloads: map[string]string{deployerVersion: pemKey, infraVersion: pemKey}},
		keys:    &fakeKeys{keys: map[string][]org.APIKey{"imp-tst-gbl-core-1a2b": {{Name: "projects/100000000002/locations/global/keys/0f1e", DisplayName: org.FirebaseBrowserKey}}}},
		runner:  &fakeRunner{runs: runs},
		srv:     srv,
		repo:    repo,
	}
}

func (f *flow) clients() *Clients {
	return &Clients{
		Builds: f.builds.open, Store: f.store.open, Secrets: f.secrets.open, Keys: f.keys.openAs, Exec: f.runner,
		GitHub: func(token string) *github.Client {
			return github.New(f.srv.URL, token)
		},
	}
}

// request is the trigger's request over the checkout.
func request(dir string) *Request {
	return &Request{BuildID: "build-1", Project: "imp-boot-gbl-core-a1b2", Location: "us-central1", Dir: dir}
}

// TestPlan plans a pull request: the layers its changed files touch, side by side as
// their plan identities, each judged in layer order, the comments posted or updated on
// the pull request as the deployer app, and the run failing when a plan does.
func TestPlan(t *testing.T) {
	t.Parallel()

	runs := map[string]tofuRun{
		"2-shr":     {text: succeededPlan, outputs: map[string][]string{"repository_names": {createAction}}},
		"2-env tst": {planFails: true, text: failedPlan, diagnostics: []diagnostic{lacking("repository_names")}},
		"2-env stg": {text: "No changes. Your infrastructure matches the configuration.\n"},
		"2-env prd": {planFails: true, text: failedPlan, diagnostics: []diagnostic{invalidRef}},
	}
	f := newFlow(t, pullRequestSubs(), deployerAppID, runs)
	dir := checkout(t, func(*org.Placement) {})
	var out bytes.Buffer
	err := Plan(t.Context(), f.clients(), request(dir), &out)
	if err == nil || !strings.Contains(err.Error(), "the plan of 2-env prd failed; the log says why") {
		t.Fatalf("Plan() error = %v, want the failed plan named; output:\n%s", err, out.String())
	}
	wantOut := []string{
		"The pull request changes 3 file(s) against master; layers to plan: 2-shr and 2-env.\n",
		"=== plan of 2-shr as imp-shr-gbl-plan@imp-shr-gbl-core-7a8b.iam.gserviceaccount.com ===\n",
		"=== plan of 2-env tst as imp-tst-gbl-plan@imp-tst-gbl-core-1a2b.iam.gserviceaccount.com ===\n",
		"2-shr: Plan: 1 to add, 0 to change, 0 to destroy.\n",
		"2-env tst: planned after 2-shr applies: this plan reads the output repository_names, which 2-shr's plan creates, and the merge applies 2-shr first.\n",
		"2-env stg: No changes. Your infrastructure matches the configuration.\n",
		"2-env prd: The plan failed: `Reference to undeclared resource: A managed resource \"google_project\" \"x\" has not been declared in the root module.`.\n",
		"Posted the plans on pull request #7 as the deployer app.\n",
	}
	for _, want := range wantOut {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
	if want := []string{"projects/imp-boot-gbl-core-a1b2/locations/us-central1/connections/imp-boot-uc1-github/repositories/imp-boot-uc1-infrastructure-repo"}; !slices.Equal(f.builds.asked, want) {
		t.Errorf("read tokens asked for %v, want %v", f.builds.asked, want)
	}
	lines := f.runner.lines()
	wantLines := []string{
		"2-shr: tofu init -input=false -no-color -backend-config=prefix=2-shr -backend-config=impersonate_service_account=imp-shr-gbl-plan@imp-shr-gbl-core-7a8b.iam.gserviceaccount.com",
		"2-env tst: tofu init -input=false -no-color -backend-config=prefix=2-env/tst -backend-config=impersonate_service_account=imp-tst-gbl-plan@imp-tst-gbl-core-1a2b.iam.gserviceaccount.com",
		"2-env prd: tofu plan -input=false -no-color -out=",
		"-lock=false -var environment=prd",
	}
	for _, want := range wantLines {
		if !slices.ContainsFunc(lines, func(line string) bool { return strings.Contains(line, want) }) {
			t.Errorf("the commands lack %q:\n%s", want, strings.Join(lines, "\n"))
		}
	}
	for _, c := range f.runner.ran {
		if !slices.Contains(c.Env, identityEnv+"="+strings.TrimPrefix(c.Args[len(c.Args)-1], "-backend-config=impersonate_service_account=")) && c.Args[0] == "init" {
			t.Errorf("init runs without the identity in its environment: %v", c.Env)
		}
		if slices.ContainsFunc(c.Env, func(e string) bool { return strings.HasPrefix(e, githubEnv+"=") }) {
			t.Errorf("a run outside 1-org carries GITHUB_TOKEN: %v", c)
		}
	}
	shows := 0
	for _, line := range lines {
		if strings.Contains(line, "tofu show -json") {
			shows++
		}
	}
	if shows != 2 {
		t.Errorf("tofu show ran %d time(s), want 2 (the plans that succeeded)", shows)
	}
	comments := f.repo.Comments[7]
	if len(comments) != 5 {
		t.Fatalf("the pull request has %d comments, want 5 (the person's, the updated one and three new):\n%+v", len(comments), comments)
	}
	if !strings.Contains(comments[1].Body, markerOf("2-shr")+"\n### plan of 2-shr\n\nAs `imp-shr-gbl-plan@imp-shr-gbl-core-7a8b.iam.gserviceaccount.com` at head1: Plan: 1 to add, 0 to change, 0 to destroy.\n") || strings.Contains(comments[1].Body, "an earlier push's plan") {
		t.Errorf("2-shr's comment was not updated in place:\n%s", comments[1].Body)
	}
	for i, name := range []string{"2-env tst", "2-env stg", "2-env prd"} {
		if !strings.HasPrefix(comments[2+i].Body, markerOf(name)) {
			t.Errorf("comment %d is not %s's:\n%s", 2+i, name, comments[2+i].Body)
		}
	}
	if !strings.Contains(comments[4].Body, "at head1: The plan failed: `Reference to undeclared resource: A managed resource \"google_project\" \"x\" has not been declared in the root module.`.\n") {
		t.Errorf("2-env prd's comment lacks its verdict:\n%s", comments[4].Body)
	}
}

// TestPlanRefusals: the plan's refusals and its quiet paths, each with its message.
func TestPlanRefusals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		subs    func(subs map[string]string)
		mutate  func(p *org.Placement)
		changed []string
		runs    map[string]tofuRun
		wantErr string
		wantOut []string
		absent  []string
	}{
		{
			name:    "no layer changes",
			changed: []string{"README.md", "placement.json"},
			wantOut: []string{"The pull request changes 2 file(s) against master; layers to plan: none.\n", "No layer changes in this pull request: nothing to plan.\n"},
		},
		{
			name:    "a layer whose project the placement does not record",
			mutate:  func(p *org.Placement) { delete(p.Projects, "net") },
			changed: []string{"2-net/dns.tf", "2-shr/registry.tf"},
			wantErr: "the identities of 2-net are under a project placement.json does not record yet (REPLACEME): record the project ids (projects.net) and run bedrock org render",
		},
		{
			name:    "no pull request number",
			subs:    func(subs map[string]string) { delete(subs, prNumberSub) },
			wantErr: `_PR_NUMBER is "": the plan trigger runs on a pull request, and Cloud Build names it there`,
		},
		{
			name:    "no base branch",
			subs:    func(subs map[string]string) { delete(subs, baseBranchSub) },
			wantErr: "_BASE_BRANCH is not set: the plan trigger runs on a pull request, and Cloud Build names its base there",
		},
		{
			name:    "no commit",
			subs:    func(subs map[string]string) { delete(subs, commitSub) },
			wantErr: "COMMIT_SHA is not set: a trigger's run names the commit it builds",
		},
		{
			name:    "a build that did not start from the trigger",
			subs:    func(subs map[string]string) { delete(subs, stepsSub) },
			wantErr: "no _BEDROCK_STEPS substitution: bedrock org plan runs from the plan trigger 0-bootstrap renders",
		},
		{
			name:    "another bedrock's steps are said, and the run goes on",
			subs:    func(subs map[string]string) { subs[stepsSub] = "0" },
			changed: []string{"2-shr/registry.tf"},
			runs:    map[string]tofuRun{"2-shr": {text: succeededPlan}},
			wantOut: []string{"This run's steps are of shape 0, and this bedrock renders shape " + org.StepsVersion, "2-shr: Plan: 1 to add, 0 to change, 0 to destroy.\n"},
		},
		{
			name:    "the deployer app not recorded: the plans stay in the log",
			mutate:  func(p *org.Placement) { p.GithubDeployerAppID, p.GithubDeployerKeyVersion = "", "" },
			changed: []string{"2-shr/registry.tf"},
			runs:    map[string]tofuRun{"2-shr": {text: succeededPlan}},
			wantOut: []string{"placement.json records no githubDeployerAppId or githubDeployerKeyVersion (0-bootstrap/README.md, \"The deployer GitHub App's key\"): the plans are in this log alone until both are recorded.\n"},
			absent:  []string{"Posted the plans"},
		},
		{
			name:    "an environment without its layer",
			subs:    func(subs map[string]string) { subs[environmentSub] = "stg" },
			wantErr: "_ENVIRONMENT is set to stg without _LAYER: name the layer too (_LAYER=2-env)",
		},
		{
			name:    "a layer that is not one",
			subs:    func(subs map[string]string) { subs[layerSub] = "3-app" },
			wantErr: `_LAYER is 3-app and _ENVIRONMENT is "": no such run; the layers are 0-bootstrap, 1-org, 2-shr, 2-spn, 2-net, 2-env, and _ENVIRONMENT names one of tst, stg, prd for 2-env`,
		},
		{
			name:    "a layer named: planned alone, whatever the pull request changes",
			subs:    func(subs map[string]string) { subs[layerSub], subs[environmentSub] = "2-env", "stg" },
			changed: []string{"2-shr/registry.tf"},
			runs:    map[string]tofuRun{"2-env stg": {text: succeededPlan}},
			wantOut: []string{"_LAYER names 2-env stg: planning it alone.\n", "2-env stg: Plan: 1 to add, 0 to change, 0 to destroy.\n"},
			absent:  []string{"2-shr:"},
		},
		{
			name:    "a plan that could not start",
			changed: []string{"2-spn/instance.tf"},
			runs:    map[string]tofuRun{"2-spn": {initFails: true}},
			wantErr: "the plan of 2-spn failed; the log says why",
			wantOut: []string{"2-spn: The plan did not run; the log says why.\n", "Error: Failed to get existing workspaces\n"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			subs := pullRequestSubs()
			if tt.subs != nil {
				tt.subs(subs)
			}
			f := newFlow(t, subs, deployerAppID, tt.runs)
			if tt.changed != nil {
				f.repo.Changed[headSHA] = tt.changed
			}
			mutate := tt.mutate
			if mutate == nil {
				mutate = func(*org.Placement) {}
			}
			dir := checkout(t, mutate)
			var out bytes.Buffer
			err := Plan(t.Context(), f.clients(), request(dir), &out)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Plan() error = %v, want %q; output:\n%s", err, tt.wantErr, out.String())
				}
			} else if err != nil {
				t.Fatalf("Plan() error = %v; output:\n%s", err, out.String())
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out.String(), want) {
					t.Errorf("output lacks %q:\n%s", want, out.String())
				}
			}
			for _, absent := range tt.absent {
				if strings.Contains(out.String(), absent) {
					t.Errorf("output carries %q:\n%s", absent, out.String())
				}
			}
		})
	}
}

// TestPlanOrgToken: a plan of 1-org gets the infrastructure app's installation token as
// GITHUB_TOKEN, minted from the key in the boot project; without the app recorded the
// run is refused, naming the steps.
func TestPlanOrgToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(p *org.Placement)
		wantErr string
	}{
		{name: "the token minted", mutate: func(p *org.Placement) { p.GithubDeployerAppID, p.GithubDeployerKeyVersion = "", "" }},
		{
			name:    "the app not recorded",
			mutate:  func(p *org.Placement) { p.GithubInfrastructureAppID, p.GithubInfrastructureKeyVersion = "", "" },
			wantErr: "1-org's GitHub provider needs the infrastructure GitHub App (0-bootstrap/README.md, \"The infrastructure GitHub App\"): create it, record its App ID as githubInfrastructureAppId in placement.json, add its key to imp-boot-gbl-github-infrastructure-key in the boot project with bedrock secret add github-infrastructure-key, and pin the version with bedrock secret pin github-infrastructure-key <version>, which records githubInfrastructureKeyVersion and runs bedrock org render",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newFlow(t, pullRequestSubs(), infraAppID, map[string]tofuRun{"1-org": {text: succeededPlan}})
			f.repo.Changed[headSHA] = []string{"1-org/projects.tf"}
			dir := checkout(t, tt.mutate)
			var out bytes.Buffer
			err := Plan(t.Context(), f.clients(), request(dir), &out)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Plan() error = %v, want %q", err, tt.wantErr)
				}
				if len(f.runner.ran) != 0 {
					t.Errorf("tofu ran %d command(s) after the refusal", len(f.runner.ran))
				}

				return
			}
			if err != nil {
				t.Fatalf("Plan() error = %v; output:\n%s", err, out.String())
			}
			for _, c := range f.runner.ran {
				if !slices.Contains(c.Env, githubEnv+"=test-token") {
					t.Errorf("%s runs without the installation token: %v", c, c.Env)
				}
			}
		})
	}
}

// TestApply applies a push: the layers changed since their markers' commits and the
// ones with no marker, in layer order as their apply identities, each marker written
// after its apply, Firebase's browser key restricted after each apply of 2-env, and
// the run stopping at the first failure.
func TestApply(t *testing.T) {
	t.Parallel()

	runs := map[string]tofuRun{
		"2-shr":     {text: succeededPlan},
		"2-net":     {text: succeededPlan},
		"2-env tst": {text: succeededPlan},
		"2-env stg": {text: succeededPlan, applyFails: true},
		"2-env prd": {text: succeededPlan},
	}
	f := newFlow(t, pushSubs(), deployerAppID, runs)
	f.store.objects = map[string]string{
		"applied/0-bootstrap": pushSHA, "applied/1-org": baseSHA, "applied/2-spn": baseSHA, "applied/2-net": baseSHA,
		"applied/2-env/tst": baseSHA, "applied/2-env/stg": baseSHA, "applied/2-env/prd": baseSHA,
	}
	dir := checkout(t, func(*org.Placement) {})
	var out bytes.Buffer
	err := Apply(t.Context(), f.clients(), request(dir), &out)
	if err == nil || !strings.Contains(err.Error(), "the apply of 2-env stg failed; the log says why") {
		t.Fatalf("Apply() error = %v, want the failed apply named; output:\n%s", err, out.String())
	}
	wantOut := []string{
		"0-bootstrap: applied at head2 already.\n",
		"1-org: unchanged since its last apply at base1.\n",
		"2-shr: never applied by the trigger (no marker gs://imp-boot-gbl-state-a1b2/applied/2-shr); applying it.\n",
		"2-spn: unchanged since its last apply at base1.\n",
		"2-net: changed since its last apply at base1; applying it.\n",
		"2-env tst: changed since its last apply at base1; applying it.\n",
		"=== apply of 2-shr as imp-shr-gbl-tofu@imp-shr-gbl-core-7a8b.iam.gserviceaccount.com ===\n",
		"2-shr: Apply complete! Resources: 1 added, 0 changed, 0 destroyed.\n",
		"Recorded the apply of 2-shr at head2 in gs://imp-boot-gbl-state-a1b2/applied/2-shr.\n",
		"Recorded the apply of 2-env tst at head2 in gs://imp-boot-gbl-state-a1b2/applied/2-env/tst.\n",
		"Restricted projects/100000000002/locations/global/keys/0f1e (\"Browser key (auto created by Firebase)\") to identitytoolkit.googleapis.com and securetoken.googleapis.com.\n",
		"=== apply of 2-env stg as imp-stg-gbl-tofu@imp-stg-gbl-core-3c4d.iam.gserviceaccount.com ===\n",
		"2-env stg: Error: creating project: quota\n",
	}
	for _, want := range wantOut {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "apply of 2-env prd") {
		t.Errorf("the run went on past the failure:\n%s", out.String())
	}
	wantWritten := []string{bucket + "/applied/2-shr=head2", bucket + "/applied/2-net=head2", bucket + "/applied/2-env/tst=head2"}
	if !slices.Equal(f.store.written, wantWritten) {
		t.Errorf("markers written = %v, want %v", f.store.written, wantWritten)
	}
	if f.store.listed != nil {
		t.Errorf("the run listed %v: the applier's grant is on the markers' names, and cannot list the bucket", f.store.listed)
	}
	if want := []string{"imp-tst-gbl-tofu@imp-tst-gbl-core-1a2b.iam.gserviceaccount.com"}; !slices.Equal(f.keys.identities, want) {
		t.Errorf("the keys were opened as %v, want %v", f.keys.identities, want)
	}
	lines := f.runner.lines()
	wantLines := []string{
		"2-shr: tofu init -input=false -no-color -backend-config=prefix=2-shr -backend-config=impersonate_service_account=imp-shr-gbl-tofu@imp-shr-gbl-core-7a8b.iam.gserviceaccount.com",
		"2-net: tofu plan -input=false -no-color -out=",
		"2-env tst: tofu apply -input=false -no-color ",
		"2-env stg: tofu apply -input=false -no-color ",
	}
	for _, want := range wantLines {
		if !slices.ContainsFunc(lines, func(line string) bool { return strings.Contains(line, want) }) {
			t.Errorf("the commands lack %q:\n%s", want, strings.Join(lines, "\n"))
		}
	}
	for _, line := range lines {
		if strings.Contains(line, "-lock=false") {
			t.Errorf("an apply's plan ran without the state lock: %s", line)
		}
		if strings.Contains(line, "tofu show") {
			t.Errorf("an apply read its plan's outputs: %s", line)
		}
	}
	order := []string{}
	for _, c := range f.runner.ran {
		if c.Args[0] == "apply" {
			order = append(order, f.runner.key(c))
		}
	}
	if want := []string{"2-shr", "2-net", "2-env tst", "2-env stg"}; !slices.Equal(order, want) {
		t.Errorf("applied in the order %v, want %v", order, want)
	}
}

// TestApplyNamed: _LAYER applies one layer again with no change check, and with
// _ENVIRONMENT one environment of 2-env; without it the three in order.
func TestApplyNamed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		layer     string
		env       string
		wantOrder []string
		wantOut   string
	}{
		{name: "one environment", layer: "2-env", env: "stg", wantOrder: []string{"2-env stg"}, wantOut: "_LAYER names 2-env stg: planning it alone.\n"},
		{name: "every environment", layer: "2-env", wantOrder: []string{"2-env tst", "2-env stg", "2-env prd"}, wantOut: "_LAYER names 2-env tst, 2-env stg and 2-env prd: planning it alone.\n"},
		{name: "a single-run layer", layer: "2-spn", wantOrder: []string{"2-spn"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			subs := pushSubs()
			subs[layerSub], subs[environmentSub] = tt.layer, tt.env
			runs := map[string]tofuRun{}
			for _, name := range tt.wantOrder {
				runs[name] = tofuRun{text: succeededPlan}
			}
			f := newFlow(t, subs, deployerAppID, runs)
			f.store.objects = map[string]string{"applied/2-env/stg": pushSHA, "applied/2-spn": pushSHA}
			dir := checkout(t, func(*org.Placement) {})
			var out bytes.Buffer
			if err := Apply(t.Context(), f.clients(), request(dir), &out); err != nil {
				t.Fatalf("Apply() error = %v; output:\n%s", err, out.String())
			}
			if tt.wantOut != "" && !strings.Contains(out.String(), tt.wantOut) {
				t.Errorf("output lacks %q:\n%s", tt.wantOut, out.String())
			}
			var order []string
			for _, c := range f.runner.ran {
				if c.Args[0] == "apply" {
					order = append(order, f.runner.key(c))
				}
			}
			if !slices.Equal(order, tt.wantOrder) {
				t.Errorf("applied %v, want %v", order, tt.wantOrder)
			}
			if len(f.store.written) != len(tt.wantOrder) {
				t.Errorf("markers written = %v, want one per run", f.store.written)
			}
		})
	}
}

// TestApplyRefusals: the apply's refusals and its quiet paths.
func TestApplyRefusals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(p *org.Placement)
		markers map[string]string
		wantErr string
		wantOut []string
	}{
		{
			name:    "every marker at this commit",
			markers: map[string]string{"applied/0-bootstrap": pushSHA, "applied/1-org": pushSHA, "applied/2-shr": pushSHA, "applied/2-spn": pushSHA, "applied/2-net": pushSHA, "applied/2-env/tst": pushSHA, "applied/2-env/stg": pushSHA, "applied/2-env/prd": pushSHA},
			wantOut: []string{"No layer changes since the last applies: nothing to apply.\n"},
		},
		{
			name:    "no state bucket recorded",
			mutate:  func(p *org.Placement) { p.StateBucket = "" },
			wantErr: "placement.json records no stateBucket: the layers' state and the apply markers live there (0-bootstrap/README.md, step 1), and bedrock org render reads it",
		},
		{
			name:    "a changed layer whose project the placement does not record",
			mutate:  func(p *org.Placement) { delete(p.Projects, "net") },
			markers: map[string]string{"applied/0-bootstrap": pushSHA, "applied/1-org": pushSHA, "applied/2-shr": pushSHA, "applied/2-spn": pushSHA, "applied/2-net": baseSHA, "applied/2-env/tst": pushSHA, "applied/2-env/stg": pushSHA, "applied/2-env/prd": pushSHA},
			wantErr: "the identities of 2-net are under a project placement.json does not record yet (REPLACEME): record the project ids (projects.net) and run bedrock org render",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newFlow(t, pushSubs(), deployerAppID, nil)
			if tt.markers != nil {
				f.store.objects = tt.markers
			}
			mutate := tt.mutate
			if mutate == nil {
				mutate = func(*org.Placement) {}
			}
			dir := checkout(t, mutate)
			var out bytes.Buffer
			err := Apply(t.Context(), f.clients(), request(dir), &out)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Apply() error = %v, want %q; output:\n%s", err, tt.wantErr, out.String())
				}
			} else if err != nil {
				t.Fatalf("Apply() error = %v; output:\n%s", err, out.String())
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out.String(), want) {
					t.Errorf("output lacks %q:\n%s", want, out.String())
				}
			}
			if len(f.runner.ran) != 0 {
				t.Errorf("tofu ran %d command(s), want none", len(f.runner.ran))
			}
		})
	}
}

// TestChangedLayersUnknownCommit: a marker naming a commit GitHub no longer knows (a
// rewritten history) counts every layer as changed.
func TestChangedLayersUnknownCommit(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"message": "Not Found"}`)
	}))
	t.Cleanup(srv.Close)
	var out bytes.Buffer
	r := &run{out: &out, owner: owner, repo: repoName, gh: github.New(srv.URL, "test-token")}
	layers, err := r.changedLayers(t.Context(), "gone1", pushSHA)
	if err != nil {
		t.Fatalf("changedLayers() error = %v", err)
	}
	if layers != nil {
		t.Errorf("changedLayers() = %v, want nil (every layer)", layers)
	}
	if want := "GitHub does not know commit gone1, which a marker names: every layer counts as changed.\n"; out.String() != want {
		t.Errorf("printed %q, want %q", out.String(), want)
	}
}

// TestClientsReady refuses a run without its seams.
func TestClientsReady(t *testing.T) {
	t.Parallel()

	f := newFlow(t, pushSubs(), deployerAppID, nil)
	clients := f.clients()
	clients.Keys = nil
	var out bytes.Buffer
	err := Apply(t.Context(), clients, request(checkout(t, func(*org.Placement) {})), &out)
	if err == nil || !strings.Contains(err.Error(), "no API Keys client is wired") {
		t.Fatalf("Apply() error = %v, want the missing seam named", err)
	}
}
