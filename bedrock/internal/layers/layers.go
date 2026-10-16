// Package layers plans and applies the organization's layers from the Cloud Build
// triggers 0-bootstrap renders: bedrock org plan on the plan trigger, on a pull request,
// and bedrock org apply on the apply trigger, on a push to the default branch or started
// by hand for one layer. Both read the build they run in through the Cloud Build API (the
// pull request, the commit, _LAYER and _ENVIRONMENT), the changed files through the
// connection's read token, the placement off the checkout, and drive tofu in each layer's
// directory as that layer's own identity, impersonated by the trigger's runner.
package layers

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/deploy"
	"github.com/cccteam/ccc/bedrock/internal/github"
	"github.com/cccteam/ccc/bedrock/internal/org"
)

// The substitutions a run reads: the trigger's own (_LAYER, _ENVIRONMENT, the steps'
// shape) and Cloud Build's (the pull request, the branches, the commit).
const (
	layerSub       = "_LAYER"
	environmentSub = "_ENVIRONMENT"
	stepsSub       = "_BEDROCK_STEPS"
	prNumberSub    = "_PR_NUMBER"
	baseBranchSub  = "_BASE_BRANCH"
	headBranchSub  = "_HEAD_BRANCH"
	commitSub      = "COMMIT_SHA"
	// tofuVersionFile is the repository's OpenTofu pin, which changes every layer.
	tofuVersionFile = ".opentofu-version"
	// lockFile is a layer's provider lock, which changes the layer.
	lockFile = ".terraform.lock.hcl"
	// stateObject is the object a run's state lives in under its prefix in the bucket.
	stateObject = "default.tfstate"
)

// The two commands, as the triggers run them, and the layer whose GitHub provider
// needs a token.
const (
	planCommand  = "plan"
	applyCommand = "apply"
	orgLayer     = "1-org"
)

// Request is what a run was started with: the build (BUILD_ID, PROJECT_ID and LOCATION,
// which the trigger's step passes) and the checkout, the repository root.
type Request struct {
	BuildID  string
	Project  string
	Location string
	Dir      string
}

// check refuses a request the trigger's step did not make.
func (req *Request) check(command string) error {
	for name, value := range map[string]string{"BUILD_ID": req.BuildID, "PROJECT_ID": req.Project, "LOCATION": req.Location} {
		if value == "" {
			return errors.Newf("%s is not set: bedrock org %s runs from the %s trigger 0-bootstrap renders (0-bootstrap/cloud-build.tf), whose step passes the build's id, project and location to it", name, command, command)
		}
	}

	return nil
}

// Clients are the seams a run reaches the world through: Cloud Build (the build and the
// connection's read token), Cloud Storage (the applied markers, and as a plan identity
// the state it would plan against), Secret Manager (the apps' keys), GitHub (with a
// token), the API Keys API as an impersonated identity, and the programs a step runs
// (tofu). Tests pass fakes.
type Clients struct {
	Builds  deploy.BuildsFunc
	Store   deploy.StoreFunc
	StoreAs deploy.StoreAsFunc
	Secrets deploy.SecretsFunc
	GitHub  deploy.GitHubFunc
	Keys    org.KeyClientAsFunc
	Exec    deploy.Runner
}

// ready refuses a run whose seams are not all wired.
func (c *Clients) ready() error {
	seams := []struct {
		name string
		nil  bool
	}{
		{"Cloud Build client", c.Builds == nil},
		{"Cloud Storage client", c.Store == nil},
		{"Cloud Storage client as an identity", c.StoreAs == nil},
		{"Secret Manager client", c.Secrets == nil},
		{"GitHub client", c.GitHub == nil},
		{"API Keys client", c.Keys == nil},
		{"program runner", c.Exec == nil},
	}
	for _, seam := range seams {
		if seam.nil {
			return errors.Newf("no %s is wired", seam.name)
		}
	}

	return nil
}

