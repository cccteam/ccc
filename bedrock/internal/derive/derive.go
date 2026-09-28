// Package derive turns what impulse's reader reports about an application, plus a
// placement, into the application's infrastructure model: the processes that run, the
// configuration levels they construct, every environment variable by level with where a
// running process gets its value, the secrets among them, the auths with their login
// callback, and the database and schema the migration owns.
//
// Everything in the model names the declaration it comes from (a struct field under the
// config package, a route, a command), so the rendered stack can say so in a comment.
package derive

import (
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/app"
)

// Model is the infrastructure model of one application in one placement.
type Model struct {
	// App is the application code: the segment every resource name carries.
	App string
	// Module is the application's module path.
	Module string
	// Repository is the application's repository URL, or empty when the module path does
	// not name a GitHub repository.
	Repository string
	// ConfigDir is the root-relative directory of the config package.
	ConfigDir string
	// ConfigPackage is the config package's name, as its importers qualify it.
	ConfigPackage string
	// EnvTemplate is the root-relative development environment template, or empty.
	EnvTemplate string
	// Levels are the configuration levels in order, lowest first.
	Levels []Level
	// Variables are the environment variables the code reads, in declaration order.
	Variables []Variable
	// Secrets are the variables that hold a credential, in declaration order.
	Secrets []Secret
	// Database is the struct that identifies the database and its three variables.
	Database Database
	// Site is the served site's process.
	Site Process
	// Migrate is the migration process, or nil when the application has no migrate
	// command.
	Migrate *Process
	// Jobs is the job process, the application's own code as a Cloud Run job, or nil
	// when the application has no cmd/jobs.
	Jobs *Process
	// Auths are the auths, sorted by name.
	Auths []Auth
	// Schema is what the migration owns.
	Schema Schema
	// Environments are the placement's environments, with the hostnames each serves.
	Environments []Environment
	// Placement is the placement the model was derived for.
	Placement *Placement
}

// Level is one configuration level of the config package.
type Level struct {
	// Name is the level's name: core, data, or site.
	Name string
	// File is the root-relative file declaring the level's struct.
	File string
	// Struct is the level's struct.
	Struct string
	// Description says which processes read the level.
	Description string
	// Constructor is the function that loads the level, empty when none was found.
	Constructor string
}

// FileBase is the level's file without its directory.
func (l *Level) FileBase() string {
	return path.Base(l.File)
}

// Variable is one environment variable the code reads, with where a running process
// gets its value.
type Variable struct {
	// Name is the environment variable.
	Name string
	// Level is the configuration level the variable is declared at.
	Level string
	// Struct is the struct declaring the field.
	Struct string
	// Field is the field's name.
	Field string
	// File and Line locate the field.
	File string
	Line int
	// Type is the field's Go type, as written.
	Type string
	// Required reports the tag's required option.
	Required bool
	// Default is the tag's default value; HasDefault reports whether one is set.
	Default    string
	HasDefault bool
	// Doc is the field's doc comment, or empty.
	Doc string
	// Role is the variable's part in the framework, when it has one.
	Role Role
	// Image reports that the container image sets the variable (a Dockerfile ENV), so no
	// deploy step does.
	Image bool
	// Secret reports that the variable holds a credential: the field carries the
	// secret tag, secret:"true". SecretTag is that tag's value as written, or empty.
	Secret    bool
	SecretTag string
}

// Declaration is the field as a reader of the code would name it: <struct>.<field>.
func (v *Variable) Declaration() string {
	return v.Struct + "." + v.Field
}

// Supply says where a running process gets the variable's value.
func (v *Variable) Supply() Supply {
	switch {
	case v.Secret:
		return SupplySecret
	case v.Image:
		return SupplyImage
	case v.Role == RolePort:
		return SupplyPlatform
	case v.Role.Derived():
		return SupplyDerived
	case v.Role.Placed():
		return SupplyPlacement
	case v.HasDefault:
		return SupplyDefault
	default:
		return SupplyUnknown
	}
}

// Supply is where a running process gets a variable's value.
type Supply int

