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
	// download the bedrock the placement pins: the linux/amd64 binary of its release.
	BedrockURL string
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
	// SeedList is the HCL list of the environments whose migrate job applies the
	// development seed.
	SeedList string
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
	// GoogleDirectory reports a Google directory sign-in with role membership read
	// from the directory's groups: the runtime identity signs for itself as the
	// administrator (keyless domain-wide delegation), which the stack grants.
	GoogleDirectory bool
	// AuthVar is the stem of the auth's placement variables: <auth>_oidc.
	AuthVar string
	// RoutesDir is the directory of the file registering the callback, empty without one.
	RoutesDir string
	// The variables by role, for the templates that name them.
	ServiceName      *derive.Variable
	LoggingProject   *derive.Variable
	Version          *derive.Variable
	Port             *derive.Variable
	ClientID         *derive.Variable
	ClientSecret     *derive.Variable
	RedirectURL      *derive.Variable
	HostedDomain     *derive.Variable
	GroupPrefix      *derive.Variable
	AdminCredentials *derive.Variable
	AdminSubject     *derive.Variable
	// CookieKeySecret, ClientSecretSecret, AdminCredentialsSecret are the secrets by role.
	CookieKeySecret        *derive.Secret
	ClientSecretSecret     *derive.Secret
	AdminCredentialsSecret *derive.Secret
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
	// SiteImageVars names the site-level variables the image sets.
	SiteImageVars string
	// MigrateLevels spells the levels the migration constructs: "core and data".
	MigrateLevels string
	// JobsLevels spells the levels the job process constructs, JobsLevelList lists them
	// as HCL, and JobsReadsData reports whether the data level is among them; all empty
	// without a job process.
	JobsLevels    string
	JobsLevelList string
	JobsReadsData bool
	// JobsJob is the site's variable naming the job process's Cloud Run job, or nil
	// when the site declares none.
	JobsJob *derive.Variable
	// AssetsBucket is the variable naming the assets bucket, or nil when the code
	// declares none; JobsReadsAssets reports that the job process constructs its level.
	AssetsBucket    *derive.Variable
	JobsReadsAssets bool
	// TasksQueue is the variable naming the task queue, or nil when the code declares
	// none; JobsReadsTasks reports that the job process constructs its level.
	TasksQueue     *derive.Variable
	JobsReadsTasks bool
	// FirestoreDatabase is the variable naming the Firestore database, or nil when the
	// code declares none; JobsReadsFirestore reports that the job process constructs
	// its level.
	FirestoreDatabase  *derive.Variable
	JobsReadsFirestore bool
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
	// BelowProduction spells the hostname shapes below production.
	BelowProduction string
	// HostnamesProse lists the hostnames for the README, wrapped after the first.
	HostnamesProse string
	// IntegrationHost is the integration environment's canonical hostname.
	IntegrationHost string
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
	// SchemaDir is the directory the migrate command reads relative to its working
	// directory (schema: the migrations, the seed, the roles), copied whole.
	SchemaDir string
	// Workspaces are the browser workspaces, in the order the site's variables name
	// them, each with the bundles built there (one per browser app); Bundles lists every
	// bundle across them. An application without a browser has none.
	Workspaces []workspace
	Bundles    []bundle
	// VersionVar is the variable the image sets to the release, empty when the code
	// declares none.
	VersionVar string
}

// workspace is one browser workspace: its root-relative directory (web), the build
// stage that builds it, and its bundles.
type workspace struct {
	Dir     string
	Stage   string
	Bundles []bundle
}

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
			iv.Workspaces = append(iv.Workspaces, workspace{Dir: b.Workspace, Stage: b.Stage})
			at = len(iv.Workspaces) - 1
		}
		iv.Workspaces[at].Bundles = append(iv.Workspaces[at].Bundles, b)
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

// HasHook reports whether the application commits a hook script for the stage: the
// pipeline has a step for it.
func (v *view) HasHook(stage string) bool {
	return slices.Contains(v.Hooks, hook.Stage(stage))
}

