package transition

import (
	"context"
	"fmt"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/app"
	"github.com/cccteam/ccc/impulse/internal/authpkg"
	"github.com/cccteam/ccc/impulse/internal/check"
	"github.com/cccteam/ccc/impulse/internal/names"
)

// Auth adds an auth to the application: a population that holds roles in its own
// permission store and signs in through the methods registered on its Auth session. An
// auth is a package, pkg/auth/<name>, rendered from the same source as the skeleton's
// auths (authpkg), so the new one owns its own tables, cookie, store prefix and role file
// from the start, with one file per sign-in method. Its session tables are written as
// migrations under its prefix, its access tables copied from an auth the application has,
// and the data level constructs it beside that auth, reading each directory method's
// settings from the environment. The outlet the auth serves is pointed at it: its
// declaration in the generator program names the new auth with its methods, so the
// regenerated router mounts the auth's session group under the outlet's prefix. The App's
// handlers that router then requires, the users the auth starts with, and the tests that
// prove its people are strangers to the other auths are the agent's.
type Auth struct {
	// Name is the auth's lowercase name, the population it serves: partners, learners.
	Name string
	// Methods are the sign-in methods the Auth starts with, at least one; the password
	// method when none is given. A directory method here takes one organization, with the
	// authority and the account rule add auth takes once for all of them; impulse add
	// method composes the other slots.
	Methods []authpkg.Method
	// Outlet is the outlet the auth serves, by the key OutletChoices gives it: the
	// outlet's name in the flat layout (default for the one GenerateRoutes declares), the
	// site's name for a site's default outlet in the sites layout. Required: an auth is
	// reachable only through the outlet bound to it, and the tool cannot know which
	// population an outlet is for.
	Outlet string
}

// The authorities for a directory method's role membership.
const (
	AuthorityDirectory   = app.AuthorityDirectory
	AuthorityApplication = app.AuthorityApplication
)

// authNameRE is the shape of an auth name.
var authNameRE = regexp.MustCompile(`^[a-z][a-z0-9]*$`)

// Command is the impulse command line for the transition.
func (au Auth) Command() string {
	cmd := "impulse add auth " + au.Name
	for i := range au.Methods {
		cmd += " --method " + methodFlag(&au.Methods[i])
	}
	if authority := au.authority(); authority != "" {
		cmd += " --authority " + authority
	}
	if accounts := au.accounts(); accounts != "" {
		cmd += " --accounts " + accounts
	}
	if au.Outlet != "" {
		cmd += " --outlet " + au.Outlet
	}

	return cmd
}

// methodFlag writes a method as the --method flag takes it: password, or
// <kind>:<registration>.
func methodFlag(m *authpkg.Method) string {
	if m.Kind == app.MethodPassword {
		return app.MethodPassword
	}

	return m.Kind + ":" + m.Name
}

// authority is the directory methods' authority, which add auth takes once for all of
// them, or empty without a directory method.
func (au Auth) authority() string {
	for i := range au.Methods {
		if au.Methods[i].Directory() {
			return au.Methods[i].Authority
		}
	}

	return ""
}

// accounts is the directory methods' account rule, which add auth takes once for all of
// them, or empty without a directory method or under the default rule.
func (au Auth) accounts() string {
	for i := range au.Methods {
		if au.Methods[i].Directory() {
			return au.Methods[i].Accounts
		}
	}

	return ""
}

// invited lists the directory methods admitting invited users only.
func (au Auth) invited() []string {
	var registrations []string
	for _, m := range au.methods() {
		if m.Directory() && m.Accounts == authpkg.AccountsInvited {
			registrations = append(registrations, m.Name)
		}
	}

	return registrations
}

// openSignUp lists the registrations admitting anyone the directory verifies.
func (au Auth) openSignUp() []string {
	var registrations []string
	for _, m := range au.methods() {
		if m.Directory() && m.Accounts == authpkg.AccountsOpen {
			registrations = append(registrations, m.Name)
		}
	}

	return registrations
}

// Pascal is the auth's table prefix and accessor name.
func (au Auth) Pascal() string { return names.Pascal(au.Name) }

// directory reports an auth with a method whose people sign in through a directory.
func (au Auth) directory() bool { return au.authority() != "" }

// methods returns the methods with the password method's name settled.
func (au Auth) methods() []authpkg.Method {
	methods := make([]authpkg.Method, 0, len(au.Methods))
	for _, m := range au.Methods {
		if m.Kind == app.MethodPassword {
			m.Name = app.PasswordRegistration
		}
		methods = append(methods, m)
	}
	if len(methods) == 0 {
		methods = append(methods, authpkg.Method{Kind: app.MethodPassword, Name: app.PasswordRegistration})
	}

	return methods
}

// authSource is the auth the new one is written beside: the one whose access tables are
// copied and whose construction on the data level is the model.
type authSource struct {
	// Name and Pascal are the source auth's name and table prefix.
	Name, Pascal string
	// Dir is the package's directory, root-relative.
	Dir string
	// AuthDir is the application's auth directory, where the new package goes.
	AuthDir string
}

