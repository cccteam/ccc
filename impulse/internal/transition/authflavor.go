package transition

import (
	"context"
	"fmt"
	"go/format"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/internal/app"
	"github.com/cccteam/ccc/impulse/internal/check"
	"github.com/cccteam/ccc/impulse/internal/skeleton"
)

// AuthFlavor changes how an existing auth's people sign in: a password or preauth auth
// moves to a directory (the OIDC flavors), or an OIDC auth moves between directories. The
// auth keeps its name, its store, its roles file, and the surfaces bound to it; what
// changes is the session manager and the tables it reads. The package is rewritten from
// the reference OIDC auth under the auth's own name, the session tables are dropped and
// created in the new shape by one migration, the data level's construction gains the
// directory registration, and the base's handler types and login route are swapped where
// they stand in the base's shape. Everyone signs in again. Role assignments are keyed by
// username, so they are dropped unless CarryRoles says the password names were already
// the names the directory presents.
type AuthFlavor struct {
	// Name is the existing auth's name.
	Name string
	// Flavor is the flavor to move to: oidc-azure or oidc-google.
	Flavor string
	// Authority is who owns role membership afterwards: directory or application. It is
	// asked, never defaulted (see Auth.Authority).
	Authority string
	// CarryRoles keeps the auth's role assignments through the move. Off by default,
	// because a role keyed by a password username outlives the person once the directory
	// presents a different name for them.
	CarryRoles bool
}

// Command is the impulse command line for the transition.
func (f AuthFlavor) Command() string {
	cmd := fmt.Sprintf("impulse swap auth %s --%s --authority %s", f.Name, f.Flavor, f.Authority)
	if f.CarryRoles {
		cmd += " --carry-roles"
	}

	return cmd
}

// target is the auth as it will be: the same name in the new flavor.
func (f AuthFlavor) target() Auth {
	return Auth{Name: f.Name, Flavor: f.Flavor, Authority: f.Authority}
}

// current finds the auth's construction in the application.
func (f AuthFlavor) current(a *app.App) (*app.Auth, error) {
	for i := range a.Auths {
		if app.AuthPackageName(a.Auths[i].File) == f.Name {
			return &a.Auths[i], nil
		}
	}

	return nil, errors.Newf("no auth named %q: an auth is pkg/auth/<name> constructing its session manager, and the application has none by that name (impulse add auth adds one)", f.Name)
}

// Validate checks the transition against the application before anything is changed.
func (f AuthFlavor) Validate(a *app.App) error {
	if !authNameRE.MatchString(f.Name) {
		return errors.Newf("auth name %q: name the auth in lowercase letters and digits, as its package is", f.Name)
	}
	switch f.Flavor {
	case FlavorOIDCAzure, FlavorOIDCGoogle:
	case FlavorPassword, FlavorPreauth:
		return errors.Newf("flavor %q: swap auth moves an auth to a directory (oidc-azure or oidc-google); moving one back to a password or preauth reintroduces accounts the application must create and is not laid in", f.Flavor)
	default:
		return errors.Newf("flavor %q: swap auth moves an auth to oidc-azure or oidc-google", f.Flavor)
	}
	if err := validateFlavor(f.Flavor, f.Authority); err != nil {
		return err
	}
	cur, err := f.current(a)
	if err != nil {
		return err
	}
	if cur.Flavor == f.Flavor {
		return errors.Newf("%s: the %s auth already signs in through %s", path.Dir(cur.File), f.Name, f.Flavor)
	}
	if cur.OptionsForwarded {
		return errors.Newf("%s:%d: the %s auth's construction forwards its callers' options, so its tables are not visible here and cannot be moved", cur.File, cur.Line, f.Name)
	}

	return nil
}

