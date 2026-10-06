package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-playground/errors/v5"
	"golang.org/x/oauth2/google"

	"github.com/cccteam/ccc/bedrock/internal/derive"
	"github.com/cccteam/ccc/bedrock/internal/github"
	"github.com/cccteam/ccc/bedrock/internal/migration"
	"github.com/cccteam/ccc/bedrock/internal/secret"
)

const (
	// BuildArgsFile holds the image build's arguments, one NAME=value per line: the
	// declared substitutions resolve writes, then what a hook before the build appends.
	BuildArgsFile = "build-args.txt"
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
	jobsJobFact       = "JOBS_JOB"
	sharedDBFact      = "SHARED_DB"
	reloadDBFact      = "RELOAD_DB"
	reloadReasonFact  = "RELOAD_DB_REASON"
	downFact          = "DOWN"
	imageTagFact      = "IMAGE_TAG"
	commitTagFact     = "COMMIT_TAG"
	revisionTagFact   = "REVISION_TAG"
	runMigrationsFact = "RUN_MIGRATIONS"
	prNumberSub       = "_PR_NUMBER"
	// restoreSub and requesterSub are the restore instruction a release build may carry
	// (bedrock restore starts the environment's version trigger with them): what the
	// environment's database is replaced with, and who asked. The facts carry them on.
	restoreSub    = "_RESTORE"
	requesterSub  = "_REQUESTER"
	restoreFact   = "RESTORE"
	requesterFact = "RESTORE_REQUESTER"
	// restoreDatabaseSub and restoreDatabaseBackupSub come with a restore from production's
	// backup, read by the operations workflow from production's deployment record:
	// production's live database (after a rollback, the generation it restored into, not
	// the stack's first database) and the backup that generation was restored from, for a
	// generation too young to have a backup of its own. Empty when production's record
	// names no database (written before database generations): its first database is read.
	restoreDatabaseSub        = "_RESTORE_DATABASE"
	restoreDatabaseBackupSub  = "_RESTORE_DATABASE_BACKUP"
	restoreSourceDatabaseFact = "RESTORE_SOURCE_DATABASE"
	restoreSourceBackupFact   = "RESTORE_SOURCE_BACKUP"
	// The build's approval, where its trigger required one: who approved it in Cloud
	// Build, when, and their comment. The record carries them.
	approverFact        = "APPROVER"
	approvedAtFact      = "APPROVED_AT"
	approvalCommentFact = "APPROVAL_COMMENT"
	// rollbackSub, rollbackFromSub and reasonSub are a rollback run's instruction, which
	// bedrock rollback sets on the rollback trigger alone: the backup to restore (a name,
	// @<moment>, or empty for the live release's pre-release backup), the release the
	// rollback leaves, and why; rollbackFact, rollbackFromFact and rollbackReasonFact
	// carry them to the steps, rollbackFact holding the backup's resource name once resolved.
	rollbackSub        = "_ROLLBACK"
	rollbackFromSub    = "_ROLLBACK_FROM"
	reasonSub          = "_REASON"
	rollbackFact       = "ROLLBACK"
	rollbackFromFact   = "ROLLBACK_FROM"
	rollbackReasonFact = "ROLLBACK_REASON"
	// keepsReleaseBackupsFact says the environment is on the placement's releaseBackups
	// list in the checkout: the release build takes a backup as of its cut (deploy backup).
	keepsReleaseBackupsFact = "KEEPS_RELEASE_BACKUPS"
	// restoreReasonFact says why a release build restores the environment's database
	// without being asked: the seed changed since the environment's live release applied
	// it (seedChanged). The record carries it beside the restore.
	restoreReasonFact = "RESTORE_REASON"
	// seedFact says the migrate command applies the development seed in this build: every
	// pull request, and a release build where the placement in the checkout names the
	// environment on its seed list (seed). The steps after resolve read it, never the
	// trigger's _SEED.
	seedFact = "SEED"
	// buildSecretsFact lists the build secrets the image build reads, NAME=<secret version
	// resource name>, comma-separated: the pins the stack's placement in the checkout
	// states, in the containers the trigger names (buildSecrets). The image build reads it,
	// never the trigger's _BUILD_SECRETS.
	buildSecretsFact = "BUILD_SECRETS"
	// The two restores: an empty database the migrations then fill, and the seed where
	// the placement's seed list names the environment (tst,
	// and an environment on the seed list), and production's most recent backup (stg).
	restoreEmpty  = "empty"
	restoreBackup = "production-backup"
	// projectFact and locationFact are where the build runs, by Cloud Build's own names.
	projectFact  = "PROJECT_ID"
	locationFact = "LOCATION"
)

var (
	// environments are the ones a trigger names in _ENV.
	environments = []string{tstEnvironment, stgEnvironment, prdEnvironment}
	// triggerSubstitutions are the substitutions a trigger sets beyond the stack's map
	// (_PR_NUMBER and, on a pull request, the branches) and the pipeline's own default
	// (_DEFAULT_BRANCH): known, so never declared.
	triggerSubstitutions = []string{prNumberSub, baseBranchSub, "_HEAD_BRANCH", "_HEAD_REPO_URL", "_DEFAULT_BRANCH"}
)

