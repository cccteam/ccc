// view.go prepares what the templates read: the model, the placement, and the phrases
// and aligned blocks that are easier to compute once than to spell in a template.

package render

import (
	"fmt"
	"hash/fnv"
	"path"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/derive"
	"github.com/cccteam/ccc/bedrock/internal/hook"
	"github.com/cccteam/ccc/bedrock/internal/release"
)

// view is the data every template executes against.
type view struct {
	*derive.Model
	P *derive.Placement
	// RepoFullName is the repository as GitHub names it, owner/name, from the module
	// path; a placeholder when the module path names no GitHub repository.
	RepoFullName string
	// SweepMinute is the minute of the hour the application's sweep runs at, derived
	// from the application code, so that many applications' sweeps in one
	// environment do not all queue at the top of the hour.
	SweepMinute int
	// BedrockURL is where the pipeline's first step and the infrastructure workflow
	// download the bedrock a release pin names: the linux/amd64 binary of the release;
	// empty for a commit pin. CommitPin reports a commit pin, which both build with go
	// install in GoImage, the Go image pinned by digest.
	BedrockURL string
	CommitPin  bool
	GoImage    string
	// GcloudImage, OpenTofuImage and DockerImage are the images the pipeline's steps run
	// bedrock in: gcloud's for most (Cloud Build workers keep it cached), OpenTofu's for
	// the steps that drive tofu, docker's for the image build.
	GcloudImage   string
	OpenTofuImage string
	DockerImage   string

	// Prefix is the placement's naming prefix.
	Prefix string
	// Integration and Production are the first and last environments.
	Integration string
	Production  string
	// SharedInstanceEnvs names the environments above integration: "stg and prd";
	// SharedInstanceEnvsWrapped is the same with the last on its own README line.
	SharedInstanceEnvs        string
	SharedInstanceEnvsWrapped string
	// EnvsOr, EnvsComma, EnvsBacktickOr, EnvList spell the environments: "tst, stg, or
	// prd", "tst, stg, prd", "`tst`, `stg`, or `prd`", ["tst", "stg", "prd"].
	EnvsOr         string
	EnvsComma      string
	EnvsBacktickOr string
	EnvList        string
	// ApprovalList is the HCL list of the environments a release waits for approval
	// in; ApprovalsProse spells them ("stg and prd", or "no environment").
	// PreviousEnvMap is the HCL map from each environment to the one before it.
	ApprovalList   string
	ApprovalsProse string
	// SeedList is the HCL list of the environments whose migration applies the
	// development seed.
	SeedList string
	// ReleaseBackupList is the HCL list of the environments whose release builds keep a
	// backup as of their cut and whose stack has the rollback trigger;
	// ReleaseBackupsProse spells them. RetentionMap is the HCL map of each environment's
	// Spanner version retention period ({ tst = "7d", stg = "7d", prd = "7d" }).
	ReleaseBackupList   string
	ReleaseBackupsProse string
	RetentionMap        string
	// BuildMachine is the Cloud Build machine the placement puts the builds on, empty
	// when it leaves them to Cloud Build's default, and BuildMachineCPUs its vCPUs, for
	// the README's arithmetic; BuildMachineNames spells the machines a placement may
	// name with their vCPUs, from the same table derive validates against.
	BuildMachine      string
	BuildMachineCPUs  int
	BuildMachineNames string
	// MaxInstancesMap is the HCL map of the placement's instance caps by environment
	// ({ tst = 2, stg = 2, prd = 2 }), the environments it caps alone, in promotion
	// order; empty when it caps none, and then the service has no max_instance_count.
	// InstancesProse spells the service's scaling for the README.
	MaxInstancesMap string
	InstancesProse  string
	// Operations are the environments the operations workflow acts on from GitHub, every
	// one (a rerun of a release reaches production; a restore and the migration
	// operations every environment but it), with what the workflow needs of each; an
	// environment the placement records no project for is listed unwired.
	// OperationsProse spells them, RestorableProse the ones a restore reaches. AuthAction
	// and GcloudAction are the pinned GitHub Actions the workflow uses; PrimaryRegion is
	// where the version triggers are.
	Operations      []operationsEnv
	OperationsProse string
	RestorableProse string
	AuthAction      string
	GcloudAction    string
	PrimaryRegion   string
	// Image is what the seeded Dockerfile derives from the code.
	Image imageView
	// SubstitutionNames are the pipeline's own substitutions, space separated: the keys
	// of cloud-build.tf's substitutions map, read off its template so the pipeline
	// tells the declared ones (var.substitutions) from the contract's by one list.
	SubstitutionNames string
	PreviousEnvMap    string
	// EnvCountWord is the number of environments as a word.
	EnvCountWord string
	// EmptyEnvStrings and EmptyEnvMaps are the per-environment default blocks, env = ""
	// and env = {}, indented four spaces for a variable block; the Tfvars forms are the
	// same at the top level of terraform.tfvars, indented two.
	EmptyEnvStrings  string
	EmptyEnvMaps     string
	TfvarsEnvStrings string
	TfvarsEnvMaps    string
	// RegionCodes joins the region codes with |; RegionsSub is the _REGIONS
	// substitution; PrimaryCode is the primary region's code.
	RegionCodes string
	RegionsSub  string
	PrimaryCode string

	// Auth is the auth the site binds to.
	Auth *derive.Auth
	// Directory reports a directory sign-in: the auth has a registration and a callback,
	// and the stack the variables, the outputs and the hand steps for them. A password
	// auth has none of that, and its stack carries the cookie key alone.
	Directory bool
	// AuthVar is the stem of the auth's placement variables: <auth>_oidc.
	AuthVar string
	// RoutesDir is the directory of the file registering the callback, empty without one.
	RoutesDir string
	// The variables by role, for the templates that name them.
	ServiceName    *derive.Variable
	LoggingProject *derive.Variable
	Version        *derive.Variable
	// TraceSampling is the variable in the trace-sampling role, nil when the
	// application declares none (an application from before the skeleton read it).
	TraceSampling *derive.Variable
	// MaintenanceVariable is the variable the pipeline sets on a maintenance revision,
	// declared empty on the service by the stack.
	MaintenanceVariable string
	// SchedulerVariable is the variable that names the invoker identity of the scheduled
	// routes to the service (scheduler.tf); ScheduledPrefix the path they are served
	// under; ReleaseFileName the generated file that lists them. JobsTemplateVariable is
	// the variable that names the job process's template job to the service, which the
	// framework names the job of its own build from (locals.tf, jobs_template_env).
	SchedulerVariable    string
	JobsTemplateVariable string
	// ServerPackage is the framework's server, whose import by the site's main package
	// is the declaration that the image speaks h2c (derive.ServerPackage).
	ServerPackage   string
	ScheduledPrefix string
	ReleaseFileName string
	Port            *derive.Variable
	ClientID        *derive.Variable
	ClientSecret    *derive.Variable
	RedirectURL     *derive.Variable
	HostedDomain    *derive.Variable
	GroupPrefix     *derive.Variable
	GroupLookup     *derive.Variable
	// CookieKeySecret and ClientSecretSecret are the secrets by role.
	CookieKeySecret    *derive.Secret
	ClientSecretSecret *derive.Secret
	// The levels by name.
	CoreLevel derive.Level
	DataLevel derive.Level
	SiteLevel derive.Level

	// SecretsFiles names the files declaring the secrets.
	SecretsFiles string
	// SecretExample is the example pin of the first two secrets.
	SecretExample string
	// SecretsCell lists the secrets for the README table.
	SecretsCell string
	// DatabaseVariablesCell lists the database variables for the README table, the
	// common prefix written once.
	DatabaseVariablesCell string
	// DatabaseEnv, DirectoryEnv are the aligned assignment blocks of locals.tf.
	DatabaseEnv  string
	DirectoryEnv string
	// CodeDefaults are the data-level variables left to their code default.
	CodeDefaults []*derive.Variable
	// CodeDefaultsCell lists them for the README.
	CodeDefaultsCell string
	// SiteImageVars names the site-level variables the image sets to where it put a
	// bundle; the job variable, which the image also sets, is told on its own.
	SiteImageVars string
	// MigrateLevels spells the levels the migration constructs: "core and data".
	MigrateLevels string
	// JobsLevels spells the levels the job process constructs, JobsLevelList lists them
	// as HCL, and JobsReadsData reports whether the data level is among them; all empty
	// without a job process.
	JobsLevels    string
	JobsLevelList string
	JobsReadsData bool
	// FileStores are the file stores the code declares, prepared for the templates, in
	// declaration order (this field stands in front of the model's list of the same
	// name); none when the code declares no store. FileStoreEnv is their aligned
	// assignment block for locals.tf, and FileStoreAddresses lists their buckets as the
	// stack addresses them, comma-separated, for the pipeline's substitution.
	// JobsFileStores are the stores whose level the job process constructs;
	// JobsFileStoreEnv is their aligned block when they are some of the stores and not
	// all, so the job process gets a map of its own, and empty otherwise.
	FileStores         []fileStore
	FileStoreEnv       string
	FileStoreAddresses string
	JobsFileStores     []fileStore
	JobsFileStoreEnv   string
	// TasksQueue is the variable naming the task queue, or nil when the code declares
	// none; JobsReadsTasks reports that the job process constructs its level.
	TasksQueue     *derive.Variable
	JobsReadsTasks bool
	// FirestoreDatabase is the variable naming the Firestore database, or nil when the
	// code declares none; JobsReadsFirestore reports that the job process constructs
	// its level, and MigrateReadsFirestore that the migrate command does: the level
	// opens the database's live service when it is constructed, and the release's
	// role migration signals the running instances through it, so the migrate command
	// receives the database's name and a grant on it like any process of that level.
	FirestoreDatabase *derive.Variable
	// FirestoreProject is the variable naming the database's project, set to the
	// environment project; nil with FirestoreDatabase.
	FirestoreProject      *derive.Variable
	JobsReadsFirestore    bool
	MigrateReadsFirestore bool
	// FirebaseAPIKey is the variable the Firebase web API key of the Firestore database
	// is handed through, or nil when the code declares none.
	FirebaseAPIKey *derive.Variable
	// FirestoreFieldsProse names the fields the indexes file settles, with their policy:
	// "subscriptions.expiry and changes.expires, each with a time-to-live policy".
	FirestoreFieldsProse string
	// IdentityLines are the aligned identity lines of the service-accounts header.
	IdentityLines string
	// LabelLines are the extra labels, aligned to the derived ones.
	LabelLines string
	// HostnameDefaults is the aligned default block of var.hostnames.
	HostnameDefaults string
	// HostnamesSummary restates the hostnames in one line: the hosts with the domain
	// written once.
	HostnamesSummary string
	// RestatedDefaults are the aligned lines of the tfvars comment restating defaults.
	RestatedDefaults string
	// Armor is the Cloud Armor policy (cloud-armor.tf), its rules in priority order.
	Armor *armorView
	// BelowProduction spells the hostname shapes below production.
	BelowProduction string
	// HostnamesProse lists the hostnames for the README, wrapped after the first.
	HostnamesProse string
	// IntegrationHost is the integration environment's canonical hostname.
	IntegrationHost string
	// BuildArguments are the build arguments the placement declares (buildArguments), in
	// name order: each a value the stack makes, carried on the triggers as
	// _BUILD_ARG_<NAME>; none when it declares none. BuildArgumentLines is their block of
	// cloud-build.tf's substitutions map, BuildArgumentList names them ("FIREBASE_API_KEY
	// and PROJECT_ID"), BuildArgumentSubs names their substitutions for the README, and
	// BuildArgumentsProse each with the value it takes. BuildValueItems lists the
	// catalog a placement may name as the README's nested items, from the table derive
	// validates against.
	BuildArguments      []buildArgument
	BuildArgumentLines  string
	BuildArgumentList   string
	BuildArgumentSubs   string
	BuildArgumentsProse string
	BuildValueItems     string
}

