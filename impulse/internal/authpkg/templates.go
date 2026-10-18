package authpkg

// authTemplate renders the Auth file: the package, its tables, its permission store, its
// role file, the Auth session with the methods registered on it, and the served surface.
const authTemplate = `// Package {{.Name}} is the {{.Name}} auth: {{.Doc}}, who hold their roles in the
// {{.Name}} permission store.
//
// An auth is a package. It owns its session manager, one Auth session with the sign-in
// methods registered on it (here {{range $i, $m := .Methods}}{{if $i}}, {{end}}{{$m.Name}}{{end}}), with its own session, user, identity, auth
// event and pending sign-in tables and cookie; its permission store, with its own table
// prefix, whose engine signals policy changes to the application's other instances
// through the live service; and its role file, embedded beside it, so the release's
// default roles travel with the binary. Each method is its own file in this package,
// named for its registration.
// A site or an outlet binds to an auth by composing its handlers, and two auths on one
// database and one host never collide, because everything an auth names carries its
// name. The same name in this auth and in another is two unrelated principals. The
// package is named for the population, never for how it signs in: the {{.Name}} can gain
// or lose a sign-in method without the package moving.
package {{.Name}}

import (
	"context"
	_ "embed" // the role file rides in the binary
	"net/http"
	"time"

	cloudspanner "cloud.google.com/go/spanner"
	"github.com/cccteam/access"
	"github.com/cccteam/access/spannerstore"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/session"
{{- range .Imports}}
	"{{.}}"
{{- end}}
	"github.com/cccteam/session/sessionstorage"
	"github.com/go-playground/errors/v5"
)

const (
	// Name is the auth's name: the cookie its sessions ride in.
	Name = "{{.Name}}"
	// TablePrefix is the name's PascalCase form, which prefixes every table the auth owns.
	TablePrefix = "{{.Pascal}}"

	// XSRFCookie is the cookie the auth issues its XSRF token in. It carries the auth's name
	// so two auths on one host never overwrite each other's token; the browser echoes it in
	// the X-XSRF-TOKEN header, so the web app that binds to this auth names the same cookie.
	XSRFCookie = Name + "-xsrf"

	// The auth's tables, which schema/migrations creates: the sessions and users of the
	// Auth session (a person is one user whichever way they sign in; an identity the
	// directory verifies links to its user through the identities table), the auth events
	// each session records and the pending sign-ins that wait for a confirmation. The
	// names are spelled out in full so the tool's session-tables check, which reads the
	// literals, finds their migrations.
	sessionsTable   = "{{.Pascal}}Sessions"
	usersTable      = "{{.Pascal}}SessionUsers"
	identitiesTable = "{{.Pascal}}SessionIdentities"
	eventsTable     = "{{.Pascal}}SessionAuthEvents"
	pendingTable    = "{{.Pascal}}SessionPendingSignIns"
)

// roleFile is the auth's default roles: the role file beside this package, embedded in the
// binary so the roles travel with the release. The file is the complete statement of the
// default roles; the store holds only the custom roles and the memberships written at run
// time, and a sign-in holds what its memberships name. A global role is held in the global
// partition and a domain role in every tenant domain, so nothing provisions the file per
// tenant.
//
//go:embed roles.json
var roleFile access.RoleFile

// Roles returns the auth's role file, which New hands to the permission engine and the
// tests validate and read.
func Roles() access.RoleFile {
	return roleFile
}

// Settings are the auth's settings: the collection the role file validates against, the
// live service the permission engine signals through, and the environment-derived values,
// the settings of each sign-in method among them.
type Settings struct {
	// Collection is the generated permission collection: the resources, permissions and
	// fields the release declares, which the role file validates against when the engine
	// opens. The package that constructs the auth passes it in; this package imports no
	// router.
	Collection access.PermissionCollection
	// Signals is the application's live service: the permission engine signals the policy
	// kind on it after every policy write and follows the kind from every other instance
	// (access.WithChangeSignal), so a role, grant or membership written on one instance
	// reaches every instance's snapshot at once rather than at the engine's next
	// heartbeat. Required, since every application wires a live service; New refuses nil.
	Signals Signals
	// CookieKey signs session cookies: a Base64-encoded string of at least 32 bytes of
	// cryptographically secure random data.
	CookieKey string
	// SessionTimeout is the idle timeout of a browser session.
	SessionTimeout time.Duration
{{- if .Directory}}
	// LoginURL is the browser page a refused or waiting sign-in returns to, with the
	// reason as a code in the query (?code=, a sessioninfo.LoginRefusalCode, never text, or
	// ?pending= for a sign-in that waits): the login page of the surface that binds to
	// this auth, which holds the sentence for each code.
	LoginURL string
{{- end}}
{{- range .Methods}}{{if .Directory}}
	// {{.Pascal}} is the {{.Name}} registration's settings: how the application is known to
	// {{.Provider}}, read from the environment ({{join .Variables ", "}}).
	{{.Pascal}} {{.SettingsType}}
{{- end}}{{end}}
}

// Signals is the channel the auth's permission engine signals a policy change on and
// follows policy changes from: the application's live service, which carries one signals
// document per application with a field per kind of change (resource.SignalKind; the
// engine's kind is resource.KindPolicy).
type Signals interface {
	resource.Signaler
	resource.SignalSubscriber
}

// Session is the auth's session manager: one Auth session over the Spanner store, whose
// hooks are typed to the store's transaction.
type Session = session.Auth[session.NoCustomData, session.NoCustomData, *cloudspanner.ReadWriteTransaction]

// Auth is the {{.Name}} auth: its permission store and its session manager with the
// sign-in methods registered on it{{if .Foothold}}, and the database client it reads its own tables
// through (the users table, when the deployment seeds the first administrator){{end}}.
type Auth struct {
{{- if .Foothold}}
	db       *cloudspanner.Client
{{- end}}
	access   *access.Client
	handlers *Handlers
}

// Handlers is the auth's served surface, the shape the generated router names for the
// outlet bound to it: the Auth's middleware and handlers, what the login page is told,
// the pending sign-in handlers, and each method's handlers under its registration name
// (in the method's own file).
type Handlers struct {
	*Session
{{- range .Methods}}
	{{.Field}} {{.Type}}
{{- end}}
}

// PendingStatus reports the sign-in that waits for a password confirmation, if any.
func (h *Handlers) PendingStatus() http.HandlerFunc {
	return h.Pending().Status()
}

// PendingConfirmWithPassword completes a waiting sign-in with the user's password.
func (h *Handlers) PendingConfirmWithPassword() http.HandlerFunc {
	return h.Pending().ConfirmWithPassword()
}

// PendingCancel discards a waiting sign-in.
func (h *Handlers) PendingCancel() http.HandlerFunc {
	return h.Pending().Cancel()
}

// New opens the auth's permission store and session manager over the database and
// registers the sign-in methods. The permission engine parses and validates the role file
// against the collection before it opens, so a release whose default roles are wrong does
// not start, blocks until its first policy snapshot is loaded, and signals its policy
// writes through the live service (access.WithChangeSignal over the policy kind), which it
// refuses to open without.
func New(ctx context.Context, db *cloudspanner.Client, settings *Settings) (*Auth, error) {
	if settings.Signals == nil {
		return nil, errors.New("{{.Name}}.Settings.Signals is nil: the permission engine signals policy changes through the application's live service, which every application wires")
	}
	store, err := spannerstore.New(db, spannerstore.WithPrefix(TablePrefix))
	if err != nil {
		return nil, errors.Wrap(err, "spannerstore.New()")
	}
	accessClient, err := access.New(store,
		access.WithDefaultRoles(settings.Collection, Roles()),
		access.WithChangeSignal(access.ChangeSignalFunc(announcePolicy(settings.Signals), watchPolicy(settings.Signals))),
	)
	if err != nil {
		return nil, errors.Wrap(err, "access.New()")
	}
	if err := accessClient.WaitReady(ctx); err != nil {
		return nil, errors.Wrap(err, "access.Client.WaitReady()")
	}

	events, err := sessionstorage.NewAuthEventsTable(eventsTable)
	if err != nil {
		return nil, errors.Wrap(err, "sessionstorage.NewAuthEventsTable()")
	}

	// One Auth session every method establishes: one cookie, one session table, one
	// middleware chain. The methods are registered on it below, before the first request.
	authSession, err := session.NewAuth[session.NoCustomData, session.NoCustomData](
		sessionstorage.NewSpannerAuth(db, sessionstorage.WithAuthEvents(events)),
		settings.CookieKey,
		session.WithSessionTableName(sessionsTable),
		session.WithUserTableName(usersTable),
		session.WithIdentitiesTableName(identitiesTable),
		session.WithPendingSignInsTableName(pendingTable),
		session.WithCookieName(Name),
		session.WithXSRFCookieName(XSRFCookie),
		session.WithSessionTimeout(settings.SessionTimeout),
	)
	if err != nil {
		return nil, errors.Wrap(err, "session.NewAuth()")
	}

	handlers := &Handlers{Session: authSession}
{{- range .Methods}}
{{- if .Fallible}}
	handlers.{{.Field}}, err = register{{.Pascal}}(authSession, accessClient, settings)
	if err != nil {
		return nil, err
	}
{{- else if .Directory}}
	handlers.{{.Field}} = register{{.Pascal}}(authSession, accessClient, settings)
{{- else}}
	handlers.{{.Field}} = register{{.Pascal}}(authSession)
{{- end}}
{{- end}}

	return &Auth{ {{- if .Foothold}}db: db, {{end}}access: accessClient, handlers: handlers}, nil
}

// Access returns the auth's permission engine: the handlers check against it, and its
// UserManager writes custom roles, their grants, and role memberships.
func (a *Auth) Access() *access.Client {
	return a.access
}

// Session returns the auth's session manager.
func (a *Auth) Session() *Session {
	return a.handlers.Session
}

// Handlers returns the auth's served surface.
func (a *Auth) Handlers() *Handlers {
	return a.handlers
}

// Close releases the permission engine, which ends its subscription to the policy kind.
func (a *Auth) Close() error {
	if err := a.access.Close(); err != nil {
		return errors.Wrap(err, "access.Client.Close()")
	}

	return nil
}

// announcePolicy is the sending half of the engine's change signal: after a policy write
// (a role, a grant, a membership) the engine signals the policy kind on the live service,
// and every instance subscribed to the kind rereads its snapshot. A failure is the
// engine's to log and never fails the write, since every instance also rereads at its
// heartbeat.
func announcePolicy(signals Signals) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		if err := signals.Signal(ctx, resource.KindPolicy); err != nil {
			return errors.Wrap(err, "resource.Signaler.Signal()")
		}

		return nil
	}
}

// watchPolicy is the receiving half: it subscribes to the policy kind, runs onChange on
// every signal of it until ctx ends, and ends the subscription on return.
func watchPolicy(signals Signals) func(ctx context.Context, onChange func()) error {
	return func(ctx context.Context, onChange func()) error {
		stop, err := signals.Subscribe(resource.KindPolicy, onChange)
		if err != nil {
			return errors.Wrap(err, "resource.SignalSubscriber.Subscribe()")
		}
		defer stop()
		<-ctx.Done()

		return nil
	}
}
`