// Apply makes the deterministic half: the package, the migration, the construction, the
// development processes, and the base's handler seams.
func (f AuthFlavor) Apply(ctx context.Context, a *app.App, exec check.Execer) (*Change, error) {
	if err := f.Validate(a); err != nil {
		return nil, err
	}
	cur, err := f.current(a)
	if err != nil {
		return nil, err
	}
	au := f.target()
	ch := &Change{Command: f.Command()}
	if err := f.rewritePackage(a, cur, ch); err != nil {
		return nil, err
	}
	if err := f.writeMigration(a, cur, ch); err != nil {
		return nil, err
	}
	if err := f.editConfig(a, ch); err != nil {
		return nil, err
	}
	if err := f.swapSeams(a, cur, ch); err != nil {
		return nil, err
	}
	if err := au.tagProcfile(a, ch); err != nil {
		return nil, err
	}
	if out, err := exec.Run(ctx, a.Root, nil, "go", "generate", "./..."); err != nil {
		ch.skipf("go generate ./... failed; fix the cause and run it:\n%s", strings.TrimSpace(string(out)))
	} else {
		ch.didf("ran go generate ./...")
	}

	return ch, nil
}

// referenceSource is the reference OIDC auth as a copy source.
func referenceSource() (*authSource, error) {
	reference, err := skeleton.FS(ReferenceCandidate)
	if err != nil {
		return nil, err
	}

	return &authSource{
		Name: oidcReferenceAuth, Pascal: strings.ToUpper(oidcReferenceAuth[:1]) + oidcReferenceAuth[1:], Dir: oidcReferenceDir, Flavor: FlavorOIDCAzure,
		FS: reference, MigrationsDir: referenceMigrationsDir,
	}, nil
}

// rewritePackage replaces the file constructing the auth with the reference OIDC package
// under the auth's name, in the flavor and with the authority asked for. Other files of
// the package stay; anything the replaced file carried beyond the base's shape is in git
// for the agent to re-apply.
func (f AuthFlavor) rewritePackage(a *app.App, cur *app.Auth, ch *Change) error {
	src, err := referenceSource()
	if err != nil {
		return err
	}
	au := f.target()
	data, err := src.readFile(a, path.Join(src.Dir, src.Name+".go"))
	if err != nil {
		return err
	}
	text := dropReferenceContext(au.rename(string(data), src))
	if au.Flavor != src.Flavor {
		text = swapFlavor(text, src.Flavor, au.Flavor)
	}
	text, authoritySet := swapAuthority(text, au.Flavor, au.Authority)
	_, mode, err := readFile(a, cur.File)
	if err != nil {
		return err
	}
	if err := os.WriteFile(a.Abs(cur.File), []byte(text), mode); err != nil {
		return errors.Wrap(err, "os.WriteFile()")
	}
	ch.didf("%s: rewritten as the %s auth in the %s flavor (%s) from the reference skeleton's %s auth, role membership the %s's (%s) (tables %sSessions and %sOIDCUsers, cookie %s, store prefix %s); what the file carried beyond the base's shape is in git to re-apply", cur.File, au.Name, au.Flavor, au.directoryLabel(), src.Name, au.Authority, roleSyncSlot(au.Flavor, au.Authority), au.Pascal(), au.Pascal(), au.Name, au.Pascal())
	if !authoritySet {
		ch.skipf("%s: the role-synchronization slot was not found where the reference keeps it; set the constructor's slot to %s", cur.File, roleSyncSlot(au.Flavor, au.Authority))
	}

	return nil
}

// dropReferenceContext removes what the reference package says about its own application
// (which surface its people are), which is not true of a copy.
func dropReferenceContext(text string) string {
	return strings.Replace(text, " In this application they are the portal's people.", "", 1)
}