// buildArgument is one declared build argument as the templates name it: the name the
// Dockerfile declares with ARG, the substitution that carries it, the catalog's value it
// takes, and the OpenTofu expression the stack reads it from.
type buildArgument struct {
	Name         string
	Substitution string
	Value        string
	Expr         string
}

// buildValueExprs are the catalog's values as the stack reads them, from its own
// resources and locals. The Firebase key's string is marked sensitive by the provider; it
// is the public value every browser receives, and marked it would make the triggers'
// substitutions and the substitutions output sensitive whole.
var buildValueExprs = map[string]string{
	derive.BuildValueFirebaseAPIKey:    "nonsensitive(google_apikeys_key.firebase.key_string)",
	derive.BuildValueFirestoreDatabase: "google_firestore_database.firestore.name",
	derive.BuildValueProjectID:         "local.project_id",
	derive.BuildValueEnvironment:       "var.environment",
	derive.BuildValueHostname:          "local.hostnames[0]",
}

// buildArguments prepares the declared build arguments for the templates, refusing a
// catalog value the stack has no expression for.
func (v *view) buildArguments() error {
	catalog := make([]string, 0, len(derive.BuildValues()))
	for _, value := range derive.BuildValues() {
		catalog = append(catalog, "  - `"+value.Name+"`: "+value.What+".")
	}
	v.BuildValueItems = strings.Join(catalog, "\n")
	var lines, names, subs, prose []string
	for _, name := range v.P.BuildArgumentNames() {
		value := v.P.BuildArguments[name]
		expr, ok := buildValueExprs[value]
		if !ok {
			return errors.Newf("buildArguments.%s names %s, which the stack has no expression for", name, value)
		}
		arg := buildArgument{Name: name, Substitution: derive.BuildArgumentSubstitution(name), Value: value, Expr: expr}
		v.BuildArguments = append(v.BuildArguments, arg)
		lines = append(lines, "    "+arg.Substitution+" = "+arg.Expr+" # "+arg.Value)
		names = append(names, arg.Name)
		subs = append(subs, "`"+arg.Substitution+"`")
		prose = append(prose, "`"+arg.Name+"` (`"+arg.Value+"`)")
	}
	v.BuildArgumentLines = strings.Join(lines, "\n")
	v.BuildArgumentList = joinAnd(names)
	v.BuildArgumentSubs = joinAnd(subs)
	v.BuildArgumentsProse = joinAnd(prose)

	return nil
}

