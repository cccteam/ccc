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
)

// Auth adds an auth to the application: a population that signs in one way and holds
// roles in its own permission store. An auth is a package, pkg/auth/<name>, copied from
// an auth the application already has with every name substituted, so the new one owns
// its own tables, cookie, store prefix, and roles file from the start. The data level
// constructs it. Binding a site or an outlet to it, the users it starts with, and the
// tests that prove its people are strangers to the other auths are the agent's.
type Auth struct {
	// Name is the auth's lowercase name, the population it serves: partners, learners.
	Name string
	// Flavor is the login flavor: password or preauth. The OIDC flavors wait on the
	// membership-authority ruling.
	Flavor string
}

// The flavors add auth lays in.
const (
	FlavorPassword = app.FlavorPassword
	FlavorPreauth  = app.FlavorPreauth
)

var authNameRE = regexp.MustCompile(`^[a-z][a-z0-9]*$`)

// Command is the impulse command line for the transition.
func (au Auth) Command() string {
	return fmt.Sprintf("impulse add auth %s --%s", au.Name, au.Flavor)
}

// Pascal is the name's PascalCase form: the table prefix.
func (au Auth) Pascal() string { return strings.ToUpper(au.Name[:1]) + au.Name[1:] }

// source is the auth package the new one is copied from.
type authSource struct {
	// Name and Pascal are the source auth's name and table prefix.
	Name, Pascal string
	// Dir is the package's root-relative directory.
	Dir string
	// Flavor is the source's login flavor.
	Flavor string
}

// Validate checks the transition against the application before anything is changed.
func (au Auth) Validate(a *app.App) error {
	if !authNameRE.MatchString(au.Name) {
		return errors.Newf("auth name %q: name the population in lowercase letters and digits, such as partners", au.Name)
	}
	if au.Flavor != FlavorPassword && au.Flavor != FlavorPreauth {
		return errors.Newf("flavor %q: add auth lays in password or preauth; the OIDC flavors follow once the membership-authority question is answered", au.Flavor)
	}
	if _, err := au.source(a); err != nil {
		return err
	}

	return nil
}

// source picks the auth to copy: one of the same flavor when the application has it,
// else the first auth package found.
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
		sources = append(sources, authSource{Name: name, Pascal: strings.ToUpper(name[:1]) + name[1:], Dir: path.Dir(existing.File), Flavor: existing.Flavor})
	}
	if len(sources) == 0 {
		return nil, errors.New("no auth package to copy: an auth is pkg/auth/<name> constructing its session manager and store, and the application has none (impulse init lays the first one in)")
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].Name < sources[j].Name })
	for i := range sources {
		if sources[i].Flavor == au.Flavor {
			return &sources[i], nil
		}
	}

	return &sources[0], nil
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
// flavor differs, the constructor swapped.
func (au Auth) copyPackage(a *app.App, src *authSource, ch *Change) error {
	dst := path.Join(path.Dir(src.Dir), au.Name)
	if _, err := os.Stat(a.Abs(dst)); err == nil {
		return errors.Newf("%s already exists", dst)
	}
	entries, err := os.ReadDir(a.Abs(src.Dir))
	if err != nil {
		return errors.Wrap(err, "os.ReadDir()")
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := os.ReadFile(a.Abs(path.Join(src.Dir, name)))
		if err != nil {
			return errors.Wrap(err, "os.ReadFile()")
		}
		text := au.rename(string(data), src)
		if src.Flavor != au.Flavor {
			text = swapFlavor(text, src.Flavor, au.Flavor)
		}
		out := strings.Replace(name, src.Name, au.Name, 1)
		if err := writeNew(a, path.Join(dst, out), text); err != nil {
			return err
		}
	}
	if src.Flavor == au.Flavor {
		ch.didf("%s: the %s auth package, a copy of %s with its names substituted (tables %sSessions and %sSessionUsers, cookie %s, store prefix %s)", dst, au.Name, src.Name, au.Pascal(), au.Pascal(), au.Name, au.Pascal())
	} else {
		ch.didf("%s: the %s auth package, a copy of %s with its names substituted and the constructor swapped from %s to %s (tables %sSessions, cookie %s, store prefix %s); read it over, since the swap is textual", dst, au.Name, src.Name, src.Flavor, au.Flavor, au.Pascal(), au.Name, au.Pascal())
	}

	return nil
}

// rename substitutes the source auth's name for the new one, in both cases.
func (au Auth) rename(text string, src *authSource) string {
	text = strings.ReplaceAll(text, src.Pascal, au.Pascal())

	return strings.ReplaceAll(text, src.Name, au.Name)
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
	p := a.Profile()
	if len(p.Sites) == 0 || len(p.Sites[0].Generator.MigrationSources) == 0 || !strings.HasPrefix(p.Sites[0].Generator.MigrationSources[0], fileScheme) {
		ch.skipf("no file:// migration source names the schema, so the %s auth's tables were not written; copy the %s auth's session, user, and access table migrations under the %s prefix", au.Name, src.Name, au.Pascal())

		return nil
	}
	dir := strings.TrimPrefix(p.Sites[0].Generator.MigrationSources[0], fileScheme)
	entries, err := os.ReadDir(a.Abs(dir))
	if err != nil {
		return errors.Wrap(err, "os.ReadDir()")
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
		data, err := os.ReadFile(a.Abs(path.Join(dir, name)))
		if err != nil {
			return errors.Wrap(err, "os.ReadFile()")
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
		down, err := os.ReadFile(a.Abs(path.Join(dir, strings.TrimSuffix(name, ".up.sql")+".down.sql")))
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
	pkgPath := modulePath + "/" + path.Join(path.Dir(src.Dir), au.Name)
	edited, err := app.AddImport(rel, data, pkgPath)
	if err != nil {
		return err
	}
	edited, err = app.AddStructField(rel, edited, "DataConfiguration", au.Name+" *"+au.Name+".Auth")
	if err != nil {
		return err
	}
	srcVar, ok := constructionVar(string(data), src.Name)
	if !ok {
		ch.skipf("%s: NewDataConfiguration constructs no %s.New(...) to copy, so the %s auth is declared on DataConfiguration but not constructed; construct it beside the %s auth and set the field", rel, src.Name, au.Name, src.Name)
	} else {
		statements := strings.ReplaceAll(strings.ReplaceAll(srcVar.statement, src.Name+".", au.Name+"."), srcVar.name, au.Name+"Auth")
		edited, err = app.AddStatementsBeforeReturn(rel, edited, "NewDataConfiguration", "DataConfiguration", statements)
		switch {
		case errors.Is(err, app.ErrNoAnchor):
			ch.skipf("%s: NewDataConfiguration does not end in \"return &DataConfiguration{...}, nil\", so the %s auth is declared but not constructed; construct it beside the %s auth", rel, au.Name, src.Name)
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
	ch.didf("%s: the %s auth constructed on DataConfiguration beside the %s auth (field, construction, import); %s: its accessor %s()", rel, au.Name, src.Name, accessor, au.Pascal())
	ch.skipf("%s: Close releases the %s auth only; release the %s auth too", rel, src.Name, au.Name)

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

func (au Auth) userTableNote() string {
	if au.Flavor == FlavorPreauth {
		return ""
	}

	return fmt.Sprintf(" and `%sSessionUsers`", au.Pascal())
}