// writeMigration writes one migration that drops the auth's session tables and creates
// them in the new flavor's shape, and, unless the roles are carried, drops and recreates
// the role-assignment table so no role stays keyed by a name the directory will not
// present. The down migration recreates the old tables from their own migrations.
func (f AuthFlavor) writeMigration(a *app.App, cur *app.Auth, ch *Change) error {
	dir := appMigrationsDir(a)
	if dir == "" {
		ch.skipf("no file:// migration source names the schema, so the %s auth's session tables were not moved; drop %s and recreate them in the %s shape", f.Name, strings.Join(f.oldTables(cur), ", "), f.Flavor)

		return nil
	}
	old, err := f.oldMigrations(a, dir, cur)
	if err != nil {
		return err
	}
	if len(old) == 0 {
		ch.skipf("%s: no migration creates %s, so the %s auth's session tables were not moved; drop them and create the %s shape", dir, strings.Join(f.oldTables(cur), ", "), f.Name, f.Flavor)

		return nil
	}
	src, err := referenceSource()
	if err != nil {
		return err
	}
	au := f.target()
	create, drop, err := f.referenceDDL(a, src)
	if err != nil {
		return err
	}
	var up, down strings.Builder
	fmt.Fprintf(&up, "-- The %s auth moves to %s: everyone signs in again, so its session tables are\n-- dropped and created in the new shape.\n\n", f.Name, au.directoryLabel())
	for i := len(old) - 1; i >= 0; i-- {
		up.WriteString(strings.TrimRight(old[i].down, "\n") + "\n\n")
	}
	up.WriteString(strings.TrimRight(create, "\n") + "\n")
	down.WriteString(strings.TrimRight(drop, "\n") + "\n\n")
	for _, m := range old {
		down.WriteString(strings.TrimRight(m.up, "\n") + "\n\n")
	}
	assignments := ""
	if !f.CarryRoles {
		ddl, err := f.assignmentsDDL(a, dir)
		if err != nil {
			return err
		}
		if ddl == "" {
			ch.skipf("%s: no migration creates %sUserRoles, so the %s auth's role assignments were not dropped; drop and recreate the table, or carry the roles on purpose", dir, au.Pascal(), f.Name)
		} else {
			fmt.Fprintf(&up, "\n-- Role assignments are keyed by username, and the directory presents its own names:\n-- the table is emptied by recreating it (--carry-roles keeps them).\n%s", ddl)
			assignments = fmt.Sprintf(", and %sUserRoles dropped and recreated so no role stays keyed by a password username", au.Pascal())
		}
	}
	next, err := nextMigration(a.Abs(dir))
	if err != nil {
		return err
	}
	base := fmt.Sprintf("%06d_%s%s", next, au.Pascal(), flavorPascal(au.Flavor))
	if err := writeNew(a, path.Join(dir, base+".up.sql"), up.String()); err != nil {
		return err
	}
	if err := writeNew(a, path.Join(dir, base+".down.sql"), strings.TrimRight(down.String(), "\n")+"\n"); err != nil {
		return err
	}
	names := make([]string, len(old))
	for i, m := range old {
		names[i] = m.base
	}
	ch.didf("%s: %s, the %s auth's session tables (%s) dropped and created in the %s shape, %sSessions and %sOIDCUsers%s; down recreates them from %s", dir, base, f.Name, strings.Join(f.oldTables(cur), ", "), au.Flavor, au.Pascal(), au.Pascal(), assignments, strings.Join(names, ", "))

	return nil
}

// flavorPascal is the flavor's PascalCase form for a migration name.
func flavorPascal(flavor string) string {
	if flavor == FlavorOIDCGoogle {
		return "OIDCGoogle"
	}

	return "OIDCAzure"
}

// oldTables lists the session tables the auth reads now.
func (f AuthFlavor) oldTables(cur *app.Auth) []string {
	tables := []string{cur.SessionTable}
	if cur.UserTable != "" {
		tables = append(tables, cur.UserTable)
	}
	tables = append(tables, cur.ExtraTables...)

	return tables
}

// migration is one migration pair the auth's old tables came from.
type migration struct{ base, up, down string }

// oldMigrations finds the migrations creating the auth's session tables, in order, with
// their down halves.
func (f AuthFlavor) oldMigrations(a *app.App, dir string, cur *app.Auth) ([]migration, error) {
	tables := map[string]bool{}
	for _, t := range f.oldTables(cur) {
		tables[t] = true
	}
	entries, err := os.ReadDir(a.Abs(dir))
	if err != nil {
		return nil, errors.Wrap(err, "os.ReadDir()")
	}
	var found []migration
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		up, err := os.ReadFile(a.Abs(path.Join(dir, name)))
		if err != nil {
			return nil, errors.Wrap(err, "os.ReadFile()")
		}
		creates := false
		for _, m := range createTableRE.FindAllSubmatch(up, -1) {
			if tables[string(m[1])] {
				creates = true
			}
		}
		if !creates {
			continue
		}
		base := strings.TrimSuffix(name, ".up.sql")
		down, err := os.ReadFile(a.Abs(path.Join(dir, base+".down.sql")))
		if err != nil {
			return nil, errors.Newf("%s creates a table of the %s auth but has no down migration to drop it", path.Join(dir, name), f.Name)
		}
		found = append(found, migration{base: base, up: string(up), down: string(down)})
	}
	sort.Slice(found, func(i, j int) bool { return found[i].base < found[j].base })

	return found, nil
}

