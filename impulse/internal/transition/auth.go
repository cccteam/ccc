package transition

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/internal/app"
	"github.com/cccteam/ccc/impulse/internal/check"
	"github.com/cccteam/ccc/impulse/internal/names"
	"github.com/cccteam/ccc/impulse/internal/skeleton"
)

// Auth adds an auth to the application: a population that signs in one way and holds
// roles in its own permission store. An auth is a package, pkg/auth/<name>, copied from
// an auth the application already has with every name substituted, so the new one owns
// its own tables, cookie, store prefix, and roles file from the start; an auth whose
// people sign in through a directory (the OIDC flavors) is copied from the reference
// skeleton's when the application has none. The data level constructs it. Binding a site
// or an outlet to it, the users it starts with, and the tests that prove its people are
// strangers to the other auths are the agent's.
type Auth struct {
	// Name is the auth's lowercase name, the population it serves: partners, learners.
	Name string
	// Flavor is the login flavor: password, preauth, or oidc-azure.
	Flavor string
	// Authority is who owns role membership for an OIDC flavor: directory (the
	// directory's role claims are synchronized at every login, session.RoleSync) or
	// application (roles are assigned in the application, session.DisableRoleSync). It
	// is asked, never defaulted, because the wrong answer deletes hand-assigned roles at
	// the person's next login. Empty for the other flavors, which are the application's.
	Authority string
}

// The flavors add auth lays in.
const (
	FlavorPassword  = app.FlavorPassword
	FlavorPreauth   = app.FlavorPreauth
	FlavorOIDCAzure = app.FlavorOIDCAzure
)

// The authorities for an OIDC auth's role membership.
const (
	AuthorityDirectory   = app.AuthorityDirectory
	AuthorityApplication = app.AuthorityApplication
)

// The reference auth for the OIDC flavor: the outlets skeleton's members auth, an Azure
// OIDC auth with the application as its authority, copied when the application has no
// OIDC auth of its own to copy.
const (
	oidcReferenceAuth = "members"
	oidcReferenceDir  = "pkg/auth/" + oidcReferenceAuth
	// referenceMigrationsDir is where the reference skeleton keeps its schema.
	referenceMigrationsDir = "schema/migrations"
)

var authNameRE = regexp.MustCompile(`^[a-z][a-z0-9]*$`)

// Command is the impulse command line for the transition.
func (au Auth) Command() string {
	cmd := fmt.Sprintf("impulse add auth %s --%s", au.Name, au.Flavor)
	if au.Authority != "" {
		cmd += " --authority " + au.Authority
	}

	return cmd
}

// Pascal is the name's PascalCase form: the table prefix.
func (au Auth) Pascal() string { return names.Pascal(au.Name) }

// oidc reports whether the flavor signs in through a directory.
func (au Auth) oidc() bool { return au.Flavor == FlavorOIDCAzure }

// authSource is the auth package the new one is copied from.
type authSource struct {
	// Name and Pascal are the source auth's name and table prefix.
	Name, Pascal string
	// Dir is the package's directory, relative to FS's root when FS is set, else to the
	// application root.
	Dir string
	// Flavor is the source's login flavor.
	Flavor string
	// FS is the embedded skeleton the source is read from; nil reads the application.
	FS fs.FS
	// MigrationsDir is where the source's table migrations are, relative like Dir.
	MigrationsDir string
	// AuthDir is the application's auth directory, where the new package goes.
	AuthDir string
	// Sibling is the auth the application already constructs, which the new
	// construction is written beside; the source itself when it is in the application.
	Sibling string
}

// Validate checks the transition against the application before anything is changed.
func (au Auth) Validate(a *app.App) error {
	if !authNameRE.MatchString(au.Name) {
		return errors.Newf("auth name %q: name the population in lowercase letters and digits, such as partners", au.Name)
	}
	switch au.Flavor {
	case FlavorPassword, FlavorPreauth:
		if au.Authority == AuthorityDirectory {
			return errors.Newf("flavor %q: only an auth that signs in through a directory can hand it role membership; a %s auth's roles are the application's", au.Flavor, au.Flavor)
		}
	case FlavorOIDCAzure:
		if au.Authority != AuthorityDirectory && au.Authority != AuthorityApplication {
			return errors.Newf("an OIDC auth needs --authority: directory (the directory's role claims are the authority: every login reconciles the person's roles to them and removes what they do not name) or application (roles are assigned in the application: the bootstrap now, an administration surface later). It is asked because the wrong answer deletes hand-assigned roles at the next login")
		}
	case app.FlavorOIDCGoogle:
		return errors.Newf("flavor %q: the Google flavor follows the Azure one", au.Flavor)
	default:
		return errors.Newf("flavor %q: add auth lays in password, preauth, or oidc-azure", au.Flavor)
	}
	if _, err := au.source(a); err != nil {
		return err
	}

	return nil
}