// The supplies.
const (
	// SupplyDerived is set by the stack from a fact it derives: the service name, the
	// logging project, the database identity, the login callback.
	SupplyDerived Supply = iota
	// SupplyPlacement is set by the stack from a placement variable.
	SupplyPlacement
	// SupplySecret is mounted from Secret Manager at a pinned version.
	SupplySecret
	// SupplyImage is baked into the image by the Dockerfile.
	SupplyImage
	// SupplyPlatform is set by Cloud Run itself.
	SupplyPlatform
	// SupplyDefault is left to the code's default.
	SupplyDefault
	// SupplyUnknown is a variable with no role, no default, and no other supplier: the
	// stack cannot say where its value comes from.
	SupplyUnknown
)

// Role is a variable's part in the framework: the well-known variables every skeleton
// declares and the directory registration of an auth.
type Role string

// The roles.
const (
	RoleNone             Role = ""
	RoleServiceName      Role = "service-name"
	RoleLoggingProject   Role = "logging-project"
	RoleVersion          Role = "version"
	RoleDatabaseProject  Role = "database-project"
	RoleDatabaseInstance Role = "database-instance"
	RoleDatabaseName     Role = "database-name"
	RolePort             Role = "port"
	RoleCookieKey        Role = "cookie-key"
	// RoleJobsJob names the job process's Cloud Run job to the site, which runs it.
	RoleJobsJob Role = "jobs-job"
	// RoleAssetsBucket names the assets bucket to the processes that construct its level.
	RoleAssetsBucket Role = "assets-bucket"
	// RoleTasksQueue names the task queue to the processes that construct its level.
	RoleTasksQueue Role = "tasks-queue"
	// RoleFirestoreDatabase names the Firestore database to the processes that construct
	// its level.
	RoleFirestoreDatabase Role = "firestore-database"
	// The directory registration of an OIDC auth, keyed as the auth package's Directory
	// struct names them.
	RoleClientID         Role = "client-id"
	RoleClientSecret     Role = "client-secret"
	RoleRedirectURL      Role = "redirect-url"
	RoleHostedDomain     Role = "hosted-domain"
	RoleGroupPrefix      Role = "group-prefix"
	RoleAdminCredentials Role = "admin-credentials"
	RoleAdminSubject     Role = "admin-subject"
)

// Derived reports a role whose value the stack derives from a fact of its own.
func (r Role) Derived() bool {
	switch r {
	case RoleServiceName, RoleLoggingProject, RoleDatabaseProject, RoleDatabaseInstance, RoleDatabaseName, RoleRedirectURL, RoleJobsJob, RoleAssetsBucket, RoleTasksQueue, RoleFirestoreDatabase:
		return true
	default:
		return false
	}
}

// Placed reports a role whose value is a placement variable.
func (r Role) Placed() bool {
	switch r {
	case RoleClientID, RoleHostedDomain, RoleGroupPrefix, RoleAdminSubject:
		return true
	default:
		return false
	}
}

// PerEnvironment reports a placed role whose value differs per environment (a
// registration in the environment's project) rather than once per organization.
func (r Role) PerEnvironment() bool {
	return r == RoleClientID || r == RoleAdminSubject
}

// ConstructionTime reports a directory role the session library reads when the auth is
// constructed, so every process that constructs the level needs it; the other directory
// roles are read when someone signs in, which only the served site does.
func (r Role) ConstructionTime() bool {
	return r == RoleHostedDomain || r == RoleGroupPrefix
}

// The levels' names, as the skeleton's config package declares them.
const (
	LevelCore = "core"
	LevelData = "data"
	LevelSite = "site"
)