// run is one run of a trigger: the placement off the checkout, the build's
// substitutions, the repository, and the GitHub client over the connection's read token.
type run struct {
	clients *Clients
	req     *Request
	out     io.Writer
	p       *org.Placement
	subs    map[string]string
	owner   string
	repo    string
	gh      *github.Client
}

// start reads the placement and the build, says what the run is, checks the steps'
// shape and mints the connection's read token.
func start(ctx context.Context, clients *Clients, req *Request, command string, out io.Writer) (*run, error) {
	if err := req.check(command); err != nil {
		return nil, err
	}
	if err := clients.ready(); err != nil {
		return nil, err
	}
	p, err := org.ReadPlacement(filepath.Join(req.Dir, org.PlacementFile))
	if err != nil {
		return nil, err
	}
	builds, err := clients.Builds(ctx)
	if err != nil {
		return nil, err
	}
	data, err := builds.Get(ctx, req.Project, req.Location, req.BuildID)
	if err != nil {
		return nil, err
	}
	build, err := deploy.ParseBuild(data)
	if err != nil {
		return nil, err
	}
	r := &run{clients: clients, req: req, out: out, p: p, subs: build.Substitutions, owner: p.GithubOrganization, repo: p.SourceRepo}
	if err := r.checkSteps(command); err != nil {
		return nil, err
	}
	if r.subs[commitSub] == "" {
		return nil, errors.Newf("%s is not set: a trigger's run names the commit it builds", commitSub)
	}
	repository := "projects/" + req.Project + "/locations/" + req.Location + "/connections/" + p.ConnectionName() + "/repositories/" + p.RepositoryLinkName()
	token, err := builds.ReadToken(ctx, repository)
	if err != nil {
		return nil, errors.Wrapf(err, "could not mint a GitHub token from connection %s", p.ConnectionName())
	}
	r.gh = clients.GitHub(token)

	return r, nil
}

// checkSteps refuses a build that did not start from the triggers (no _BEDROCK_STEPS),
// and says when the steps are of another shape than this bedrock renders: the trigger's
// steps lag one apply behind the repository, so a run on the old steps is expected right
// after a change to them, and the commands keep working under the previous shape.
func (r *run) checkSteps(command string) error {
	steps, ok := r.subs[stepsSub]
	if !ok {
		return errors.Newf("no %s substitution: bedrock org %s runs from the %s trigger 0-bootstrap renders (0-bootstrap/cloud-build.tf), whose substitutions carry the steps' shape; this build did not start from it", stepsSub, command, command)
	}
	if shape := org.StepsShape(r.p); steps != shape {
		fmt.Fprintf(r.out, "This run's steps are of shape %s, and this bedrock renders shape %s for the placement's pin: the trigger's steps lag one apply behind the repository, and the next run of the trigger is the first on the shape this bedrock renders (0-bootstrap/README.md, \"The Cloud Build triggers\"). This run goes on with what the steps pass.\n", steps, shape)
	}

	return nil
}

// layersOf maps the paths a change touches to the layers it changes, in layer order: a
// layer's .tf and .tfvars files and its lock file change the layer, and the OpenTofu
// version changes every layer.
func layersOf(paths []string) []string {
	touched := map[string]bool{}
	for _, path := range paths {
		if path == tofuVersionFile {
			return slices.Clone(org.Layers)
		}
		layer, name, ok := strings.Cut(path, "/")
		if !ok || strings.Contains(name, "/") {
			continue
		}
		if name == lockFile || strings.HasSuffix(name, ".tf") || strings.HasSuffix(name, ".tfvars") {
			touched[layer] = true
		}
	}
	var layers []string
	for _, layer := range org.Layers {
		if touched[layer] {
			layers = append(layers, layer)
		}
	}

	return layers
}

