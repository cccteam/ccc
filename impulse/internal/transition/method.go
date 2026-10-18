package transition

import (
	"context"
	"fmt"
	"os"
	"path"
	"regexp"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/app"
	"github.com/cccteam/ccc/impulse/internal/authpkg"
	"github.com/cccteam/ccc/impulse/internal/check"
	"github.com/cccteam/ccc/impulse/internal/names"
)

// Method adds a sign-in method to an auth the application has: a registration on its
// Auth session, in a file of its own in the auth's package, with its settings read from
// the environment under the registration's variables, declared on every outlet bound to
// the auth so the generated router mounts its routes, and simulated in development
// (skipAuth) when it is a directory method. The login page renders from the methods
// route, so a method declared is a button offered; nothing else about the surface
// changes.
type Method struct {
	// Auth is the name of the auth the method is added to.
	Auth string
	// Spec is the method: its kind, registration name, authority, slot and account rule.
	Spec authpkg.Method
}

// Command is the impulse command line for the transition.
func (m *Method) Command() string {
	cmd := fmt.Sprintf("impulse add method %s %s", m.Auth, m.Spec.Kind)
	if m.Spec.Kind != app.MethodPassword {
		cmd += " " + m.Spec.Name
	}
	if m.Spec.Authority != "" {
		cmd += " --authority " + m.Spec.Authority
	}
	if m.Spec.Slot != "" {
		cmd += " --slot " + m.Spec.Slot
	}
	if m.Spec.Accounts != "" {
		cmd += " --accounts " + m.Spec.Accounts
	}

	return cmd
}

// spec returns the method with the password method's name settled.
func (m *Method) spec() *authpkg.Method {
	s := m.Spec
	if s.Kind == app.MethodPassword {
		s.Name = app.PasswordRegistration
	}

	return &s
}

// Validate checks the transition against the application before anything is changed:
// the auth exists and constructs an Auth the scan reads, the method is well formed, and
// no method of that registration name is registered yet.
func (m *Method) Validate(a *app.App) error {
	pkg, auth, err := m.target(a)
	if err != nil {
		return err
	}
	spec := m.spec()
	if spec.Directory() && spec.Authority == "" {
		return errors.Newf("the %s method needs --authority: directory (the directory's roles are synchronized at every sign-in and roles it does not name are removed) or application (roles are assigned in the application)", spec.Name)
	}
	if err := spec.Validate(); err != nil {
		return err
	}
	for i := range auth.Methods {
		if auth.Methods[i].Name == spec.Name {
			return errors.Newf("%s: the %s auth registers %s already (%s:%d)", pkg.Dir, m.Auth, spec.Name, auth.Methods[i].File, auth.Methods[i].Line)
		}
	}
	if _, err := os.Stat(a.Abs(path.Join(pkg.Dir, spec.Name+".go"))); err == nil {
		return errors.Newf("%s/%s.go already exists", pkg.Dir, spec.Name)
	}

	return nil
}

// target finds the auth package and the Auth construction the method goes on.
func (m *Method) target(a *app.App) (*app.AuthPackage, *app.Auth, error) {
	for i := range a.AuthPackages {
		p := &a.AuthPackages[i]
		if p.Name != m.Auth {
			continue
		}
		for j := range a.Auths {
			if path.Dir(a.Auths[j].File) == p.Dir {
				return p, &a.Auths[j], nil
			}
		}

		return nil, nil, errors.Newf("%s: the %s auth constructs no session.NewAuth the scan reads, so a method cannot be registered on it", p.Dir, m.Auth)
	}

	return nil, nil, errors.Newf("no auth package pkg/auth/%s; the auths are %s (impulse add auth adds one)", m.Auth, authNames(a))
}