// Validate checks the transition against the application before anything is changed.
func (au Auth) Validate(a *app.App) error {
	if !authNameRE.MatchString(au.Name) {
		return errors.Newf("auth name %q: %s", au.Name, names.Guidance)
	}
	methods := au.methods()
	seen := map[string]bool{}
	for i := range methods {
		m := &methods[i]
		if m.Directory() && m.Authority == "" {
			return errors.Newf("the %s method needs --authority: directory (the directory's roles are synchronized at every sign-in and roles it does not name are removed) or application (roles are assigned in the application)", m.Name)
		}
		if err := m.Validate(); err != nil {
			return errors.Wrapf(err, "--method %s", methodFlag(m))
		}
		if seen[m.Name] {
			return errors.Newf("--method %s: the %s registration is given twice", methodFlag(m), m.Name)
		}
		seen[m.Name] = true
	}
	if _, err := au.source(a); err != nil {
		return err
	}
	if _, err := au.target(a); err != nil {
		return err
	}

	return nil
}

// OutletChoice is one outlet of the application, by the key --outlet takes.
type OutletChoice struct {
	// Key is what --outlet takes: the outlet's name in the flat layout (default for the
	// one GenerateRoutes declares), the site's name for a site's default outlet in the
	// sites layout and <site>/<outlet> for another outlet of that site.
	Key string
	// Site is the site declaring the outlet.
	Site app.Site
	// Outlet is the declaration as the profile reads it. An API-key outlet is listed so
	// a person sees why it is not a choice: a machine surface has no sign-in.
	Outlet app.Outlet
}

// OutletChoices lists the application's outlets in declaration order, by the key
// --outlet takes, the ones an auth may serve and the API-key outlets it may not.
func OutletChoices(a *app.App) []OutletChoice {
	p := a.Profile()
	var choices []OutletChoice
	for i := range p.Sites {
		site := &p.Sites[i]
		outlets := site.AllOutlets()
		for j := range outlets {
			o := &outlets[j]
			if o.Name == "" {
				continue
			}
			key := o.Name
			if p.Layout == app.LayoutSites {
				key = site.Name
				if o.Name != app.DefaultOutletName {
					key += "/" + o.Name
				}
			}
			choices = append(choices, OutletChoice{Key: key, Site: *site, Outlet: *o})
		}
	}

	return choices
}

// sessionOutletKeys lists the keys of the outlets an auth may serve, or says there are
// none.
func sessionOutletKeys(choices []OutletChoice) string {
	var keys []string
	for i := range choices {
		if !choices[i].Outlet.APIKey {
			keys = append(keys, choices[i].Key)
		}
	}

	return listed(keys)
}

// target finds the outlet the auth serves: a session outlet of the application, by its
// key.
func (au Auth) target(a *app.App) (*OutletChoice, error) {
	choices := OutletChoices(a)
	if au.Outlet == "" {
		return nil, errors.Newf("--outlet is required: the outlet the %s auth serves, whose declaration is pointed at the auth; the outlets are %s (impulse add outlet adds one)", au.Name, sessionOutletKeys(choices))
	}
	for i := range choices {
		c := &choices[i]
		if c.Key != au.Outlet {
			continue
		}
		if c.Outlet.APIKey {
			return nil, errors.Newf("%s: the %s outlet is an API-key outlet (APIKey()), a machine surface with no sign-in; an auth serves a session outlet, and the outlets are %s", c.Outlet.Pos, au.Outlet, sessionOutletKeys(choices))
		}

		return c, nil
	}

	return nil, errors.Newf("no %s outlet for the %s auth to serve; the outlets are %s (impulse add outlet adds one)", au.Outlet, au.Name, sessionOutletKeys(choices))
}

// source finds the auth the new one is written beside: the first auth package by name.
func (au Auth) source(a *app.App) (*authSource, error) {
	var sources []authSource
	seen := map[string]bool{}
	for i := range a.Auths {
		existing := &a.Auths[i]
		name := app.AuthPackageName(existing.File)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		if name == au.Name {
			return nil, errors.Newf("%s: the %s auth already exists", path.Dir(existing.File), au.Name)
		}
		dir := path.Dir(existing.File)
		sources = append(sources, authSource{Name: name, Pascal: names.Pascal(name), Dir: dir, AuthDir: path.Dir(dir)})
	}
	if len(sources) == 0 {
		return nil, errors.New("no auth package to write beside: an auth is pkg/auth/<name> constructing its Auth session and store, and the application has none (impulse new lays the first one in)")
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].Name < sources[j].Name })

	return &sources[0], nil
}

// appMigrationsDir is the application's first file:// migration source, or empty.
func appMigrationsDir(a *app.App) string {
	p := a.Profile()
	if len(p.Sites) == 0 || len(p.Sites[0].Generator.MigrationSources) == 0 || !strings.HasPrefix(p.Sites[0].Generator.MigrationSources[0], fileScheme) {
		return ""
	}

	return strings.TrimPrefix(p.Sites[0].Generator.MigrationSources[0], fileScheme)
}