// HookScript is the stage's script, as the application commits it.
func (v *view) HookScript(stage string) string {
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
	v.BedrockURL = release.URL(p.BedrockVersion, release.PipelineAsset())
	v.GcloudImage, v.OpenTofuImage, v.DockerImage = gcloudImage, openTofuImage, dockerImage
	v.Auth = &m.Auths[0]
	v.Directory = v.Auth.OIDC()
	v.GoogleDirectory = v.Auth.Flavor == googleFlavor && v.Auth.Directory[derive.RoleAdminSubject] != nil
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
	v.blocks()

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
			{derive.RoleAdminCredentials, &v.AdminCredentials},
			{derive.RoleAdminSubject, &v.AdminSubject},
		}...)
	}
	for _, r := range roles {
		*r.dst = v.byRole(r.role)
		if *r.dst == nil {
			return errors.Newf("no variable in the %s role: the stack needs one", r.role)
		}
	}
	for _, s := range v.Secrets {
		switch s.Variable.Role {
		case derive.RoleCookieKey:
			v.CookieKeySecret = ptr(s)
		case derive.RoleClientSecret:
			v.ClientSecretSecret = ptr(s)
		case derive.RoleAdminCredentials:
			v.AdminCredentialsSecret = ptr(s)
		default:
		}
	}
	if v.CookieKeySecret == nil {
		return errors.New("the stack needs the cookie key among the secrets")
	}
	if v.Directory && (v.ClientSecretSecret == nil || v.AdminCredentialsSecret == nil) {
		return errors.New("the stack needs the client secret and the admin credentials among the secrets of a directory sign-in")
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
		{stem + v.Migrate.Name, v.Migrate.Dir + " (Cloud Run job)"},
	}
	if v.Jobs != nil {
		v.JobsLevels = joinAnd(v.Jobs.Levels)
		v.JobsLevelList = `["` + strings.Join(v.Jobs.Levels, `", "`) + `"]`
		v.JobsReadsData = v.Jobs.Reads(v.DataLevel.Name)
		v.JobsJob = v.byRole(derive.RoleJobsJob)
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
// assets bucket, the task queue) and whether the job process constructs their levels.
func (v *view) declarations() {
	v.AssetsBucket = v.byRole(derive.RoleAssetsBucket)
	if v.AssetsBucket != nil && v.Jobs != nil {
		v.JobsReadsAssets = v.Jobs.Reads(v.AssetsBucket.Level)
	}
	v.TasksQueue = v.byRole(derive.RoleTasksQueue)
	if v.TasksQueue != nil && v.Jobs != nil {
		v.JobsReadsTasks = v.Jobs.Reads(v.TasksQueue.Level)
	}
	v.FirestoreDatabase = v.byRole(derive.RoleFirestoreDatabase)
	if v.FirestoreDatabase != nil && v.Jobs != nil {
		v.JobsReadsFirestore = v.Jobs.Reads(v.FirestoreDatabase.Level)
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
	case derive.RoleAdminCredentials:
		return "Service-account key (JSON) with domain-wide delegation for the Admin SDK groups scope, through which the " + v.Auth.Name + " auth reads role groups."
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
	v.Image = newImageView(v.Model, v.SiteLevel.Name)
	v.ApprovalsProse = joinAnd(v.P.ApprovalEnvironments())
	if v.ApprovalsProse == "" {
		v.ApprovalsProse = "no environment"
	}
	previous := make([]string, 0, len(envs))
	for _, env := range envs {
		previous = append(previous, env+" = "+strconv.Quote(v.P.Previous(env)))
	}
	v.PreviousEnvMap = "{ " + strings.Join(previous, ", ") + " }"
}

// googleFlavor is the login flavor of a Google OpenID Connect auth, as impulse's reader
// reports it.
const googleFlavor = "oidc-google"

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