// runsOf is every run of the layers named, in layer order (2-env once per environment).
func (r *run) runsOf(layers []string) []org.LayerRun {
	var runs []org.LayerRun
	for _, lr := range r.p.LayerRuns() {
		if slices.Contains(layers, lr.Layer) {
			runs = append(runs, lr)
		}
	}

	return runs
}

// recorded refuses a run whose identity is under a project the placement does not
// record yet (REPLACEME).
func (r *run) recorded(runs []org.LayerRun) error {
	var unrecorded []string
	for _, lr := range runs {
		if lr.Unrecorded() {
			unrecorded = append(unrecorded, lr.Name)
		}
	}
	if len(unrecorded) == 0 {
		return nil
	}

	return errors.Newf("the identities of %s are under a project placement.json does not record yet (REPLACEME): record the project ids (%s) and run bedrock org render", joined(unrecorded), strings.Join(r.p.UnrecordedProjects(), ", "))
}

// changedFiles is every file head changes against base, as GitHub compares the two
// (base...head: the changes since their merge base).
func (r *run) changedFiles(ctx context.Context, base, head string) ([]string, error) {
	cmp, err := r.gh.Compare(ctx, r.owner, r.repo, base, head)
	if err != nil {
		return nil, err
	}
	files := make([]string, 0, len(cmp.Files))
	for _, f := range cmp.Files {
		files = append(files, f.Filename)
	}

	return files, nil
}

// infrastructureToken mints the infrastructure GitHub App's installation token, which
// 1-org's GitHub provider takes as GITHUB_TOKEN, from the key the boot project holds at
// the version placement.json pins. It is refused while the app is not recorded.
func (r *run) infrastructureToken(ctx context.Context) (string, error) {
	return r.appToken(ctx, org.InfrastructureKey, r.p.GithubInfrastructureAppID, "the infrastructure app", "1-org's GitHub provider needs the infrastructure GitHub App (0-bootstrap/README.md, \"The infrastructure GitHub App\"): create it, record its App ID as githubInfrastructureAppId in placement.json, add its key to %s in the boot project with bedrock secret add github-infrastructure-key, and pin the version with bedrock secret pin github-infrastructure-key <version>, which records githubInfrastructureKeyVersion and runs bedrock org render")
}

// deployerToken mints the deployer GitHub App's installation token, which the plan
// comments are posted with; empty, with the reason, while the app is not recorded.
func (r *run) deployerToken(ctx context.Context) (token, reason string, err error) {
	if r.p.GithubDeployerAppID == "" || r.p.GithubDeployerKeyVersion == "" {
		return "", "placement.json records no githubDeployerAppId or githubDeployerKeyVersion (0-bootstrap/README.md, \"The deployer GitHub App's key\"): the plans are in this log alone until both are recorded", nil
	}
	token, err = r.appToken(ctx, org.DeployerKey, r.p.GithubDeployerAppID, "the deployer app", "")
	if err != nil {
		return "", "", err
	}

	return token, "", nil
}

// appToken mints an app's installation token from its key in the boot project; unset
// names the refusal when the app's id or its key's version is not recorded, with the
// container to add the key to.
func (r *run) appToken(ctx context.Context, key, appID, what, unset string) (string, error) {
	version := r.p.SecretVersionName(key)
	if appID == "" || version == "" {
		return "", errors.Newf(unset, r.p.SecretContainer(key))
	}
	secrets, err := r.clients.Secrets(ctx)
	if err != nil {
		return "", err
	}
	defer secrets.Close()
	pemKey, err := secrets.Access(ctx, version)
	if err != nil {
		return "", errors.Wrapf(err, "%s's key (%s)", what, version)
	}

	return deploy.AppInstallationToken(ctx, r.clients.GitHub, appID, pemKey, r.owner, r.repo, what)
}

// joined joins words as a sentence does: a, b and c.
func joined(words []string) string {
	if len(words) <= 1 {
		return strings.Join(words, "")
	}

	return strings.Join(words[:len(words)-1], ", ") + " and " + words[len(words)-1]
}