// fileScheme prefixes a migration source on disk.
const fileScheme = "file://"

// Apply makes the deterministic half: the package, its migrations, its role file, its
// construction on the data level, and the development environment for its directory
// methods.
func (au Auth) Apply(ctx context.Context, a *app.App, exec check.Execer) (*Change, error) {
	if err := au.Validate(a); err != nil {
		return nil, err
	}
	src, err := au.source(a)
	if err != nil {
		return nil, err
	}
	outlet, err := au.target(a)
	if err != nil {
		return nil, err
	}
	ch := &Change{Command: au.Command()}
	spec := &authpkg.Spec{Module: modulePath(a), AuthDir: src.AuthDir, Name: au.Name, Methods: au.methods(), TenantPolicy: tenantPolicy(a, &outlet.Site)}
	if err := au.writePackage(a, src, spec, ch); err != nil {
		return nil, err
	}
	if err := au.writeMigrations(a, src, ch); err != nil {
		return nil, err
	}
	if err := au.writeRoles(a, src, ch); err != nil {
		return nil, err
	}
	if err := au.editConfig(a, src, spec, ch); err != nil {
		return nil, err
	}
	if err := au.bindOutlet(a, outlet, spec, ch); err != nil {
		return nil, err
	}
	if err := au.rebindWeb(a, outlet, src, ch); err != nil {
		return nil, err
	}
	if au.directory() {
		if err := tagProcfile(a, au.Name, ch); err != nil {
			return nil, err
		}
		for i := range spec.Methods {
			if spec.Methods[i].Directory() {
				if err := writeMethodEnv(a, au.Name, &spec.Methods[i], outletCallback(a, &outlet.Site, &outlet.Outlet, spec.Methods[i].Name), ch); err != nil {
					return nil, err
				}
				slotHandoff(ch, au.Name, &spec.Methods[i])
			}
		}
	}
	if err := generate(ctx, a, exec, ch, "go generate ./... failed, so the outlet's routes for the auth are not generated yet; fix the cause and run it:", "ran go generate ./..."); err != nil {
		return nil, err
	}

	return ch, nil
}

// bindOutlet points the outlet's declaration at the new auth: its Auth option names the
// package and the methods the Auth starts with, in place of the auth it declared, so the
// regenerated router mounts the auth's session group under the outlet's prefix. Under a
// hand-written router the program declares no Auth to point, and mounting the group is
// the agent's.
func (au Auth) bindOutlet(a *app.App, choice *OutletChoice, spec *authpkg.Spec, ch *Change) error {
	site := &choice.Site
	file := site.Generator.File
	prefix := "/" + choice.Outlet.Prefix
	if !site.GeneratedRouter {
		ch.skipf("%s: the %s outlet is served by a hand-written router, so its declaration carries no Auth to point at the %s auth; mount the auth's session group under %s in the router", file, choice.Key, au.Name, prefix)

		return nil
	}
	if spec.Module == "" {
		ch.skipf("%s: no module path in go.mod, so the %s outlet's Auth declaration cannot name the %s auth's package; point it at the package", file, choice.Key, au.Name)

		return nil
	}
	auth := &app.OutletAuth{ImportPath: path.Join(spec.Module, spec.AuthDir, au.Name)}
	for i := range spec.Methods {
		m := &spec.Methods[i]
		auth.Methods = append(auth.Methods, m.Arg())
	}
	declaration := authDeclaration(auth)
	src, mode, err := readFile(a, file)
	if err != nil {
		return err
	}
	out, _, err := app.BindOutletAuth(file, src, choice.Outlet.Name, declaration)
	if err != nil {
		return err
	}
	if err := os.WriteFile(a.Abs(file), out, mode); err != nil {
		return errors.Wrap(err, "os.WriteFile()")
	}
	was := "which declared no auth before"
	if choice.Outlet.Auth != nil {
		was = fmt.Sprintf("in place of the %s auth's", authName(a, choice.Outlet.Auth.ImportPath))
	}
	ch.didf("%s: the %s outlet's declaration names the %s auth with its methods, %s, %s, so the regenerated router mounts the auth's session group under %s: one login route per method, the methods route its login page renders from, and the pending sign-in routes", file, choice.Key, au.Name, declaration, was, prefix)

	return nil
}

// modulePath is the application's module path, or empty.
func modulePath(a *app.App) string {
	if a.GoMod != nil && a.GoMod.Module != nil {
		return a.GoMod.Module.Mod.Path
	}

	return ""
}