// loginFormTemplate renders the password method's file: the method that signs in from the
// login page's form.
const loginFormTemplate = `package {{.Auth}}

import (
	"net/http"

	"github.com/cccteam/session"
)

// {{.Registration}} is the name the password method is registered under on the Auth,
// always password: the segment of its login route (user/password/login). A password
// sign-in is checked against the user's hash in the users table; the bootstrap creates
// the development logins with their passwords, and a deployed environment starts from the
// administrator the migrate command seeds (foothold.go), the rest coming from its
// administration surface.
const {{.Registration}} = "password"

// PasswordLogin checks a username and password and starts the session.
func (h *Handlers) PasswordLogin() http.HandlerFunc {
	return h.{{.Field}}.Login()
}

// PasswordChange changes the signed-in user's password, given the old one.
func (h *Handlers) PasswordChange() http.HandlerFunc {
	return h.{{.Field}}.ChangeUserPassword()
}

// register{{.Pascal}} registers the password method on the Auth, before the first request.
func register{{.Pascal}}(authSession *Session) *session.PasswordMethod {
	return session.PasswordSignIn(authSession)
}
`

// footholdTemplate renders the foothold file: the first administrator the deployment
// seeds while the auth's users table holds no rows, in the form the auth's sign-in allows.
const footholdTemplate = `package {{.Name}}

import (
	"context"

	cloudspanner "cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
{{- if or (eq .Foothold "password") .FootholdCreatesUser}}
	"github.com/cccteam/session"
{{- end}}
	"github.com/go-playground/errors/v5"
)
{{if eq .Foothold "password"}}
// Administrator is the username of the auth's first administrator: the foothold a deployed
// environment starts from. The migrate command seeds it while the auth's users table holds
// no rows, with the password the stack generates for it (a generated secret the data level
// declares), and never again while a user exists; the first sign-in changes the password,
// and every user after the first comes from the administration surface. The development
// bootstrap creates its own logins instead (cmd/bootstrap/users.json).
const Administrator = "administrator"
{{end}}
// AdministratorRoles are the roles the first administrator holds, by where they are held:
{{- if .AdministratorAuthored}}
// what the development identities hold (cmd/bootstrap/users.json), a domain role in every
// tenant domain, so the foothold can do what the development logins can.
{{- else}}
// none until they are authored with the role file (roles.json), since the seed names
// nothing the file does not define, and the first administrator holds nothing until then.
{{- end}}
func AdministratorRoles() (global, everyDomain []accesstypes.Role) {
	return {{.AdministratorGlobal}}, {{.AdministratorEveryDomain}}
}
{{if eq .Foothold "password"}}
// SeedAdministrator creates the first administrator when the auth's users table holds no
// rows and reports whether it did. The memberships are written first, since writing one
// that exists is a no-op, then the user: a run that fails between the two leaves the table
// empty, so the next deploy finishes the job. An empty password is refused while the table
// is empty: a deployment carries the generated one, and nothing seeds a user without a
// password.
func (a *Auth) SeedAdministrator(ctx context.Context, password string) (bool, error) {
	empty, err := a.usersTableEmpty(ctx)
	if err != nil {
		return false, err
	}
	if !empty {
		return false, nil
	}
	if password == "" {
		return false, errors.Newf("the %s auth's users table holds no rows and no administrator password was given: a deployment generates one as a secret, and the migrate command seeds nothing without it", Name)
	}
	if err := a.assignAdministrator(ctx, Administrator); err != nil {
		return false, err
	}
	if _, err := a.handlers.API().CreateSessionUser(ctx, &session.CreateUserRequest{Username: Administrator, Password: &password}); err != nil {
		return false, errors.Wrapf(err, "session.AuthAPI.CreateSessionUser(): %s", Administrator)
	}

	return true, nil
}
{{else}}
// SeedAdministrator writes the administrator role memberships under signInName when the
// auth's users table holds no rows and reports whether it did: the foothold a deployed
// environment starts from. The directory proves who someone is and the application assigns
// the roles (signin.DisableRoleSync), so until a membership exists the first person in holds
// nothing; the memberships wait for them under the sign-in name the directory presents (the
// user principal name on Entra, the email on Google), which the placement names per
// environment.{{if .FootholdCreatesUser}} The directory provisions nobody (invited users only), so the user row is
// created too, password-less, for the first sign-in to link.{{else}} The directory provisions the person at their first sign-in, which finds the
// roles.{{end}} Writing a membership that exists is a no-op, so a deploy before anyone has signed
// in writes them again harmlessly, and once a user exists nothing is seeded: every user
// after the first comes from the administration surface. An empty name is refused while the
// table is empty. The development bootstrap seeds the development identities instead.
func (a *Auth) SeedAdministrator(ctx context.Context, signInName string) (bool, error) {
	empty, err := a.usersTableEmpty(ctx)
	if err != nil {
		return false, err
	}
	if !empty {
		return false, nil
	}
	if signInName == "" {
		return false, errors.Newf("the %s auth's users table holds no rows and no first administrator is named: a deployment names one per environment, and the migrate command seeds nothing without it", Name)
	}
	if err := a.assignAdministrator(ctx, signInName); err != nil {
		return false, err
	}
{{- if .FootholdCreatesUser}}
	if _, err := a.handlers.API().CreateSessionUser(ctx, &session.CreateUserRequest{Username: signInName}); err != nil {
		return false, errors.Wrapf(err, "session.AuthAPI.CreateSessionUser(): %s", signInName)
	}
{{- end}}

	return true, nil
}
{{end}}
// assignAdministrator writes the first administrator's memberships under username, each
// where its roles are held; a membership that exists is left as it is.
func (a *Auth) assignAdministrator(ctx context.Context, username string) error {
	global, everyDomain := AdministratorRoles()
	manager := a.access.UserManager()
	if len(global) > 0 {
		if err := manager.AddUserRoles(ctx, accesstypes.GlobalPolicyScope(), accesstypes.User(username), global...); err != nil {
			return errors.Wrapf(err, "access.UserManager.AddUserRoles(): %s in the global partition", username)
		}
	}
	if len(everyDomain) > 0 {
		if err := manager.AddUserRoles(ctx, accesstypes.EveryDomainPolicyScope(), accesstypes.User(username), everyDomain...); err != nil {
			return errors.Wrapf(err, "access.UserManager.AddUserRoles(): %s in every domain", username)
		}
	}

	return nil
}

// usersTableEmpty reads one row of the users table, any row, and reports there is none.
func (a *Auth) usersTableEmpty(ctx context.Context) (bool, error) {
	rows := 0
	iter := a.db.Single().ReadWithOptions(ctx, usersTable, cloudspanner.AllKeys(), []string{"Id"}, &cloudspanner.ReadOptions{Limit: 1})
	if err := iter.Do(func(*cloudspanner.Row) error {
		rows++

		return nil
	}); err != nil {
		return false, errors.Wrapf(err, "spanner.RowIterator.Do(): %s", usersTable)
	}

	return rows == 0, nil
}
`