// fileStore is one file store as the templates name it: the derived store, the value
// the stack hands its variable (the bucket's gs:// URL, as an OpenTofu interpolation of
// the bucket's name), its label and title for the comments and the README ("the
// documents file store", "The documents file store"), and whether the job process
// constructs its level.
type fileStore struct {
	derive.FileStore
	Value     string
	Label     string
	Title     string
	JobsReads bool
}

// defaultStoreLabel is the default store's label; a named store's names it.
const defaultStoreLabel = "the default file store"

// newFileStore prepares one store for the templates.
func newFileStore(s derive.FileStore, jobs *derive.Process) fileStore {
	label := defaultStoreLabel
	if !s.Default() {
		label = "the " + s.Name + " file store"
	}

	return fileStore{
		FileStore: s,
		Value:     `"gs://${local.` + s.Resource + `_bucket_name}"`,
		Label:     label,
		Title:     "The" + strings.TrimPrefix(label, "the"),
		JobsReads: jobs != nil && jobs.Reads(s.Variable.Level),
	}
}

// The width the labels block aligns to: its longest derived key.
const labelsWidth = len("terraform_source_path")

// The job process's defaults (var.jobs_timeout, var.jobs_retries, var.jobs_resources),
// restated in the seeded tfvars: thirty minutes, no retry, one CPU and 512 MiB.
const (
	jobsTimeout = "1800s"
	jobsRetries = "0"
	jobsCPU     = "1"
	jobsMemory  = "512Mi"
)

// The task queue's defaults (var.tasks_max_concurrent, var.tasks_max_attempts): ten
// tasks in flight, five attempts.
const (
	tasksMaxConcurrent = "10"
	tasksMaxAttempts   = "5"
)

// numberWords spell the small counts the prose uses.
var numberWords = map[int]string{1: "one", 2: "two", 3: "three", 4: "four", 5: "five", 6: "six"}

// newView prepares the view, refusing a model the templates cannot render.
// imageView is what the Dockerfile template needs: the packages the image builds, the
// schema directory the migrate command reads, the browser workspace and its bundles,
// and the variable the image sets to the release.
type imageView struct {
	// SitePkg and MigratePkg are the packages go build compiles, as go names them from
	// the root: "." for the site at the root, "./cmd/deployment/migrate"; JobsPkg is
	// the job process's, empty without one.
	SitePkg    string
	MigratePkg string
	JobsPkg    string
	// HooksPkg is the hooks program's package, empty without one.
	HooksPkg string
	// SchemaDir is the directory the migrate command reads relative to its working
	// directory (schema: the migrations, the seed), copied whole.
	SchemaDir string
	// Workspaces are the browser workspaces, in the order the site's variables name
	// them, each with the bundles built there (one per browser app); Bundles lists every
	// bundle across them. An application without a browser has none.
	Workspaces []workspace
	Bundles    []bundle
	// VersionVar is the variable the image sets to the release, empty when the code
	// declares none.
	VersionVar string
	// RootGo reports Go files in the root package, which the Go stage copies as *.go;
	// GoDirs are the top-level directories holding Go packages, copied one line each.
	RootGo bool
	GoDirs []string
	// Ignores are the paths the seeded .dockerignore keeps out of a local build's
	// context: the git and workflow directories, the stack, the tests, and each
	// workspace's installs and outputs; a name that is a Go package directory stays in.
	Ignores []string
}

// workspace is one browser workspace: its root-relative directory (web), the build
// stage that builds it, its bundles, and the files its install stage copies, space
// separated as a COPY instruction's sources (web/package.json web/bun.lock).
type workspace struct {
	Dir     string
	Stage   string
	Bundles []bundle
	Install string
}

// ignoredPaths are the names the seeded .dockerignore lists at the root, before each
// workspace's installs and outputs.
var ignoredPaths = []string{".git", ".github", "infrastructure", "test"}

// workspaceIgnores are what a workspace's install and build leave under it, which a
// local build must not send: the installed packages, the CLI's cache, the bundles.
var workspaceIgnores = []string{"node_modules", ".angular", "dist"}

// bundle is one built browser bundle: the variable naming its directory to the server
// (APP_CONSOLE_DIST), its path under the workspace (dist/console), and the workspace and
// build stage it comes from.
type bundle struct {
	Var       string
	Path      string
	Workspace string
	Stage     string
}

// stageName names the build stage of a workspace after its directory: web-build-env
// for web, with a nested directory's slashes as dashes.
func stageName(dir string) string {
	return strings.ReplaceAll(dir, "/", "-") + "-build-env"
}

// pkgPath is a root-relative directory as go build names the package: "." at the root.
func pkgPath(dir string) string {
	if dir == "" || dir == "." {
		return "."
	}

	return "./" + strings.TrimPrefix(dir, "./")
}

// newImageView reads the image facts off the model.
func newImageView(m *derive.Model, siteLevel string) imageView {
	iv := imageView{SitePkg: pkgPath(m.Site.Dir), SchemaDir: path.Dir(m.Schema.MigrationsDir)}
	if m.Migrate != nil {
		iv.MigratePkg = pkgPath(m.Migrate.Dir)
	}
	if m.Jobs != nil {
		iv.JobsPkg = pkgPath(m.Jobs.Dir)
	}
	if m.HookProgram != nil {
		iv.HooksPkg = pkgPath(m.HookProgram.Dir)
	}
	if iv.SchemaDir == "." || iv.SchemaDir == "" {
		iv.SchemaDir = m.Schema.MigrationsDir
	}
	for i := range m.Variables {
		v := &m.Variables[i]
		if v.Role == derive.RoleVersion {
			iv.VersionVar = v.Name
		}
		if v.Level != siteLevel || !v.HasDefault {
			continue
		}
		parts := derive.BundleRE.FindStringSubmatch(v.Default)
		if parts == nil {
			continue
		}
		b := bundle{Var: v.Name, Path: "dist/" + parts[2], Workspace: parts[1], Stage: stageName(parts[1])}
		iv.Bundles = append(iv.Bundles, b)
		at := slices.IndexFunc(iv.Workspaces, func(w workspace) bool {
			return w.Dir == b.Workspace
		})
		if at < 0 {
			iv.Workspaces = append(iv.Workspaces, workspace{Dir: b.Workspace, Stage: b.Stage, Install: strings.Join(m.ImageInputs.WorkspaceInputs(b.Workspace), " ")})
			at = len(iv.Workspaces) - 1
		}
		iv.Workspaces[at].Bundles = append(iv.Workspaces[at].Bundles, b)
	}
	iv.RootGo, iv.GoDirs = m.ImageInputs.RootGo, m.ImageInputs.GoDirs
	for _, name := range ignoredPaths {
		if !slices.Contains(iv.GoDirs, name) {
			iv.Ignores = append(iv.Ignores, name)
		}
	}
	for _, w := range iv.Workspaces {
		for _, name := range workspaceIgnores {
			iv.Ignores = append(iv.Ignores, w.Dir+"/"+name)
		}
	}

	return iv
}

