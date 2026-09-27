package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/go-playground/errors/v5"
	"golang.org/x/oauth2/google"

	"github.com/cccteam/ccc/bedrock/internal/github"
)

const (
	// BuildArgsFile holds the declared substitutions as build arguments, a bash array
	// (BUILD_ARGS+=(--build-arg _NAME=value)) the image build sources; an array stays
	// out of the environment file, which the OpenTofu steps read with a plain sh.
	BuildArgsFile = "build-args.sh"
	// notAuthorized starts the connection name the stack passes before 2-env holds the
	// environment's GitHub connection: only a hand-submitted build reaches the pipeline
	// then, and the GitHub checks are skipped with a notice.
	notAuthorized = "CONNECTION_NOT_AUTHORIZED"
	// gcbrun starts the comment that runs a pull-request build; the words after it are
	// its options.
	gcbrun = "/gcbrun"
	// cloudBuildAPI is where a build is described and a repository's token minted.
	cloudBuildAPI = "https://cloudbuild.googleapis.com"
	// cloudPlatformScope is the scope the default credentials are asked for.
	cloudPlatformScope = "https://www.googleapis.com/auth/cloud-platform"
	// maxAnswer bounds what one API answer may carry.
	maxAnswer = 8 << 20
	// tstEnvironment is the one environment a pull request deploys to; the others follow it.
	tstEnvironment = "tst"
	stgEnvironment = "stg"
	prdEnvironment = "prd"
	// trueValue is a boolean fact set, the way the shell steps test it.
	trueValue = "true"
)

// The facts resolve exports, by the names the shell steps read (the record step's are
// beside it), and the trigger's substitution naming the pull request.
const (
	migrateJobFact    = "MIGRATE_JOB"
	sharedDBFact      = "SHARED_DB"
	reloadDBFact      = "RELOAD_DB"
	reloadReasonFact  = "RELOAD_DB_REASON"
	downFact          = "DOWN"
	imageTagFact      = "IMAGE_TAG"
	commitTagFact     = "COMMIT_TAG"
	revisionTagFact   = "REVISION_TAG"
	runMigrationsFact = "RUN_MIGRATIONS"
	prNumberSub       = "_PR_NUMBER"
)

var (
	// environments are the ones a trigger names in _ENV.
	environments = []string{tstEnvironment, stgEnvironment, prdEnvironment}
	// triggerSubstitutions are the substitutions a trigger sets beyond the stack's map
	// (_PR_NUMBER and, on a pull request, the branches) and the pipeline's own default
	// (_DEFAULT_BRANCH): known, so never declared.
	triggerSubstitutions = []string{prNumberSub, "_BASE_BRANCH", "_HEAD_BRANCH", "_HEAD_REPO_URL", "_DEFAULT_BRANCH"}
)

// Clients are the deploy sequence's seams: what its commands open, replaced by fakes in
// tests. Each is opened by the command that needs it, never before.
type Clients struct {
	// Storage opens Cloud Storage, for the deployment records.
	Storage StoreFunc
	// Builds opens Cloud Build, for the build's own description and the repository's
	// GitHub token.
	Builds BuildsFunc
	// Comments reads a pull request's comments with that token.
	Comments CommentsFunc
	// GitHub opens the GitHub client with that token, for the release checks.
	GitHub GitHubFunc
	// Registry opens Artifact Registry, for the release check on the image.
	Registry RegistryFunc
	// Run opens Cloud Run, for the migrate job and the services.
	Run RunFunc
}

// DefaultClients opens the real services.
func DefaultClients() *Clients {
	return &Clients{Storage: NewStorage, Builds: NewCloudBuild, Comments: GitHubComments, GitHub: PublicGitHub, Registry: NewArtifactRegistry, Run: NewCloudRun}
}

// Builds reads a build and mints the GitHub token of the repository it came from,
// through Cloud Build: the API, or a fake in tests.
type Builds interface {
	// Get is the build as Cloud Build describes it, the JSON the API answers.
	Get(ctx context.Context, project, location, id string) ([]byte, error)
	// ReadToken mints a read token for the repository (projects/<p>/locations/<l>/
	// connections/<c>/repositories/<r>) from the connection it is linked through.
	ReadToken(ctx context.Context, repository string) (string, error)
}