// Apply makes the deterministic half: the method's file, the Auth file's field,
// registration and settings, the data level's variables, the outlets' declarations, and
// the development environment for a directory method.
func (m *Method) Apply(ctx context.Context, a *app.App, exec check.Execer) (*Change, error) {
	if err := m.Validate(a); err != nil {
		return nil, err
	}
	pkg, auth, err := m.target(a)
	if err != nil {
		return nil, err
	}
	spec := m.spec()
	ch := &Change{Command: m.Command()}
	pkgSpec := &authpkg.Spec{Module: modulePath(a), AuthDir: path.Dir(pkg.Dir), Name: m.Auth, Methods: []authpkg.Method{*spec}}
	if err := m.writeFile(a, pkg, pkgSpec, ch); err != nil {
		return nil, err
	}
	if err := m.editAuth(a, auth, spec, ch); err != nil {
		return nil, err
	}
	if spec.Directory() {
		if err := m.editConfig(a, spec, ch); err != nil {
			return nil, err
		}
	}
	if err := m.declare(a, pkg, spec, ch); err != nil {
		return nil, err
	}
	if spec.Directory() {
		if err := tagProcfile(a, m.Auth, ch); err != nil {
			return nil, err
		}
		if err := writeMethodEnv(a, m.Auth, spec, ch); err != nil {
			return nil, err
		}
	}
	generate(ctx, a, exec, ch, "go generate ./... failed; fix the cause and run it:", "ran go generate ./..., so the router mounts the method's routes and the login page offers it")

	return ch, nil
}

// writeFile renders the method's file into the auth package.
func (m *Method) writeFile(a *app.App, pkg *app.AuthPackage, pkgSpec *authpkg.Spec, ch *Change) error {
	data, err := authpkg.MethodFile(pkgSpec, &pkgSpec.Methods[0])
	if err != nil {
		return err
	}
	rel := path.Join(pkg.Dir, pkgSpec.Methods[0].Name+".go")
	if err := writeNew(a, rel, string(data)); err != nil {
		return err
	}
	spec := &pkgSpec.Methods[0]
	shape := ""
	if spec.Directory() {
		shape = fmt.Sprintf(" (%s, %s, %s, role membership the %s's)", spec.Kind, slotSentence(spec), accountsSentence(spec), spec.Authority)
	}
	ch.didf("%s: the %s method%s: its registration constant, its settings, its handlers (%sLogin and the rest), and register%s, which registers it on the Auth", rel, spec.Name, shape, spec.Pascal(), spec.Pascal())

	return nil
}

// slotSentence names a method's organization slot for a report.
func slotSentence(m *authpkg.Method) string {
	switch m.Slot {
	case "", authpkg.SlotOne:
		if m.Kind == app.MethodGoogle {
			return "one Workspace"
		}

		return "one tenant"
	case authpkg.SlotListed:
		if m.Kind == app.MethodGoogle {
			return "named Workspaces"
		}

		return "named tenants"
	case authpkg.SlotAny:
		if m.Kind == app.MethodGoogle {
			return "any Google account"
		}

		return "any tenant"
	case authpkg.SlotPersonal:
		return "Microsoft personal accounts"
	case authpkg.SlotOnePersonal:
		return "one tenant and personal accounts"
	case authpkg.SlotAnyPersonal:
		return "any tenant and personal accounts"
	case authpkg.SlotAnyWorkspace:
		return "any Workspace"
	case authpkg.SlotMixed:
		return "one Workspace and any Google account"
	default:
		return m.Slot
	}
}

// accountsSentence names a method's account rule for a report.
func accountsSentence(m *authpkg.Method) string {
	switch m.Accounts {
	case authpkg.AccountsInvited:
		return "invited users only"
	case authpkg.AccountsOpen:
		return "open sign-up"
	default:
		return "members of the directory"
	}
}

// editAuth adds the method to the Auth file: the Handlers field, the registration in New,
// the settings field, the login page for the first directory method, and the imports.
func (m *Method) editAuth(a *app.App, auth *app.Auth, spec *authpkg.Method, ch *Change) error {
	rel := auth.File
	src, mode, err := readFile(a, rel)
	if err != nil {
		return err
	}
	field := lowerFirst(spec.Pascal())
	typ := methodType(spec.Kind)
	edited, err := app.AddStructField(rel, src, "Handlers", field+" "+typ)
	if err != nil {
		return err
	}
	if spec.Directory() {
		edited, err = app.AddStructField(rel, edited, "Settings", fmt.Sprintf("// %s is the %s registration's settings: how the application is known to\n// %s, read from the environment (%s).\n%s %s", spec.Pascal(), spec.Name, spec.Provider(), strings.Join(methodVariables(m.Auth, spec), ", "), spec.Pascal(), spec.SettingsType()))
		if err != nil {
			return err
		}
		edited, err = app.AddStructField(rel, edited, "Settings", "// LoginURL is the browser page a refused or waiting sign-in returns to, with the\n// reason as a code in the query (?code=, never text, or ?pending= for a sign-in that\n// waits): the login page of the surface that binds to this auth.\nLoginURL string")
		if err != nil {
			return err
		}
		edited, err = app.AddImport(rel, edited, methodImport(spec.Kind))
		if err != nil {
			return err
		}
	}
	registration := fmt.Sprintf("\thandlers.%s = register%s(authSession)\n", field, spec.Pascal())
	if spec.Directory() {
		registration = fmt.Sprintf("\thandlers.%s, err = register%s(authSession, accessClient, settings)\n\tif err != nil {\n\t\treturn nil, err\n\t}\n", field, spec.Pascal())
	}
	loc := returnAuthRE.FindIndex(edited)
	if loc == nil {
		ch.skipf("%s: New does not end in `return &Auth{access: accessClient, handlers: handlers}, nil` as the skeleton's does, so the %s method is not registered; call register%s in New and set handlers.%s", rel, spec.Name, spec.Pascal(), field)
	} else {
		edited = append(edited[:loc[0]:loc[0]], append([]byte(registration), edited[loc[0]:]...)...)
	}
	if err := os.WriteFile(a.Abs(rel), edited, mode); err != nil {
		return errors.Wrap(err, "os.WriteFile()")
	}
	ch.didf("%s: Handlers holds the %s method (%s %s), New registers it (register%s)%s", rel, spec.Name, field, typ, spec.Pascal(), settingsNote(spec))

	return nil
}