// The well-known variables every skeleton declares.
const (
	varServiceName      = "APP_SERVICE_NAME"
	varLoggingProject   = "GOOGLE_CLOUD_LOGGING_PROJECT"
	varVersion          = "APP_VERSION"
	varDatabaseProject  = "GOOGLE_CLOUD_SPANNER_PROJECT"
	varDatabaseInstance = "GOOGLE_CLOUD_SPANNER_INSTANCE_ID"
	varDatabaseName     = "GOOGLE_CLOUD_SPANNER_DATABASE_NAME"
	varPort             = "PORT"
	// varJobsJob is the variable a site declares to run the job process: the stack sets
	// it to the job's resource name and grants the site's identity on the job.
	varJobsJob = "APP_JOBS_JOB"
	// varAssetsBucket is the variable an application declares to keep files in Cloud
	// Storage: the stack creates the bucket, sets the variable to its name and grants the
	// processes that construct the variable's level on it.
	varAssetsBucket = "APP_ASSETS_BUCKET"
	// varTasksQueue is the variable an application declares to enqueue Cloud Tasks: the
	// stack creates the queue, sets the variable to its resource name and grants the
	// processes that construct the variable's level to enqueue on it and to sign as
	// themselves for the call back.
	varTasksQueue = "APP_TASKS_QUEUE"
	// varFirestoreDatabase is the variable an application declares to keep documents in
	// Firestore beside its Spanner database: the stack creates the database, sets the
	// variable to its id and grants the processes that construct the variable's level
	// on that database alone.
	varFirestoreDatabase = "APP_FIRESTORE_DATABASE"
)

// wellKnown are the well-known variables by name.
var wellKnown = map[string]Role{
	varServiceName:       RoleServiceName,
	varLoggingProject:    RoleLoggingProject,
	varVersion:           RoleVersion,
	varDatabaseProject:   RoleDatabaseProject,
	varDatabaseInstance:  RoleDatabaseInstance,
	varDatabaseName:      RoleDatabaseName,
	varPort:              RolePort,
	varJobsJob:           RoleJobsJob,
	varAssetsBucket:      RoleAssetsBucket,
	varTasksQueue:        RoleTasksQueue,
	varFirestoreDatabase: RoleFirestoreDatabase,
}

// directoryRoles maps the fields of an auth's Directory struct to their roles.
var directoryRoles = map[string]Role{
	"ClientID":         RoleClientID,
	"ClientSecret":     RoleClientSecret,
	"RedirectURL":      RoleRedirectURL,
	"HostedDomain":     RoleHostedDomain,
	"GroupPrefix":      RoleGroupPrefix,
	"AdminCredentials": RoleAdminCredentials,
	"AdminSubject":     RoleAdminSubject,
}

// cookieKeyField is the data-level field that signs session cookies, by the skeleton's
// name for it.
const cookieKeyField = "CookieKey"

// secretTag is the struct tag that declares a secret: secret:"true" mounts the
// variable from Secret Manager at a pinned version, secret:"false" says a value whose
// name sounds like a credential is a plain one. The environment library reads its own
// env tag only, so the second tag costs the running program nothing.
const (
	secretTag   = "secret"
	secretTrue  = "true"
	secretFalse = "false"
)

// secretSuffixes are the name endings that sound like a credential: a key, a secret,
// credentials, a password, or a token. They decide nothing; a variable with such a name
// and no secret tag is refused, so the author says which it is.
var secretSuffixes = []string{"_KEY", "_SECRET", "_CREDENTIALS", "_PASSWORD", "_TOKEN"}

// Secret is one variable that holds a credential.
type Secret struct {
	Variable *Variable
	// Name is the secret container's name segment: the variable without its APP_ prefix,
	// in kebab case.
	Name string
}

// Database is the struct that identifies the database, with its three variables.
type Database struct {
	Struct   string
	Project  *Variable
	Instance *Variable
	Name     *Variable
}

// Process is one deployed process: the served site or the migration.
type Process struct {
	// Name is the process's short name: app for the site, migrate for the migration.
	Name string
	// Dir is the root-relative directory of the main package.
	Dir string
	// Main is how the code names the process: main.go for the site at the root, the
	// command directory otherwise.
	Main string
	// Levels are the configuration levels the process constructs, lowest first.
	Levels []string
}

// Reads reports whether the process constructs the level.
func (p *Process) Reads(level string) bool {
	return slices.Contains(p.Levels, level)
}

// Auth is one auth: its login flavor and, for a directory sign-in, its registration and
// its login callback. A password or preauth auth has neither.
type Auth struct {
	// Name is the auth's name.
	Name string
	// Dir is the auth package's root-relative directory.
	Dir string
	// Flavor is the login flavor as the reader reports it.
	Flavor string
	// Directory holds the registration's variables by role, for the roles the config
	// package feeds from the environment.
	Directory map[Role]*Variable
	// Callback is the route the browser returns to after the directory sign-in.
	Callback Route
	// GroupPrefixDefault is the group prefix the development environment template sets,
	// or empty.
	GroupPrefixDefault string
}