// Clients are the deploy sequence's seams: what its commands open, replaced by fakes in
// tests. Each is opened by the command that needs it, never before.
type Clients struct {
	// Storage opens Cloud Storage, for the deployment records.
	Storage StoreFunc
	// StorageAs opens Cloud Storage as an impersonated identity, for a pull-request
	// build's read of an environment's records as that environment's plan identity.
	StorageAs StoreAsFunc
	// Builds opens Cloud Build, for the build's own description and the repository's
	// GitHub token.
	Builds BuildsFunc
	// Comments reads a pull request's comments with that token.
	Comments CommentsFunc
	// GitHub opens the GitHub client with that token, for the release checks.
	GitHub GitHubFunc
	// Registry opens Artifact Registry, for the release check on the image.
	Registry RegistryFunc
	// Run opens Cloud Run, for the services and the job process's jobs.
	Run RunFunc
	// Tasks opens Cloud Tasks, for the queue a maintenance step pauses and resumes.
	Tasks TasksFunc
	// Metrics opens Cloud Monitoring, for the active instances a maintenance step waits on.
	Metrics MetricsFunc
	// HTTP is the client a maintenance step probes the maintenance revision with.
	HTTP *http.Client
	// Sleep waits between a maintenance step's tries and through the wait for a
	// maintenance window; nil waits for real.
	Sleep SleepFunc
	// Now is the clock the maintenance window is read against; nil is the real one.
	Now func() time.Time
	// FirestoreAs opens Firestore as the apply identity, for the documents a restore run
	// deletes.
	FirestoreAs FirestoreAsFunc
	// SpannerAs opens Spanner as the apply identity, for the restore from production's
	// backup.
	SpannerAs SpannerAsFunc
	// Secrets opens Secret Manager, for the build secrets and the deployer app's key.
	Secrets SecretsFunc
	// SecretsAs opens Secret Manager as an impersonated identity, for the tests of a
	// stack's plan.
	SecretsAs SecretsAsFunc
	// Grants reads the application's databases as the worker, for the migrate step's wait
	// on the deploy identity's grants.
	Grants GrantsFunc
	// Exec runs the programs a step drives: tofu, docker, the migrate command, a hook's
	// script.
	Exec Runner
}

// DefaultClients opens the real services.
func DefaultClients() *Clients {
	return &Clients{
		Storage: NewStorage, StorageAs: NewStorageAs, Builds: NewCloudBuild, Comments: GitHubComments, GitHub: PublicGitHub,
		Registry: NewArtifactRegistry, Run: NewCloudRun, Secrets: NewSecretManager, Exec: OSRunner{}, Grants: NewGrants,
		SecretsAs: NewSecretManagerAs, Tasks: NewCloudTasks, Metrics: NewCloudMonitoring, HTTP: &http.Client{Timeout: 30 * time.Second}, FirestoreAs: NewFirestoreAs, SpannerAs: NewSpannerAs,
	}
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
	// files are read when a pull request's records are compared with the tree, and the
	// placement whose seed list a release build follows.
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
	// Token is the repository's GitHub token, minted from the connection the trigger
	// reads the repository through.
	Token string
	// The facts the environment file exports, as the later steps read them. JobsJob
	// names the job process's Cloud Run job, empty for an application without one.
	Services string
	JobsJob  string
	SharedDB bool
	ReloadDB bool
	// ReloadReason says why the pull request's database is recreated: the comment
	// asked (/gcbrun reload-db), or the migrations the last build applied are no longer
	// in the tree.
	ReloadReason string
	Down         bool
	// Restore is a release build's restore instruction (empty, or production-backup):
	// the environment's database is replaced before the release deploys, and Requester
	// says who asked. A requester with no restore and no migration operation is a rerun
	// (bedrock rerun: the release's tag build again, production included), and the
	// record names them. Both empty for a tag's own build.
	Restore   string
	Requester string
	// RestoreDatabase and RestoreDatabaseBackup are production's live database and the
	// backup it was restored from, for a restore from production's backup; see
	// restoreDatabaseSub.
	RestoreDatabase       string
	RestoreDatabaseBackup string
	// RestoreReason is set with Restore when the build decided the restore itself (the
	// seed changed in an environment on the placement's seed list); empty for a restore
	// a person asked for.
	RestoreReason string
	// Rollback is a rollback run's instruction (rollback.go): the backup restored into the
	// database's next generation, by resource name or as @<moment>; RollbackFrom the
	// release the environment leaves and RollbackReason why, with Requester saying who
	// asked. All empty for any other run.
	Rollback       string
	RollbackFrom   string
	RollbackReason string
	// Approver is who approved the build in Cloud Build where its trigger required an
	// approval (a release, a rerun or a rollback in an environment on placement.json's
	// approvals), ApprovedAt when, and ApprovalComment what they wrote; the record names
	// them. All empty where no approval was required.
	Approver        string
	ApprovedAt      string
	ApprovalComment string
	// Seed says the migrate command applies the development seed: a pull request's
	// database is new and always seeded; a release build seeds where the placement in the
	// checkout names the environment on its seed list.
	Seed bool
	// KeepsReleaseBackups says a release build takes a backup of the database as of its
	// cut, before its migrations: the placement in the checkout names the environment on
	// its releaseBackups list (production alone unless it says otherwise). Never on a
	// pull-request build.
	KeepsReleaseBackups bool
	// Migration is the migration operation a release build carries (the operations
	// workflow's version, rerun or force), which the migrate step does; nil for none.
	Migration     *MigrateAction
	Image         string
	ImageTag      string
	CommitTag     string
	Version       string
	Release       string
	RevisionTag   string
	RunMigrations bool
	ShiftTraffic  bool
	// Project and Location are where the build runs, for the steps that link to its log.
	Project  string
	Location string
	// Substitutions are every substitution of the build, as the trigger passed them;
	// Declared are the substitutions the application declares for the environment beyond
	// the contract, sorted, as the stack's placement in the checkout states them
	// (declared holds their values; declare).
	Substitutions map[string]string
	Declared      []string
	declared      map[string]string
	// contract are the substitutions the trigger passes by the pipeline's contract and its
	// own (the stack's map, the pull request's); what the trigger carries beyond them and
	// the checkout no longer declares is left out of the environment file.
	contract []string
	// BuildSecrets are the build secrets the image build reads, NAME=<secret version
	// resource name>, comma-separated (buildSecrets).
	BuildSecrets string
	// BuildArguments are the build arguments the placement in the checkout declares
	// (buildArguments) whose values a release build's trigger carries (_BUILD_ARG_<NAME>):
	// the environment file exports those substitutions for the image build, sorted. A
	// pull-request build exports none, its values coming from the pull request's own
	// stack (stackFacts).
	BuildArguments []string
	// build is the build as the API described it, kept for the build file.
	build []byte
}