// writePackage renders the auth package: the Auth file and one file per method.
// tenantPolicy is the sign-in rule of an auth serving a tenanted site: the library's
// tenant policy over the site's tenant record's declaration, read through the generated
// reader the data level hands in (the settings copied from the source auth carry it),
// admitting a person who holds no tenant where the record is created by its own people.
// Nil for a site without tenants.
func tenantPolicy(a *app.App, site *app.Site) *authpkg.TenantPolicy {
	if !site.Tenanted() {
		return nil
	}
	record := site.TenantRecord.Name

	return &authpkg.TenantPolicy{
		Record:             record,
		Reader:             packageNameOf(a, site.Generator.HandlersDir()) + ".New" + record + "DeclarationReader",
		AdmitWithoutTenant: site.TenantRecord.SelfService(),
	}
}

func (au Auth) writePackage(a *app.App, src *authSource, spec *authpkg.Spec, ch *Change) error {
	dir := path.Join(src.AuthDir, au.Name)
	if _, err := os.Stat(a.Abs(dir)); err == nil {
		return errors.Newf("%s already exists", dir)
	}
	files, err := authpkg.Files(spec)
	if err != nil {
		return err
	}
	for _, name := range authpkg.FileNames(spec) {
		if err := writeNew(a, path.Join(dir, name), string(files[name])); err != nil {
			return err
		}
	}
	var described []string
	for i := range spec.Methods {
		m := &spec.Methods[i]
		desc := m.Name
		if m.Directory() {
			desc += " (" + m.Kind + ", " + accountsSentence(m) + ", role membership the " + m.Authority + "'s)"
		}
		described = append(described, desc)
	}
	rule := ""
	if spec.TenantPolicy != nil {
		rule = fmt.Sprintf(", and the sign-in rule over the %s record's declaration (signin.TenantPolicy through the reader its settings carry, %s)", spec.TenantPolicy.Record, spec.TenantPolicy.Reader)
		if spec.TenantPolicy.AdmitWithoutTenant {
			rule += ", admitting a person who holds no tenant yet (signin.AdmitWithoutTenant), since the record is created by its own people"
		}
	}
	ch.didf("%s: the %s auth package (%s), rendered from the skeleton's auth source: the Auth session over tables prefixed %s, the %s permission store, and the methods %s, each in its own file%s", dir, au.Name, strings.Join(authpkg.FileNames(spec), ", "), au.Pascal(), au.Name, strings.Join(described, ", "), rule)
	switch authpkg.Foothold(spec) {
	case authpkg.FootholdPassword:
		ch.skipf("%s/foothold.go: the first administrator the migrate command seeds while the auth's users table holds no rows, a user named administrator with a generated password; author AdministratorRoles with the role file, declare the password in the data level (%s, secret:\"generated\") and seed it from pkg/deploy SeedAdministrators", dir, au.variable("ADMINISTRATOR_PASSWORD"))
	case authpkg.FootholdMembership:
		ch.skipf("%s/foothold.go: the first administrator the migrate command seeds while the auth's users table holds no rows, the administrator role memberships under the sign-in name the placement names; author AdministratorRoles with the role file, declare the name in the data level (%s) and seed it from pkg/deploy SeedAdministrators", dir, au.variable("ADMINISTRATOR"))
	}

	return nil
}

// variable is a setting of the auth under the convention APP_<AUTH>_<SUFFIX>.
func (au Auth) variable(suffix string) string {
	return "APP_" + strings.ToUpper(strings.ReplaceAll(au.Name, "-", "_")) + "_" + suffix
}

// writeMigrations writes the auth's tables: the Auth session tables rendered under the
// new prefix, and the access tables copied from the source auth's migration.
func (au Auth) writeMigrations(a *app.App, src *authSource, ch *Change) error {
	dir := appMigrationsDir(a)
	if dir == "" {
		ch.skipf("no file:// migration source names the schema, so the %s auth's tables were not written; write its access and session table migrations under the %s prefix", au.Name, au.Pascal())

		return nil
	}
	next, err := nextMigration(a.Abs(dir))
	if err != nil {
		return err
	}
	var written []string
	// The access tables first, copied from the source auth's migration.
	entries, err := os.ReadDir(a.Abs(dir))
	if err != nil {
		return errors.Wrap(err, "os.ReadDir()")
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		data, err := os.ReadFile(a.Abs(path.Join(dir, name)))
		if err != nil {
			return errors.Wrap(err, "os.ReadFile()")
		}
		if !createsPrefixed(data, src.Pascal) || createsSessionTable(data, src.Pascal) {
			continue
		}
		_, stem, _ := strings.Cut(strings.TrimSuffix(name, ".up.sql"), "_")
		base := fmt.Sprintf("%06d_%s", next, strings.Replace(stem, src.Pascal, au.Pascal(), 1))
		next++
		if err := writeNew(a, path.Join(dir, base+".up.sql"), names.Rename(string(data), src.Name, au.Name)); err != nil {
			return err
		}
		if down, err := os.ReadFile(a.Abs(path.Join(dir, strings.TrimSuffix(name, ".up.sql")+".down.sql"))); err == nil {
			if err := writeNew(a, path.Join(dir, base+".down.sql"), names.Rename(string(down), src.Name, au.Name)); err != nil {
				return err
			}
		}
		written = append(written, base)
	}
	for _, m := range authpkg.Migrations(au.Pascal(), au.Name) {
		base := fmt.Sprintf("%06d_%s", next, m.Stem)
		next++
		if err := writeNew(a, path.Join(dir, base+".up.sql"), m.Up); err != nil {
			return err
		}
		if err := writeNew(a, path.Join(dir, base+".down.sql"), m.Down); err != nil {
			return err
		}
		written = append(written, base)
	}
	ch.didf("%s: %s, the %s auth's tables: the access tables copied from the %s auth's under the %s prefix, and the Auth session tables (sessions, users, identities, auth events, pending sign-ins, sign-in attempts)", dir, strings.Join(written, ", "), au.Name, src.Name, au.Pascal())

	return nil
}