// OIDC reports whether the auth signs in through a directory (an OpenID Connect
// flavor): it then has a registration and a callback route, and the stack carries the
// variables and the hand steps for them.
func (a *Auth) OIDC() bool {
	return a.Flavor == app.FlavorOIDCAzure || a.Flavor == app.FlavorOIDCGoogle
}

// Variable returns the registration variable in the role, or nil.
func (a *Auth) Variable(role Role) *Variable {
	return a.Directory[role]
}

// VariablePrefix is the stem the placement variables of the auth carry: <auth>_oidc.
func (a *Auth) VariablePrefix() string {
	return a.Name + "_oidc"
}

// Route is one registered route.
type Route struct {
	Method string
	Path   string
	// File is the root-relative file registering the route.
	File string
}

// Schema is what the migration owns.
type Schema struct {
	// MigrationsDir is the root-relative directory of the schema migrations.
	MigrationsDir string
	// MigrateCall names the function the migration calls to apply them, as a reader
	// would: <package dir> <function>.
	MigrateCall string
	// GenerateDir is the root-relative directory holding the //go:generate directive
	// that runs the site generator, and GeneratePackage that file's package: where
	// bedrock's generate-time step (the migration renumber) goes, in a file that sorts
	// before the application's so it runs first. Both empty when no directive runs
	// the generator (impulse check reports that).
	GenerateDir     string
	GeneratePackage string
}

// Environment is one environment of the placement with the hostnames it serves.
type Environment struct {
	Name      string
	Hostnames []string
}

// The process names.
const (
	siteProcess    = "app"
	migrateProcess = "migrate"
	jobsProcess    = "jobs"
	// migrateDir is where the skeleton keeps the migrate command; jobsDir is where an
	// application keeps its job process, when it has one.
	migrateDir = "cmd/deployment/migrate"
	jobsDir    = "cmd/jobs"
	// appPrefix is the prefix the application's own variables carry.
	appPrefix = "APP_"
)

// BundleRE matches the default of a variable naming a built browser bundle, as the
// skeleton declares them: the browser workspace, dist, the bundle (web/dist/console).
var BundleRE = regexp.MustCompile(`^([^/]+)/dist/([^/]+)$`)

// appCodeRE is the shape of an application code: one to six lowercase letters, the
// segment every resource name carries.
var appCodeRE = regexp.MustCompile(`^[a-z]{1,6}$`)

// ApplicationCode is the application code the module path names: its last segment,
// which must be one to six lowercase letters.
func ApplicationCode(module string) (string, error) {
	code := path.Base(module)
	if !appCodeRE.MatchString(code) {
		return "", errors.Newf("application code %q (the module path's last segment) is not one to six lowercase letters", code)
	}

	return code, nil
}

// Derive reads the application's infrastructure model.
func Derive(a *app.App, p *Placement) (*Model, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	if a.GoMod == nil || a.GoMod.Module == nil {
		return nil, errors.New("the application's go.mod has no module directive")
	}
	module := a.GoMod.Module.Mod.Path
	code, err := ApplicationCode(module)
	if err != nil {
		return nil, err
	}

	m := &Model{App: code, Module: module, Repository: repositoryURL(module), EnvTemplate: a.EnvTemplate, Placement: p}
	cfg, err := readConfig(a)
	if err != nil {
		return nil, err
	}
	m.ConfigDir, m.ConfigPackage, m.Levels = cfg.dir, cfg.pkg, cfg.levels
	m.Variables = cfg.variables
	if err := m.classify(cfg); err != nil {
		return nil, err
	}
	if err := m.processes(a, cfg); err != nil {
		return nil, err
	}
	if err := m.auths(a, cfg); err != nil {
		return nil, err
	}
	if err := m.schema(a); err != nil {
		return nil, err
	}
	if err := m.generateStep(a); err != nil {
		return nil, err
	}
	m.Environments = p.environments(code)

	return m, nil
}