// returnAuthRE matches the Auth file's final statement, before which a method's
// registration goes.
var returnAuthRE = regexp.MustCompile(`(?m)^\n?\treturn &Auth\{access: accessClient, handlers: handlers\}, nil`)

func settingsNote(spec *authpkg.Method) string {
	if !spec.Directory() {
		return ""
	}

	return ", and Settings carries its " + spec.SettingsType() + " and the LoginURL"
}

// methodType is the Go type of a method's handle.
func methodType(kind string) string {
	switch kind {
	case app.MethodAzure:
		return "*azure.Method"
	case app.MethodGoogle:
		return "*google.Method"
	default:
		return "*session.PasswordMethod"
	}
}

// methodImport is the session package a method comes from.
func methodImport(kind string) string {
	switch kind {
	case app.MethodAzure:
		return "github.com/cccteam/session/azure"
	case app.MethodGoogle:
		return "github.com/cccteam/session/google"
	default:
		return "github.com/cccteam/session"
	}
}

func lowerFirst(s string) string {
	if s == "" {
		return ""
	}

	return strings.ToLower(s[:1]) + s[1:]
}

// editConfig reads the method's settings from the environment on the data level: the
// environment struct gains the registration's variables, and the auth's Settings literal
// gains the method's settings and, when it has none yet, the login page.
func (m *Method) editConfig(a *app.App, spec *authpkg.Method, ch *Change) error {
	rel, data, mode, err := findDeclaringFile(a, "DataConfiguration")
	if err != nil {
		return err
	}
	if rel == "" {
		ch.skipf("no file declares a DataConfiguration struct, so the %s method's settings (%s) are not read from the environment; read them where the %s auth is constructed and pass them in %s.Settings.%s", spec.Name, strings.Join(methodVariables(m.Auth, spec), ", "), m.Auth, m.Auth, spec.Pascal())

		return nil
	}
	env := envVarRE.FindSubmatch(data)
	if env == nil {
		ch.skipf("%s: no environment struct (env := &T{}) to add the %s method's variables to; read %s and pass them in %s.Settings.%s", rel, spec.Name, strings.Join(methodVariables(m.Auth, spec), ", "), m.Auth, spec.Pascal())

		return nil
	}
	structName := string(env[1])
	literal, edited, err := addMethodEnv(rel, data, structName, m.Auth, spec)
	if err != nil {
		return err
	}
	settingsType := m.Auth + ".Settings"
	edited, err = app.AddLiteralElement(rel, edited, "NewDataConfiguration", settingsType, `LoginURL: "/login"`)
	switch {
	case errors.Is(err, app.ErrNoAnchor):
		ch.skipf("%s: NewDataConfiguration builds no %s{...} literal, so the %s method's settings are read into %s but not passed; set %s.Settings.%s from them", rel, settingsType, spec.Name, structName, m.Auth, spec.Pascal())
	case err != nil:
		return err
	default:
		edited, err = app.AddLiteralElement(rel, edited, "NewDataConfiguration", settingsType, spec.Pascal()+": "+m.Auth+"."+literal)
		if err != nil {
			return err
		}
	}
	if err := os.WriteFile(a.Abs(rel), edited, mode); err != nil {
		return errors.Wrap(err, "os.WriteFile()")
	}
	ch.didf("%s: %s reads the %s auth's %s registration from %s, and the %s.Settings literal passes them", rel, structName, m.Auth, spec.Name, strings.Join(methodVariables(m.Auth, spec), ", "), m.Auth)
	ch.skipf("%s: LoginURL is set to /login when the construction had none; make it the login page of the surface bound to the %s auth", rel, m.Auth)

	return nil
}