// source picks the auth to copy: one of the same flavor when the application has it,
// else the reference skeleton's for an OIDC flavor, else the first auth package found.
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
		sources = append(sources, authSource{
			Name: name, Pascal: strings.ToUpper(name[:1]) + name[1:], Dir: dir, Flavor: existing.Flavor,
			MigrationsDir: appMigrationsDir(a), AuthDir: path.Dir(dir), Sibling: name,
		})
	}
	if len(sources) == 0 {
		return nil, errors.New("no auth package to copy: an auth is pkg/auth/<name> constructing its session manager and store, and the application has none (impulse new lays the first one in)")
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].Name < sources[j].Name })
	for i := range sources {
		if sources[i].Flavor == au.Flavor {
			return &sources[i], nil
		}
	}
	if au.oidc() {
		reference, err := skeleton.FS(ReferenceCandidate)
		if err != nil {
			return nil, err
		}

		return &authSource{
			Name: oidcReferenceAuth, Pascal: strings.ToUpper(oidcReferenceAuth[:1]) + oidcReferenceAuth[1:], Dir: oidcReferenceDir, Flavor: FlavorOIDCAzure,
			FS: reference, MigrationsDir: referenceMigrationsDir, AuthDir: sources[0].AuthDir, Sibling: sources[0].Name,
		}, nil
	}

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

// readDir lists a source directory, in the embedded skeleton or the application.
func (src *authSource) readDir(a *app.App, dir string) ([]fs.DirEntry, error) {
	if src.FS != nil {
		entries, err := fs.ReadDir(src.FS, dir)
		if err != nil {
			return nil, errors.Wrap(err, "fs.ReadDir()")
		}

		return entries, nil
	}
	entries, err := os.ReadDir(a.Abs(dir))
	if err != nil {
		return nil, errors.Wrap(err, "os.ReadDir()")
	}

	return entries, nil
}

// readFile reads a source file, in the embedded skeleton or the application.
func (src *authSource) readFile(a *app.App, rel string) ([]byte, error) {
	if src.FS != nil {
		data, err := fs.ReadFile(src.FS, rel)
		if err != nil {
			return nil, errors.Wrap(err, "fs.ReadFile()")
		}

		return data, nil
	}
	data, err := os.ReadFile(a.Abs(rel))
	if err != nil {
		return nil, errors.Wrap(err, "os.ReadFile()")
	}

	return data, nil
}

// Apply makes the deterministic half: the package, its migrations, its roles file, and
// its construction on the data level.
func (au Auth) Apply(ctx context.Context, a *app.App, exec check.Execer) (*Change, error) {
	if err := au.Validate(a); err != nil {
		return nil, err
	}
	src, err := au.source(a)
	if err != nil {
		return nil, err
	}
	ch := &Change{Command: au.Command()}
	if err := au.copyPackage(a, src, ch); err != nil {
		return nil, err
	}
	if err := au.copyMigrations(a, src, ch); err != nil {
		return nil, err
	}
	if err := au.writeRoles(a, src, ch); err != nil {
		return nil, err
	}
	if err := au.editConfig(a, src, ch); err != nil {
		return nil, err
	}
	if au.oidc() {
		if err := au.tagProcfile(a, ch); err != nil {
			return nil, err
		}
	}
	// Nothing here changes what the generator reads, but a regeneration proves the
	// tree still generates.
	if out, err := exec.Run(ctx, a.Root, nil, "go", "generate", "./..."); err != nil {
		ch.skipf("go generate ./... failed; fix the cause and run it:\n%s", strings.TrimSpace(string(out)))
	} else {
		ch.didf("ran go generate ./...")
	}

	return ch, nil
}