// The images the pipeline runs bedrock in. The OpenTofu version is the one the stack's
// infrastructure workflow checks with.
const (
	gcloudImage   = "gcr.io/cloud-builders/gcloud"
	openTofuImage = "ghcr.io/opentofu/opentofu:1.12.6"
	dockerImage   = "gcr.io/cloud-builders/docker"
)

// goImage is the image a commit pin is built in, by the pipeline's first step and the
// infrastructure workflow: the Go image the seeded Dockerfile builds the application in,
// pinned at the same digest, so bedrock names one Go image. goImageGo is the Go it carries,
// which must be at least the go line of bedrock's go.mod: the builds set
// GOTOOLCHAIN=local, so a newer go line fails them rather than downloading a toolchain,
// and the fix is a new digest here (a test holds the two together).
const (
	goImage   = "cgr.dev/chainguard/go@sha256:75c0c2c118e36951cb63da108fa795f4724bde0c36ae84c9c17a4e08255ad324"
	goImageGo = "1.27.2"
)

// goInstallEnv is the environment go install builds a commit pin in, the pipeline's and
// the infrastructure workflow's alike. A static binary (CGO_ENABLED=0): the later steps run
// it in OpenTofu's image, which is Alpine and has no glibc. The image's Go and no other
// (GOTOOLCHAIN=local). The build directory kept out of the binary (-trimpath). The caches
// and GOPATH (where Go keeps what the checksum database said) under /tmp, off the home
// directory the pipeline's later steps share. The module proxy with no direct fallback,
// which serves a commit even after the tag its pseudo-version was built on is gone. Go's
// checksum database verifies the module, whatever the image or the runner would set.
var goInstallEnv = []string{
	"CGO_ENABLED=0", "GOTOOLCHAIN=local", "GOFLAGS=-trimpath",
	"GOPATH=/tmp/go", "GOCACHE=/tmp/go-build", "GOMODCACHE=/tmp/go-mod",
	"GOPROXY=https://proxy.golang.org", "GOSUMDB=sum.golang.org", "GONOSUMDB=", "GOPRIVATE=", "GOINSECURE=",
}

// The FetchBedrock step's timeout in seconds: a release pin's download (three retries of an
// attempt bounded to a minute, so a GitHub download that hangs or answers 502 for a while,
// which the lab saw three times in a quarter of an hour, is tried again inside the step's
// time), or a commit pin's go install, which pulls the Go image and builds bedrock. The
// sweep's whole-build timeout
// is what it spends after that step plus the step's own timeout, so the longer build of a
// commit pin never eats the time the later steps had. The pipeline's whole-build timeout
// is Cloud Build's ceiling: a release that waits for its maintenance window waits inside
// the run, and every step keeps its own timeout, so a hung step still fails on time and
// only the wait uses the headroom.
const (
	releaseFetchSeconds = 300
	commitFetchSeconds  = 600
	// pipelineCeilingSeconds is Cloud Build's longest build, 24 hours.
	pipelineCeilingSeconds = 86400
	// sweepAfterFetchSeconds is the sweep's time after FetchBedrock: the sweep step's
	// own timeout and 480 seconds for pulling the images and starting the steps.
	sweepAfterFetchSeconds = sweepStepSeconds + 480
	sweepStepSeconds       = 3600
)

// FetchBedrockStep is the first step of the pipeline and of the sweep, which both print
// it, so the two cannot drift: it leaves the pinned bedrock at /builder/home/bedrock for
// every later step. For a release pin the release's linux/amd64 binary is downloaded and
// verified against the placement's checksum. For a commit pin the commit is built with go
// install in the Go image, where Go's checksum database verifies it; the script holds no
// $, which Cloud Build would read as a substitution.
func (v *view) FetchBedrockStep() string {
	var b strings.Builder
	b.WriteString("  - id: FetchBedrock\n")
	if !v.CommitPin {
		fmt.Fprintf(&b, `    name: %s
    entrypoint: bash
    timeout: %ds
    args:
      - -c
      - |
        set -euo pipefail
        curl -fsSL --connect-timeout 10 --max-time 60 --retry 3 -o /builder/home/bedrock "%s"
        echo "%s  /builder/home/bedrock" | sha256sum --check -
        chmod 0755 /builder/home/bedrock
        /builder/home/bedrock --version
`, v.GcloudImage, releaseFetchSeconds, v.BedrockURL, v.P.BedrockSHA256)

		return b.String()
	}
	fmt.Fprintf(&b, "    name: %s\n    entrypoint: sh\n    timeout: %ds\n    env:\n", v.GoImage, commitFetchSeconds)
	for _, e := range append([]string{"GOBIN=/builder/home"}, goInstallEnv...) {
		fmt.Fprintf(&b, "      - %s\n", e)
	}
	fmt.Fprintf(&b, `    args:
      - -c
      - |
        set -eu
        go install -ldflags="-s -w" %s@%s
        /builder/home/bedrock --version
`, release.Module, v.P.BedrockVersion)

	return b.String()
}

// fetchBedrockSeconds is the FetchBedrock step's timeout for the placement's pin.
func (v *view) fetchBedrockSeconds() int {
	if v.CommitPin {
		return commitFetchSeconds
	}

	return releaseFetchSeconds
}

// PipelineTimeout is the pipeline's whole-build timeout: Cloud Build's ceiling, so that a
// release may wait for its maintenance window inside the run.
func (v *view) PipelineTimeout() string {
	return strconv.Itoa(pipelineCeilingSeconds) + "s"
}

// SweepStepTimeout is the timeout of the sweep's SweepClosedPullRequests step.
func (v *view) SweepStepTimeout() string {
	return strconv.Itoa(sweepStepSeconds) + "s"
}

// SweepTimeout is the sweep's whole-build timeout.
func (v *view) SweepTimeout() string {
	return strconv.Itoa(v.fetchBedrockSeconds()+sweepAfterFetchSeconds) + "s"
}

// BedrockModule is the module go install builds for a commit pin.
func (v *view) BedrockModule() string {
	return release.Module
}

// GoInstallDockerEnv is goInstallEnv as the -e options of docker run, for the
// infrastructure workflow, which builds a commit pin in the same image: one line each,
// continued with a backslash and indented to sit under its docker run.
func (v *view) GoInstallDockerEnv() string {
	lines := make([]string, 0, len(goInstallEnv))
	for _, e := range goInstallEnv {
		lines = append(lines, "            -e "+e+" \\")
	}

	return strings.Join(lines, "\n")
}