// azureTemplate renders a Microsoft Entra method's file.
const azureTemplate = `package {{.Auth}}

import (
{{- range .StdImports}}
	"{{.}}"
{{- end}}
{{ range .Imports}}
	"{{.}}"
{{- end}}
)

// {{.Registration}} is the name the {{.Name}} registration goes under on the Auth: the
// segment of its login, callback and front-channel logout routes (user/{{.Name}}/login).
// In development the simulated directory signs it in as {{.SimulatedUsername}}, or the
// shared APP_USERNAME when that is unset, with the roles {{.SimulatedRoles}} or APP_ROLES
// name.
const {{.Registration}} = "{{.Name}}"

// {{.SettingsType}} is how the application is known to Microsoft for the {{.Name}}
// registration, read from the environment. Under the session library's skipAuth build tag
// the directory is simulated and only RedirectURL is read: it is where the simulated
// sign-in returns.
type {{.SettingsType}} struct {
{{- range .Fields}}
	// {{.Name}} is {{.Doc}}.
	{{.Name}} string
{{- end}}
}

// {{.Pascal}}Login sends the browser to Microsoft, to sign in with an account the
// registration admits.
func (h *Handlers) {{.Pascal}}Login() http.HandlerFunc {
	return h.{{.Field}}.Login()
}

// {{.Pascal}}Callback receives the browser back from Microsoft, resolves the identity,
// applies the account rule and starts the session.
func (h *Handlers) {{.Pascal}}Callback() http.HandlerFunc {
	return h.{{.Field}}.Callback()
}

// {{.Pascal}}FrontChannelLogout ends the session Microsoft reports signed out: the
// front-channel logout the app registration names.
func (h *Handlers) {{.Pascal}}FrontChannelLogout() http.HandlerFunc {
	return h.{{.Field}}.FrontChannelLogout()
}

// register{{.Pascal}} registers the {{.Name}} registration on the Auth, before the first
// request.
{{- if eq .Authority "directory"}}
// The directory is the authority for role membership: every sign-in reconciles the
// person's roles to the token's application roles (signin.RoleSync), removing any it does
// not name, and a sign-in naming no known role is refused. Hand-assigned roles do not
// survive it, so nothing in the application assigns roles in this store.
{{- else}}
// The application is the authority for role membership (signin.DisableRoleSync): the
// directory proves who someone is and the application decides what they may do, so a
// sign-in neither reads nor changes the person's roles.
{{- end}}
// The account rule for an identity Microsoft verifies for the first time is the
// library's standard one{{if eq .AccountsExpr "signin.MembersOfDirectory"}}, members of the directory: the person becomes a user under their
// user principal name, with no password, linked by the directory's immutable tenant and
// object ids so a renamed account stays the same person{{else if eq .AccountsExpr "signin.InvitedUsersOnly"}}, invited users only: a user created beforehand under the
// person's user principal name is linked and signed in, anyone else is refused
// (not_invited){{else}}, open sign-up: anyone the slot admits becomes a user under their
// user principal name{{end}}.
func register{{.Pascal}}(authSession *Session, accessClient *access.Client, settings *Settings) (*azure.Method{{if .Fallible}}, error{{end}}) {
{{- if eq .SlotExpr "tenants"}}
	tenants, err := parse{{.Pascal}}Tenants(settings.{{.Pascal}}.Tenants)
	if err != nil {
		return nil, err
	}
{{- end}}
	_ = accessClient

	return azure.SignIn(authSession, {{.Registration}}, nil, &azure.Config{
		Tenants:      {{.SlotExpr}},
		ClientID:     settings.{{.Pascal}}.ClientID,
		ClientSecret: settings.{{.Pascal}}.ClientSecret,
		RedirectURL:  settings.{{.Pascal}}.RedirectURL,
		RoleSync:     {{.RoleSyncExpr}},
		Accounts:     {{.AccountsExpr}},
		LoginURL:     settings.LoginURL,
	}){{if .Fallible}}, nil{{end}}
}
{{- if eq .SlotExpr "tenants"}}

// parse{{.Pascal}}Tenants reads the listed tenants from their setting: <name>=<tenant id>,
// comma-separated. The name is the tenant key every link made through the tenant
// carries, so it is never changed once people have signed in.
func parse{{.Pascal}}Tenants(value string) (azure.TenantSet, error) {
	var sets []azure.TenantSet
	for _, entry := range strings.Split(value, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		name, id, ok := strings.Cut(entry, "=")
		if !ok || name == "" || id == "" {
			return azure.TenantSet{}, errors.Newf("{{.Registration}}: tenant %q is not <name>=<tenant id>", entry)
		}
		sets = append(sets, azure.NamedTenant(strings.TrimSpace(id), strings.TrimSpace(name)))
	}
	if len(sets) == 0 {
		return azure.TenantSet{}, errors.New("{{.Registration}}: no tenant listed; set the tenants as <name>=<tenant id>, comma-separated")
	}

	return azure.Tenants(sets...), nil
}
{{- end}}
`