// Resolve reads the build and works out the facts: the trigger's kind, the pull
// request's instruction, the image and its tags, whether the migrations run and
// traffic shifts. It refuses a build that is neither a tag's nor a pull request's, one
// that names no services, a pull-request build outside tst, one with no
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
	f.Project, f.Location = req.Project, req.Location
	if err := f.mint(ctx, builds, req); err != nil {
		return nil, err
	}
	if err := f.trigger(ctx, clients.Comments, out); err != nil {
		return nil, err
	}
	if err := f.staleDatabase(ctx, clients.Storage, req.Source, out); err != nil {
		return nil, err
	}
	if err := f.seed(req.Source, out); err != nil {
		return nil, err
	}
	if err := f.releaseBackups(req.Source, out); err != nil {
		return nil, err
	}
	if err := f.declare(req.Known, req.Source, out); err != nil {
		return nil, err
	}
	if err := f.buildSecrets(req.Source, out); err != nil {
		return nil, err
	}
	if err := f.stackArguments(req.Source, out); err != nil {
		return nil, err
	}
	if err := f.seedChanged(ctx, clients.Storage, req.Source, out); err != nil {
		return nil, err
	}
	if err := f.rehearsal(ctx, clients.Storage, req.Source, out); err != nil {
		return nil, err
	}
	f.tags()
	f.report(out)

	return f, nil
}

// newFacts reads the build and refuses one the pipeline cannot run: a build is a tag's
// or a pull request's, in one of the environments, and names what it updates (the
// services, region=name comma-separated, from the application layer's substitutions
// output; the job process's job the same way when the application has one).
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
	f.Approver, f.ApprovedAt, f.ApprovalComment = build.approver()
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
	f.Services, f.JobsJob = subs["_SERVICES"], subs["_JOBS_JOB"]
	if f.Services == "" {
		return nil, errors.New("_SERVICES names the Cloud Run services this build updates; it is empty")
	}
	if err := f.restore(); err != nil {
		return nil, err
	}
	if err := f.rollback(); err != nil {
		return nil, err
	}
	if err := f.migration(); err != nil {
		return nil, err
	}

	return f, nil
}

// restore reads the restore instruction. A restore is a release build's: it replaces the
// environment's database before the release deploys, so a pull-request build, whose
// database is its own and recreated on /gcbrun reload-db, carries none; production is
// never restored by a run; the instruction names one of the two restores, and who asked.
// An empty database is any environment's but production's; production's backup is
// restored into stg, the environment on production's instance where its backups are. A
// requester with no restore and no migration operation is a rerun: the operations
// workflow ran the release's version trigger again with nothing but who asked (bedrock
// rerun), in any environment, production included, and the build runs as the tag's did.
func (f *Facts) restore() error {
	restore, requester := f.Substitutions[restoreSub], f.Substitutions[requesterSub]
	if restore == "" {
		if requester != "" && f.Substitutions[migrateActionSub] == "" {
			if f.Tag == "" {
				return errors.Newf("%s=%s on a pull-request build: a rerun is a release build's; a pull request is built again with a /gcbrun comment", requesterSub, requester)
			}
			f.Requester = requester
		}

		return nil
	}
	if f.Tag == "" {
		return errors.Newf("%s=%s on a pull-request build: a restore is a release build's instruction; a pull request's own database is recreated with /gcbrun reload-db", restoreSub, restore)
	}
	if f.Environment == prdEnvironment {
		return errors.Newf("%s=%s in %s: production is never restored by a run", restoreSub, restore, prdEnvironment)
	}
	switch restore {
	case restoreEmpty:
	case restoreBackup:
		if f.Environment != stgEnvironment {
			return errors.Newf("%s=%s in %s: production's backup is restored into %s, the environment on production's instance; %s is restored to an empty database (%s=%s)", restoreSub, restore, f.Environment, stgEnvironment, f.Environment, restoreSub, restoreEmpty)
		}
	default:
		return errors.Newf("unknown %s %q (the restores are %s and %s)", restoreSub, restore, restoreEmpty, restoreBackup)
	}
	if requester == "" {
		return errors.Newf("%s=%s names no requester (%s): a restore says who asked for it", restoreSub, restore, requesterSub)
	}
	f.Restore, f.Requester = restore, requester
	if restore == restoreBackup {
		f.RestoreDatabase, f.RestoreDatabaseBackup = f.Substitutions[restoreDatabaseSub], f.Substitutions[restoreDatabaseBackupSub]
	}

	return nil
}

// migration reads the migration operation (bedrock migration version|rerun|force, through
// the operations workflow). It is a release build's: a pull request's database is its own
// and recreated instead; and never a restore run's, whose database is replaced, so there
// is no migration state to operate on. What it asks is checked here, before the image
// builds, and done by the migrate step.
func (f *Facts) migration() error {
	action, err := migrateAction(f.Substitutions)
	if err != nil || action == nil {
		return err
	}
	if f.Tag == "" {
		return errors.Newf("%s=%s on a pull-request build: a migration operation is a release build's; a pull request's own database is recreated with /gcbrun reload-db", migrateActionSub, action.Action)
	}
	if f.Restore != "" {
		return errors.Newf("%s=%s with %s=%s: a restore replaces the database, so there is no migration state to operate on", migrateActionSub, action.Action, restoreSub, f.Restore)
	}
	f.Migration = action

	return nil
}