// referenceDDL renders the reference's session DDL under the auth's name in the target
// flavor: the create statements and the drop statements.
func (f AuthFlavor) referenceDDL(a *app.App, src *authSource) (create, drop string, err error) {
	entries, err := src.readDir(a, src.MigrationsDir)
	if err != nil {
		return "", "", err
	}
	au := f.target()
	var ups, downs []string
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		data, err := src.readFile(a, path.Join(src.MigrationsDir, name))
		if err != nil {
			return "", "", err
		}
		if !createsPrefixed(data, src.Pascal) || createsPrefixed(data, src.Pascal+"Roles") {
			continue
		}
		down, err := src.readFile(a, path.Join(src.MigrationsDir, strings.TrimSuffix(name, ".up.sql")+".down.sql"))
		if err != nil {
			return "", "", err
		}
		ups = append(ups, strings.TrimRight(swapMigrationFlavor(au.rename(string(data), src), src.Flavor, au.Flavor), "\n"))
		downs = append([]string{strings.TrimRight(swapMigrationFlavor(au.rename(string(down), src), src.Flavor, au.Flavor), "\n")}, downs...)
	}
	if len(ups) == 0 {
		return "", "", errors.Newf("the reference skeleton's %s has no migration creating the %s auth's session tables", src.MigrationsDir, src.Name)
	}

	return strings.Join(ups, "\n\n"), strings.Join(downs, "\n\n"), nil
}

// assignmentsDDL renders a drop and recreate of the auth's role-assignment table from the
// migration that created it: its indexes dropped, the table dropped, then the table and
// its indexes created again as they were.
func (f AuthFlavor) assignmentsDDL(a *app.App, dir string) (string, error) {
	table := f.target().Pascal() + "UserRoles"
	entries, err := os.ReadDir(a.Abs(dir))
	if err != nil {
		return "", errors.Wrap(err, "os.ReadDir()")
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		data, err := os.ReadFile(a.Abs(path.Join(dir, name)))
		if err != nil {
			return "", errors.Wrap(err, "os.ReadFile()")
		}
		if !createsPrefixed(data, table) {
			continue
		}
		var create []string
		for _, stmt := range strings.Split(string(data), ";") {
			stmt = strings.TrimSpace(stmt)
			if stmt == "" {
				continue
			}
			m := createTableRE.FindStringSubmatch(stmt)
			if (len(m) > 1 && m[1] == table) || indexOnRE(table).MatchString(stmt) {
				create = append(create, stmt+";")
			}
		}
		var b strings.Builder
		for _, stmt := range create {
			if m := createIndexRE.FindStringSubmatch(stmt); m != nil {
				fmt.Fprintf(&b, "DROP INDEX %s;\n\n", m[1])
			}
		}
		fmt.Fprintf(&b, "DROP TABLE %s;\n\n", table)
		b.WriteString(strings.Join(create, "\n\n") + "\n")

		return b.String(), nil
	}

	return "", nil
}

var createIndexRE = regexp.MustCompile(`(?i)CREATE\s+(?:UNIQUE\s+)?(?:NULL_FILTERED\s+)?INDEX\s+([A-Za-z_][A-Za-z0-9_]*)`)

// indexOnRE matches an index statement on the table.
func indexOnRE(table string) *regexp.Regexp {
	return regexp.MustCompile(`(?is)CREATE\s+(?:UNIQUE\s+)?(?:NULL_FILTERED\s+)?INDEX\s+\w+\s+ON\s+` + regexp.QuoteMeta(table) + `\b`)
}