// classify settles each variable's role, whether the image sets it, and whether it is a
// secret, then collects the secrets and the database.
func (m *Model) classify(cfg *config) error {
	for i := range m.Variables {
		v := &m.Variables[i]
		v.Role = wellKnown[v.Name]
		if v.Role == RoleNone && v.Field == cookieKeyField {
			v.Role = RoleCookieKey
		}
		v.Image = cfg.image[v.Name]
		secret, err := secretFor(v)
		if err != nil {
			return err
		}
		v.Secret = secret
		if v.Secret {
			m.Secrets = append(m.Secrets, Secret{Variable: v, Name: secretName(v.Name)})
		}
	}
	for role, field := range cfg.directory {
		v := m.variable(field)
		if v == nil {
			return errors.Newf("the directory literal in %s assigns env.%s, which is not an env-tagged field", m.ConfigDir, field)
		}
		v.Role = role
	}

	m.Database = Database{
		Project:  m.byRole(RoleDatabaseProject),
		Instance: m.byRole(RoleDatabaseInstance),
		Name:     m.byRole(RoleDatabaseName),
	}
	if m.Database.Project == nil || m.Database.Instance == nil || m.Database.Name == nil {
		return errors.Newf("the config package declares no database identity (%s)", strings.Join(databaseVariables(), ", "))
	}
	m.Database.Struct = m.Database.Project.Struct

	return nil
}

// databaseVariables lists the well-known database variables in order.
func databaseVariables() []string {
	return []string{varDatabaseProject, varDatabaseInstance, varDatabaseName}
}

// variable returns the variable declared by the field, or nil.
func (m *Model) variable(field string) *Variable {
	for i := range m.Variables {
		if m.Variables[i].Field == field {
			return &m.Variables[i]
		}
	}

	return nil
}

// byRole returns the variable in the role, or nil.
func (m *Model) byRole(role Role) *Variable {
	for i := range m.Variables {
		if m.Variables[i].Role == role {
			return &m.Variables[i]
		}
	}

	return nil
}

// ByLevel lists the variables declared at the level, in declaration order.
func (m *Model) ByLevel(level string) []*Variable {
	var vars []*Variable
	for i := range m.Variables {
		if m.Variables[i].Level == level {
			vars = append(vars, &m.Variables[i])
		}
	}

	return vars
}

// Level returns the named level.
func (m *Model) Level(name string) (Level, bool) {
	for _, l := range m.Levels {
		if l.Name == name {
			return l, true
		}
	}

	return Level{}, false
}

// secretFor reads the variable's secret tag: "true" makes it a secret, "false" a plain
// value. Any other value is refused, and so is a variable whose name sounds like a
// credential and carries no tag, since the name alone decides nothing.
func secretFor(v *Variable) (bool, error) {
	switch v.SecretTag {
	case secretTrue:
		return true, nil
	case secretFalse:
		return false, nil
	case "":
		if isSecret(v.Name) {
			return false, errors.Newf("%s:%d: %s sounds like a credential (its name ends in %s) and %s carries no secret tag: add secret:\"true\" to mount it from Secret Manager, or secret:\"false\" if it is a plain value", v.File, v.Line, v.Name, suffixOf(v.Name), v.Declaration())
		}

		return false, nil
	default:
		return false, errors.Newf("%s:%d: secret:%q on %s: the secret tag takes \"true\" or \"false\"", v.File, v.Line, v.SecretTag, v.Declaration())
	}
}

// suffixOf is the credential-sounding ending of the name.
func suffixOf(name string) string {
	for _, suffix := range secretSuffixes {
		if strings.HasSuffix(name, suffix) {
			return suffix
		}
	}

	return ""
}

// isSecret reports whether the variable's name sounds like a credential.
func isSecret(name string) bool {
	for _, suffix := range secretSuffixes {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}

	return false
}

// secretName is the secret container's name segment for the variable.
func secretName(variable string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimPrefix(variable, appPrefix), "_", "-"))
}

// repositoryURL is the repository a GitHub module path names, or empty.
func repositoryURL(module string) string {
	parts := strings.Split(module, "/")
	if len(parts) < 3 || parts[0] != "github.com" {
		return ""
	}

	return "https://" + strings.Join(parts[:3], "/")
}