// HasHook reports whether the application implements a hook for the stage, a script or
// its hooks program: the pipeline has a step for it.
func (v *view) HasHook(stage string) bool {
	return slices.Contains(v.Hooks, hook.Stage(stage)) || v.HookByProgram(stage)
}

// HookByProgram reports whether the stage's hook is the hooks program's.
func (v *view) HookByProgram(stage string) bool {
	return v.HookProgram != nil && slices.Contains(v.HookProgram.Stages, hook.Stage(stage))
}

// HookSource is where the stage's hook is: its script, or the hooks program.
func (v *view) HookSource(stage string) string {
	if v.HookByProgram(stage) {
		return "the hooks program (" + v.HookProgram.Dir + ")"
	}

	return hook.Stage(stage).Script()
}

func newView(m *derive.Model) (*view, error) {
	if len(m.Auths) != 1 {
		return nil, errors.Newf("the stack binds one auth; %s has %d", m.App, len(m.Auths))
	}
	p := m.Placement
	v := &view{Model: m, P: p, Prefix: p.Prefix, Integration: p.Integration(), Production: p.Production()}
	v.RepoFullName = repoFullName(m.Repository)
	v.SweepMinute = sweepMinute(m.App)
	v.CommitPin = release.IsCommitPin(p.BedrockVersion)
	if !v.CommitPin {
		v.BedrockURL = release.URL(p.BedrockVersion, release.PipelineAsset())
	}
	v.GoImage = goImage
	v.GcloudImage, v.OpenTofuImage, v.DockerImage = gcloudImage, openTofuImage, dockerImage
	v.Auth = &m.Auths[0]
	v.MaintenanceVariable = derive.MaintenanceVariable
	v.SchedulerVariable, v.ScheduledPrefix, v.ReleaseFileName = derive.SchedulerInvokerVariable, derive.ScheduledPrefix, derive.ReleaseFileName
	v.JobsTemplateVariable = derive.JobsTemplateVariable
	v.ServerPackage = derive.ServerPackage
	v.Directory = v.Auth.OIDC()
	v.AuthVar = v.Auth.VariablePrefix()
	if v.Directory {
		v.RoutesDir = path.Dir(v.Auth.Callback.File)
	}
	if err := v.roles(); err != nil {
		return nil, err
	}
	v.environments()
	v.regions()
	v.secrets()
	v.Armor = newArmorView(m)
	v.blocks()
	v.operations()
	if err := v.buildArguments(); err != nil {
		return nil, err
	}

	names, err := SubstitutionNames()
	if err != nil {
		return nil, err
	}
	v.SubstitutionNames = strings.Join(names, " ")

	return v, nil
}

// roles finds the variables and levels the templates name: the well-known ones always,
// the directory registration's only for a directory sign-in.
func (v *view) roles() error {
	roles := []struct {
		role derive.Role
		dst  **derive.Variable
	}{
		{derive.RoleServiceName, &v.ServiceName},
		{derive.RoleLoggingProject, &v.LoggingProject},
		{derive.RoleVersion, &v.Version},
		{derive.RolePort, &v.Port},
	}
	if v.Directory {
		roles = append(roles, []struct {
			role derive.Role
			dst  **derive.Variable
		}{
			{derive.RoleClientID, &v.ClientID},
			{derive.RoleClientSecret, &v.ClientSecret},
			{derive.RoleRedirectURL, &v.RedirectURL},
			{derive.RoleHostedDomain, &v.HostedDomain},
			{derive.RoleGroupPrefix, &v.GroupPrefix},
			{derive.RoleGroupLookup, &v.GroupLookup},
		}...)
	}
	for _, r := range roles {
		*r.dst = v.byRole(r.role)
		if *r.dst == nil {
			return errors.Newf("no variable in the %s role: the stack needs one", r.role)
		}
	}
	v.TraceSampling = v.byRole(derive.RoleTraceSampling)
	for _, s := range v.Secrets {
		switch s.Variable.Role {
		case derive.RoleCookieKey:
			v.CookieKeySecret = ptr(s)
		case derive.RoleClientSecret:
			v.ClientSecretSecret = ptr(s)
		default:
		}
	}
	if v.CookieKeySecret == nil {
		return errors.New("the stack needs the cookie key among the secrets")
	}
	if v.Directory && v.ClientSecretSecret == nil {
		return errors.New("the stack needs the client secret among the secrets of a directory sign-in")
	}
	levels := []struct {
		name string
		dst  *derive.Level
	}{{derive.LevelCore, &v.CoreLevel}, {derive.LevelData, &v.DataLevel}, {derive.LevelSite, &v.SiteLevel}}
	for _, l := range levels {
		level, ok := v.Level(l.name)
		if !ok {
			return errors.Newf("no %s level", l.name)
		}
		*l.dst = level
	}

	return nil
}

// ptr copies a secret onto the heap.
func ptr(s derive.Secret) *derive.Secret {
	return &s
}

// byRole returns the variable in the role, or nil.
func (v *view) byRole(role derive.Role) *derive.Variable {
	for i := range v.Variables {
		if v.Variables[i].Role == role {
			return &v.Variables[i]
		}
	}

	return nil
}

// environments spells the environments and their hostnames.
func (v *view) environments() {
	envs := v.P.Environments
	quoted := make([]string, 0, len(envs))
	ticked := make([]string, 0, len(envs))
	for _, e := range envs {
		quoted = append(quoted, `"`+e+`"`)
		ticked = append(ticked, "`"+e+"`")
	}
	v.EnvsOr = joinOr(envs)
	v.EnvsComma = strings.Join(envs, ", ")
	v.EnvsBacktickOr = joinOr(ticked)
	v.EnvList = "[" + strings.Join(quoted, ", ") + "]"
	v.order(envs)
	v.EnvCountWord = numberWords[len(envs)]
	if v.EnvCountWord == "" {
		v.EnvCountWord = fmt.Sprint(len(envs))
	}
	v.SharedInstanceEnvs = joinAnd(envs[1:])
	v.SharedInstanceEnvsWrapped = v.SharedInstanceEnvs
	if len(envs) > 2 {
		v.SharedInstanceEnvsWrapped = strings.TrimSuffix(v.SharedInstanceEnvs, " "+v.Production) + "\n  " + v.Production
	}

	strs := make([][2]string, 0, len(envs))
	maps := make([][2]string, 0, len(envs))
	hosts := make([][2]string, 0, len(envs))
	var summary, below []string
	for _, e := range v.Environments {
		strs = append(strs, [2]string{e.Name, `""`})
		maps = append(maps, [2]string{e.Name, "{}"})
		hosts = append(hosts, [2]string{e.Name, `["` + strings.Join(e.Hostnames, `", "`) + `"]`})
		host := e.Hostnames[0]
		if e.Name != v.Production {
			summary = append(summary, strings.TrimSuffix(host, v.P.AppsDomain))
			below = append(below, "app-"+e.Name+".domain")
		} else {
			summary = append(summary, host)
		}
	}
	v.EmptyEnvStrings = aligned("    ", strs)
	v.EmptyEnvMaps = aligned("    ", maps)
	v.TfvarsEnvStrings = aligned("  ", strs)
	v.TfvarsEnvMaps = aligned("  ", maps)
	v.HostnameDefaults = aligned("    ", hosts)
	v.HostnamesSummary = strings.Join(summary, " / ")
	// Named from the one nearest production down.
	slices.Reverse(below)
	v.BelowProduction = joinAnd(below)
	all := make([]string, 0, len(v.Environments))
	for _, e := range v.Environments {
		all = append(all, "`"+e.Hostnames[0]+"`")
	}
	v.HostnamesProse = all[0] + ",\n" + strings.Join(all[1:], ", ")
	v.IntegrationHost = v.Environments[0].Hostnames[0]
}