// mint takes a GitHub token for the repository from the Cloud Build connection the
// trigger reads it through; the deploy identity needs Read Token Accessor on it. Every
// build starts from a trigger, and the triggers exist once 2-env holds the environment's
// connection and the repository's link, so a build whose connection or repository name
// is empty is one no trigger started, and the step refuses it: nothing is submitted by
// hand.
func (f *Facts) mint(ctx context.Context, builds Builds, req *ResolveRequest) error {
	connection, repo := f.Substitutions["_REPO_CONNECTION_NAME"], f.Substitutions["_REPO_NAME"]
	if connection == "" || repo == "" {
		return errors.Newf("_REPO_CONNECTION_NAME=%q _REPO_NAME=%q: every build starts from a trigger, which passes the environment's Cloud Build connection and the repository's link (both exist once 2-env holds the GitHub authorization); nothing is submitted by hand", connection, repo)
	}
	// The branch check and the comment read address the repository by its full name, a
	// trigger built-in; an empty name is a build no trigger started.
	if f.Substitutions["REPO_FULL_NAME"] == "" {
		return errors.New("REPO_FULL_NAME is not set: a trigger passes it as <organization>/<repository>, and every build starts from a trigger")
	}
	repository := "projects/" + req.Project + "/locations/" + req.Location + "/connections/" + connection + "/repositories/" + repo
	token, err := builds.ReadToken(ctx, repository)
	if err != nil {
		return errors.Wrapf(err, "could not mint a GitHub token from connection %s", connection)
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
		switch {
		case f.Restore != "":
			fmt.Fprintf(out, "Restore run: %s's database is replaced (%s) before %s deploys, asked for by %s.\n", f.Environment, f.Restore, f.Tag, f.Requester)
		case f.Migration != nil:
			fmt.Fprintf(out, "Migration operation %s.\n", f.Migration)
		case f.Requester != "":
			fmt.Fprintf(out, "Rerun: %s runs again in %s, asked for by %s.\n", f.Tag, f.Environment, f.Requester)
		}

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

// seed decides whether the build applies the development seed. A pull request's database
// is new and always seeded. A release build seeds where the placement in the checkout
// (infrastructure/placement.json at the commit the build runs) names the environment on
// its seed list; production is never on it, which the placement refuses. The list is read
// from the source and not from the trigger's _SEED: the trigger's substitutions are what
// the stack said at its last apply, and this build applies the stack only later, so a
// release that puts the environment on the list seeds in that release, and one that takes
// it off stops seeding in that release. The trigger's _SEED stays in the build's
// substitutions as what the trigger said, and the log says when the two differ.
func (f *Facts) seed(source string, out io.Writer) error {
	if f.Tag == "" {
		f.Seed = true

		return nil
	}
	placement, err := checkoutPlacement(Workspace(source), "the seed list is written")
	if err != nil {
		return err
	}
	f.Seed = slices.Contains(placement.SeedEnvironments(), f.Environment)
	listed := "is not on"
	if f.Seed {
		listed = "is on"
	}
	fmt.Fprintf(out, "Seed: %s %s the seed list of the placement in the checkout (%s).\n", f.Environment, listed, path.Join(stackDir, placementFile))
	if said, ok := f.Substitutions[seedSub]; ok && said != strconv.FormatBool(f.Seed) {
		fmt.Fprintf(out, "The trigger's %s=%s is what the stack said at its last apply; the placement in the checkout decides for this release.\n", seedSub, said)
	}

	return nil
}

// releaseBackups reads whether the environment keeps release backups: a release build
// takes a backup of the database as of its cut where the placement in the checkout names
// the environment on its releaseBackups list (production alone unless it says otherwise);
// a pull-request build never does, its database being its own.
func (f *Facts) releaseBackups(source string, out io.Writer) error {
	if f.Tag == "" {
		return nil
	}
	placement, err := checkoutPlacement(Workspace(source), "the release backups are decided")
	if err != nil {
		return err
	}
	f.KeepsReleaseBackups = placement.KeepsReleaseBackups(f.Environment)
	if f.Rollback != "" && !f.KeepsReleaseBackups {
		return errors.Newf("%s=%s in %s: the placement's releaseBackups list (%s) does not name it, so no release backup exists there to return to; bedrock restore serves it", rollbackSub, f.Rollback, f.Environment, strings.Join(placement.ReleaseBackupEnvironments(), ", "))
	}
	if f.KeepsReleaseBackups {
		fmt.Fprintf(out, "Release backup: %s keeps a backup of its database as of the cut, the moment before the migrations run (placement.json's releaseBackups); bedrock rollback restores it.\n", f.Environment)
	} else {
		fmt.Fprintf(out, "Release backup: %s is not on the placement's releaseBackups list (%s), so this run keeps no backup as of the cut and bedrock rollback does not serve it.\n", f.Environment, strings.Join(placement.ReleaseBackupEnvironments(), ", "))
	}

	return nil
}

// seedChanged decides whether a release build restores the environment's database
// without being asked, because the seed changed. An environment on the placement's seed
// list (Seed: the list as the checkout's placement states it) holds the development seed
// its live release applied, and that release's record lists the seed files by name and
// content; when the tree no longer carries one of them as it was applied (edited,
// renumbered or removed since), the database holds data the seed no longer describes,
// and the migrate command would apply none of it again (a seeded database takes nothing
// twice). So the build restores the environment to an empty database, as a restore run
// asked for by the release itself (RESTORE=empty), and the migrations and the seed apply
// from the start; the reason goes on the record. A seed file added beside the applied ones is a new data migration the
// migrate command applies, and recreates nothing. A pull-request build has its own rule
// (staleDatabase); a restore asked for already replaces the database; an environment off
// the seed list never applied the seed, so a changed seed is nothing to it; production is
// never on the list, and is never restored by a run. Only a live record of a release
// counts: a preview is a build whose traffic never shifted, and a pull request's record is
// its own environment's. The hotfix preview reads the same list against a pull request's
// tree (hotfixPreviewer), so what it promises is what this decides.
func (f *Facts) seedChanged(ctx context.Context, open StoreFunc, source string, out io.Writer) error {
	if f.Tag == "" || f.Restore != "" || !f.RunMigrations || f.Environment == prdEnvironment || !f.Seed {
		return nil
	}
	bucket, app, dir := f.Substitutions[recordsBucket], f.Substitutions[appSub], f.Substitutions[migrationsSub]
	if bucket == "" || app == "" || dir == "" {
		return nil
	}
	seedDir := seedDirBeside(dir)
	store, err := open(ctx)
	if err != nil {
		return err
	}
	defer store.Close()
	live, err := newestLiveRelease(ctx, store, bucket, app, f.Environment)
	if err != nil {
		return err
	}
	if live == nil {
		return nil
	}
	gone, err := seedGone(live, seedDir, source)
	if err != nil {
		return err
	}
	if len(gone) == 0 {
		return nil
	}
	f.Restore, f.Requester = restoreEmpty, "release "+f.Tag
	f.RestoreReason = seedRestoreReason(live, gone)
	fmt.Fprintf(out, "Restore run: %s's database is replaced (%s) before %s deploys, %s.\n", f.Environment, f.Restore, f.Tag, f.RestoreReason)

	return nil
}

// rehearsal is staging's part in a release: staging runs a release against production's
// data before production does, so a release that carries migrations production has not
// applied restores staging's database from production's newest backup before it deploys
// there, as a restore run asked for by the release itself (RESTORE=production-backup,
// the steps a restore from GitHub takes), and a release without such migrations deploys
// to staging as it stands, since there may be things to see against production's data
// anyway. Whether migrations will run is read from applied versions, never from the
// files in the image: tst's live record of this release lists the migrations tst applied
// when the release deployed there (the highest schema migration's index is the release's
// version, migrationVersion), and production's newest live record lists what production
// applied; the release's version above production's means migrations will run. Staging
// is restored as well when its own live record lists a migration file the release does
// not carry as applied (a failed release's, stagingAhead): between releases staging sits
// at production's release, and the hotfix check would refuse the release otherwise.
// Both records are read as the build: tst's as the gate reads it, production's from the
// bucket _RECORDS_BUCKETS names, which production's 2-env lets this environment's deploy
// identity read; production's record also names its live database and the backup a
// rollback restored it from, which the restore needs (RESTORE_SOURCE_DATABASE,
// RESTORE_SOURCE_BACKUP). A read production's bucket refuses (the grant arrives with
// production's 2-env, applied before the first release that rehearses; a release that
// came before it would otherwise never reach production) is said and left: staging
// deploys as it stands. A build in any other environment, a pull request's, a restore or
// rollback asked for already, a migration operation and a run that applies no migration
// decide nothing here; a missing tst record is left to the gate, which refuses the
// release; production with no live record (the first release ever) has no data to run
// against, and staging deploys as it stands.
func (f *Facts) rehearsal(ctx context.Context, open StoreFunc, source string, out io.Writer) error {
	if f.Tag == "" || f.Environment != stgEnvironment || f.Restore != "" || f.Rollback != "" || f.Migration != nil || !f.RunMigrations {
		return nil
	}
	subs := f.Substitutions
	previous, previousBucket, app := subs[previousEnvSub], subs[previousRecordsSub], subs[appSub]
	bucket := pairs(subs[recordsBucketsSub])[prdEnvironment]
	if previous == "" || previousBucket == "" || app == "" || bucket == "" {
		fmt.Fprintf(out, "Staging rehearsal: the records of %s and %s are not named (%s, %s, %s); %s deploys as it stands.\n", previous, prdEnvironment, previousEnvSub, previousRecordsSub, recordsBucketsSub, f.Environment)

		return nil
	}
	store, err := open(ctx)
	if err != nil {
		return err
	}
	defer store.Close()
	release, err := newestRecordWhere(ctx, store, previousBucket, app+"/"+previous+"/"+f.Tag+"/", func(r *Record) bool { return r.Status == Live })
	if err != nil {
		return err
	}
	if release == nil {
		fmt.Fprintf(out, "Staging rehearsal: %s has no live deployment record of %s yet; the release check decides whether %s may reach %s.\n", previous, f.Tag, f.Tag, f.Environment)

		return nil
	}
	production, err := newestLiveRelease(ctx, store, bucket, app, prdEnvironment)
	if err != nil {
		if !deniedRead(err) {
			return err
		}
		fmt.Fprintf(out, "Staging rehearsal: %s's deployment records could not be read as the build (%v); %s deploys as it stands. Production's records bucket lets this environment's deploy identity read once production's 2-env is applied at this bedrock.\n", prdEnvironment, err, f.Environment)

		return nil
	}
	if production == nil {
		fmt.Fprintf(out, "Staging rehearsal: %s has no live deployment record, so there is no production data to run %s against; %s deploys as it stands.\n", prdEnvironment, f.Tag, f.Environment)

		return nil
	}
	reason, err := f.rehearsalReason(ctx, store, source, release, production, out)
	if err != nil || reason == "" {
		return err
	}
	f.Restore, f.Requester, f.RestoreReason = restoreBackup, "release "+f.Tag, reason
	if production.Database != nil {
		f.RestoreDatabase = production.Database.Name
	}
	if production.Rollback != nil {
		f.RestoreDatabaseBackup = production.Rollback.Backup
	}
	fmt.Fprintf(out, "Restore run: %s's database is replaced (%s) before %s deploys, %s.\n", f.Environment, f.Restore, f.Tag, f.RestoreReason)

	return nil
}

// rehearsalReason is why staging's database is restored before the release deploys
// (the release carries migrations production has not applied, or staging's database
// holds a migration the release does not carry), or "" when staging deploys as it
// stands, which it says.
func (f *Facts) rehearsalReason(ctx context.Context, store Store, source string, release, production *Record, out io.Writer) (string, error) {
	subs := f.Substitutions
	previous, app, dir := subs[previousEnvSub], subs[appSub], subs[migrationsSub]
	releaseVersion, productionVersion := migrationVersion(release.Migrations, dir), migrationVersion(production.Migrations, dir)
	versions := fmt.Sprintf("%s applied version %d, build %s; %s runs version %d, release %s, build %s", previous, releaseVersion, release.Build, prdEnvironment, productionVersion, production.Version, production.Build)
	var reason string
	switch {
	case releaseVersion > productionVersion:
		reason = fmt.Sprintf("%s carries migrations %s has not applied (%s), so %s's database is restored from production's newest backup and the release runs against production's data before production does", f.Tag, prdEnvironment, versions, f.Environment)
	default:
		staging, err := newestLiveRelease(ctx, store, subs[recordsBucket], app, f.Environment)
		if err != nil {
			return "", err
		}
		ahead, err := stagingAhead(source, staging, f.Tag)
		if err != nil {
			return "", err
		}
		if ahead == "" {
			fmt.Fprintf(out, "Staging rehearsal: %s carries no migration %s has not applied (%s); %s deploys as it stands, on its database as it is.\n", f.Tag, prdEnvironment, versions, f.Environment)

			return "", nil
		}
		reason = fmt.Sprintf("%s, so %s's database is restored from production's newest backup to production's state (%s) before the release deploys", ahead, f.Environment, versions)
	}

	return reason, nil
}

// deniedRead says whether an error is Cloud Storage refusing the read for want of a
// grant (a 403, or the permission named), as against any other failure.
func deniedRead(err error) bool {
	text := err.Error()

	return strings.Contains(text, "403") || strings.Contains(text, "PERMISSION_DENIED") || strings.Contains(text, "does not have storage.objects")
}

// migrationVersion is the version a record's migration list stands for: the highest
// index among the schema migrations (the files under dir; the seed's are left out when
// dir is named), 0 when the list has none.
func migrationVersion(applied []Migration, dir string) int {
	version := 0
	for _, m := range applied {
		if dir != "" && m.Dir != dir {
			continue
		}
		match := migration.NameRE.FindStringSubmatch(m.Name)
		if match == nil {
			continue
		}
		if idx, err := strconv.Atoi(match[1]); err == nil && idx > version {
			version = idx
		}
	}

	return version
}

// stagingAhead says what staging's database holds, by its live record, that the release's
// tree does not carry as applied: a migration file missing from the tree or with other
// content (a failed release's, which never reached production), or nothing. The record
// names the release live when it was written, not the one that applied each file.
func stagingAhead(source string, live *Record, tag string) (string, error) {
	if live == nil {
		return "", nil
	}
	for _, m := range upFirst(live.Migrations) {
		hash, err := hashFile(filepath.Join(source, filepath.FromSlash(m.Dir), m.Name))
		if err != nil {
			return "", err
		}
		file := path.Join(m.Dir, m.Name)
		switch {
		case hash == "":
			return fmt.Sprintf("%s's database holds %s (in %s's record), which %s does not carry", stgEnvironment, file, live.Version, tag), nil
		case hash != m.Hash:
			return fmt.Sprintf("%s's database holds %s with other content than %s carries (by %s's record)", stgEnvironment, file, tag, live.Version), nil
		}
	}

	return "", nil
}

// newestRecord reads the records under the prefix and returns the newest that lists
// migrations, or nil.
func newestRecord(ctx context.Context, store Store, bucket, prefix string) (*Record, error) {
	return newestRecordWhere(ctx, store, bucket, prefix, func(r *Record) bool {
		return len(r.Migrations) > 0
	})
}

// newestLiveRelease is the environment's newest live record of a release: what the
// environment runs. A pull request's records lie under the same prefix
// (<app>/<env>/pr<N>-<commit>/) and are live too, for the pull request's own environment;
// they are not the environment's release and are left out.
func newestLiveRelease(ctx context.Context, store Store, bucket, app, env string) (*Record, error) {
	return newestRecordWhere(ctx, store, bucket, app+"/"+env+"/", func(r *Record) bool {
		return r.Status == Live && strings.HasPrefix(r.Version, "v")
	})
}

// newestRecordWhere reads the records under the prefix and returns the newest, by its
// timestamp, that keep accepts, or nil.
func newestRecordWhere(ctx context.Context, store Store, bucket, prefix string, keep func(*Record) bool) (*Record, error) {
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
		if !keep(&r) {
			continue
		}
		if newest == nil || r.Timestamp > newest.Timestamp {
			newest = &r
		}
	}

	return newest, nil
}

// instruction reads the latest /gcbrun comment: the words after it are its options
// (shared-db: the site runs against tst's database and the migrations do not run;
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
		// tst's schema is tst's: the migrate command never runs against the shared
		// database (the guard refuses shared-db when the migrations changed at all).
		f.RunMigrations = false
	}

	return nil
}