// addMethodEnv adds the method's variables to the environment struct, each a field
// named <Auth><Method><Field> under the registration's variable, and returns the
// method's settings literal reading them (<Pascal>Settings{Field: env.<...>, ...}) and
// the edited source.
func addMethodEnv(rel string, src []byte, structName, auth string, spec *authpkg.Method) (literal string, edited []byte, err error) {
	edited = src
	authPascal := names.Pascal(auth)
	var b strings.Builder
	fmt.Fprintf(&b, "%s{\n", spec.SettingsType())
	fields := spec.Fields()
	for i := range fields {
		f := &fields[i]
		field := fmt.Sprintf("%s%s%s string `env:%q%s`", authPascal, spec.Pascal(), f.Name, spec.Variable(auth, f), secretTag(f))
		if i == 0 {
			field = fmt.Sprintf("// The %s auth's %s registration (pkg/auth/%s/%s.go): %s, under the registration's\n// variables (APP_<AUTH>_<REGISTRATION>_*). Under the session library's skipAuth build tag\n// the directory is simulated and only what the simulated sign-in presents is read.\n", auth, spec.Name, auth, spec.Name, fieldsSentence(spec)) + field
		}
		edited, err = app.AddStructField(rel, edited, structName, field)
		if err != nil {
			return "", nil, err
		}
		fmt.Fprintf(&b, "\t%s: env.%s%s%s,\n", f.Name, authPascal, spec.Pascal(), f.Name)
	}
	b.WriteString("}")

	return b.String(), edited, nil
}

// secretTag is the secret marker a credential field carries.
func secretTag(f *authpkg.Field) string {
	if f.Secret {
		return ` secret:"true"`
	}

	return ""
}

// fieldsSentence lists a method's settings for a comment.
func fieldsSentence(spec *authpkg.Method) string {
	fields := spec.Fields()
	parts := make([]string, 0, len(fields))
	for i := range fields {
		parts = append(parts, fields[i].Doc)
	}

	return strings.Join(parts, "; ")
}

// methodVariables lists a method's environment variables.
func methodVariables(auth string, spec *authpkg.Method) []string {
	fields := spec.Fields()
	vars := make([]string, 0, len(fields))
	for i := range fields {
		vars = append(vars, spec.Variable(auth, &fields[i]))
	}

	return vars
}

// declare adds the method's declaration to every outlet bound to the auth in the
// generator programs, so the generated router mounts its routes and the methods route
// offers it.
func (m *Method) declare(a *app.App, pkg *app.AuthPackage, spec *authpkg.Method, ch *Change) error {
	declaration := app.MethodDeclaration(spec.Kind, spec.Name, "")
	edited := 0
	var files []string
	for _, g := range a.SiteGenerators() {
		src, mode, err := readFile(a, g.File)
		if err != nil {
			return err
		}
		out, n, err := app.AddAuthMethod(g.File, src, pkg.Path, spec.Name, declaration)
		if err != nil {
			return err
		}
		if n == 0 {
			continue
		}
		if err := os.WriteFile(a.Abs(g.File), out, mode); err != nil {
			return errors.Wrap(err, "os.WriteFile()")
		}
		edited += n
		files = append(files, g.File)
	}
	if edited == 0 {
		ch.skipf("no outlet declares Auth(%q, ...), so the %s method's routes are mounted nowhere; add %s to the Auth option of the outlet that serves the %s auth, or bind one with impulse add outlet", pkg.Path, spec.Name, declaration, m.Auth)

		return nil
	}
	ch.didf("%s: %s declared on %d outlet Auth option(s) bound to the %s auth, so the generated router mounts user/%s/login%s and the methods route offers it", strings.Join(files, ", "), declaration, edited, m.Auth, spec.Name, callbackNote(spec))

	return nil
}

func callbackNote(spec *authpkg.Method) string {
	switch spec.Kind {
	case app.MethodAzure:
		return ", the callback and the front-channel logout"
	case app.MethodGoogle:
		return " and the callback"
	default:
		return ""
	}
}