// createsSessionTable reports a migration creating one of the prefix's session tables.
func createsSessionTable(data []byte, prefix string) bool {
	for _, m := range createTableRE.FindAllSubmatch(data, -1) {
		if strings.HasPrefix(string(m[1]), prefix+"Session") || strings.HasPrefix(string(m[1]), prefix+"OIDC") {
			return true
		}
	}

	return false
}

// createsPrefixed reports whether a migration creates a table carrying the prefix.
func createsPrefixed(data []byte, prefix string) bool {
	for _, m := range createTableRE.FindAllSubmatch(data, -1) {
		if strings.HasPrefix(string(m[1]), prefix) {
			return true
		}
	}

	return false
}

var createTableRE = regexp.MustCompile("(?i)CREATE\\s+TABLE\\s+(?:IF\\s+NOT\\s+EXISTS\\s+)?`?([A-Za-z_][A-Za-z0-9_]*)`?")

// writeRoles writes the auth's role file, empty, in the package: the file its Roles()
// embeds.
func (au Auth) writeRoles(a *app.App, src *authSource, ch *Change) error {
	rel := path.Join(src.AuthDir, au.Name, app.RolesFileName)
	if _, err := os.Stat(a.Abs(rel)); err == nil {
		ch.skipf("%s already exists and was left alone", rel)

		return nil
	}
	if err := writeNew(a, rel, "{\n  \"roles\": {\n    \"global\": [],\n    \"domain\": []\n  }\n}\n"); err != nil {
		return err
	}
	ch.didf("%s: the %s auth's role file, empty: author its roles when its surfaces are bound", rel, au.Name)

	return nil
}

// editConfig constructs the auth on the data level beside the source auth: an import, a
// field, the construction before the return, and the literal element. An accessor goes
// in a new file. Each directory method's settings are read from the environment under
// the registration's variables.
func (au Auth) editConfig(a *app.App, src *authSource, spec *authpkg.Spec, ch *Change) error {
	rel, data, mode, err := findDeclaringFile(a, "DataConfiguration")
	if err != nil {
		return err
	}
	if rel == "" {
		ch.skipf("no file declares a DataConfiguration struct, so the %s auth is not constructed anywhere; construct it where the %s auth is and hand its session manager and store to the surface that binds to it", au.Name, src.Name)

		return nil
	}
	pkgPath := path.Join(modulePath(a), src.AuthDir, au.Name)
	edited, err := app.AddImport(rel, data, pkgPath)
	if err != nil {
		return err
	}
	edited, err = app.AddStructField(rel, edited, "DataConfiguration", au.Name+" *"+au.Name+".Auth")
	if err != nil {
		return err
	}
	srcVar, ok := constructionVar(string(data), src.Name)
	settings := ""
	if !ok {
		ch.skipf("%s: NewDataConfiguration constructs no %s.New(...) to copy, so the %s auth is declared on DataConfiguration but not constructed; construct it beside the %s auth and set the field", rel, src.Name, au.Name, src.Name)
	} else {
		statements := strings.ReplaceAll(strings.ReplaceAll(srcVar.statement, src.Name+".", au.Name+"."), srcVar.name, au.Name+"Auth")
		statements, settings, edited, err = au.copySettings(rel, edited, src, statements, spec, ch)
		if err != nil {
			return err
		}
		// An anchor miss leaves the file as edited so far: the working copy is only
		// replaced by a result the editor produced.
		constructed, err := app.AddStatementsBeforeConstruction(rel, edited, "NewDataConfiguration", "DataConfiguration", statements)
		switch {
		case errors.Is(err, app.ErrNoAnchor):
			ch.skipf("%s: NewDataConfiguration builds no &DataConfiguration{...} literal, so the %s auth is declared but not constructed; construct it beside the %s auth and set the field", rel, au.Name, src.Name)
		case err != nil:
			return err
		default:
			edited, err = app.AddLiteralElement(rel, constructed, "NewDataConfiguration", "DataConfiguration", au.Name+": "+au.Name+"Auth")
			if err != nil {
				return err
			}
		}
	}
	if err := os.WriteFile(a.Abs(rel), edited, mode); err != nil {
		return errors.Wrap(err, "os.WriteFile()")
	}
	pkg, err := app.PackageName(rel, data)
	if err != nil {
		return err
	}
	accessor := path.Join(path.Dir(rel), au.Name+".go")
	if err := writeNew(a, accessor, fmt.Sprintf(`package %[1]s

import %[2]q

// %[3]s returns the %[4]s auth: its session manager and permission store, for the
// surface that binds to it.
func (c *DataConfiguration) %[3]s() *%[4]s.Auth {
	return c.%[4]s
}
`, pkg, pkgPath, au.Pascal(), au.Name)); err != nil {
		return err
	}
	ch.didf("%s: the %s auth constructed on DataConfiguration beside the %s auth (field, construction, import)%s; %s: its accessor %s()", rel, au.Name, src.Name, settings, accessor, au.Pascal())
	ch.skipf("%s: Close releases the %s auth only; release the %s auth too", rel, src.Name, au.Name)

	return nil
}