// tags names the image and its tags. One registry serves every environment and each
// environment is a fresh build of the tag (nothing is promoted), so the image tags carry
// the environment: <release>-<env> and <commit>-<env>; the release check compares those.
func (f *Facts) tags() {
	f.Image = f.Substitutions["_REGISTRY"] + "/" + f.Substitutions["_APP"]
	f.ImageTag = f.Release + "-" + f.Environment
	f.CommitTag = f.Substitutions["COMMIT_SHA"] + "-" + f.Environment
}

// declaredNameRE is a declared substitution's name, as the stack's var.substitutions
// validates it: an underscore, then upper snake case.
var declaredNameRE = regexp.MustCompile(`^_[A-Z][A-Z0-9_]*$`)

// stackTfvars is the stack's placement in the checkout, for messages: the per-environment
// values the stack's variables take (terraform.tfvars beside the stack).
var stackTfvars = path.Join(stackDir, "terraform.tfvars")

// declare reads the substitutions the application declares for its hooks and its image
// build from the stack's placement in the checkout (substitutions.<env> in
// infrastructure/terraform.tfvars, which the stack takes with lookup(var.substitutions,
// env, {})), not from the trigger: the trigger carries them as its stack's last apply set
// them, and this build applies the stack only later, so a release that declares, changes
// or drops one builds and runs its hooks with its own. A name that is not _UPPER_SNAKE or
// that the pipeline's contract carries is refused here, before anything is built, as the
// stack refuses it at the apply. The trigger's values stay in the build's substitutions
// as what the trigger said, and the log names each the checkout changes.
func (f *Facts) declare(known []string, source string, out io.Writer) error {
	values, err := secret.EnvironmentValues(filepath.Join(source, stackDir), secret.SubstitutionsKey, f.Environment)
	if err != nil {
		return errors.Wrap(err, "the checkout's declared substitutions")
	}
	f.contract = slices.Concat(known, triggerSubstitutions)
	f.declared = values
	f.Declared = slices.Sorted(maps.Keys(values))
	for _, name := range f.Declared {
		if !declaredNameRE.MatchString(name) {
			return errors.Newf("%s declares %q for %s: a declared substitution starts with an underscore and is upper snake case (_NAME)", stackTfvars, name, f.Environment)
		}
		if slices.Contains(f.contract, name) {
			return errors.Newf("%s declares %s for %s, a substitution the pipeline's contract carries; rename it", stackTfvars, name, f.Environment)
		}
		if strings.HasPrefix(name, derive.BuildArgumentPrefix) {
			return errors.Newf("%s declares %s for %s: a substitution starting with %s carries a build argument %s declares (buildArguments), from the stack's own values; rename it", stackTfvars, name, f.Environment, derive.BuildArgumentPrefix, checkoutPlacementPath)
		}
		if said, ok := f.Substitutions[name]; !ok {
			fmt.Fprintf(out, "%s declares %s, which the trigger does not carry yet (its stack's last apply): this build passes it.\n", stackTfvars, name)
		} else if said != values[name] {
			fmt.Fprintf(out, "%s declares another value for %s than the trigger carries (its stack's last apply): this build passes the checkout's.\n", stackTfvars, name)
		}
	}
	for _, name := range f.substitutionNames() {
		if _, ok := values[name]; !ok && !slices.Contains(f.contract, name) && !strings.HasPrefix(name, derive.BuildArgumentPrefix) {
			fmt.Fprintf(out, "The trigger carries %s, which %s no longer declares for %s: this build leaves it out.\n", name, stackTfvars, f.Environment)
		}
	}

	return nil
}