// editConfig rewrites the auth's construction on the data level in the OIDC form: the
// Settings literal gains the login page and the directory registration, and the
// environment struct gains the registration's variables.
func (f AuthFlavor) editConfig(a *app.App, ch *Change) error {
	rel, data, mode, err := findDeclaringFile(a, "DataConfiguration")
	if err != nil {
		return err
	}
	if rel == "" {
		ch.skipf("no file declares a DataConfiguration struct, so the %s auth's construction was not changed; pass the directory registration in %s.Settings.Directory where it is constructed", f.Name, f.Name)

		return nil
	}
	cur, ok := constructionVar(string(data), f.Name)
	if !ok {
		ch.skipf("%s: NewDataConfiguration constructs no %s.New(...) to rewrite; pass the directory registration in %s.Settings.Directory where it is constructed", rel, f.Name, f.Name)

		return nil
	}
	au := f.target()
	statements, edited, err := au.oidcConstruction(rel, data, cur.statement, ch)
	if err != nil {
		return err
	}
	old := indentStatement(cur.statement)
	if !strings.Contains(string(edited), old) {
		ch.skipf("%s: the %s auth's construction could not be rewritten in place; extend its Settings with LoginURL and Directory", rel, f.Name)

		return nil
	}
	edited = []byte(strings.Replace(string(edited), old, indentStatement(statements), 1))
	formatted, err := format.Source(edited)
	if err != nil {
		return errors.Wrap(err, "format.Source()")
	}
	if err := os.WriteFile(a.Abs(rel), formatted, mode); err != nil {
		return errors.Wrap(err, "os.WriteFile()")
	}
	ch.didf("%s: the %s auth's construction now passes the login page and the directory registration", rel, f.Name)

	return nil
}

// indentStatement restores the function-body indentation constructionVar strips.
func indentStatement(statement string) string {
	lines := strings.Split(statement, "\n")
	for i, l := range lines {
		if l != "" {
			lines[i] = "\t" + l
		}
	}

	return strings.Join(lines, "\n")
}

// The base's handler seams: how the App embeds an auth's session manager and how the
// router declares its handlers and mounts the login route. The swap rewrites them where
// they stand in the base's shape; anything else is the agent's.
var (
	handlersEmbed = map[string]string{
		FlavorPassword:   "session.PasswordAuthHandlers",
		FlavorPreauth:    "session.PreauthHandlers",
		FlavorOIDCAzure:  "session.OIDCAzureHandlers",
		FlavorOIDCGoogle: "session.OIDCGoogleHandlers",
	}
	sessionType = map[string]string{
		FlavorPassword:   "*session.PasswordAuth[session.NoCustomData, session.NoCustomData]",
		FlavorPreauth:    "*session.Preauth[session.NoCustomData]",
		FlavorOIDCAzure:  "*session.OIDCAzure[session.NoCustomData, session.NoCustomData]",
		FlavorOIDCGoogle: "*session.OIDCGoogle[session.NoCustomData, session.NoCustomData]",
	}
	// passwordLoginRouteRE is the base's password login route with its comment.
	passwordLoginRouteRE = regexp.MustCompile(`(?m)^([ \t]*)// Login validates the credentials and starts a session\.\n[ \t]*r\.Post\((\w+)\+"/user/login", h\.Login\(\)\)\n`)
)

// embeddedField is how the App assigns its embedded session manager: the field carries
// the type's name.
func embeddedField(flavor string) string {
	name, _, _ := strings.Cut(strings.TrimPrefix(sessionType[flavor], "*session."), "[")

	return "." + name + " = "
}