// BuildsFunc opens Builds.
type BuildsFunc func(ctx context.Context) (Builds, error)

// NewCloudBuild opens the Cloud Build REST API with the process's default credentials:
// the build's own identity on Cloud Build.
func NewCloudBuild(ctx context.Context) (Builds, error) {
	client, err := google.DefaultClient(ctx, cloudPlatformScope)
	if err != nil {
		return nil, errors.Wrap(err, "google.DefaultClient()")
	}

	return &cloudBuild{http: client, base: cloudBuildAPI}, nil
}

// cloudBuild is Builds over the Cloud Build REST API.
type cloudBuild struct {
	http *http.Client
	base string
}

func (c *cloudBuild) Get(ctx context.Context, project, location, id string) ([]byte, error) {
	return c.call(ctx, http.MethodGet, "/v1/projects/"+project+"/locations/"+location+"/builds/"+id)
}

func (c *cloudBuild) ReadToken(ctx context.Context, repository string) (string, error) {
	data, err := c.call(ctx, http.MethodPost, "/v2/"+repository+":accessReadToken")
	if err != nil {
		return "", err
	}
	var answer struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(data, &answer); err != nil {
		return "", errors.Wrap(err, "json.Unmarshal()")
	}
	if answer.Token == "" {
		return "", errors.Newf("Cloud Build minted no token for %s", repository)
	}

	return answer.Token, nil
}

// call sends one request and returns the answer's body; a status outside 2xx is an
// error carrying the API's message.
func (c *cloudBuild) call(ctx context.Context, method, endpoint string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.base+endpoint, http.NoBody)
	if err != nil {
		return nil, errors.Wrap(err, "http.NewRequestWithContext()")
	}
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "http.Client.Do()")
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswer))
	if err != nil {
		return nil, errors.Wrap(err, "io.ReadAll()")
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode > 299 {
		var refusal struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(data, &refusal)
		msg := refusal.Error.Message
		if msg == "" {
			msg = strings.TrimSpace(string(data))
		}

		return nil, errors.Newf("Cloud Build answered HTTP %d to %s %s: %s", resp.StatusCode, method, endpoint, msg)
	}

	return data, nil
}

// CommentsFunc reads a pull request's comments with the token minted for the build:
// the GitHub API, or a fake in tests.
type CommentsFunc func(ctx context.Context, token, repoFullName string, number int) ([]github.Comment, error)

// GitHubComments reads the comments through the public API.
func GitHubComments(ctx context.Context, token, repoFullName string, number int) ([]github.Comment, error) {
	owner, repo, err := splitRepo(repoFullName)
	if err != nil {
		return nil, err
	}

	return PublicGitHub(token).IssueComments(ctx, owner, repo, number)
}

// ResolveRequest names the build to resolve: its id and where it runs (the pipeline
// passes BUILD_ID, PROJECT_ID and LOCATION to the step), and the pipeline's own
// substitutions (the stack's map), so the declared ones can be told apart.
type ResolveRequest struct {
	BuildID  string
	Project  string
	Location string
	Known    []string
	// Source is the checkout the build runs in (the workspace): where the migration
	// files are read when a pull request's records are compared with the tree.
	Source string
}

// check refuses a request that does not name the build.
func (r *ResolveRequest) check() error {
	for _, v := range []struct{ name, value string }{{"BUILD_ID", r.BuildID}, {"PROJECT_ID", r.Project}, {"LOCATION", r.Location}} {
		if v.value == "" {
			return errors.Newf("%s is not set: the pipeline's resolve step passes the build's id, project and location to the command", v.name)
		}
	}

	return nil
}