// googleTemplate renders a Google method's file.
const googleTemplate = `package {{.Auth}}

import (
{{- range .StdImports}}
	"{{.}}"
{{- end}}
{{ range .Imports}}
	"{{.}}"
{{- end}}
)

// {{.Registration}} is the name the {{.Name}} registration goes under on the Auth: the
// segment of its login and callback routes (user/{{.Name}}/login), and the connection every
// link made through its Workspace carries. In development the simulated directory signs
// it in as {{.SimulatedUsername}}, or the shared APP_USERNAME when that is unset, with the
// groups {{.SimulatedRoles}} or APP_ROLES name.
const {{.Registration}} = "{{.Name}}"

// {{.SettingsType}} is how the application is known to Google for the {{.Name}}
// registration, read from the environment. Under the session library's skipAuth build tag
// Google is simulated and only the fields the simulated sign-in presents are read.
type {{.SettingsType}} struct {
{{- range .Fields}}
	// {{.Name}} is {{.Doc}}.
	{{.Name}} string
{{- end}}
}

// {{.Pascal}}Login sends the browser to Google, to sign in with an account the
// registration admits.
func (h *Handlers) {{.Pascal}}Login() http.HandlerFunc {
	return h.{{.Field}}.Login()
}

// {{.Pascal}}Callback receives the browser back from Google, resolves the identity,
// applies the account rule and starts the session. Google has no directory-initiated
// logout, so there is no front-channel route: the session's own logout route ends it.
func (h *Handlers) {{.Pascal}}Callback() http.HandlerFunc {
	return h.{{.Field}}.Callback()
}

// register{{.Pascal}} registers the {{.Name}} registration on the Auth, before the first
// request.
{{- if eq .Authority "directory"}}
// The directory's groups are the authority for role membership (google.RoleSync): every
// sign-in reconciles the person's roles to the Google Groups the directory places them in,
// those named by the group prefix, read through the Cloud Identity Groups API with the
// person's own sign-in token as far as the group lookup reaches, removing any role no
// group names; a sign-in in no role group is refused. Hand-assigned roles do not survive
// it, so nothing in the application assigns roles in this store.
{{- else}}
// The application is the authority for role membership (google.DisableRoleSync): Google
// proves who someone is and the application decides what they may do, so a sign-in
// neither reads nor changes the person's roles.
{{- end}}
// The account rule for an identity Google verifies for the first time is the library's
// standard one{{if eq .AccountsExpr "signin.MembersOfDirectory"}}, members of the directory: the person becomes a user under their
// verified email, with no password, linked by Google's immutable subject so a renamed
// account stays the same person{{else if eq .AccountsExpr "signin.InvitedUsersOnly"}}, invited users only: a user created beforehand under the
// person's verified email is linked and signed in, anyone else is refused
// (not_invited){{else}}, open sign-up: anyone the slot admits becomes a user under their
// verified email{{end}}.
func register{{.Pascal}}(authSession *Session, accessClient *access.Client, settings *Settings) (*google.Method{{if .Fallible}}, error{{end}}) {
{{- if eq .SlotExpr "workspaces"}}
	workspaces, err := parse{{.Pascal}}Workspaces(settings.{{.Pascal}}.Workspaces)
	if err != nil {
		return nil, err
	}
{{- end}}
{{- if eq .Authority "directory"}}
	lookup, err := google.ParseGroupLookup(settings.{{.Pascal}}.GroupLookup)
	if err != nil {
		return nil, errors.Wrap(err, "google.ParseGroupLookup()")
	}
{{- end}}
	_ = accessClient

	return google.SignIn(authSession, {{.Registration}}, nil, &google.Config{
		ClientID:     settings.{{.Pascal}}.ClientID,
		ClientSecret: settings.{{.Pascal}}.ClientSecret,
		RedirectURL:  settings.{{.Pascal}}.RedirectURL,
		Workspaces:   {{.SlotExpr}},
		RoleSync:     {{.RoleSyncExpr}},
		Accounts:     {{.AccountsExpr}},
		LoginURL:     settings.LoginURL,
	}){{if .Fallible}}, nil{{end}}
}
{{- if eq .SlotExpr "workspaces"}}

// parse{{.Pascal}}Workspaces reads the listed Workspaces from their setting:
// <name>=<domain>[+<domain>], comma-separated. The name is the connection every link made
// through the Workspace carries, so it is never changed once people have signed in.
func parse{{.Pascal}}Workspaces(value string) (google.WorkspaceSet, error) {
	var sets []google.WorkspaceSet
	for _, entry := range strings.Split(value, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		name, domains, ok := strings.Cut(entry, "=")
		if !ok || name == "" || domains == "" {
			return google.WorkspaceSet{}, errors.Newf("{{.Registration}}: Workspace %q is not <name>=<domain>[+<domain>]", entry)
		}
		sets = append(sets, google.Workspace(strings.TrimSpace(name), strings.Split(domains, "+")...))
	}
	if len(sets) == 0 {
		return google.WorkspaceSet{}, errors.New("{{.Registration}}: no Workspace listed; set the Workspaces as <name>=<domain>[+<domain>], comma-separated")
	}

	return google.Workspaces(sets...), nil
}
{{- end}}
`