// swapSeams swaps the handler types and the login route in the base's shape. When the
// application has other auths of the old flavor, only the files referencing this auth's
// package are touched, since the shared types could be theirs.
func (f AuthFlavor) swapSeams(a *app.App, cur *app.Auth, ch *Change) error {
	sameFlavor := 0
	for i := range a.Auths {
		if a.Auths[i].Flavor == cur.Flavor && app.AuthPackageName(a.Auths[i].File) != "" {
			sameFlavor++
		}
	}
	var files []string
	if sameFlavor > 1 {
		for i := range a.AuthPackages {
			if a.AuthPackages[i].Name == f.Name {
				for _, r := range a.AuthPackages[i].Refs {
					files = append(files, r.File)
				}
			}
		}
		ch.skipf("other auths sign in with a %s too, so only the files referencing the %s auth had their handler types swapped; swap %s and %s in the router and surfaces bound to the %s auth", cur.Flavor, f.Name, handlersEmbed[cur.Flavor], sessionType[cur.Flavor], f.Name)
	} else {
		for _, rel := range a.GoFiles() {
			if !strings.HasPrefix(rel, path.Dir(cur.File)+"/") && !strings.HasSuffix(rel, "_test.go") {
				files = append(files, rel)
			}
		}
	}
	var typed, routed []string
	for _, rel := range files {
		data, mode, err := readFile(a, rel)
		if err != nil {
			return err
		}
		text := string(data)
		edited := strings.ReplaceAll(text, handlersEmbed[cur.Flavor], handlersEmbed[f.Flavor])
		edited = strings.ReplaceAll(edited, sessionType[cur.Flavor], sessionType[f.Flavor])
		edited = strings.ReplaceAll(edited, embeddedField(cur.Flavor), embeddedField(f.Flavor))
		if edited != text {
			typed = append(typed, rel)
		}
		if cur.Flavor == FlavorPassword && passwordLoginRouteRE.MatchString(edited) {
			edited = passwordLoginRouteRE.ReplaceAllString(edited, f.oidcRoutes())
			routed = append(routed, rel)
		}
		if edited == text {
			continue
		}
		if err := os.WriteFile(a.Abs(rel), []byte(edited), mode); err != nil {
			return errors.Wrap(err, "os.WriteFile()")
		}
	}
	if len(typed) > 0 {
		ch.didf("%s: %s and %s swapped for %s and %s", strings.Join(typed, ", "), handlersEmbed[cur.Flavor], sessionType[cur.Flavor], handlersEmbed[f.Flavor], sessionType[f.Flavor])
	} else {
		ch.skipf("no file embeds %s or %s, so the handler types were not swapped; make the App and the router hand out the %s auth's %s", handlersEmbed[cur.Flavor], sessionType[cur.Flavor], f.Name, handlersEmbed[f.Flavor])
	}
	if len(routed) > 0 {
		ch.didf("%s: the password login route replaced by the directory's: GET /user/login (the redirect), GET /user/callback (the return)%s", strings.Join(routed, ", "), f.logoutRouteNote())
	} else {
		ch.skipf("the password login route (r.Post(prefix+\"/user/login\", h.Login())) was not found in the base's shape, so the directory's routes were not mounted; mount GET <prefix>/user/login, GET <prefix>/user/callback%s in the %s auth's session group", f.logoutRouteNote(), f.Name)
	}

	return nil
}

// oidcRoutes is the directory's login block in the base router's shape.
func (f AuthFlavor) oidcRoutes() string {
	routes := "${1}// Login sends the browser to the directory; the directory returns it to the\n${1}// callback, which starts the session and returns the browser to the page it left.\n${1}r.Get(${2}+\"/user/login\", h.Login())\n${1}r.Get(${2}+\"/user/callback\", h.CallbackOIDC())\n"
	if f.Flavor == FlavorOIDCAzure {
		routes += "${1}r.Get(${2}+\"/user/logout\", h.FrontChannelLogout())\n"
	}

	return routes
}

func (f AuthFlavor) logoutRouteNote() string {
	if f.Flavor == FlavorOIDCAzure {
		return ", and GET /user/logout (the directory's front-channel logout)"
	}

	return ""
}