// settingsLiteralRE matches the copied construction's Settings literal, whose fields are
// the source auth's (collection, signals, cookie key, session timeout).
var settingsLiteralRE = regexp.MustCompile(`&?(\w+)\.Settings\{([^{}]*)\}`)

// envVarRE finds the data level's environment struct: the variable envconfig fills.
var envVarRE = regexp.MustCompile(`(?m)^\s*env := &(\w+)\{\}`)

// copySettings gives the copied construction the new auth's settings. A construction
// reading the source auth's settings from its settings function on the environment
// struct (env.<src>Settings(...), the skeleton's shape) has the call renamed, and the
// function copied for the new auth after the source auth's, under the new auth's name;
// a construction carrying the Settings literal inline (an application composed before
// the function) keeps the literal in the copy. The directory methods' settings then go
// where the literal is. It returns the construction, the words the did line names the
// settings by, and the file.
func (au Auth) copySettings(rel string, edited []byte, src *authSource, statements string, spec *authpkg.Spec, ch *Change) (construction, settings string, file []byte, err error) {
	call := regexp.MustCompile(`\b(\w+)\.` + regexp.QuoteMeta(src.Name) + `Settings\(`).FindStringSubmatch(statements)
	if call == nil {
		statements, edited, err = au.methodSettings(rel, edited, statements, spec, ch)
		if err != nil {
			return "", "", nil, err
		}

		return statements, ", its settings in the construction's literal as the " + src.Name + " auth's are (an application composed before the " + src.Name + "Settings method)", edited, nil
	}
	srcMethod, method := src.Name+"Settings", au.Name+"Settings"
	statements = strings.Replace(statements, call[0], call[1]+"."+method+"(", 1)
	env := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(call[1]) + ` := &(\w+)\{\}`).FindSubmatch(edited)
	if env == nil {
		ch.skipf("%s: the construction reads the %s auth's settings from %s.%s(...), but no %s := &T{} names the struct the method is declared on, so %s was not copied; write it for the %s auth beside %s, and pass the %s auth's directory registrations in it", rel, src.Name, call[1], srcMethod, call[1], method, au.Name, srcMethod, au.Name)

		return statements, "", edited, nil
	}
	structName := string(env[1])
	edited, err = app.CopyMethod(rel, edited, structName, srcMethod, func(text string) string {
		return names.Rename(text, src.Name, au.Name)
	})
	if err != nil {
		return "", "", nil, err
	}
	fields, edited, err := au.directoryFields(rel, edited, structName, spec, ch)
	if err != nil {
		return "", "", nil, err
	}
	for _, field := range fields {
		edited, err = app.AddMethodLiteralElement(rel, edited, structName, method, au.Name+".Settings", field)
		if err != nil {
			return "", "", nil, err
		}
	}

	return statements, fmt.Sprintf(", its settings built by %s.%s, copied from %s", structName, method, srcMethod), edited, nil
}

// methodSettings extends a copied construction carrying its Settings literal inline with
// the directory methods' settings: the literal gains the login page and one settings
// literal per directory method, and the environment struct gains each method's
// variables. Without an environment struct to read them from, the registrations are
// left to the agent.
func (au Auth) methodSettings(rel string, edited []byte, statements string, spec *authpkg.Spec, ch *Change) (extended string, file []byte, err error) {
	if !au.directory() {
		return statements, edited, nil
	}
	m := settingsLiteralRE.FindStringSubmatch(statements)
	if m == nil {
		ch.skipf("%s: the copied construction has no %s.Settings literal to extend, so the %s auth's directory registrations are not read from the environment; pass them in %s.Settings", rel, au.Name, au.Name, au.Name)

		return statements, edited, nil
	}
	fields := []string{}
	for _, f := range strings.Split(m[2], ",") {
		if f = strings.TrimSpace(f); f != "" {
			fields = append(fields, f)
		}
	}
	structName := ""
	if env := envVarRE.FindSubmatch(edited); env != nil {
		structName = string(env[1])
	} else {
		ch.skipf("%s: no environment struct (env := &T{}) to add the %s auth's registration variables to; read them and pass them in %s.Settings", rel, au.Name, au.Name)
	}
	added, edited, err := au.directoryFields(rel, edited, structName, spec, ch)
	if err != nil {
		return "", nil, err
	}
	fields = append(fields, added...)
	literal := "&" + au.Name + ".Settings{\n\t" + strings.Join(fields, ",\n\t") + ",\n}"

	return strings.Replace(statements, m[0], literal, 1), edited, nil
}