// operationsEnv is one environment as the operations workflow addresses it: its
// project, the workload identity provider and the operations identity (both named
// after the project, as 2-env creates them), the version trigger, the restore a run
// makes there (an empty database, or production's backup; none for production, whose
// database is restored to a backup alone), the rollback trigger, and the log bucket
// holding the application's build logs, where the migration's lines are (named as the
// application stack names it). Wired is false for an environment the placement records
// no project for. Restorable is false for production, whose own restore does not exist:
// a restore to a backup, a rerun and a rollback reach it, and its migrations are the
// platform operator's.
type operationsEnv struct {
	Env        string
	Wired      bool
	Restorable bool
	Project    string
	Provider   string
	Identity   string
	Trigger    string
	Restore    string
	Logs       string
	// Rollback is the environment's rollback trigger, which bedrock rollback runs: the
	// earlier release's build again with nothing of the database, in every environment.
	Rollback string
}

// The pinned GitHub Actions the operations workflow uses, by commit, with the release
// each commit is.
const (
	authAction   = "7c6bc770dae815cd3e89ee6cdf493a5fab2cc093 # v3"
	gcloudAction = "aa5489c8933f4cc7a4f7d45035b3b1440c9c10db # v3.0.1"
)

// operations lists the environments the operations workflow acts on: every one; a
// rerun, a rollback, a restore to a backup, the listing and the maintenance step reach
// production, and the environment's own restore and the migration operations every
// environment but production.
func (v *view) operations() {
	v.AuthAction, v.GcloudAction = authAction, gcloudAction
	v.PrimaryRegion = v.P.Regions[0].Name
	for _, env := range v.P.Environments {
		o := operationsEnv{
			Env: env, Restorable: env != v.Production,
			Trigger: v.Prefix + "-" + env + "-" + v.PrimaryCode + "-" + v.App + "-version",
			Logs:    v.Prefix + "-" + env + "-gbl-" + v.App + "-migrate-logs",
		}
		if o.Restorable {
			o.Restore = v.P.RestoreKind(env)
		}
		o.Rollback = v.Prefix + "-" + env + "-" + v.PrimaryCode + "-" + v.App + "-rollback"
		if project, ok := v.P.Project(env); ok {
			o.Wired = true
			o.Project = project.ID
			o.Provider = "projects/" + project.Number + "/locations/global/workloadIdentityPools/" + v.Prefix + "-" + env + "-github/providers/github"
			o.Identity = v.Prefix + "-" + env + "-gbl-" + v.App + "-ops@" + project.ID + ".iam.gserviceaccount.com"
		}
		v.Operations = append(v.Operations, o)
	}
	v.OperationsProse = joinOr(v.P.Environments)
	v.RestorableProse = joinOr(v.P.Restorable())
}

// regions spells the regions.
func (v *view) regions() {
	codes := make([]string, 0, len(v.P.Regions))
	subs := make([]string, 0, len(v.P.Regions))
	for _, r := range v.P.Regions {
		codes = append(codes, r.Code)
		subs = append(subs, r.Name+":"+r.Code)
	}
	v.RegionCodes = strings.Join(codes, "|")
	v.RegionsSub = strings.Join(subs, ",")
	v.PrimaryCode = v.P.Primary().Code
}

// secrets spells the secrets.
func (v *view) secrets() {
	var files, example []string
	cells := make([]string, 0, len(v.Secrets))
	for i, s := range v.Secrets {
		if len(files) == 0 || files[len(files)-1] != s.Variable.File {
			files = append(files, s.Variable.File)
		}
		cells = append(cells, "`"+s.Variable.Name+"`")
		if i < 2 {
			example = append(example, s.Variable.Name+` = "1"`)
		}
	}
	v.SecretsFiles = strings.Join(files, ", ")
	v.SecretsCell = strings.Join(cells, ", ")
	v.SecretExample = strings.Join(example, ", ")
}

// blocks prepares the aligned blocks and the remaining phrases.
func (v *view) blocks() {
	db := v.Database
	v.DatabaseEnv = aligned("    ", [][2]string{
		{db.Project.Name, "local.instance.project"},
		{db.Instance.Name, "local.instance.name"},
		{db.Name.Name, "local.database_name"},
	})
	if v.Directory {
		v.DirectoryEnv = aligned("    ", [][2]string{
			{v.HostedDomain.Name, "var." + v.AuthVar + "_hosted_domain"},
			{v.GroupPrefix.Name, "var." + v.AuthVar + "_group_prefix"},
		})
	}
	prefix := commonPrefix(db.Project.Name, db.Instance.Name, db.Name.Name)
	v.DatabaseVariablesCell = "`" + db.Project.Name + "`, `" + strings.TrimPrefix(db.Instance.Name, prefix) + "`, `" + strings.TrimPrefix(db.Name.Name, prefix) + "`"

	var defaults []string
	for _, x := range v.ByLevel(v.DataLevel.Name) {
		if x.Supply() == derive.SupplyDefault {
			v.CodeDefaults = append(v.CodeDefaults, x)
			defaults = append(defaults, "`"+x.Name+"` (code default)")
		}
	}
	v.CodeDefaultsCell = strings.Join(defaults, ", ")
	var image []string
	for _, x := range v.ByLevel(v.SiteLevel.Name) {
		if x.Image {
			image = append(image, x.Name)
		}
	}
	v.SiteImageVars = joinAnd(image)
	v.MigrateLevels = joinAnd(v.Migrate.Levels)

	stem := v.Prefix + "-<env>-gbl-" + v.App + "-"
	identities := [][2]string{
		{stem + v.Site.Name, v.Site.Main + ", the served site (Cloud Run service)"},
	}
	if v.Jobs != nil {
		v.JobsLevels = joinAnd(v.Jobs.Levels)
		v.JobsLevelList = `["` + strings.Join(v.Jobs.Levels, `", "`) + `"]`
		v.JobsReadsData = v.Jobs.Reads(v.DataLevel.Name)
		identities = append(identities, [2]string{stem + v.Jobs.Name, v.Jobs.Dir + ", the job process (Cloud Run job)"})
	}
	v.IdentityLines = aligned("#   ", identities, "  ")
	v.declarations()

	keys := make([]string, 0, len(v.P.Labels))
	width := labelsWidth
	for k := range v.P.Labels {
		keys = append(keys, k)
		width = max(width, len(k))
	}
	sort.Strings(keys)
	labels := make([]string, 0, len(keys))
	for _, k := range keys {
		labels = append(labels, fmt.Sprintf("    %-*s = %q", width, k, v.P.Labels[k]))
	}
	v.LabelLines = strings.Join(labels, "\n")

	restated := [][2]string{
		{"hostnames", v.HostnamesSummary},
		{"placeholder_image", v.P.PlaceholderImage},
	}
	if v.Directory {
		restated = append(restated,
			[2]string{v.AuthVar + "_group_lookup", `"` + v.Auth.GroupLookupDefault + `"`},
			[2]string{v.AuthVar + "_group_prefix", `"` + v.Auth.GroupPrefixDefault + `"`},
			[2]string{v.AuthVar + "_hosted_domain", `"` + v.P.HostedDomain + `"`},
		)
	}
	if v.Jobs != nil {
		restated = append(restated,
			[2]string{"jobs_timeout", `"` + jobsTimeout + `"`},
			[2]string{"jobs_retries", jobsRetries},
			[2]string{"jobs_resources", "cpu " + jobsCPU + ", memory " + jobsMemory},
		)
	}
	if v.TasksQueue != nil {
		restated = append(restated,
			[2]string{"tasks_max_concurrent", tasksMaxConcurrent},
			[2]string{"tasks_max_attempts", tasksMaxAttempts},
		)
	}
	v.RestatedDefaults = aligned("#   ", restated, " = ")
}