// Meaning explains the method and names the wiring left to do.
func (m *Method) Meaning() string {
	spec := m.spec()
	var b strings.Builder
	fmt.Fprintf(&b, "A sign-in method is a registration on an auth's Auth session: the %[1]s auth now offers %[2]s beside its other methods, in `pkg/auth/%[1]s/%[3]s.go`, which holds the registration's constant, its settings, its handlers and the `register%[4]s` call New makes before the first request. ", m.Auth, methodSentence(spec), spec.Name, spec.Pascal())
	if spec.Directory() {
		fmt.Fprintf(&b, "Its organization slot is %s and its account rule %s. ", slotSentence(spec), accountsSentence(spec))
		if spec.Authority == AuthorityDirectory {
			b.WriteString("Role membership is the directory's: every sign-in reconciles the person's roles to what the directory asserts and removes any it does not name, so nothing in the application assigns roles in this store for people who sign in this way. ")
		} else {
			b.WriteString("Role membership stays the application's: the directory proves who someone is and the application decides what they may do. ")
		}
		fmt.Fprintf(&b, "The data level reads its settings from the environment under the registration's variables (%s), the convention bedrock supplies them under per environment, and the Procfile builds with the session library's `skipAuth` tag, which simulates the directory in development: a sign-in through this registration is `APP_%s_USERNAME`, or `APP_USERNAME` when that is unset. ", strings.Join(methodVariables(m.Auth, spec), ", "), strings.ToUpper(strings.ReplaceAll(spec.Name, "-", "_")))
	}
	b.WriteString("Every outlet bound to the auth declares the method on its `Auth` option, so the generated router mounts its routes and the methods route the login page renders from offers it; a person is one user whichever way they sign in, linked to the identity the directory verifies.\n\nLeft to wire:\n\n")
	items := []string{
		fmt.Sprintf("Look at the login page. The surface bound to the %s auth renders its sign-in methods from `GET <prefix>/user/methods`; a page that lists the methods itself gains the %s button (`<prefix>/user/%s/login?returnUrl=<page>`)%s.", m.Auth, spec.Name, spec.Name, passwordFormNote(spec)),
	}
	if spec.Directory() {
		items = append(items,
			fmt.Sprintf("Register it with %s and fill in %s in the environment template, with the redirect URL the browser-facing callback of the surface (`<base>/<outlet>/api/user/%s/callback`); drop the `skipAuth` tag to sign in through the directory for real.", spec.Provider(), strings.Join(methodVariables(m.Auth, spec), ", "), spec.Name),
			fmt.Sprintf("Prove it under `-tags skipAuth` as the reference's `test/integration/portal_login_skipauth_test.go` does, setting `APP_%s_USERNAME` for the registration and following `user/%s/login` to the callback.", strings.ToUpper(strings.ReplaceAll(spec.Name, "-", "_")), spec.Name),
		)
		if spec.Authority == AuthorityDirectory {
			items = append(items, "Seed no roles for people who sign in this way; define the roles in the role file and assign them in the directory.")
		}
	} else {
		items = append(items, fmt.Sprintf("Give the %s auth its development logins with passwords in the bootstrap identities, and the test harness a login that posts to `user/password/login`.", m.Auth))
	}
	for i, item := range items {
		fmt.Fprintf(&b, "%d. %s\n", i+1, item)
	}
	b.WriteString("\nData consequence: none. An existing person who signs in through the new method is linked to their user when the standard account rule finds them under the name the directory presents (or confirms the link with their password), and a newcomer is provisioned or refused as the account rule says.\n")

	return b.String()
}

func passwordFormNote(spec *authpkg.Method) string {
	if spec.Kind == app.MethodPassword {
		return " (a form posting the username and password to `<prefix>/user/password/login`)"
	}

	return ""
}