// checkoutPlacementPath is the placement in the checkout, for messages.
var checkoutPlacementPath = path.Join(stackDir, placementFile)

// stackArguments reads the build arguments the placement in the checkout declares
// (buildArguments: a build argument's name and the value of the stack's it takes). Their
// values are the stack's own (the Firebase web API key, the Firestore database's id), which
// exist only once it is applied, so they come from the stack and never from the checkout:
// a release build passes what its trigger carries (_BUILD_ARG_<NAME>, as its stack's last
// apply set it), for the names the checkout declares, and the environment file exports
// them for the image build; a pull-request build passes the pull request's own stack's,
// which deploy pr-stack apply reads from that stack's substitutions output before the
// image build. A name the checkout declares that a release build's trigger does not carry
// yet comes with this build's stack apply, after the image build, so the image build
// passes it from the next release on; one the trigger carries that the checkout no longer
// declares is left out. The log names the arguments and never their values.
func (f *Facts) stackArguments(source string, out io.Writer) error {
	placement, err := checkoutPlacement(Workspace(source), "the build arguments are declared")
	if err != nil {
		return err
	}
	declared := placement.BuildArgumentNames()
	for _, sub := range f.substitutionNames() {
		name, ok := strings.CutPrefix(sub, derive.BuildArgumentPrefix)
		if ok && !slices.Contains(declared, name) {
			fmt.Fprintf(out, "The trigger carries %s, which %s no longer declares (buildArguments): the image build leaves it out.\n", sub, checkoutPlacementPath)
		}
	}
	if len(declared) == 0 {
		return nil
	}
	if f.Tag == "" {
		fmt.Fprintf(out, "Build arguments %s declares (buildArguments): %s; the image build passes the pull request's own, from its stack once applied (deploy pr-stack apply).\n", checkoutPlacementPath, strings.Join(declared, ", "))

		return nil
	}
	for _, name := range declared {
		sub := derive.BuildArgumentSubstitution(name)
		if _, ok := f.Substitutions[sub]; !ok {
			fmt.Fprintf(out, "%s declares build argument %s (%s), which the trigger does not carry yet: %s comes with this build's stack apply, after the image build, so the image build passes it from the next release on.\n", checkoutPlacementPath, name, placement.BuildArguments[name], sub)

			continue
		}
		f.BuildArguments = append(f.BuildArguments, name)
	}
	if len(f.BuildArguments) > 0 {
		fmt.Fprintf(out, "Build arguments %s declares (buildArguments), as the trigger carries them: %s.\n", checkoutPlacementPath, strings.Join(f.BuildArguments, ", "))
	}

	return nil
}