// Facts are what resolve found and computed: what the steps after it read.
type Facts struct {
	// Tag or PullRequest names the trigger: a release build carries TAG_NAME, a
	// pull-request build _PR_NUMBER.
	Tag         string
	PullRequest string
	// Environment is the trigger's: tst, stg or prd.
	Environment string
	// Comment is the /gcbrun comment a pull-request build ran on.
	Comment string
	// Token is the repository's GitHub token, minted from the connection; empty for a
	// hand-submitted build before the environment holds one, with Notice saying so.
	Token  string
	Notice string
	// The facts the environment file exports, as the later steps read them.
	Services   string
	MigrateJob string
	SharedDB   bool
	ReloadDB   bool
	// ReloadReason says why the pull request's database is recreated: the comment
	// asked (/gcbrun reload-db), or the migrations the last build applied are no longer
	// in the tree.
	ReloadReason  string
	Down          bool
	Image         string
	ImageTag      string
	CommitTag     string
	Version       string
	Release       string
	RevisionTag   string
	RunMigrations bool
	ShiftTraffic  bool
	// Substitutions are every substitution of the build; Declared are the ones the
	// placement added beyond the contract, sorted.
	Substitutions map[string]string
	Declared      []string
	// build is the build as the API described it, kept for the build file.
	build []byte
}