// tagProcfile builds the development processes with the session library's skipAuth tag,
// which simulates the directory an auth's directory methods sign in through, so the
// application can be signed in to before it is registered with one.
func tagProcfile(a *app.App, auth string, ch *Change) error {
	data, mode, err := readFile(a, "Procfile")
	switch {
	case err != nil:
		ch.skipf("Procfile: not read (%v); build the development server with -tags skipAuth so the %s auth's directory is simulated", err, auth)
	case strings.Contains(string(data), "skipAuth"):
		ch.didf("Procfile: already builds with -tags skipAuth, so the %s auth's directory is simulated in development", auth)
	default:
		n := 0
		text := goRunRE.ReplaceAllStringFunc(string(data), func(string) string {
			n++

			return "go run -tags skipAuth "
		})
		if n == 0 {
			ch.skipf("Procfile: no `go run` to tag; build the development server with -tags skipAuth so the %s auth's directory is simulated", auth)

			return nil
		}
		if err := os.WriteFile(a.Abs("Procfile"), []byte(text), mode); err != nil {
			return errors.Wrap(err, "os.WriteFile()")
		}
		ch.didf("Procfile: %d go run command(s) build with -tags skipAuth, so the %s auth's directory is simulated in development", n, auth)
	}

	return nil
}

var goRunRE = regexp.MustCompile(`\bgo run `)

// writeMethodEnv adds a directory method's variables to the development environment
// template, with the simulated directory's shared pair when the template has none.
func writeMethodEnv(a *app.App, auth string, spec *authpkg.Method, ch *Change) error {
	if a.EnvTemplate == "" {
		ch.skipf("no environment template (.envrc.template, .env.template, .env.example) to add the %s method's variables to (%s)", spec.Name, strings.Join(methodVariables(auth, spec), ", "))

		return nil
	}
	data, mode, err := readFile(a, a.EnvTemplate)
	if err != nil {
		return err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\n# --- %s auth, %s method: %s over OpenID Connect (pkg/auth/%s/%s.go) ---\n", auth, spec.Name, spec.Provider(), auth, spec.Name)
	fmt.Fprintf(&b, "# The Procfile builds with the session library's skipAuth tag, which simulates %s:\n# every sign-in through this registration is APP_%s_USERNAME, or APP_USERNAME when that\n# is unset (the roles in APP_%s_ROLES or APP_ROLES stand in for the directory's), and only\n# the settings marked below are read. Register the application with %s, fill the rest\n# in, and drop the tag to use it.\n", spec.Provider(), strings.ToUpper(strings.ReplaceAll(spec.Name, "-", "_")), strings.ToUpper(strings.ReplaceAll(spec.Name, "-", "_")), spec.Provider())
	if !strings.Contains(string(data), "APP_USERNAME=") {
		fmt.Fprintf(&b, "export APP_USERNAME=%s-dev\nexport APP_ROLES=\n", auth)
	}
	fields := spec.Fields()
	for i := range fields {
		f := &fields[i]
		variable := spec.Variable(auth, f)
		switch {
		case f.Name == authpkg.FieldRedirectURL:
			fmt.Fprintf(&b, "# %s is the browser-facing callback of the surface that binds to the %s auth, such as\n# http://127.0.0.1:4300/api/user/%s/callback through the dev proxy; read under the\n# simulated directory too, since it is where the simulated sign-in returns.\nexport %s=\n", variable, auth, spec.Name, variable)
		case f.Simulated:
			fmt.Fprintf(&b, "# %s: %s. Read under the simulated directory too.\nexport %s=%s\n", variable, f.Doc, variable, devValue(f, auth))
		default:
			fmt.Fprintf(&b, "# %s: %s.\n# export %s=\n", variable, f.Doc, variable)
		}
	}
	text := strings.TrimRight(string(data), "\n") + "\n" + b.String()
	if err := os.WriteFile(a.Abs(a.EnvTemplate), []byte(text), mode); err != nil {
		return errors.Wrap(err, "os.WriteFile()")
	}
	ch.didf("%s: the %s method's variables (%s), to fill in", a.EnvTemplate, spec.Name, strings.Join(methodVariables(auth, spec), ", "))

	return nil
}

// placeholderTenant is a well-formed tenant id for the simulated directory: the Entra
// method validates its organization slot at registration whether or not the directory
// is simulated, so the development template names a tenant that is nobody's.
const placeholderTenant = "00000000-0000-0000-0000-000000000000"

// devValue is what the development template sets for a field the simulated directory
// reads: a placeholder that lets the simulation run.
func devValue(f *authpkg.Field, auth string) string {
	switch f.Name {
	case authpkg.FieldTenantID:
		return placeholderTenant
	case authpkg.FieldTenants:
		return "lab-a=" + placeholderTenant
	case authpkg.FieldHostedDomain:
		return "example.com"
	case authpkg.FieldWorkspaces:
		return "lab=example.com"
	case authpkg.FieldGroupPrefix:
		return auth + "-"
	default:
		return ""
	}
}