// declarations finds the resources the code declares by a well-known variable (the
// file stores, the task queue, the Firestore database) and whether the job process,
// and for the Firestore database the migrate command, constructs their levels.
func (v *view) declarations() {
	v.fileStores()
	v.TasksQueue = v.byRole(derive.RoleTasksQueue)
	if v.TasksQueue != nil && v.Jobs != nil {
		v.JobsReadsTasks = v.Jobs.Reads(v.TasksQueue.Level)
	}
	v.FirestoreDatabase = v.byRole(derive.RoleFirestoreDatabase)
	if v.FirestoreDatabase != nil {
		v.FirestoreProject = v.byRole(derive.RoleFirestoreProject)
		v.MigrateReadsFirestore = v.Migrate.Reads(v.FirestoreDatabase.Level)
	}
	if v.FirestoreDatabase != nil && v.Jobs != nil {
		v.JobsReadsFirestore = v.Jobs.Reads(v.FirestoreDatabase.Level)
	}
	v.FirebaseAPIKey = v.byRole(derive.RoleFirebaseAPIKey)
	if v.Firestore != nil {
		v.FirestoreFieldsProse = firestoreFieldsProse(v.Firestore.Fields)
	}
}

// fileStores prepares the file stores for the templates: each store with its value and
// label, the aligned block of locals.tf, the buckets' addresses for the pipeline, and
// the stores whose level the job process constructs, with a block of their own when
// they are some of the stores and not all.
func (v *view) fileStores() {
	stores := v.Model.FileStores
	all := make([][2]string, 0, len(stores))
	addresses := make([]string, 0, len(stores))
	var jobs [][2]string
	for i := range stores {
		s := newFileStore(stores[i], v.Jobs)
		v.FileStores = append(v.FileStores, s)
		all = append(all, [2]string{s.Variable.Name, s.Value})
		addresses = append(addresses, s.Address())
		if s.JobsReads {
			v.JobsFileStores = append(v.JobsFileStores, s)
			jobs = append(jobs, [2]string{s.Variable.Name, s.Value})
		}
	}
	v.FileStoreEnv = aligned("    ", all)
	v.FileStoreAddresses = strings.Join(addresses, ",")
	if len(jobs) > 0 && len(jobs) < len(all) {
		v.JobsFileStoreEnv = aligned("    ", jobs)
	}
}

// firestoreFieldsProse names the fields the indexes file settles as a sentence
// fragment: each as collection.field, and whether a time-to-live policy is on every one,
// on some (named), or on none.
func firestoreFieldsProse(fields []derive.FirestoreField) string {
	if len(fields) == 0 {
		return "none"
	}
	names := make([]string, 0, len(fields))
	var ttl []string
	for i := range fields {
		name := "`" + fields[i].Collection + "." + fields[i].Field + "`"
		names = append(names, name)
		if fields[i].TTL {
			ttl = append(ttl, name)
		}
	}
	switch {
	case len(ttl) == len(fields) && len(fields) == 1:
		return names[0] + ", with a time-to-live policy"
	case len(ttl) == len(fields):
		return joinAnd(names) + ", each with a time-to-live policy"
	case len(ttl) == 0:
		return joinAnd(names) + ", with no time-to-live policy"
	default:
		return joinAnd(names) + ", a time-to-live policy on " + joinAnd(ttl)
	}
}

// Purpose says what a secret is for: the framework's words for the roles it knows,
// the field's doc comment for a secret of the application's own.
func (v *view) Purpose(s *derive.Secret) string {
	switch s.Variable.Role {
	case derive.RoleCookieKey:
		return "Signs session cookies and seals list cursors: Base64 of 32+ random bytes. Rotating it ends every session and cursor."
	case derive.RoleClientSecret:
		return "OAuth client secret of the application's registration with Google (pairs with " + v.ClientID.Name + ")."
	default:
		if s.Variable.Doc != "" {
			return firstSentence(s.Variable.Doc)
		}

		return "A credential " + s.Variable.Declaration() + " reads."
	}
}

// aligned writes key = value lines with the values in one column. The separator
// defaults to " = ".
func aligned(indent string, pairs [][2]string, separator ...string) string {
	sep := " = "
	if len(separator) > 0 {
		sep = separator[0]
	}
	width := 0
	for _, p := range pairs {
		width = max(width, len(p[0]))
	}
	lines := make([]string, 0, len(pairs))
	for _, p := range pairs {
		lines = append(lines, indent+fmt.Sprintf("%-*s", width, p[0])+sep+p[1])
	}

	return strings.Join(lines, "\n")
}

// joinOr writes "a, b, or c".
func joinOr(items []string) string {
	return joinWith(items, "or")
}

// machineNames spells the machines a placement's buildMachine may name with their vCPUs
// ("`E2_MEDIUM` (1 vCPU), ..., and `E2_HIGHCPU_32` (32 vCPUs)"), so the README lists what
// derive accepts and the two never drift apart.
func machineNames() string {
	machines := derive.Machines()
	names := make([]string, 0, len(machines))
	for _, m := range machines {
		unit := "vCPUs"
		if m.CPUs == 1 {
			unit = "vCPU"
		}
		names = append(names, fmt.Sprintf("`%s` (%d %s)", m.Name, m.CPUs, unit))
	}

	return joinAnd(names)
}

// joinAnd writes "a, b, and c" (or "a and b").
func joinAnd(items []string) string {
	return joinWith(items, "and")
}