// buildSecrets reads the build secrets the image build reads from the stack's placement
// in the checkout (build_secrets.<env> in infrastructure/terraform.tfvars: each name with
// its pinned version), not from the trigger's _BUILD_SECRETS, so a release that pins
// another version builds with it. The containers are the stack's (it makes each and grants
// the deploy identity access to it), named on the trigger: a name the trigger does not
// carry yet is one this release declares, whose container and access come with this
// build's stack apply, after the image build, so the image build reads it from the next
// release on, and the log says so. A name that is not upper snake case and a pin that is
// not a version number are refused, as the stack refuses them.
func (f *Facts) buildSecrets(source string, out io.Writer) error {
	pins, err := secret.EnvironmentValues(filepath.Join(source, stackDir), secret.BuildSecretsKey, f.Environment)
	if err != nil {
		return errors.Wrap(err, "the checkout's build secrets")
	}
	containers, versions := map[string]string{}, map[string]string{}
	if said := f.Substitutions[buildSecretsSub]; said != "" {
		for _, entry := range strings.Split(said, ",") {
			name, resource, _ := strings.Cut(entry, "=")
			container, version, _ := strings.Cut(resource, "/versions/")
			containers[name], versions[name] = container, version
		}
	}
	var entries []string
	for _, name := range slices.Sorted(maps.Keys(pins)) {
		if err := secret.ValidateBuildSecret(name); err != nil {
			return errors.Wrapf(err, "%s, build_secrets.%s", stackTfvars, f.Environment)
		}
		pin := pins[name]
		if !buildPinRE.MatchString(pin) {
			return errors.Newf("%s pins build secret %s at %q for %s: a build reads one version, a number, never latest", stackTfvars, name, pin, f.Environment)
		}
		container, ok := containers[name]
		if !ok || container == "" {
			fmt.Fprintf(out, "%s declares build secret %s, which the trigger does not carry yet: its container and the deploy identity's access come with this build's stack apply, after the image build, so the image build reads it from the next release on.\n", stackTfvars, name)

			continue
		}
		if versions[name] != pin {
			fmt.Fprintf(out, "%s pins build secret %s at version %s, where the trigger (its stack's last apply) pins %s: the image build reads %s.\n", stackTfvars, name, pin, versions[name], pin)
		}
		entries = append(entries, name+"="+container+"/versions/"+pin)
	}
	for _, name := range slices.Sorted(maps.Keys(containers)) {
		if _, ok := pins[name]; !ok {
			fmt.Fprintf(out, "The trigger carries build secret %s, which %s no longer declares for %s: the image build leaves it out.\n", name, stackTfvars, f.Environment)
		}
	}
	f.BuildSecrets = strings.Join(entries, ",")

	return nil
}