// copyPackage copies the source auth package with the names substituted and, when the
// flavor differs, the constructor swapped; for an OIDC auth, the role-membership authority
// is set to the one asked for.
func (au Auth) copyPackage(a *app.App, src *authSource, ch *Change) error {
	dst := path.Join(src.AuthDir, au.Name)
	if _, err := os.Stat(a.Abs(dst)); err == nil {
		return errors.Newf("%s already exists", dst)
	}
	entries, err := src.readDir(a, src.Dir)
	if err != nil {
		return err
	}
	authoritySet := false
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := src.readFile(a, path.Join(src.Dir, name))
		if err != nil {
			return err
		}
		text := au.rename(string(data), src)
		if src.Flavor != au.Flavor {
			text = swapFlavor(text, src.Flavor, au.Flavor)
		}
		if au.oidc() {
			if swapped, ok := swapAuthority(text, au.Authority); ok {
				text, authoritySet = swapped, true
			}
		}
		out := strings.Replace(name, src.Name, au.Name, 1)
		if err := writeNew(a, path.Join(dst, out), text); err != nil {
			return err
		}
	}
	switch {
	case au.oidc():
		from := "the " + src.Name + " auth"
		if src.FS != nil {
			from = "the reference skeleton's " + src.Name + " auth"
		}
		ch.didf("%s: the %s auth package, a copy of %s (Azure OpenID Connect) with its names substituted, role membership the %s's (%s) (tables %sSessions and %sOIDCUsers, cookie %s, store prefix %s)", dst, au.Name, from, au.Authority, roleSyncSlot(au.Authority), au.Pascal(), au.Pascal(), au.Name, au.Pascal())
		if !authoritySet {
			ch.skipf("%s: the role-synchronization slot was not found where the reference keeps it, so the authority may not be %s; set the constructor's slot to %s", dst, au.Authority, roleSyncSlot(au.Authority))
		}
	case src.Flavor == au.Flavor:
		ch.didf("%s: the %s auth package, a copy of %s with its names substituted (tables %sSessions and %sSessionUsers, cookie %s, store prefix %s)", dst, au.Name, src.Name, au.Pascal(), au.Pascal(), au.Name, au.Pascal())
	default:
		ch.didf("%s: the %s auth package, a copy of %s with its names substituted and the constructor swapped from %s to %s (tables %sSessions, cookie %s, store prefix %s); read it over, since the swap is textual", dst, au.Name, src.Name, src.Flavor, au.Flavor, au.Pascal(), au.Name, au.Pascal())
	}

	return nil
}

// roleSyncSlot is the session library's role-synchronization slot for an authority.
func roleSyncSlot(authority string) string {
	if authority == AuthorityDirectory {
		return "session.RoleSync"
	}

	return "session.DisableRoleSync"
}

// The role-synchronization slot and its explanation as the reference auth package writes
// them, one form per authority. swapAuthority rewrites one into the other; the package
// documentation's paragraph on the authority goes with it.
const (
	applicationSlot = `		// The application is the authority for role membership: a login neither reads
		// nor changes the person's roles.
		session.DisableRoleSync(),
`
	directorySlot = `		// The directory is the authority for role membership: every login reconciles the
		// person's roles to the directory's role claims, removing any it does not name,
		// and a login naming no known role is refused. Hand-assigned roles do not survive
		// it, so nothing in the application assigns roles in this store.
		session.RoleSync(accessClient.UserManager(), settings.Domains),
`
	directoryField = `	// Directory identifies the application to the directory that verifies its logins.
	Directory Directory
`
	domainsField = `	// Domains lists the tenants role synchronization sweeps besides the global scope:
	// the application's tenant roster, or nil for an application without tenants.
	Domains session.DomainsProvider
`
	applicationDoc = `// The directory proves who someone is; the application decides what they may do. Role
// membership is the application's (session.DisableRoleSync): the bootstrap assigns the
// development identities their roles, and a deployed application assigns them through its
// administration surface. The directory-run alternative, session.RoleSync, makes the
// directory's role claims the authority and removes every hand-assigned role at the next
// login, so one auth is never both.
`
	directoryDoc = `// The directory proves who someone is and says what they may do. Role membership is the
// directory's (session.RoleSync): every login reconciles the person's roles to the
// directory's role claims and removes any it does not name, so nothing in the application
// assigns roles in this store and the bootstrap seeds none. The application-run
// alternative, session.DisableRoleSync, leaves role membership to the application, so one
// auth is never both.
`
)

// swapAuthority sets an OIDC auth package's role-membership authority: the constructor's
// role-synchronization slot, the Settings field the directory-run form needs, and the
// package documentation. It reports whether the slot was found in either form.
func swapAuthority(text, authority string) (string, bool) {
	switch {
	case authority == AuthorityDirectory && strings.Contains(text, applicationSlot):
		text = strings.Replace(text, applicationSlot, directorySlot, 1)
		text = strings.Replace(text, directoryField, directoryField+domainsField, 1)
		text = strings.Replace(text, applicationDoc, directoryDoc, 1)

		return text, true
	case authority == AuthorityApplication && strings.Contains(text, directorySlot):
		text = strings.Replace(text, directorySlot, applicationSlot, 1)
		text = strings.Replace(text, domainsField, "", 1)
		text = strings.Replace(text, directoryDoc, applicationDoc, 1)

		return text, true
	case authority == AuthorityDirectory && strings.Contains(text, directorySlot),
		authority == AuthorityApplication && strings.Contains(text, applicationSlot):
		return text, true
	default:
		return text, false
	}
}