func joinWith(items []string, word string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	case 2:
		return items[0] + " " + word + " " + items[1]
	default:
		return strings.Join(items[:len(items)-1], ", ") + ", " + word + " " + items[len(items)-1]
	}
}

// commonPrefix is the longest prefix the names share, cut at an underscore.
func commonPrefix(names ...string) string {
	if len(names) == 0 {
		return ""
	}
	prefix := names[0]
	for _, n := range names[1:] {
		for !strings.HasPrefix(n, prefix) {
			prefix = prefix[:len(prefix)-1]
		}
	}
	if i := strings.LastIndex(prefix, "_"); i >= 0 {
		prefix = prefix[:i]
	}

	return prefix
}

// firstSentence returns the doc comment's first sentence, on one line.
func firstSentence(doc string) string {
	text := strings.Join(strings.Fields(doc), " ")
	if i := strings.Index(text, ". "); i >= 0 {
		return text[:i+1]
	}

	return text
}

// order fills the promotion-order fields: which environments wait for approval, and
// the environment before each.
func (v *view) order(envs []string) {
	approvals := make([]string, 0, len(v.P.ApprovalEnvironments()))
	for _, env := range v.P.ApprovalEnvironments() {
		approvals = append(approvals, strconv.Quote(env))
	}
	v.ApprovalList = "[" + strings.Join(approvals, ", ") + "]"
	seeds := make([]string, 0, len(v.P.SeedEnvironments()))
	for _, env := range v.P.SeedEnvironments() {
		seeds = append(seeds, strconv.Quote(env))
	}
	v.SeedList = "[" + strings.Join(seeds, ", ") + "]"
	backups := make([]string, 0, len(v.P.ReleaseBackupEnvironments()))
	for _, env := range v.P.ReleaseBackupEnvironments() {
		backups = append(backups, strconv.Quote(env))
	}
	v.ReleaseBackupList = "[" + strings.Join(backups, ", ") + "]"
	v.ReleaseBackupsProse = joinAnd(v.P.ReleaseBackupEnvironments())
	retention := make([]string, 0, len(envs))
	for _, env := range envs {
		retention = append(retention, env+" = "+strconv.Quote(v.P.Retention(env)))
	}
	v.RetentionMap = "{ " + strings.Join(retention, ", ") + " }"
	v.Image = newImageView(v.Model, v.SiteLevel.Name)
	v.ApprovalsProse = joinAnd(v.P.ApprovalEnvironments())
	if v.ApprovalsProse == "" {
		v.ApprovalsProse = "no environment"
	}
	if machine, ok := v.P.Machine(); ok {
		v.BuildMachine, v.BuildMachineCPUs = machine.Name, machine.CPUs
	}
	v.BuildMachineNames = machineNames()
	v.MaxInstancesMap, v.InstancesProse = maxInstances(v.P)
	previous := make([]string, 0, len(envs))
	for _, env := range envs {
		previous = append(previous, env+" = "+strconv.Quote(v.P.Previous(env)))
	}
	v.PreviousEnvMap = "{ " + strings.Join(previous, ", ") + " }"
}

// maxInstances spells the placement's instance caps: the HCL map of the environments it
// caps, in promotion order (empty when it caps none), and the service's scaling as the
// README says it, from zero instances per region up to the cap of each environment, or
// to Cloud Run's default where it sets none.
func maxInstances(p *derive.Placement) (hclMap, readme string) {
	type capped struct {
		n    int
		envs []string
	}
	var (
		entries  []string
		caps     []capped
		uncapped []string
	)
	for _, env := range p.Environments {
		n, ok := p.MaxInstanceCount(env)
		if !ok {
			uncapped = append(uncapped, env)

			continue
		}
		entries = append(entries, env+" = "+strconv.Itoa(n))
		i := len(caps)
		for j := range caps {
			if caps[j].n == n {
				i = j
			}
		}
		if i == len(caps) {
			caps = append(caps, capped{n: n})
		}
		caps[i].envs = append(caps[i].envs, env)
	}
	if len(caps) == 0 {
		return "", "from zero to Cloud Run's default maximum instances per region, the placement capping no environment (`maxInstances`)"
	}
	hclMap = "{ " + strings.Join(entries, ", ") + " }"
	if len(caps) == 1 && len(uncapped) == 0 {
		return hclMap, fmt.Sprintf("from zero to at most %s per region in every environment, the placement's cap (`maxInstances`)", instances(caps[0].n))
	}
	parts := make([]string, 0, len(caps))
	for i, c := range caps {
		count := strconv.Itoa(c.n)
		if i == 0 {
			count = instances(c.n) + " per region"
		}
		parts = append(parts, count+" in "+joinAnd(c.envs))
	}
	readme = "from zero to at most " + strings.Join(parts, ", ") + ", the placement's caps (`maxInstances`)"
	if len(uncapped) > 0 {
		readme += ", and to Cloud Run's default maximum in " + joinAnd(uncapped)
	}

	return hclMap, readme
}

// instances counts instances: 1 instance, 2 instances.
func instances(n int) string {
	if n == 1 {
		return "1 instance"
	}

	return strconv.Itoa(n) + " instances"
}

// repoFullName is owner/name from a GitHub repository URL, or the placeholder the
// pipeline reads as a refusal.
func repoFullName(repository string) string {
	const host = "https://github.com/"
	if !strings.HasPrefix(repository, host) {
		return "REPOSITORY_NOT_ON_GITHUB"
	}

	return strings.TrimSuffix(strings.TrimPrefix(repository, host), "/")
}

// sweepMinute spreads the applications' hourly sweeps over the hour: the minute is a
// hash of the application code, stable across renders.
func sweepMinute(app string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(app))

	return int(h.Sum32() % 60)
}

// substitutionKeyRE matches a key of the substitutions map in cloud-build.tf's template:
// four spaces, the underscored name, spaces, an equals sign.
var substitutionKeyRE = regexp.MustCompile(`(?m)^ {4}(_[A-Z0-9_]+) += `)

// SubstitutionNames reads the pipeline's contract off cloud-build.tf's template: the keys
// of local.substitutions, in the template's order. The resolve command tells the
// declared substitutions of a build apart by it.
func SubstitutionNames() ([]string, error) {
	src, err := templates.ReadFile(templateDir + "/cloud-build.tf.tmpl")
	if err != nil {
		return nil, errors.Wrap(err, "embed.FS.ReadFile(): cloud-build.tf.tmpl")
	}
	block := string(src)
	start := strings.Index(block, "  substitutions = {")
	if start < 0 {
		return nil, errors.New("cloud-build.tf.tmpl: no substitutions map to read the contract from")
	}
	end := strings.Index(block[start:], "\n  }\n")
	if end < 0 {
		return nil, errors.New("cloud-build.tf.tmpl: the substitutions map does not close")
	}
	var names []string
	for _, m := range substitutionKeyRE.FindAllStringSubmatch(block[start:start+end], -1) {
		names = append(names, m[1])
	}

	return names, nil
}
