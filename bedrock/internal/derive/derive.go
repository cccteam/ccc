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

	"github.com/cccteam/ccc/bedrock/internal/hook"
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
	// Hooks are the stages the application commits a hook script for
	// (infrastructure/hooks/<stage>.sh), in the pipeline's order, and HookProgram its hooks
	// program (cmd/deployment/hooks) with the stages it implements, when it has one: the
	// pipeline has a step for each stage either implements.
	Hooks       []hook.Stage
	HookProgram *HookProgram
	// Auths are the auths, sorted by name.
	Auths []Auth
	// Schema is what the migration owns.
	Schema Schema
	// RouterDir is the root-relative directory of the generated router's package
	// (GenerateRoutes), where the resource generator writes the release file the deploy
	// reads for the maintenance window; empty when the site generator declares none.
	RouterDir string
	// Firestore is what the application's Firestore database carries beyond the
	// database itself (the composite indexes, the time-to-live policies and the security
	// rules the files beside the schema migrations declare), or nil when the code
	// declares no database.
	Firestore *Firestore
	// FileStores are the Cloud Storage buckets the file-store variables declare, in
	// declaration order: the default store (APP_FILE_STORE) and the named ones
	// (APP_FILE_STORE_<NAME>); none when the code declares no store.
	FileStores []FileStore
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
	// RoleFileStore hands a file store, a Cloud Storage bucket of the application's own,
	// to the processes that construct its level as a gs:// URL.
	RoleFileStore Role = "file-store"
	// RoleTasksQueue names the task queue to the processes that construct its level.
	RoleTasksQueue Role = "tasks-queue"
	// RoleFirestoreProject names the Firestore database's project to the processes that
	// construct its level: the environment project, where the database is. The Spanner
	// project is the shared instance's in an environment that shares one, so the
	// database's project is told on its own.
	RoleFirestoreProject Role = "firestore-project"
	// RoleFirestoreDatabase names the Firestore database to the processes that construct
	// its level.
	RoleFirestoreDatabase Role = "firestore-database"
	// RoleFirebaseAPIKey hands the Firebase web API key of the Firestore database to the
	// processes that construct its level: the key the browser presents to sign in with
	// the custom token the site mints.
	RoleFirebaseAPIKey Role = "firebase-api-key"
	// The directory registration of an OIDC auth, keyed as the auth package's Directory
	// struct names them.
	RoleClientID     Role = "client-id"
	RoleClientSecret Role = "client-secret"
	RoleRedirectURL  Role = "redirect-url"
	RoleHostedDomain Role = "hosted-domain"
	RoleGroupPrefix  Role = "group-prefix"
	RoleGroupLookup  Role = "group-lookup"
)

// defaultGroupLookup is how far a sign-in's groups read reaches when the development
// environment template does not say: the groups the person is a direct member of.
const defaultGroupLookup = "direct"

// Derived reports a role whose value the stack derives from a fact of its own.
func (r Role) Derived() bool {
	switch r {
	case RoleServiceName, RoleLoggingProject, RoleDatabaseProject, RoleDatabaseInstance, RoleDatabaseName, RoleRedirectURL, RoleJobsJob, RoleFileStore, RoleTasksQueue, RoleFirestoreProject, RoleFirestoreDatabase, RoleFirebaseAPIKey:
		return true
	default:
		return false
	}
}

// Placed reports a role whose value is a placement variable.
func (r Role) Placed() bool {
	switch r {
	case RoleClientID, RoleHostedDomain, RoleGroupPrefix, RoleGroupLookup:
		return true
	default:
		return false
	}
}