// buildPinRE is a build secret's pin, as the stack's var.build_secrets validates it.
var buildPinRE = regexp.MustCompile(`^\d+$`)

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
	if f.Approver != "" {
		fmt.Fprintf(out, "Approved in Cloud Build by %s at %s; the record names them.\n", f.Approver, f.ApprovedAt)
	}
	f.statement(out)
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
		{jobsJobFact, f.JobsJob},
		{sharedDBFact, flag(f.SharedDB)},
		{reloadDBFact, flag(f.ReloadDB)},
		{reloadReasonFact, f.ReloadReason},
		{downFact, flag(f.Down)},
		{restoreFact, f.Restore},
		{requesterFact, f.Requester},
		{restoreSourceDatabaseFact, f.RestoreDatabase},
		{restoreSourceBackupFact, f.RestoreDatabaseBackup},
		{restoreReasonFact, f.RestoreReason},
		{rollbackFact, f.Rollback},
		{rollbackFromFact, f.RollbackFrom},
		{rollbackReasonFact, f.RollbackReason},
		{approverFact, f.Approver},
		{approvedAtFact, f.ApprovedAt},
		{approvalCommentFact, f.ApprovalComment},
		{seedFact, flag(f.Seed)},
		{keepsReleaseBackupsFact, flag(f.KeepsReleaseBackups)},
		{buildSecretsFact, f.BuildSecrets},
		{skipDeploy, ""},
		{imageFact, f.Image},
		{imageTagFact, f.ImageTag},
		{commitTagFact, f.CommitTag},
		{versionFact, f.Version},
		{releaseFact, f.Release},
		{revisionTagFact, f.RevisionTag},
		{runMigrationsFact, strconv.FormatBool(f.RunMigrations)},
		{shiftTraffic, strconv.FormatBool(f.ShiftTraffic)},
		{projectFact, f.Project},
		{locationFact, f.Location},
	}
	for _, kv := range facts {
		b.WriteString("export " + kv[0] + "=" + doubleQuote(kv[1]) + "\n")
	}
	subs := f.exported()
	for _, name := range slices.Sorted(maps.Keys(subs)) {
		b.WriteString("export " + name + "=" + singleQuote(subs[name]) + "\n")
	}

	return b.String()
}

// exported are the substitutions the environment file exports for the hooks: the
// contract's and the trigger's own as the trigger passed them, the build arguments the
// checkout declares as the trigger carries them (a release build's; stackArguments), and
// the declared ones as the checkout declares them; what the trigger carries beyond those
// is a declaration the checkout no longer makes, and is left out.
func (f *Facts) exported() map[string]string {
	subs := map[string]string{}
	for _, name := range f.substitutionNames() {
		if slices.Contains(f.contract, name) {
			subs[name] = f.Substitutions[name]
		}
	}
	for _, name := range f.BuildArguments {
		sub := derive.BuildArgumentSubstitution(name)
		subs[sub] = f.Substitutions[sub]
	}
	maps.Copy(subs, f.declared)

	return subs
}

// buildArgs is the build arguments file: one _NAME=value line per declared substitution,
// as the checkout declares it.
func (f *Facts) buildArgs() string {
	var b strings.Builder
	for _, name := range f.Declared {
		b.WriteString(name + "=" + f.declared[name] + "\n")
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