// rename substitutes the source auth's name for the new one, in both cases, where the
// name stands on its own or starts an identifier and not inside an English word that
// happens to begin with it (names.Rename).
func (au Auth) rename(text string, src *authSource) string {
	return names.Rename(text, src.Name, au.Name)
}

// swapFlavor rewrites a password auth package into a preauth one or back: the session
// constructor, the storage constructor, the type, and the user table, which preauth
// has none of.
func swapFlavor(text, from, to string) string {
	switch {
	case from == FlavorPassword && to == FlavorPreauth:
		text = strings.ReplaceAll(text, "session.NewPasswordAuth[session.NoCustomData, session.NoCustomData]", "session.NewPreauth[session.NoCustomData]")
		text = strings.ReplaceAll(text, "*session.PasswordAuth[session.NoCustomData, session.NoCustomData]", "*session.Preauth[session.NoCustomData]")
		text = strings.ReplaceAll(text, "sessionstorage.NewSpannerPasswordAuth(", "sessionstorage.NewSpannerPreauth(")
		text = strings.ReplaceAll(text, "sessionstorage.NewPostgresPassword(", "sessionstorage.NewPostgresPreauth(")
		text = regexp.MustCompile(`(?m)^\s*session\.WithUserTableName\([^)]*\),\n`).ReplaceAllString(text, "")
		text = regexp.MustCompile(`(?m)^\s*usersTable\s*=.*\n`).ReplaceAllString(text, "")
		text = strings.ReplaceAll(text, `"session.NewPasswordAuth()"`, `"session.NewPreauth()"`)
		text = strings.ReplaceAll(text, "with a password", "by the application's own proof of identity (preauth)")
	case from == FlavorPreauth && to == FlavorPassword:
		text = strings.ReplaceAll(text, "session.NewPreauth[session.NoCustomData]", "session.NewPasswordAuth[session.NoCustomData, session.NoCustomData]")
		text = strings.ReplaceAll(text, "*session.Preauth[session.NoCustomData]", "*session.PasswordAuth[session.NoCustomData, session.NoCustomData]")
		text = strings.ReplaceAll(text, "sessionstorage.NewSpannerPreauth(", "sessionstorage.NewSpannerPasswordAuth(")
		text = strings.ReplaceAll(text, "sessionstorage.NewPostgresPreauth(", "sessionstorage.NewPostgresPassword(")
		text = strings.ReplaceAll(text, `"session.NewPreauth()"`, `"session.NewPasswordAuth()"`)
	}

	return text
}

// copyMigrations copies the migrations creating the source auth's tables, renamed, as
// the next migrations. A preauth auth gets no users table.
func (au Auth) copyMigrations(a *app.App, src *authSource, ch *Change) error {
	dir := appMigrationsDir(a)
	if dir == "" {
		ch.skipf("no file:// migration source names the schema, so the %s auth's tables were not written; copy the %s auth's session, user, and access table migrations under the %s prefix", au.Name, src.Name, au.Pascal())

		return nil
	}
	entries, err := src.readDir(a, src.MigrationsDir)
	if err != nil {
		return err
	}
	next, err := nextMigration(a.Abs(dir))
	if err != nil {
		return err
	}
	var copied []string
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		data, err := src.readFile(a, path.Join(src.MigrationsDir, name))
		if err != nil {
			return err
		}
		if !createsPrefixed(data, src.Pascal) {
			continue
		}
		if au.Flavor == FlavorPreauth && strings.Contains(string(data), src.Pascal+"SessionUsers") {
			continue
		}
		_, stem, _ := strings.Cut(strings.TrimSuffix(name, ".up.sql"), "_")
		base := fmt.Sprintf("%06d_%s", next, strings.Replace(stem, src.Pascal, au.Pascal(), 1))
		next++
		if err := writeNew(a, path.Join(dir, base+".up.sql"), au.rename(string(data), src)); err != nil {
			return err
		}
		down, err := src.readFile(a, path.Join(src.MigrationsDir, strings.TrimSuffix(name, ".up.sql")+".down.sql"))
		if err == nil {
			if err := writeNew(a, path.Join(dir, base+".down.sql"), au.rename(string(down), src)); err != nil {
				return err
			}
		}
		copied = append(copied, base)
	}
	if len(copied) == 0 {
		ch.skipf("%s: no migration creates a table prefixed %s, so the %s auth's tables were not written; add its session, user, and access tables under the %s prefix", dir, src.Pascal, au.Name, au.Pascal())

		return nil
	}
	ch.didf("%s: %s, the %s auth's tables copied from the %s auth's under the %s prefix", dir, strings.Join(copied, ", "), au.Name, src.Name, au.Pascal())

	return nil
}