// PerEnvironment reports a placed role whose value differs per environment (a
// registration in the environment's project) rather than once per organization.
func (r Role) PerEnvironment() bool {
	return r == RoleClientID
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
	varServiceName    = "APP_SERVICE_NAME"
	varLoggingProject = "GOOGLE_CLOUD_LOGGING_PROJECT"
	varVersion        = "APP_VERSION"
	// varMaintenance is the variable the pipeline sets on a maintenance revision, whose
	// process then serves the maintenance page and opens no database; the stack
	// declares it empty on the service. It is not a declared variable of the
	// application: the framework's maintenance package reads it before the configuration
	// is built.
	varMaintenance      = "APP_MAINTENANCE"
	varDatabaseProject  = "GOOGLE_CLOUD_SPANNER_PROJECT"
	varDatabaseInstance = "GOOGLE_CLOUD_SPANNER_INSTANCE_ID"
	varDatabaseName     = "GOOGLE_CLOUD_SPANNER_DATABASE_NAME"
	varPort             = "PORT"
	// varJobsJob is the variable a site declares to run the job process: the stack sets
	// it to the job's resource name and grants the site's identity on the job.
	varJobsJob = "APP_JOBS_JOB"
	// varFileStore is the variable an application declares to keep files in Cloud
	// Storage, its default file store; a named store is varFileStore, an underscore and
	// the store's name in upper snake case (APP_FILE_STORE_DOCUMENTS). The stack creates
	// one bucket per such variable, sets the variable to the bucket's gs:// URL and grants
	// the processes that construct the variable's level on it.
	varFileStore = "APP_FILE_STORE"
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
	// varFirestoreProject is the variable an application with a Firestore database
	// declares for the database's project: the stack sets it to the environment project,
	// where the database is; empty, the application falls back to the Spanner project,
	// which is the shared instance's in an environment that shares one, so the stack
	// never leaves it empty.
	varFirestoreProject = "GOOGLE_CLOUD_FIRESTORE_PROJECT"
	// varFirebaseAPIKey is the variable an application with a Firestore database declares
	// to serve live pages to a browser: the stack creates a web API key restricted to the
	// APIs the browser's sign-in calls and sets the variable to it. The key is a public
	// value by design (the browser presents it), not a secret.
	varFirebaseAPIKey = "APP_FIREBASE_API_KEY"
)

// MaintenanceVariable is the variable the pipeline sets on a maintenance revision, which
// the stack declares empty on the service (see varMaintenance).
const MaintenanceVariable = varMaintenance

// wellKnown are the well-known variables by their exact name. The file-store variables
// are matched by prefix instead (fileStoreName); roleOf consults both.
var wellKnown = map[string]Role{
	varServiceName:       RoleServiceName,
	varLoggingProject:    RoleLoggingProject,
	varVersion:           RoleVersion,
	varDatabaseProject:   RoleDatabaseProject,
	varDatabaseInstance:  RoleDatabaseInstance,
	varDatabaseName:      RoleDatabaseName,
	varPort:              RolePort,
	varJobsJob:           RoleJobsJob,
	varTasksQueue:        RoleTasksQueue,
	varFirestoreDatabase: RoleFirestoreDatabase,
	varFirestoreProject:  RoleFirestoreProject,
	varFirebaseAPIKey:    RoleFirebaseAPIKey,
}

// roleOf is the table's lookup for a variable: a well-known variable by its exact name,
// then a file-store variable by its prefix, else no role.
func roleOf(variable string) Role {
	if role, ok := wellKnown[variable]; ok {
		return role
	}
	if _, ok := fileStoreName(variable); ok {
		return RoleFileStore
	}

	return RoleNone
}

// fileStoreRE matches a file-store variable: varFileStore exactly, or varFileStore, an
// underscore and a name in upper snake case (APP_FILE_STORE_DOCUMENTS,
// APP_FILE_STORE_CLIENT_FILES). A variable that merely continues the letters
// (APP_FILE_STOREROOM), one with nothing after the underscore, or one whose name is not
// upper snake case is not a store.
var fileStoreRE = regexp.MustCompile(`^` + regexp.QuoteMeta(varFileStore) + `(?:_([A-Z0-9]+(?:_[A-Z0-9]+)*))?$`)

// fileStoreName reports whether the variable declares a file store and, when it does,
// the store's name: empty for the default store, else the variable's suffix in lower
// case with hyphens for its underscores (DOCUMENTS gives documents, CLIENT_FILES gives
// client-files).
func fileStoreName(variable string) (string, bool) {
	m := fileStoreRE.FindStringSubmatch(variable)
	if m == nil {
		return "", false
	}

	return strings.ToLower(strings.ReplaceAll(m[1], "_", "-")), true
}