// Resolve reads the build and works out the facts: the trigger's kind, the pull
// request's instruction, the image and its tags, whether the migrate job runs and
// traffic shifts. It refuses a build that is neither a tag's nor a pull request's, one
// that names no services or migrate job, a pull-request build outside tst, one with no
// connection to read its comment through or no /gcbrun comment, an unknown option, and
// shared-db with reload-db.
func Resolve(ctx context.Context, clients *Clients, req *ResolveRequest, out io.Writer) (*Facts, error) {
	if err := req.check(); err != nil {
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
	f, err := newFacts(data)
	if err != nil {
		return nil, err
	}
	if err := f.mint(ctx, builds, req, out); err != nil {
		return nil, err
	}
	if err := f.trigger(ctx, clients.Comments, out); err != nil {
		return nil, err
	}
	if err := f.staleDatabase(ctx, clients.Storage, req.Source, out); err != nil {
		return nil, err
	}
	f.tags(req.Known)
	f.report(out)

	return f, nil
}

// newFacts reads the build and refuses one the pipeline cannot run: a build is a tag's
// or a pull request's, in one of the environments, and names what it updates (the
// services, region=name comma-separated, and the migrate job, region=name, from the
// application layer's substitutions output).
func newFacts(data []byte) (*Facts, error) {
	var build Build
	if err := json.Unmarshal(data, &build); err != nil {
		return nil, errors.Wrap(err, "json.Unmarshal(): the build")
	}
	subs := build.Substitutions
	if subs == nil {
		subs = map[string]string{}
	}
	f := &Facts{Tag: subs["TAG_NAME"], PullRequest: subs[prNumberSub], Substitutions: subs, build: data, RunMigrations: true, ShiftTraffic: true}
	if f.Tag == "" && f.PullRequest == "" {
		return nil, errors.New("neither TAG_NAME nor _PR_NUMBER is set; a build is a tag's or a pull request's")
	}
	f.Environment = subs["_ENV"]
	if !slices.Contains(environments, f.Environment) {
		return nil, errors.Newf("_ENV must be tst, stg, or prd (got %q)", f.Environment)
	}
	if f.Tag == "" && f.Environment != tstEnvironment {
		return nil, errors.Newf("a pull-request build deploys only to tst (this trigger's _ENV is %s)", f.Environment)
	}
	f.Services, f.MigrateJob = subs["_SERVICES"], subs["_MIGRATE_JOB"]
	if f.Services == "" || f.MigrateJob == "" {
		return nil, errors.New("_SERVICES and _MIGRATE_JOB name the Cloud Run services and the migrate job this build updates; one is empty")
	}

	return f, nil
}

// mint takes a GitHub token for the repository from the Cloud Build connection the
// trigger reads it through; the deploy identity needs Read Token Accessor on it.
// Before the environment holds a connection (3-app passes CONNECTION_NOT_AUTHORIZED_IN_
// 2-ENV), only a build submitted by hand reaches here, and the GitHub checks are skipped
// with a notice; a triggered build always has a connection.
func (f *Facts) mint(ctx context.Context, builds Builds, req *ResolveRequest, out io.Writer) error {
	connection := f.Substitutions["_REPO_CONNECTION_NAME"]
	if strings.HasPrefix(connection, notAuthorized) {
		f.Notice = fmt.Sprintf("Notice: no Cloud Build connection in %s yet; the branch check and the pull-request comment are skipped (hand-submitted build).", f.Environment)
		fmt.Fprintln(out, f.Notice)

		return nil
	}
	repository := "projects/" + req.Project + "/locations/" + req.Location + "/connections/" + connection + "/repositories/" + f.Substitutions["_REPO_NAME"]
	token, err := builds.ReadToken(ctx, repository)
	if err != nil {
		return errors.Wrapf(err, "could not mint a GitHub token from connection %s", connection)
	}
	// The branch check and the comment read address the repository by its full name, a
	// trigger built-in; a hand-submitted build passes it, or the compare answers 404 for
	// an empty name.
	if f.Substitutions["REPO_FULL_NAME"] == "" {
		return errors.New("REPO_FULL_NAME is not set; a hand-submitted build passes it as <organization>/<repository>")
	}
	f.Token = token

	return nil
}

// trigger reads the trigger: a tag names the version and the release (whether it may
// deploy is the release check's question); a pull request's build reads its latest
// /gcbrun comment for the instruction. A pull request has an environment of its own
// (its stack, applied by the ApplyPullRequestStack step): the service is its own, so
// the revision takes traffic, and the database is its own, so the migrations run every
// build.
func (f *Facts) trigger(ctx context.Context, comments CommentsFunc, out io.Writer) error {
	if f.Tag != "" {
		fmt.Fprintf(out, "Triggered by tag %s\n", f.Tag)
		f.Version, f.Release = f.Tag, f.Tag

		return nil
	}
	fmt.Fprintf(out, "Triggered by pull request %s\n", f.PullRequest)
	short := f.Substitutions["SHORT_SHA"]
	f.Version = "pr" + f.PullRequest + "@" + short
	// The registry's tags are immutable and the records bucket refuses overwrites, so a
	// pull request's release carries the commit: each build of the pull request is its
	// own image tag and record, while the revision tag stays pr<N>.
	f.Release = "pr" + f.PullRequest + "-" + short
	if f.Token == "" {
		return errors.New("a pull-request build needs the Cloud Build connection to read its /gcbrun comment")
	}
	number, err := strconv.Atoi(f.PullRequest)
	if err != nil || number <= 0 {
		return errors.Newf("_PR_NUMBER %q is not a pull request number", f.PullRequest)
	}
	all, err := comments(ctx, f.Token, f.Substitutions["REPO_FULL_NAME"], number)
	if err != nil {
		return err
	}
	if err := f.instruction(all); err != nil {
		return err
	}
	fmt.Fprintf(out, "COMMENT_BODY=%s\n", f.Comment)

	return nil
}

// staleDatabase decides whether a pull request's own database is recreated without
// being asked. The newest record of the pull request lists the migrations its build
// applied, by name and content; when one of them is no longer in the tree as it was
// (renumbered past an index the default branch took, or changed before it reached the
// default branch), the database holds a history the files no longer describe, so it is
// replaced and the migrations apply afresh: a pull request's database is disposable.
// The first build has no record and decides nothing; a record left by a build whose
// environment was torn down since is harmless, since the plan step replaces only a
// database that exists.
func (f *Facts) staleDatabase(ctx context.Context, open StoreFunc, source string, out io.Writer) error {
	if f.PullRequest == "" || f.Down || f.SharedDB || f.ReloadDB || !f.RunMigrations {
		return nil
	}
	bucket, app := f.Substitutions[recordsBucket], f.Substitutions[appSub]
	if bucket == "" || app == "" {
		return nil
	}
	store, err := open(ctx)
	if err != nil {
		return err
	}
	defer store.Close()
	newest, err := newestRecord(ctx, store, bucket, app+"/"+f.Environment+"/pr"+f.PullRequest+"-")
	if err != nil {
		return err
	}
	if newest == nil {
		return nil
	}
	var gone []string
	for _, m := range newest.Migrations {
		hash, err := hashFile(filepath.Join(source, filepath.FromSlash(m.Dir), m.Name))
		if err != nil {
			return err
		}
		if hash != m.Hash {
			gone = append(gone, path.Join(m.Dir, m.Name))
		}
	}
	if len(gone) == 0 {
		return nil
	}
	f.ReloadDB = true
	f.ReloadReason = fmt.Sprintf("the database applied %s (build %s), which the tree no longer carries as applied: renumbered or changed since, so the database is recreated and the migrations apply afresh", strings.Join(gone, ", "), newest.Build)
	fmt.Fprintf(out, "Reload: %s\n", f.ReloadReason)

	return nil
}

// newestRecord reads the records under the prefix and returns the newest that lists
// migrations, or nil.
func newestRecord(ctx context.Context, store Store, bucket, prefix string) (*Record, error) {
	names, err := store.List(ctx, bucket, prefix)
	if err != nil {
		return nil, err
	}
	var newest *Record
	for _, name := range names {
		data, err := store.Read(ctx, bucket, name)
		if err != nil {
			return nil, err
		}
		var r Record
		if err := json.Unmarshal(data, &r); err != nil {
			return nil, errors.Wrapf(err, "json.Unmarshal(): gs://%s/%s", bucket, name)
		}
		if len(r.Migrations) == 0 {
			continue
		}
		if newest == nil || r.Timestamp > newest.Timestamp {
			newest = &r
		}
	}

	return newest, nil
}

// instruction reads the latest /gcbrun comment: the words after it are its options
// (shared-db: the site runs against tst's database and the migrate job does not run;
// reload-db: the pull request's own database is recreated; down: the pull request's
// environment is destroyed and nothing deploys). Anything else is a typo and stops the
// build rather than being ignored.
func (f *Facts) instruction(all []github.Comment) error {
	for i := len(all) - 1; i >= 0; i-- {
		if strings.HasPrefix(all[i].Body, gcbrun) {
			f.Comment = strings.TrimSpace(all[i].Body)

			break
		}
	}
	if f.Comment == "" {
		return errors.Newf("no %s comment found on pull request %s", gcbrun, f.PullRequest)
	}
	for _, word := range strings.Fields(f.Comment)[1:] {
		switch word {
		case "shared-db":
			f.SharedDB = true
		case "reload-db":
			f.ReloadDB, f.ReloadReason = true, gcbrun+" reload-db"
		case "down":
			f.Down = true
		default:
			return errors.Newf("unknown %s option %q (the options are shared-db, reload-db and down)", gcbrun, word)
		}
	}
	if f.SharedDB && f.ReloadDB {
		return errors.Newf("%s shared-db with reload-db: reload-db recreates the pull request's own database, and in shared mode there is none", gcbrun)
	}
	if f.SharedDB {
		// tst's schema is tst's: the migrate job never runs against the shared database
		// (the guard refuses shared-db when the migrations changed at all).
		f.RunMigrations = false
	}

	return nil
}

// tags names the image and its tags and sorts out the declared substitutions. One
// registry serves every environment and each environment is a fresh build of the tag
// (nothing is promoted), so the image tags carry the environment: <release>-<env> and
// <commit>-<env>; the release check compares those.
func (f *Facts) tags(known []string) {
	f.Image = f.Substitutions["_REGISTRY"] + "/" + f.Substitutions["_APP"]
	f.ImageTag = f.Release + "-" + f.Environment
	f.CommitTag = f.Substitutions["COMMIT_SHA"] + "-" + f.Environment
	for _, name := range f.substitutionNames() {
		if !slices.Contains(known, name) && !slices.Contains(triggerSubstitutions, name) {
			f.Declared = append(f.Declared, name)
		}
	}
}

// substitutionNames are the build's substitutions that are not Cloud Build's own, sorted.
func (f *Facts) substitutionNames() []string {
	names := make([]string, 0, len(f.Substitutions))
	for name := range f.Substitutions {
		if strings.HasPrefix(name, "_") {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	return names
}

// report prints the facts the way the build's log has always shown them.
func (f *Facts) report(out io.Writer) {
	fmt.Fprintf(out, "IMAGE=%s IMAGE_TAG=%s VERSION=%s RELEASE=%s\n", f.Image, f.ImageTag, f.Version, f.Release)
	fmt.Fprintf(out, "RUN_MIGRATIONS=%t SHIFT_TRAFFIC=%t REVISION_TAG=%s\n", f.RunMigrations, f.ShiftTraffic, f.RevisionTag)
	declared := "none"
	if len(f.Declared) > 0 {
		declared = strings.Join(f.Declared, ", ")
	}
	fmt.Fprintf(out, "Declared substitutions for the hooks and the image build: %s\n", declared)
}

// Write leaves the facts in the workspace for the steps after: the environment file
// (the facts, then every substitution of the build as export _NAME=value, for the
// hooks), the build arguments file (the declared substitutions, one --build-arg each,
// for the Dockerfile's ARG _NAME) and the build file. The files are the step's own
// (0600): every step runs as root in its image and reads them regardless.
func (f *Facts) Write(w Workspace) error {
	files := map[string][]byte{EnvironmentFile: []byte(f.environment()), BuildArgsFile: []byte(f.buildArgs()), BuildFile: f.build}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(string(w), name), content, 0o600); err != nil {
			return errors.Wrapf(err, "os.WriteFile(): %s", name)
		}
	}

	return nil
}

// environment is the environment file: the facts, double-quoted, then the substitutions,
// single-quoted, each an export line a shell sources.
func (f *Facts) environment() string {
	var b strings.Builder
	facts := [][2]string{
		{"GITHUB_TOKEN", f.Token},
		{services, f.Services},
		{migrateJobFact, f.MigrateJob},
		{sharedDBFact, flag(f.SharedDB)},
		{reloadDBFact, flag(f.ReloadDB)},
		{reloadReasonFact, f.ReloadReason},
		{downFact, flag(f.Down)},
		{skipDeploy, ""},
		{imageFact, f.Image},
		{imageTagFact, f.ImageTag},
		{commitTagFact, f.CommitTag},
		{versionFact, f.Version},
		{releaseFact, f.Release},
		{revisionTagFact, f.RevisionTag},
		{runMigrationsFact, strconv.FormatBool(f.RunMigrations)},
		{shiftTraffic, strconv.FormatBool(f.ShiftTraffic)},
	}
	for _, kv := range facts {
		b.WriteString("export " + kv[0] + "=" + doubleQuote(kv[1]) + "\n")
	}
	for _, name := range f.substitutionNames() {
		b.WriteString("export " + name + "=" + singleQuote(f.Substitutions[name]) + "\n")
	}

	return b.String()
}

// buildArgs is the build arguments file: a bash array of --build-arg _NAME=value, one
// per declared substitution.
func (f *Facts) buildArgs() string {
	var b strings.Builder
	b.WriteString("BUILD_ARGS=()\n")
	for _, name := range f.Declared {
		b.WriteString("BUILD_ARGS+=(--build-arg " + singleQuote(name+"="+f.Substitutions[name]) + ")\n")
	}

	return b.String()
}

// flag is a boolean fact the way the shell steps test it: "true", or empty.
func flag(b bool) string {
	if b {
		return trueValue
	}

	return ""
}

// doubleQuote writes a value the shell reads back verbatim from double quotes.
func doubleQuote(v string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`, "`", "\\`")

	return `"` + r.Replace(v) + `"`
}

// singleQuote writes a value the shell reads back verbatim from single quotes, a quote
// inside written as '\”.
func singleQuote(v string) string {
	return `'` + strings.ReplaceAll(v, `'`, `'\''`) + `'`
}