// directoryFields reads the auth's directory methods' settings from the environment:
// the environment struct gains each method's variables, and the returned elements are
// the login page and one settings literal per method, for the auth's Settings literal.
// An auth with no directory method takes none; without an environment struct
// (structName empty) the login page alone.
func (au Auth) directoryFields(rel string, edited []byte, structName string, spec *authpkg.Spec, ch *Change) (fields []string, file []byte, err error) {
	if !au.directory() {
		return nil, edited, nil
	}
	fields = []string{`LoginURL: "/login"`}
	if structName == "" {
		return fields, edited, nil
	}
	for i := range spec.Methods {
		method := &spec.Methods[i]
		if !method.Directory() {
			continue
		}
		literal, edited2, err := addMethodEnv(rel, edited, structName, au.Name, method)
		if err != nil {
			return nil, nil, err
		}
		edited = edited2
		fields = append(fields, method.Pascal()+": "+au.Name+"."+literal)
		ch.didf("%s: %s reads the %s auth's %s registration from %s", rel, structName, au.Name, method.Name, strings.Join(methodVariables(au.Name, method), ", "))
	}

	return fields, edited, nil
}

// construction is a "<var>, err := <pkg>.New(...)" statement with its error check.
type construction struct {
	name      string
	statement string
}

// constructionVar finds the source auth's construction in the data level's source: the
// assignment calling <src>.New and the error check that follows it.
func constructionVar(text, srcName string) (construction, bool) {
	re := regexp.MustCompile(`(?m)^\t(\w+), err := ` + regexp.QuoteMeta(srcName) + `\.New\((?s:.*?)\n\t\}\n`)
	m := re.FindStringSubmatch(text)
	if m == nil {
		return construction{}, false
	}
	lines := strings.Split(strings.TrimRight(m[0], "\n"), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimPrefix(l, "\t")
	}

	return construction{name: m[1], statement: strings.Join(lines, "\n")}, true
}