// fileStoreResource is the stack's resource name for the default store's bucket, and the
// stem of a named store's (files_documents); bucketResourceType is the bucket's resource
// type in the stack, which with the resource name addresses the bucket.
const (
	fileStoreResource  = "files"
	bucketResourceType = "google_storage_bucket"
)

// FileStore is one Cloud Storage bucket the application declares by a file-store
// variable: APP_FILE_STORE for its default store, APP_FILE_STORE_<NAME> for a named one.
// The stack creates the bucket, sets the variable to the bucket's gs:// URL, and grants
// the processes that construct the variable's level on its objects.
type FileStore struct {
	// Variable is the variable the store is declared by.
	Variable *Variable
	// Name is the store's name: empty for the default store, else the variable's suffix
	// in lower case with hyphens (documents, client-files).
	Name string
	// Resource is the bucket's resource name in the stack: files for the default store,
	// files_<name> with underscores for a named one (files_client_files), since a
	// resource name admits no hyphen.
	Resource string
	// Suffix is the segment the bucket's name ends in before the project number: files
	// for the default store, files-<name> for a named one (files-client-files).
	Suffix string
}

// newFileStore is the store the variable declares under the name.
func newFileStore(v *Variable, name string) FileStore {
	s := FileStore{Variable: v, Name: name, Resource: fileStoreResource, Suffix: fileStoreResource}
	if name != "" {
		s.Resource += "_" + strings.ReplaceAll(name, "-", "_")
		s.Suffix += "-" + name
	}

	return s
}

// Address is the bucket's address in the stack: google_storage_bucket.<resource>.
func (s *FileStore) Address() string {
	return bucketResourceType + "." + s.Resource
}

// Default reports the application's default store, the one APP_FILE_STORE declares.
func (s *FileStore) Default() bool {
	return s.Name == ""
}

// directoryRoles maps the fields of an auth's Directory struct to their roles.
var directoryRoles = map[string]Role{
	"ClientID":     RoleClientID,
	"ClientSecret": RoleClientSecret,
	"RedirectURL":  RoleRedirectURL,
	"HostedDomain": RoleHostedDomain,
	"GroupPrefix":  RoleGroupPrefix,
	"GroupLookup":  RoleGroupLookup,
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
	// GroupLookupDefault is how far the sign-in's groups read reaches, direct or nested:
	// what the development environment template sets, or direct.
	GroupLookupDefault string
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
	if err := m.firestore(a); err != nil {
		return nil, err
	}
	if err := m.hooks(a); err != nil {
		return nil, err
	}
	if err := m.generateStep(a); err != nil {
		return nil, err
	}
	m.router(a)
	m.Environments = p.environments(code)

	return m, nil
}

// classify settles each variable's role, whether the image sets it, and whether it is a
// secret, then collects the secrets and the database.
func (m *Model) classify(cfg *config) error {
	for i := range m.Variables {
		v := &m.Variables[i]
		v.Role = roleOf(v.Name)
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
	m.fileStores()

	return nil
}

// fileStores collects the stores the file-store variables declare, in declaration order.
func (m *Model) fileStores() {
	for i := range m.Variables {
		v := &m.Variables[i]
		if v.Role != RoleFileStore {
			continue
		}
		name, _ := fileStoreName(v.Name)
		m.FileStores = append(m.FileStores, newFileStore(v, name))
	}
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

// ByRoleAtLevel is the variable in the role at the level, or nil.
func (m *Model) ByRoleAtLevel(role Role, level string) *Variable {
	for _, v := range m.ByLevel(level) {
		if v.Role == role {
			return v
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
// credential and carries no tag, since the name alone decides nothing. A variable in a
// role the stack derives is the stack's to set, whatever its name sounds like (the
// Firebase web API key is one), and tagging it a secret is refused: the stack sets it on
// the process as a plain value and would never mount it.
func secretFor(v *Variable) (bool, error) {
	switch v.SecretTag {
	case secretTrue:
		if v.Role.Derived() {
			return false, errors.Newf("%s:%d: %s (%s) is a value the stack derives and sets on the process, not a secret: drop the secret tag", v.File, v.Line, v.Name, v.Declaration())
		}

		return true, nil
	case secretFalse:
		return false, nil
	case "":
		if isSecret(v.Name) && !v.Role.Derived() {
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