// Meaning explains the move and names the wiring left to do.
func (f AuthFlavor) Meaning() string {
	au := f.target()
	var b strings.Builder
	fmt.Fprintf(&b, "The %s auth keeps its name, its permission store (tables prefixed `%s`), its roles file, and every surface bound to it; how its people sign in changed. `pkg/auth/%s` now constructs the %s session manager over `%sSessions` and `%sOIDCUsers` (the user anchor the directory's identifiers key), and its people sign in through the organization's directory over OpenID Connect (%s): the login route sends the browser to the directory, the directory returns it to the callback, and the callback starts the session. The data level reads the directory registration from the environment.\n\n", f.Name, au.Pascal(), f.Name, f.Flavor, au.Pascal(), au.Pascal(), au.directoryLabel())
	switch f.Authority {
	case AuthorityDirectory:
		fmt.Fprintf(&b, "Role membership is now the directory's (`session.RoleSync`): every login reconciles the person's roles to the directory's role claims and removes any it does not name, and a login naming no known role is refused. So nothing in the application may assign roles in the %s store any more, and the bootstrap seeds none; the roles file only defines the roles and their grants. In a tenanted application, pass the tenant roster as `%s.Settings.Domains`.\n\n", f.Name, f.Name)
	default:
		fmt.Fprintf(&b, "Role membership stays the application's (`session.DisableRoleSync`): the directory proves who someone is and the application decides what they may do. The bootstrap assigns the development %s identities their roles by username, with no password and no account to create, since the directory presents the name.\n\n", f.Name)
	}
	b.WriteString("Left to wire:\n\n")
	items := []string{
		fmt.Sprintf("Make the bootstrap match. Its development identities for the %s auth are logins the directory presents (`APP_USERNAME` under the simulated directory) with their roles, and no passwords: drop the password fields from the identities file and the account creation (`CreateSessionUser`) from the code, as the reference's bootstrap seeds its `members`.%s", f.Name, au.identitiesNote()),
		fmt.Sprintf("Remove password user management for the %s auth wherever it is: account creation, password changes, and their routes and pages, since the directory holds the accounts now.", f.Name),
		fmt.Sprintf("Check the session group. The App and the router now hand out `%s`; read the %s auth's session group over against the reference's `oidcGroup` (`pkg/router/router.go`): session start and XSRF, `GET <prefix>/user/login`, `GET <prefix>/user/callback`%s, the session and logout routes, then the API behind session validation and the XSRF guard. Set `LoginURL` in the data level's construction to the surface's login page.", handlersEmbed[f.Flavor], f.Name, f.logoutRouteNote()),
		"Sign in from the browser. The surface's login page becomes a button that sends the browser to `<prefix>/user/login?returnUrl=<page>`, as the reference's portal login component does, in place of the credentials form; a refused login returns to the login page with the reason in `?message=`.",
		fmt.Sprintf("Register it. The environment template now carries the %s auth's `APP_%s_OIDC_*` variables: set the redirect URL to the browser-facing callback of its surface (through the dev proxy in development), and fill in %s when the application is registered in a directory. Until then the Procfile builds with the session library's `skipAuth` tag, which simulates the directory: every %s login is `APP_USERNAME`.", f.Name, strings.ToUpper(f.Name), au.registrationToFill(), f.Name),
		fmt.Sprintf("Replace the harness login helper: under `-tags skipAuth`, as the reference's `test/integration/portal_login_skipauth_test.go` does, set `APP_USERNAME` under a mutex and follow the login route to the callback, with a `!skipAuth` twin that skips with the reason, so `go test ./...` without the tag skips the %s login tests visibly and CI runs with it.", f.Name),
	}
	for i, item := range items {
		fmt.Fprintf(&b, "%d. %s\n", i+1, item)
	}
	b.WriteString("\nData consequence: everyone in the ")
	b.WriteString(f.Name)
	b.WriteString(" auth signs in again, since the session tables are recreated in the new shape and the user anchor moves to the directory's identifiers. ")
	if f.CarryRoles {
		b.WriteString("Role assignments are carried (`--carry-roles`): they stay keyed by the old usernames, so they apply only to people the directory presents under the same name; review the rest by hand.")
	} else {
		b.WriteString("Role assignments are dropped, since they were keyed by password usernames the directory need not present; the bootstrap reseeds the development identities, and a deployed application assigns roles again through its administration surface (or the directory does, under the directory authority). `--carry-roles` would have kept them.")
	}
	b.WriteString("\n")

	return b.String()
}