// Meaning explains an auth in this framework and names the wiring left to do.
func (au Auth) Meaning() string {
	var b strings.Builder
	methods := au.methods()
	listed := make([]string, 0, len(methods))
	declarations := make([]string, 0, len(methods))
	for i := range methods {
		m := &methods[i]
		listed = append(listed, methodSentence(m))
		declarations = append(declarations, m.Declaration())
	}
	fmt.Fprintf(&b, "An auth is a population that holds roles in its own permission store and signs in through the methods registered on its Auth session, and it is a package: `pkg/auth/%[1]s` owns the %[1]s session manager (one `session.Auth` over the tables `%[2]sSessions`, `%[2]sSessionUsers`, `%[2]sSessionIdentities`, `%[2]sSessionAuthEvents`, `%[2]sSessionPendingSignIns` and `%[2]sSessionSignInAttempts`; cookie `%[1]s`), the %[1]s permission store (tables prefixed `%[2]s`), and the role file `pkg/auth/%[1]s/roles.json`, embedded in the package and handed to the permission engine as the release's default roles (`access.WithDefaultRoles`). The engine signals its policy writes to the application's other instances through the live service (`access.WithChangeSignal` over `resource.KindPolicy`, built by the package's `announcePolicy` and `watchPolicy` from the `Settings.Signals` the data level passes), which the `change-signal` check holds every engine to. Its methods, each in its own file named for the registration: %[3]s. A person in two auths is two unrelated principals. The data level now constructs it beside the other auths%[4]s.\n\n", au.Name, au.Pascal(), strings.Join(listed, "; "), au.registrationNote())
	b.WriteString("Left to wire:\n\n")
	items := []string{
		fmt.Sprintf("Serve it from the %[3]s outlet. The outlet's declaration now names the auth with its methods (`generation.Auth(\"<module>/pkg/auth/%[1]s\", %[2]s)` on its `WithRouterOutlet`, or on `GenerateRoutes` for the default outlet), so the regenerated router mounts the %[1]s auth's session group under the outlet's prefix with one login route per method (`user/<registration>/login`, the callback and front-channel logout of a directory method, the password method's `user/password/login`), the methods route the login page renders from (`user/methods`) and the pending sign-in routes, and with two auths declared binds the group's requests to the auth (`BindAuth(%[1]s.Name)`, which the App implements over `pkg/auth.Bind` as the reference's does) so permission checks and tenant visibility answer from the %[1]s store. Give the App what the generated `Handlers` now requires: for an additional outlet a `<Outlet>()` method returning the %[1]s auth's `*%[1]s.Handlers`, in place of the one returning the auth the outlet served before; the default outlet's session surface is the App's embedded `*<auth>.Handlers`. The outlet's web app names the auth's XSRF cookie, `%[1]s.XSRFCookie` (`%[1]s-xsrf`), in its HttpClient configuration (`withXsrfConfiguration`), since the browser echoes that cookie in the X-XSRF-TOKEN header, and its login page renders from the methods route. %[4]s Under a hand-written router the declaration carries no Auth to point, and the skipped list above says to mount the group in the router.", au.Name, strings.Join(declarations, ", "), au.Outlet, webPlan(au.Outlet)),
		fmt.Sprintf("Check its roles. The package hands its role file to the engine when the data level constructs it; call `deploy.CheckRoles` for the %[1]s auth in the bootstrap and the deployment's migrate step, so a deploy prints what the store holds that the release cannot use as written.%[2]s Add the %[1]s auth's row to the roles validation test in `pkg/deploy` (`access.ValidateRoles` over the collection, parsing `%[1]s.Roles()`), its expected warnings empty, so a warning the deploy would print is accepted in code or fixed in the role; `impulse check` warns while the row is missing.", au.Name, au.identitiesNote()),
	}
	if invited := au.invited(); len(invited) > 0 {
		items = append(items, invitationsItem(au.Name, invited))
	}
	if open := au.openSignUp(); len(open) > 0 {
		items = append(items, openSignUpItem(open))
	}
	if au.directory() {
		items = append(items, fmt.Sprintf("Register it. The environment template now carries each directory method's variables (`APP_%s_<REGISTRATION>_*`): set the redirect URL to the browser-facing callback under the %s outlet's prefix (through the dev proxy in development), and fill the rest in when the application is registered with its directory. Until then the Procfile builds with the session library's `skipAuth` tag, which simulates the directory: every sign-in through a registration is `APP_<REGISTRATION>_USERNAME`, or `APP_USERNAME` when that is unset.", strings.ToUpper(au.Name), au.Outlet))
	}
	items = append(items,
		fmt.Sprintf("Release it. Close the %s auth where the data level closes the others.", au.Name),
		fmt.Sprintf("Prove the segmentation in the integration tests: a %[1]s sign-in is refused by every other auth's surface, another auth's session is refused by the %[1]s surface, and a username that exists in two auths is two principals with separate roles.%[2]s", au.Name, au.skipAuthTests()),
	)
	for i, item := range items {
		fmt.Fprintf(&b, "%d. %s\n", i+1, item)
	}
	b.WriteString("\nData consequence: none for existing people, since the new auth starts empty. If people are to move from another auth into this one, their accounts and roles are re-created here by a migration a person reviews, and the old auth keeps or deletes them by decision, never by default.\n")

	return b.String()
}

// methodSentence describes one method for the brief.
func methodSentence(m *authpkg.Method) string {
	switch m.Kind {
	case app.MethodPassword:
		return "`password`, a username and password checked against the users table"
	case app.MethodAzure:
		return fmt.Sprintf("`%s`, Microsoft Entra over OpenID Connect (`azure.SignIn`), %s, role membership the %s's", m.Name, accountsSentence(m), m.Authority)
	case app.MethodGoogle:
		return fmt.Sprintf("`%s`, Google Workspace over OpenID Connect (`google.SignIn`), %s, role membership the %s's", m.Name, accountsSentence(m), m.Authority)
	default:
		return "`" + m.Name + "`"
	}
}

// registrationNote says how the data level reads a directory method's settings.
func (au Auth) registrationNote() string {
	if !au.directory() {
		return ""
	}

	return ", reading each directory method's settings from the environment under the registration's variables (`APP_<AUTH>_<REGISTRATION>_*`), the convention bedrock supplies them under per environment"
}

// identitiesNote says what the bootstrap does with development identities for the
// methods' authorities.
func (au Auth) identitiesNote() string {
	if au.authority() == AuthorityDirectory {
		return " Seed no role assignments for the directory-run method: the directory assigns them, and the role file defines what each role may do."
	}

	return fmt.Sprintf(" Give the %s auth its development identities in the bootstrap identities: a login with a password for the password method, role assignments by username under `global`, `everyDomain` and `domains` for a directory method, kept apart from the other auths' identities.", au.Name)
}

// skipAuthTests says how a directory method is proven in the tests.
func (au Auth) skipAuthTests() string {
	if !au.directory() {
		return ""
	}

	return " Prove the directory methods under `-tags skipAuth`, as the reference's `test/integration/portal_login_skipauth_test.go` does: a sign-in helper that sets the registration's simulated username under a mutex and follows the login route to the callback, with a `!skipAuth` twin that skips with the reason, so `go test ./...` without the tag skips them visibly and CI runs with it."
}