// fileScheme prefixes a migration source the transition can read.
const fileScheme = "file://"

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

// writeRoles writes the auth's roles file, empty, beside the source auth's.
func (au Auth) writeRoles(a *app.App, src *authSource, ch *Change) error {
	rolesDir := "schema/roles"
	if entries, err := fs.Glob(os.DirFS(a.Root), "schema/roles/"+src.Name+".json"); err == nil && len(entries) > 0 {
		rolesDir = path.Dir(entries[0])
	}
	rel := path.Join(rolesDir, au.Name+".json")
	if _, err := os.Stat(a.Abs(rel)); err == nil {
		ch.skipf("%s already exists and was left alone", rel)

		return nil
	}
	if err := writeNew(a, rel, "{\n  \"roles\": {\n    \"global\": [],\n    \"domain\": []\n  }\n}\n"); err != nil {
		return err
	}
	ch.didf("%s: the %s auth's role configuration, empty (the Administrator role at each scope is implicit)", rel, au.Name)

	return nil
}

// editConfig constructs the auth on the data level beside the source auth: an import, a
// field, the construction before the return, and the literal element. An accessor goes
// in a new file.
func (au Auth) editConfig(a *app.App, src *authSource, ch *Change) error {
	rel, data, mode, err := findDeclaringFile(a, "DataConfiguration")
	if err != nil {
		return err
	}
	if rel == "" {
		ch.skipf("no file declares a DataConfiguration struct, so the %s auth is not constructed anywhere; construct it where the %s auth is and hand its session manager and store to the surface that binds to it", au.Name, src.Name)

		return nil
	}
	modulePath := ""
	if a.GoMod != nil && a.GoMod.Module != nil {
		modulePath = a.GoMod.Module.Mod.Path
	}
	pkgPath := modulePath + "/" + path.Join(src.AuthDir, au.Name)
	edited, err := app.AddImport(rel, data, pkgPath)
	if err != nil {
		return err
	}
	edited, err = app.AddStructField(rel, edited, "DataConfiguration", au.Name+" *"+au.Name+".Auth")
	if err != nil {
		return err
	}
	srcVar, ok := constructionVar(string(data), src.Sibling)
	if !ok {
		ch.skipf("%s: NewDataConfiguration constructs no %s.New(...) to copy, so the %s auth is declared on DataConfiguration but not constructed; construct it beside the %s auth and set the field", rel, src.Sibling, au.Name, src.Sibling)
	} else {
		statements := strings.ReplaceAll(strings.ReplaceAll(srcVar.statement, src.Sibling+".", au.Name+"."), srcVar.name, au.Name+"Auth")
		if au.oidc() {
			statements, edited, err = au.oidcConstruction(rel, edited, statements, ch)
			if err != nil {
				return err
			}
		}
		edited, err = app.AddStatementsBeforeReturn(rel, edited, "NewDataConfiguration", "DataConfiguration", statements)
		switch {
		case errors.Is(err, app.ErrNoAnchor):
			ch.skipf("%s: NewDataConfiguration does not end in \"return &DataConfiguration{...}, nil\", so the %s auth is declared but not constructed; construct it beside the %s auth", rel, au.Name, src.Sibling)
		case err != nil:
			return err
		default:
			edited, err = app.AddLiteralElement(rel, edited, "NewDataConfiguration", "DataConfiguration", au.Name+": "+au.Name+"Auth")
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
	ch.didf("%s: the %s auth constructed on DataConfiguration beside the %s auth (field, construction, import); %s: its accessor %s()", rel, au.Name, src.Sibling, accessor, au.Pascal())
	ch.skipf("%s: Close releases the %s auth only; release the %s auth too", rel, src.Sibling, au.Name)

	return nil
}

// settingsLiteralRE matches the copied construction's Settings literal, whose fields are
// the sibling auth's (cookie key, session timeout).
var settingsLiteralRE = regexp.MustCompile(`&?(\w+)\.Settings\{([^{}]*)\}`)

// envVarRE finds the data level's environment struct: the variable envconfig fills.
var envVarRE = regexp.MustCompile(`(?m)^\s*env := &(\w+)\{\}`)

// oidcConstruction turns the copied construction into an OIDC auth's: the Settings
// literal gains the login page and the directory registration, and the environment
// struct gains the registration's four variables, prefixed with the auth's name. Without
// an environment struct to read them from, the registration is left to the agent.
func (au Auth) oidcConstruction(rel string, edited []byte, statements string, ch *Change) (extended string, file []byte, err error) {
	m := settingsLiteralRE.FindStringSubmatch(statements)
	if m == nil {
		ch.skipf("%s: the copied construction has no %s.Settings literal to extend, so the %s auth's directory registration (issuer, client, secret, callback) is not read from the environment; pass it in %s.Settings.Directory", rel, au.Name, au.Name, au.Name)

		return statements, edited, nil
	}
	fields := []string{}
	for _, f := range strings.Split(m[2], ",") {
		if f = strings.TrimSpace(f); f != "" {
			fields = append(fields, f)
		}
	}
	fields = append(fields, `LoginURL: "/login"`)

	upper := strings.ToUpper(au.Name)
	if env := envVarRE.FindSubmatch(edited); env != nil {
		structName := string(env[1])
		comment := fmt.Sprintf("// The %s auth's directory registration (pkg/auth/%s): the OpenID Connect issuer, the\n// application's client credentials, and the callback the directory returns the browser to.\n// Under the session library's skipAuth build tag only the redirect URL is read.\n", au.Name, au.Name)
		for i, v := range [][2]string{{"IssuerURL", "ISSUER_URL"}, {"ClientID", "CLIENT_ID"}, {"ClientSecret", "CLIENT_SECRET"}, {"RedirectURL", "REDIRECT_URL"}} {
			field := fmt.Sprintf("%s%s string `env:\"APP_%s_OIDC_%s\"`", au.Pascal(), v[0], upper, v[1])
			if i == 0 {
				field = comment + field
			}
			edited, err = app.AddStructField(rel, edited, structName, field)
			if err != nil {
				return "", nil, err
			}
		}
		fields = append(fields, fmt.Sprintf("Directory: %s.Directory{\n\tIssuerURL: env.%[2]sIssuerURL,\n\tClientID: env.%[2]sClientID,\n\tClientSecret: env.%[2]sClientSecret,\n\tRedirectURL: env.%[2]sRedirectURL,\n}", au.Name, au.Pascal()))
		ch.didf("%s: %s reads the %s auth's directory registration from APP_%s_OIDC_ISSUER_URL, _CLIENT_ID, _CLIENT_SECRET, and _REDIRECT_URL", rel, structName, au.Name, upper)
	} else {
		ch.skipf("%s: no environment struct (env := &T{}) to add the %s auth's directory registration to; read APP_%s_OIDC_ISSUER_URL, _CLIENT_ID, _CLIENT_SECRET, and _REDIRECT_URL and pass them in %s.Settings.Directory", rel, au.Name, upper, au.Name)
	}
	literal := "&" + au.Name + ".Settings{\n\t" + strings.Join(fields, ",\n\t") + ",\n}"

	return strings.Replace(statements, m[0], literal, 1), edited, nil
}

// tagProcfile builds the development processes with the session library's skipAuth tag,
// which simulates the directory an OIDC auth signs in through, so the application can be
// signed in to before it is registered with one. Every `go run` in the Procfile gains the
// tag, and the environment template gains the simulated directory's variables and the
// registration's, for a person to fill in.
func (au Auth) tagProcfile(a *app.App, ch *Change) error {
	data, mode, err := readFile(a, "Procfile")
	switch {
	case err != nil:
		ch.skipf("Procfile: not read (%v); build the development server with -tags skipAuth so the %s auth's directory is simulated", err, au.Name)
	case strings.Contains(string(data), "skipAuth"):
		ch.didf("Procfile: already builds with -tags skipAuth, so the %s auth's directory is simulated in development", au.Name)
	default:
		n := 0
		text := goRunRE.ReplaceAllStringFunc(string(data), func(string) string {
			n++

			return "go run -tags skipAuth "
		})
		if n == 0 {
			ch.skipf("Procfile: no `go run` to tag; build the development server with -tags skipAuth so the %s auth's directory is simulated", au.Name)

			break
		}
		if err := os.WriteFile(a.Abs("Procfile"), []byte(text), mode); err != nil {
			return errors.Wrap(err, "os.WriteFile()")
		}
		ch.didf("Procfile: %d go run command(s) build with -tags skipAuth, so the %s auth's directory is simulated in development and every %s login is APP_USERNAME", n, au.Name, au.Name)
	}

	return au.writeEnvTemplate(a, ch)
}

var goRunRE = regexp.MustCompile(`\bgo run `)

// writeEnvTemplate adds the simulated directory's variables and the auth's registration to
// the development environment template.
func (au Auth) writeEnvTemplate(a *app.App, ch *Change) error {
	if a.EnvTemplate == "" {
		ch.skipf("no environment template (.envrc.template, .env.template, .env.example) to add APP_USERNAME and the %s auth's APP_%s_OIDC_* variables to", au.Name, strings.ToUpper(au.Name))

		return nil
	}
	data, mode, err := readFile(a, a.EnvTemplate)
	if err != nil {
		return err
	}
	upper := strings.ToUpper(au.Name)
	var b strings.Builder
	fmt.Fprintf(&b, "\n# --- %s auth: the directory its people sign in through (pkg/auth/%s) ---\n", au.Name, au.Name)
	b.WriteString("# The Procfile builds with the session library's skipAuth tag, which simulates the\n")
	if !strings.Contains(string(data), "APP_USERNAME=") {
		fmt.Fprintf(&b, "# directory: every login through it is APP_USERNAME (APP_ROLES are the role claims), and\n# only the redirect URL below is read. Register the application in a directory, fill the\n# rest in, and drop the tag to use one.\nexport APP_USERNAME=%s-dev\nexport APP_ROLES=\n", au.Name)
	} else {
		b.WriteString("# directory: every login through it is APP_USERNAME (set above), and only the redirect\n# URL below is read. Register the application in a directory, fill the rest in, and drop\n# the tag to use one.\n")
	}
	fmt.Fprintf(&b, "# export APP_%[1]s_OIDC_ISSUER_URL=\n# export APP_%[1]s_OIDC_CLIENT_ID=\n# export APP_%[1]s_OIDC_CLIENT_SECRET=\n# APP_%[1]s_OIDC_REDIRECT_URL is the browser-facing callback of the surface that binds to\n# the %[2]s auth, such as http://127.0.0.1:4300/api/user/callback through the dev proxy.\nexport APP_%[1]s_OIDC_REDIRECT_URL=\n", upper, au.Name)
	text := strings.TrimRight(string(data), "\n") + "\n" + b.String()
	if err := os.WriteFile(a.Abs(a.EnvTemplate), []byte(text), mode); err != nil {
		return errors.Wrap(err, "os.WriteFile()")
	}
	ch.didf("%s: APP_USERNAME and APP_ROLES for the simulated directory, and the %s auth's APP_%s_OIDC_* registration, to fill in", a.EnvTemplate, au.Name, upper)

	return nil
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
	if au.oidc() {
		return au.oidcMeaning()
	}
	var b strings.Builder
	fmt.Fprintf(&b, "An auth is a population that signs in one way and holds roles in its own permission store, and it is a package: `pkg/auth/%s` owns the %s session manager (tables `%sSessions`%s, cookie `%s`), the %s permission store (tables prefixed `%s`), and the roles file `schema/roles/%s.json`. A person in two auths is two unrelated principals. The data level now constructs it beside the other auths.\n\n", au.Name, au.Flavor, au.Pascal(), au.userTableNote(), au.Name, au.Name, au.Pascal(), au.Name)
	b.WriteString("Left to wire:\n\n")
	items := []string{
		fmt.Sprintf("Bind a surface to it. Decide which site or outlet serves the %s population and compose its session group around the %s auth's handlers (session start, XSRF, login, and logout for a password auth; the application's own proof-of-identity handler issuing the session for preauth), separate from the other auths' groups, so a %s session opens nothing bound to another auth. If the population has no surface yet, add an outlet for it first.", au.Name, au.Name, au.Name),
		fmt.Sprintf("Provision its roles. Call the roles migration for the %s auth in the bootstrap and the deployment's migrate step with `%s.RolesPath` and the %s auth's user manager, across the tenants when the application is tenanted. Give the %s auth its development identities (a login and its roles) in the bootstrap identities, kept apart from the other auths' identities.", au.Name, au.Name, au.Name, au.Name),
		fmt.Sprintf("Release it. Close the %s auth where the data level closes the others.", au.Name),
		fmt.Sprintf("Prove the segmentation in the integration tests: a %s login is refused by every other auth's surface, another auth's session is refused by the %s surface, and a username that exists in two auths is two principals with separate roles.", au.Name, au.Name),
	}
	for i, item := range items {
		fmt.Fprintf(&b, "%d. %s\n", i+1, item)
	}
	b.WriteString("\nData consequence: none for existing people, since the new auth starts empty. If people are to move from another auth into this one, their accounts and roles are re-created here by a migration a person reviews, and the old auth keeps or deletes them by decision, never by default.\n")

	return b.String()
}

// oidcMeaning explains an auth whose people sign in through a directory.
func (au Auth) oidcMeaning() string {
	var b strings.Builder
	fmt.Fprintf(&b, "An auth is a population that signs in one way and holds roles in its own permission store, and it is a package: `pkg/auth/%s` owns the %s session manager (tables `%sSessions` and `%sOIDCUsers`, the user anchor keyed by the directory's immutable tenant and object identifiers; cookie `%s`), the %s permission store (tables prefixed `%s`), and the roles file `schema/roles/%s.json`. Its people sign in through the organization's directory over OpenID Connect (Azure): the login route sends the browser to the directory, the directory returns it to the callback, and the callback starts the session. A person in two auths is two unrelated principals. The data level now constructs it beside the other auths, reading the directory registration from the environment.\n\n", au.Name, au.Flavor, au.Pascal(), au.Pascal(), au.Name, au.Name, au.Pascal(), au.Name)
	switch au.Authority {
	case AuthorityDirectory:
		fmt.Fprintf(&b, "Role membership is the directory's (`session.RoleSync`): every login reconciles the person's roles to the directory's role claims and removes any it does not name, and a login naming no known role is refused. So nothing in the application assigns roles in the %s store, the bootstrap seeds no %s identities, and the roles file only defines the roles and their grants; the directory assigns them. In a tenanted application, pass the tenant roster as `%s.Settings.Domains` so the sweep covers every tenant scope.\n\n", au.Name, au.Name, au.Name)
	default:
		fmt.Fprintf(&b, "Role membership is the application's (`session.DisableRoleSync`): the directory proves who someone is and the application decides what they may do. A login neither reads nor changes roles, so the bootstrap assigns the development %s identities their roles in the %s store by username, with no password and no account to create, since the directory presents the name.\n\n", au.Name, au.Name)
	}
	b.WriteString("Left to wire:\n\n")
	items := []string{
		fmt.Sprintf("Bind a surface to it. Decide which site or outlet serves the %s population and compose an OIDC session group around the %s auth's handlers, as the reference's `pkg/router/router.go` does for its members auth (`oidcGroup`): session start and XSRF, `GET <prefix>/user/login` (the redirect to the directory), `GET <prefix>/user/callback` (the directory's return), `GET <prefix>/user/logout` (the directory's front-channel logout), the session and logout routes, then the API behind session validation and the XSRF guard. Give the App a method returning the %s auth's `session.OIDCAzureHandlers` for the router. Bind the group's requests to the auth (`pkg/auth.Bind`, the reference's `BindAuth` middleware) so permission checks and tenant visibility answer from the %s store, and set `LoginURL` in the data level's construction to the surface's login page. If the population has no surface yet, add an outlet for it first.", au.Name, au.Name, au.Name, au.Name),
		fmt.Sprintf("Provision its roles. Call the roles migration for the %s auth in the bootstrap and the deployment's migrate step with `%s.RolesPath` and the %s auth's user manager, across the tenants when the application is tenanted.%s", au.Name, au.Name, au.Name, au.identitiesNote()),
		fmt.Sprintf("Register it. The environment template now carries the %s auth's `APP_%s_OIDC_*` variables: set the redirect URL to the browser-facing callback of the surface it binds to (through the dev proxy in development), and fill in the issuer, client, and secret when the application is registered in a directory. Until then the Procfile builds with the session library's `skipAuth` tag, which simulates the directory: every %s login is `APP_USERNAME`.", au.Name, strings.ToUpper(au.Name), au.Name),
		"Sign in from the browser. The surface's login page becomes a button that sends the browser to `<prefix>/user/login?returnUrl=<page>`, as the reference's portal login component does; a refused login returns to the login page with the reason in `?message=`.",
		fmt.Sprintf("Release it. Close the %s auth where the data level closes the others.", au.Name),
		fmt.Sprintf("Prove it in the integration tests under `-tags skipAuth`, as the reference's `test/integration/portal_login_skipauth_test.go` does: a login helper that sets `APP_USERNAME` under a mutex and follows the login route to the callback, with a `!skipAuth` twin that skips with the reason, so `go test ./...` without the tag skips the %s login tests visibly and CI runs with it. Prove the segmentation: a %s session is refused by every other auth's surface, another auth's session by the %s surface, and a username that exists in two auths is two principals with separate roles.", au.Name, au.Name, au.Name),
	}
	for i, item := range items {
		fmt.Fprintf(&b, "%d. %s\n", i+1, item)
	}
	b.WriteString("\nData consequence: none for existing people, since the new auth starts empty. If people are to move from another auth into this one, their roles are re-created here by a migration a person reviews, keyed by the username the directory will present, and the old auth keeps or deletes them by decision, never by default.\n")

	return b.String()
}

// identitiesNote says what the bootstrap does with development identities for the
// authority.
func (au Auth) identitiesNote() string {
	if au.Authority == AuthorityDirectory {
		return " Seed no role assignments for it: the directory assigns them, and the roles file defines what each role may do."
	}

	return fmt.Sprintf(" Give the %s auth its development identities in the bootstrap identities: role assignments by username, no passwords, kept apart from the other auths' identities.", au.Name)
}

func (au Auth) userTableNote() string {
	if au.Flavor == FlavorPreauth {
		return ""
	}

	return fmt.Sprintf(" and `%sSessionUsers`", au.Pascal())
}
