package authpkg

// secondFactorRule is the sign-in rule every auth package ends with, as the template
// writes it into the package and impulse upgrade's second-factor recipe writes it into an
// auth package that set none (SecondFactorRule).
const secondFactorRule = `// requireSecondFactor is the sign-in rule every session ends with: the second factor, the
// sign-in code mailed to the person's address (sessioninfo.MethodEmailCode), unless the
// sign-in carries one already, a remembered browser's token or the attestation of a
// directory the deployment administers or whose token says a second factor was passed,
// which the library takes before it asks. A sign-in the rule holds waits for the code
// (?pending=mfa on the login page), where the person asks for it, enters it and may ask
// for the browser to be remembered.
func requireSecondFactor(context.Context, *cloudspanner.ReadWriteTransaction, *sessioninfo.NewSessionRequest) (*signin.Decision, error) {
	return signin.RequireMFA(sessioninfo.MethodEmailCode), nil
}
`

// authTemplate renders the Auth file: the package, its tables, its permission store, its
// role file, the Auth session with the methods registered on it, and the served surface.
const authTemplate = `// Package {{.Name}} is the {{.Name}} auth: {{.Doc}}, who hold their roles in the
// {{.Name}} permission store.
//
// An auth is a package. It owns its session manager, one Auth session with the sign-in
// methods registered on it (here {{range $i, $m := .Methods}}{{if $i}}, {{end}}{{$m.Name}}{{end}}), with its own session, user, identity, auth
// event, pending sign-in, sign-in attempt, mail link, sign-in code and trusted device
// tables and cookies; its permission
// store, with its own table prefix, whose engine signals policy changes to the
// application's other instances through the live service; and its role file, embedded
// beside it, so the release's default roles travel with the binary. Each method is its
// own file in this package, named for its registration.
// A site or an outlet binds to an auth by composing its handlers, and two auths on one
// database and one host never collide, because everything an auth names carries its
// name. The same name in this auth and in another is two unrelated principals. The
// package is named for the population, never for how it signs in: the {{.Name}} can gain
// or lose a sign-in method without the package moving.
//
// The auth mails: an administrator invites a person by address, with the roles the
// invitation seats, and the invitee follows the link to accept it; a signed-in person
// adds an address and proves it by following the link sent to it{{if .OpenSignUp}}; and a stranger
// signs up from a link mailed to the address they give{{end}}.
// The links open the login page of the surface that binds to this auth
// (Settings.MailLoginURL), through the sender the data level hands in (Settings.Mail).
//
// Every session carries a second factor. A sign-in the password admitted, or a directory
// the deployment does not administer, waits for the sign-in code the auth mails to the
// person's verified address (a person who holds none gives one first), unless the
// browser is one the person asked to be remembered after a code (the {{.Name}}-trusted
// cookie); a sign-in through a directory the deployment administers, or whose token says
// a second factor was passed, carries the directory's and is asked for nothing. An
// administrator resets the browsers a person asked to be remembered from the users
// surface, so their next sign-in asks again.
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
	"github.com/cccteam/session/mail"
	"github.com/cccteam/session/sessionstorage"
	"github.com/go-playground/errors/v5"
)

const (
	// Name is the auth's name: the cookie its sessions ride in.
	Name = "{{.Name}}"
	// TablePrefix is the name's PascalCase form, which prefixes every table the auth owns.
	TablePrefix = "{{.Pascal}}"
	// Application is the application's name as the mail the auth sends says it: the
	// subject and the first line of an invitation name it.
	Application = "{{.Application}}"

	// XSRFCookie is the cookie the auth issues its XSRF token in. It carries the auth's name
	// so two auths on one host never overwrite each other's token; the browser echoes it in
	// the X-XSRF-TOKEN header, so the web app that binds to this auth names the same cookie.
	XSRFCookie = Name + "-xsrf"
	// TrustedDeviceCookie is the cookie a browser the person asked to be remembered after a
	// sign-in code carries its token in; it carries the auth's name as XSRFCookie does.
	TrustedDeviceCookie = Name + "-trusted"

	// The auth's tables, which schema/migrations creates: the sessions and users of the
	// Auth session (a person is one user whichever way they sign in; an identity the
	// directory verifies links to its user through the identities table), the auth events
	// each session records, the pending sign-ins that wait for a confirmation, the
	// sign-in attempts the password method's attempt limiter counts per sign-in name, and
	// the mail links: the invitations, address verifications and sign-ups the auth mails,
	// each a single-use token its follower proves; the tokens: the bearer credentials of
	// the machine outlets whose tokens this auth issues (the application tokens its people
	// issue and the access and refresh tokens of the device grants they approve); the
	// device codes: the device authorizations command-line clients wait on; and the second
	// factor's tables: the sign-in codes mailed to people and the browsers they asked to
	// be remembered. The names are spelled out in full so the tool's session-tables
	// check, which reads the literals, finds their migrations.
	sessionsTable    = "{{.Pascal}}Sessions"
	usersTable       = "{{.Pascal}}SessionUsers"
	identitiesTable  = "{{.Pascal}}SessionIdentities"
	eventsTable      = "{{.Pascal}}SessionAuthEvents"
	pendingTable     = "{{.Pascal}}SessionPendingSignIns"
	attemptsTable    = "{{.Pascal}}SessionSignInAttempts"
	mailLinksTable   = "{{.Pascal}}SessionMailLinks"
	tokensTable      = "{{.Pascal}}SessionTokens"
	deviceCodesTable = "{{.Pascal}}SessionDeviceCodes"
	factorCodesTable    = "{{.Pascal}}SessionFactorCodes"
	trustedDevicesTable = "{{.Pascal}}SessionTrustedDevices"
{{- range .Outlets}}

	// {{.Pascal}}Outlet is a machine outlet whose tokens the auth issues: the generator
	// program declares it with Tokens of this package, so the generated router composes
	// the auth's token check in front of the outlet's routes, mounts the device flow's
	// endpoints under it, and serves the token routes on the auth's session outlet. A
	// request there carries a bearer token the auth issued: an application token a person
	// issued, or one a command-line client obtained with a person's approval. New
	// registers the outlet on the Auth before the first request, as it registers the
	// sign-in methods.
	{{.Pascal}}Outlet = "{{.Name}}"
{{- end}}
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
	// Mail sends the auth's mail: the invitation an administrator sends to an address{{if .OpenSignUp}},
	// the link that verifies an address a person adds, and the link a sign-up starts from.{{else}}
	// and the link that verifies an address a person adds.{{end}}
	// The application's one sender, which the data level opens: the Workspace relay
	// (session/mail/workspace) where the deployment names a mailbox, and the log (mail.Log)
	// in development, where nothing is sent and a developer follows the link from the log.
	// Required; New refuses nil.
	Mail mail.Sender
	// MailLoginURL is the absolute address of the login page of the surface that binds
	// to this auth (https://host/login), which every mailed link opens with its token in
	// the query: the application's public address and the surface's login path.
	MailLoginURL string
{{- if .Outlets}}
	// DevicePage is the console page where a signed-in person approves a command-line
	// client's request to act as them, which a device authorization tells the client
	// (verification_uri): a path in the application, the mount path of the surface that
	// binds to this auth joined with /device ("/console/device"), where the console
	// mounts the library's device approval page.
	DevicePage string
{{- end}}
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
{{- if .TenantPolicy}}
	// TenantDeclarations reads what a tenant declares about who may enter it, in the
	// sign-in's transaction, which the sign-in rule decides with: the generated reader
	// over the {{.TenantPolicy.Record}} record and its directories ({{.TenantPolicy.Reader}}).
	// Required; New refuses nil.
	TenantDeclarations signin.DeclarationReader[*cloudspanner.ReadWriteTransaction]
{{- end}}
{{- if .DirectoryAdmission}}
	// TenantDirectories reads every tenant's declaration of one directory with the
	// default seat its row carries, in the sign-in's transaction, which the directory
	// admission decides a stranger's first sign-in with: the generated reader
	// ({{.TenantPolicy.DirectoryReader}}). Required; New refuses nil.
	TenantDirectories signin.DirectoryReader[*cloudspanner.ReadWriteTransaction]
{{- end}}
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
// the pending sign-in handlers, each method's handlers under its registration name
// (in the method's own file){{if .Outlets}}, and the token check of each machine outlet whose tokens
// the auth issues{{end}}.
type Handlers struct {
	*Session
{{- range .Methods}}
	{{.Field}} {{.Type}}
{{- end}}
{{- range .Outlets}}
	{{.Name}} *session.MachineOutlet
{{- end}}
}
{{- range .Outlets}}

// {{.Pascal}}Tokens returns the {{.Name}} outlet's token check (router.{{.Pascal}}Tokens): the
// machine outlet registered on the Auth under {{.Pascal}}Outlet, whose ValidateToken binds
// each request under the outlet's prefix to the person its bearer token acts for.
func (h *Handlers) {{.Pascal}}Tokens() *session.MachineOutlet {
	return h.{{.Name}}
}
{{- end}}

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

// PendingSendCode mails the sign-in code to the address of the person whose sign-in
// waits for the second factor, and again when they ask for another.
func (h *Handlers) PendingSendCode() http.HandlerFunc {
	return h.Pending().SendCode()
}

// PendingVerifyCode completes a waiting sign-in with the code the person was mailed,
// remembering the browser when they ask.
func (h *Handlers) PendingVerifyCode() http.HandlerFunc {
	return h.Pending().VerifyCode()
}

// PendingEnrollEmail takes the address the sign-in code goes to, from a person whose
// sign-in waits for the second factor and who holds no address yet.
func (h *Handlers) PendingEnrollEmail() http.HandlerFunc {
	return h.Pending().EnrollEmail()
}

// InvitationOpen reads the invitation a mailed link carries: where it seats the invitee
// and how they may accept it, for the login page to offer.
func (h *Handlers) InvitationOpen() http.HandlerFunc {
	return h.Invitation().Open()
}

// InvitationAccept accepts an invitation as the signed-in person, or as a person who
// signs in through a registration the invitation admits: the roles it names are seated
// once the link is consumed.
func (h *Handlers) InvitationAccept() http.HandlerFunc {
	return h.Invitation().Accept()
}

// EmailAdd starts an address change for the signed-in person: a verification link is
// mailed to the address, and nothing is stored until it is followed.
func (h *Handlers) EmailAdd() http.HandlerFunc {
	return h.Email().Add()
}

// EmailVerify completes an address change from its link: the address is stored verified.
func (h *Handlers) EmailVerify() http.HandlerFunc {
	return h.Email().Verify()
}

// DeviceAuthorize starts a command-line client's device authorization ({"client_id",
// "outlet"}, form-encoded or JSON): the client is told the code the person types and
// the device code it polls with. The generated router mounts it under the machine
// outlet whose tokens this auth issues, in front of the token check.
func (h *Handlers) DeviceAuthorize() http.HandlerFunc {
	return h.Device().Authorize()
}

// DeviceToken answers the client's polls (grant_type device_code) with its access and
// refresh tokens once a person approved, and exchanges a refresh token for the next
// pair (grant_type refresh_token); a rotated refresh token presented again revokes the
// grant's family. Mounted beside DeviceAuthorize.
func (h *Handlers) DeviceToken() http.HandlerFunc {
	return h.Device().Token()
}

// DeviceLookup answers the approval page which client asks, for which outlet, for the
// code the person typed (?userCode=).
func (h *Handlers) DeviceLookup() http.HandlerFunc {
	return h.Device().Lookup()
}

// DeviceApprove approves the device authorization the signed-in person's code names
// ({"userCode"}), as them: the client's next poll receives tokens acting as them.
func (h *Handlers) DeviceApprove() http.HandlerFunc {
	return h.Device().Approve()
}

// DeviceDeny denies the device authorization the code names ({"userCode"}).
func (h *Handlers) DeviceDeny() http.HandlerFunc {
	return h.Device().Deny()
}

// TokensList lists the signed-in person's live tokens: the application tokens they
// issued and the tokens of the device grants they approved, never a secret.
func (h *Handlers) TokensList() http.HandlerFunc {
	return h.ApplicationTokens().List()
}

// TokensCreate issues an application token for a program of the signed-in person's own
// ({"name", "outlet", "expiresIn"}), the secret shown this once.
func (h *Handlers) TokensCreate() http.HandlerFunc {
	return h.ApplicationTokens().Create()
}

// TokensRevoke revokes one of the signed-in person's tokens ({"id"}), a device grant's
// with its family.
func (h *Handlers) TokensRevoke() http.HandlerFunc {
	return h.ApplicationTokens().Revoke()
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
	if settings.Mail == nil {
		return nil, errors.New("{{.Name}}.Settings.Mail is nil: the auth mails its invitations and address verifications{{if .OpenSignUp}} and its sign-up links{{end}} through the application's sender (the Workspace relay deployed, the log in development), which the data level opens")
	}
{{- if .TenantPolicy}}
	if settings.TenantDeclarations == nil {
		return nil, errors.New("{{.Name}}.Settings.TenantDeclarations is nil: the sign-in rule reads what each tenant declares about who may enter it through the generated reader ({{.TenantPolicy.Reader}}), which the data level hands in")
	}
{{- end}}
{{- if .DirectoryAdmission}}
	if settings.TenantDirectories == nil {
		return nil, errors.New("{{.Name}}.Settings.TenantDirectories is nil: the directory admission reads what the tenants declare about one directory, its default seat included, through the generated reader ({{.TenantPolicy.DirectoryReader}}), which the data level hands in")
	}
{{- end}}
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
		session.WithSignInAttemptsTableName(attemptsTable),
		session.WithMailLinksTableName(mailLinksTable),
		session.WithTokensTableName(tokensTable),
		session.WithDeviceCodesTableName(deviceCodesTable),
		session.WithFactorCodesTableName(factorCodesTable),
		session.WithTrustedDevicesTableName(trustedDevicesTable),
		session.WithTrustedDeviceCookieName(TrustedDeviceCookie),
		session.WithMail(settings.Mail, session.MailSettings{Application: Application, LoginURL: settings.MailLoginURL}),
		session.WithCookieName(Name),
		session.WithXSRFCookieName(XSRFCookie),
		session.WithSessionTimeout(settings.SessionTimeout),
{{- if .Outlets}}
		session.WithDeviceVerificationURL(settings.DevicePage),
{{- end}}
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
{{- range .Outlets}}
	// The {{.Name}} outlet, registered before the first request as the methods are: its
	// token check is what the generated router composes in front of the outlet's routes.
	// Registration closes with the first use of the Auth, so it happens here rather than
	// where the App is built, which a bootstrap or a test harness may reach after
	// creating users through the API.
	handlers.{{.Name}} = session.RegisterMachineOutlet(authSession, {{.Pascal}}Outlet)
{{- end}}
{{- if .TenantPolicy}}

	// The sign-in rule: the library's tenant policy. A person's tenants are where they
	// hold a role (the engine's memberships), and what each tenant admits is its
	// declaration, read in the sign-in's transaction: a password is admitted unless
	// every one of the person's tenants admits its people through directories only, a
	// sign-in through a directory when one of their tenants declares it (its method and
	// the connection its identities carry); the application's own staff (a global role,
	// or one held in every tenant) are admitted on the library's checks.
{{- if .TenantPolicy.AdmitWithoutTenant}}
	// A person who holds no tenant is admitted too (signin.AdmitWithoutTenant), since
	// the {{.TenantPolicy.Record}} record's creation shape lets a stranger sign in first and create
	// their own; an application whose administrator alone creates tenants drops the
	// option, and such a person is refused (no_tenant).
{{- else}}
	// A person who holds no tenant is refused (no_tenant), since the administrator
	// creates tenants here; a record created by its own people (@tenant(creation:
	// self-service) or both) adds signin.AdmitWithoutTenant().
{{- end}}
	// Which tenants the session then reaches is decided again, tenant by tenant, where
	// its tenant list is computed (UserPermissions). The memberships are read outside
	// the sign-in's transaction (resource.SignInRoles): the Spanner client refuses the
	// engine's read under the transaction's context as a nested transaction. A sign-in
	// the rule admits then carries a second factor (requireSecondFactor).
	authSession.SetSignInPolicy(signin.TenantPolicy(resource.SignInRoles(accessClient.UserManager()), settings.TenantDeclarations{{if .TenantPolicy.AdmitWithoutTenant}}, signin.AdmitWithoutTenant(){{end}}).Then(requireSecondFactor))
{{- else}}

	// The sign-in rule: every session carries a second factor (requireSecondFactor).
	authSession.SetSignInPolicy(requireSecondFactor)
{{- end}}
{{- if .DirectoryAdmission}}

	// The directory admission: what a stranger gets at their first sign-in through a
	// directory a tenant declares, before the registration's account rule is reached.
	// The directory rows the tenants declare for the identity's connection decide: a
	// row with a default seat creates the person and seats them with it (written
	// through the engine once the sign-in's transaction has committed, resource.SeatDeclared,
	// before the sign-in rule decides, so the rule finds the membership); rows with
	// none refuse them (invitation_only) and no account is created, unless they
	// followed an invitation link, which is the way in; a person already seated
	// somewhere is the tenant's own and passes. A directory no tenant declares falls to
	// the registration's rule. The auth's roles are the application's, so the admission
	// is set here; an auth whose directory writes the roles sets none.
	authSession.SetDirectoryAdmission(settings.TenantDirectories, resource.SignInRoles(accessClient.UserManager()), resource.SeatDeclared(accessClient.UserManager()))
{{- end}}

	// An invitation accepted seats its roles: once the transaction that consumed the
	// invitation has committed, the hook writes the invitee's memberships through the
	// permission engine, outside the sign-in's transaction (the engine's own read-write
	// transaction would wait on it), and the sign-in rule that follows finds them.
	authSession.SetInvitationAccepted(resource.SeatInvited(accessClient.UserManager()))
{{- if .UsernameIsEmail}}
	// The sign-in name is the address, so a verified change of address renames the user,
	// and the seats held under the old name follow it.
	authSession.SetUserRenamed(resource.SeatsFollowRename(accessClient.UserManager()))
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

` + secondFactorRule + `
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
// administration surface{{if .OpenSignUp}}, from the invitations it mails, and from the strangers who
// sign up from a mailed link{{else}} and from the invitations it mails{{end}}.
const {{.Registration}} = "password"

// PasswordLogin checks a username and password and starts the session.
func (h *Handlers) PasswordLogin() http.HandlerFunc {
	return h.{{.Field}}.Login()
}

// PasswordChange changes the signed-in user's password, given the old one.
func (h *Handlers) PasswordChange() http.HandlerFunc {
	return h.{{.Field}}.ChangeUserPassword()
}

// InvitationSignUp accepts an invitation as a stranger with a new password: the invitee
// becomes a user under the sign-in name they give{{if .UsernameIsEmail}}, the invited address{{end}}, with the
// address verified by the link, seated in the roles the invitation names.
func (h *Handlers) InvitationSignUp() http.HandlerFunc {
	return h.Invitation().SignUp()
}
{{- if .OpenSignUp}}

// SignUpStart mails a sign-up link to the address a stranger gives; an address already
// held gets a notice instead, and the caller learns neither.
func (h *Handlers) SignUpStart() http.HandlerFunc {
	return h.SignUp().Start()
}

// SignUpOpen reads the sign-up a mailed link carries, for the login page to offer the
// form.
func (h *Handlers) SignUpOpen() http.HandlerFunc {
	return h.SignUp().Open()
}

// SignUpComplete creates the user from the link, with the password they choose and the
// address verified, and starts their session.
func (h *Handlers) SignUpComplete() http.HandlerFunc {
	return h.SignUp().Complete()
}
{{- end}}

// register{{.Pascal}} registers the password method on the Auth, before the first request.
{{- if .OpenSignUp}}
// Open sign-up (session.OpenSignUp): a stranger gives their address, follows the link
// mailed to it and chooses a password, and the router serves the sign-up routes.
{{- end}}
{{- if .UsernameIsEmail}}
// The sign-in name is the person's email address (session.UsernameIsEmail): the users
// surface derives the address from it, and a verified change of address renames the user.
{{- end}}
func register{{.Pascal}}(authSession *Session) *session.PasswordMethod {
	return session.PasswordSignIn(authSession{{if .OpenSignUp}}, session.OpenSignUp(){{end}}{{if .UsernameIsEmail}}, session.UsernameIsEmail(){{end}})
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
// survive it, so nothing in the application assigns roles in this store, and the
// registration's declaration on every outlet chains .SyncsRoles(), so the generated
// users surface shows these people's roles as the directory's and never writes them.
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
{{- if .SlotFallible}}
	tenants, err := {{.Field}}Slot(settings)
	if err != nil {
		return nil, err
	}
{{- else if .Administered}}
	tenants := {{.Field}}Slot(settings)
{{- else}}
	tenants := {{.Field}}Slot()
{{- end}}
	_ = accessClient

	return azure.SignIn(authSession, {{.Registration}}, nil, &azure.Config{
		Tenants:      tenants,
		ClientID:     settings.{{.Pascal}}.ClientID,
		ClientSecret: settings.{{.Pascal}}.ClientSecret,
		RedirectURL:  settings.{{.Pascal}}.RedirectURL,
		RoleSync:     {{.RoleSyncExpr}},
		Accounts:     {{.AccountsExpr}},
		LoginURL:     settings.LoginURL,
	}){{if .Fallible}}, nil{{end}}
}
{{- if .Administered}}

// {{.Field}}Slot is the registration's organization slot, {{.SlotWords}}, with the
// tenants the deployment administers declared as attesting (AdministeredTenants): a
// sign-in from one carries the tenant's own second factor, which the deployment vouches
// for, and is not asked for the sign-in code. bedrock reads the registration's tenant
// shape from this function, for the app registration's supported account types.
{{- if eq .SlotKind "one" "one-personal"}}
func {{.Field}}Slot(settings *Settings) (azure.TenantSet, error) {
	tenant := azure.Tenant(settings.{{.Pascal}}.TenantID)
	for _, id := range parse{{.Pascal}}Administered(settings.{{.Pascal}}.AdministeredTenants) {
		if !strings.EqualFold(id, settings.{{.Pascal}}.TenantID) {
			return azure.TenantSet{}, errors.Newf("{{.Registration}}: the administered tenant %q is not the tenant admitted, %s", id, settings.{{.Pascal}}.TenantID)
		}
		tenant = tenant.Attesting()
	}

	return {{if eq .SlotKind "one"}}tenant{{else}}azure.Tenants(tenant, azure.PersonalAccounts()){{end}}, nil
}
{{- else if eq .SlotKind "listed"}}
func {{.Field}}Slot(settings *Settings) (azure.TenantSet, error) {
	return parse{{.Pascal}}Tenants(settings.{{.Pascal}}.Tenants, parse{{.Pascal}}Administered(settings.{{.Pascal}}.AdministeredTenants))
}
{{- else}}
func {{.Field}}Slot(settings *Settings) azure.TenantSet {
	administered := parse{{.Pascal}}Administered(settings.{{.Pascal}}.AdministeredTenants)
	sets := make([]azure.TenantSet, 0, len(administered)+{{if eq .SlotKind "any-personal"}}2{{else}}1{{end}})
	sets = append(sets, azure.AnyTenant(){{if eq .SlotKind "any-personal"}}, azure.PersonalAccounts(){{end}})
	for _, id := range administered {
		sets = append(sets, azure.Tenant(id).Attesting())
	}

	return azure.Tenants(sets...)
}
{{- end}}

// parse{{.Pascal}}Administered reads the administered tenants from their setting: the
// tenant ids, comma-separated, lower-cased; none for an empty setting.
func parse{{.Pascal}}Administered(value string) []string {
	var ids []string
	for _, id := range strings.Split(value, ",") {
		if id = strings.ToLower(strings.TrimSpace(id)); id != "" {
			ids = append(ids, id)
		}
	}

	return ids
}
{{- else}}

// {{.Field}}Slot is the registration's organization slot, {{.SlotWords}}: there is no
// organization to administer, so it reads no setting. bedrock reads the registration's
// tenant shape from this function, as it does from every registration's slot function.
func {{.Field}}Slot() azure.TenantSet {
	return azure.PersonalAccounts()
}
{{- end}}
{{- if eq .SlotKind "listed"}}

// parse{{.Pascal}}Tenants reads the listed tenants from their setting: <name>=<tenant id>,
// comma-separated, each declared attesting when its id is among the administered. The
// name is the tenant key every link made through the tenant carries, so it is never
// changed once people have signed in. An administered tenant that is not listed is
// refused.
func parse{{.Pascal}}Tenants(value string, administered []string) (azure.TenantSet, error) {
	var sets []azure.TenantSet
	attesting := map[string]bool{}
	for _, entry := range strings.Split(value, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		name, id, ok := strings.Cut(entry, "=")
		if !ok || name == "" || id == "" {
			return azure.TenantSet{}, errors.Newf("{{.Registration}}: tenant %q is not <name>=<tenant id>", entry)
		}
		id = strings.TrimSpace(id)
		tenant := azure.NamedTenant(id, strings.TrimSpace(name))
		if slices.Contains(administered, strings.ToLower(id)) {
			tenant = tenant.Attesting()
			attesting[strings.ToLower(id)] = true
		}
		sets = append(sets, tenant)
	}
	if len(sets) == 0 {
		return azure.TenantSet{}, errors.New("{{.Registration}}: no tenant listed; set the tenants as <name>=<tenant id>, comma-separated")
	}
	for _, id := range administered {
		if !attesting[id] {
			return azure.TenantSet{}, errors.Newf("{{.Registration}}: the administered tenant %q is not among the tenants listed", id)
		}
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
// it, so nothing in the application assigns roles in this store, and the registration's
// declaration on every outlet chains .SyncsRoles(), so the generated users surface shows
// these people's roles as the directory's and never writes them.
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
{{- if eq .Slot "any"}}
// The slot admits any Google account, with any Workspace named beside it: a Workspace
// account's identity then carries its Workspace's domain as its connection, which is
// what a tenant's declaration names a Google directory by, and an account outside a
// Workspace carries the provider itself (google.AnyAccountConnection).
{{- end}}
func register{{.Pascal}}(authSession *Session, accessClient *access.Client, settings *Settings) (*google.Method{{if .Fallible}}, error{{end}}) {
{{- if .SlotFallible}}
	workspaces, err := {{.Field}}Slot(settings)
	if err != nil {
		return nil, err
	}
{{- else}}
	workspaces := {{.Field}}Slot(settings)
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
		Workspaces:   workspaces,
		RoleSync:     {{.RoleSyncExpr}},
		Accounts:     {{.AccountsExpr}},
		LoginURL:     settings.LoginURL,
	}){{if .Fallible}}, nil{{end}}
}

// {{.Field}}Slot is the registration's organization slot, {{.SlotWords}}, with the
// Workspaces the deployment administers declared as attesting (AdministeredWorkspaces):
// a sign-in from one carries the Workspace's own second factor, which the deployment
// vouches for, and is not asked for the sign-in code.
{{- if eq .SlotKind "one" "mixed"}}
func {{.Field}}Slot(settings *Settings) (google.WorkspaceSet, error) {
	workspace := google.Workspace({{.Registration}}, settings.{{.Pascal}}.HostedDomain)
	for _, domain := range parse{{.Pascal}}Administered(settings.{{.Pascal}}.AdministeredWorkspaces) {
		if !strings.EqualFold(domain, settings.{{.Pascal}}.HostedDomain) {
			return google.WorkspaceSet{}, errors.Newf("{{.Registration}}: the administered Workspace %q is not the Workspace admitted, %s", domain, settings.{{.Pascal}}.HostedDomain)
		}
		workspace = workspace.Attesting()
	}

	return {{if eq .SlotKind "one"}}workspace{{else}}google.Workspaces(workspace, google.AnyGoogleAccount()){{end}}, nil
}
{{- else if eq .SlotKind "listed"}}
func {{.Field}}Slot(settings *Settings) (google.WorkspaceSet, error) {
	return parse{{.Pascal}}Workspaces(settings.{{.Pascal}}.Workspaces, parse{{.Pascal}}Administered(settings.{{.Pascal}}.AdministeredWorkspaces))
}
{{- else}}
func {{.Field}}Slot(settings *Settings) google.WorkspaceSet {
	administered := parse{{.Pascal}}Administered(settings.{{.Pascal}}.AdministeredWorkspaces)
	sets := make([]google.WorkspaceSet, 0, len(administered)+{{if eq .SlotKind "any"}}2{{else}}1{{end}})
	sets = append(sets, google.AnyWorkspace(){{if eq .SlotKind "any"}}, google.AnyGoogleAccount(){{end}})
	for _, domain := range administered {
		// Named by its domain: the connection a Workspace account's link carries under the
		// open entry, so the links made before the Workspace was declared keep theirs.
		sets = append(sets, google.Workspace(domain, domain).Attesting())
	}

	return google.Workspaces(sets...)
}
{{- end}}

// parse{{.Pascal}}Administered reads the administered Workspaces from their setting: the
// domains, comma-separated, lower-cased; none for an empty setting.
func parse{{.Pascal}}Administered(value string) []string {
	var domains []string
	for _, domain := range strings.Split(value, ",") {
		if domain = strings.ToLower(strings.TrimSpace(domain)); domain != "" {
			domains = append(domains, domain)
		}
	}

	return domains
}
{{- if eq .SlotKind "listed"}}

// parse{{.Pascal}}Workspaces reads the listed Workspaces from their setting:
// <name>=<domain>[+<domain>], comma-separated, each declared attesting when one of its
// domains is among the administered. The name is the connection every link made through
// the Workspace carries, so it is never changed once people have signed in. An
// administered Workspace that is not listed is refused.
func parse{{.Pascal}}Workspaces(value string, administered []string) (google.WorkspaceSet, error) {
	var sets []google.WorkspaceSet
	attesting := map[string]bool{}
	for _, entry := range strings.Split(value, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		name, domains, ok := strings.Cut(entry, "=")
		if !ok || name == "" || domains == "" {
			return google.WorkspaceSet{}, errors.Newf("{{.Registration}}: Workspace %q is not <name>=<domain>[+<domain>]", entry)
		}
		workspace := google.Workspace(strings.TrimSpace(name), strings.Split(domains, "+")...)
		for _, domain := range strings.Split(domains, "+") {
			if slices.Contains(administered, strings.ToLower(domain)) {
				workspace = workspace.Attesting()
				attesting[strings.ToLower(domain)] = true
			}
		}
		sets = append(sets, workspace)
	}
	if len(sets) == 0 {
		return google.WorkspaceSet{}, errors.New("{{.Registration}}: no Workspace listed; set the Workspaces as <name>=<domain>[+<domain>], comma-separated")
	}
	for _, domain := range administered {
		if !attesting[domain] {
			return google.WorkspaceSet{}, errors.Newf("{{.Registration}}: the administered Workspace %q is not among the Workspaces listed", domain)
		}
	}

	return google.Workspaces(sets...), nil
}
{{- end}}
`

// SecondFactorRule is the text of requireSecondFactor, the sign-in rule every auth
// package ends with, as the auth template writes it: a package-level function over the
// Spanner transaction (imported as cloudspanner), sessioninfo and signin, returning
// signin.RequireMFA(sessioninfo.MethodEmailCode). impulse upgrade's second-factor recipe
// and impulse check --fix write it into an auth package that has none.
func SecondFactorRule() string {
	return secondFactorRule
}

// personaTemplate renders the persona method's file: the session library's generic OpenID
// Connect method against the persona provider, registered only where the provider is
// configured.
const personaTemplate = `package {{.Auth}}

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
// link made through the persona provider carries. In development the simulated provider
// signs it in as {{.SimulatedUsername}}, or the shared APP_USERNAME when that is unset, with
// the roles {{.SimulatedRoles}} or APP_ROLES name.
const {{.Registration}} = "{{.Name}}"

// {{.SettingsType}} is how the application is known to the persona provider for the
// {{.Name}} registration, read from the environment. The provider is configured in the
// first environment and its pull requests alone: an empty Issuer leaves the method
// unregistered, so a later environment never offers it, and nothing in the build differs
// between them. Under the session library's skipAuth build tag the provider is simulated
// and only the fields the simulated sign-in presents are read.
type {{.SettingsType}} struct {
{{- range .Fields}}
	// {{.Name}} is {{.Doc}}.
	{{.Name}} string
{{- end}}
}

// {{.Pascal}}Login sends the browser to the persona provider, where the person picks a
// persona of this deployment's cast, or is signed in straight away as the persona the
// login_hint query parameter names. Nil where the method is not registered: the generated
// router mounts the route and tells the login page of the method only where it is.
func (h *Handlers) {{.Pascal}}Login() http.HandlerFunc {
	if h.{{.Field}} == nil {
		return nil
	}

	return h.{{.Field}}.Login()
}

// {{.Pascal}}Callback receives the browser back from the provider, resolves the persona's
// identity, links it to the seeded account of the same login and starts the session. Nil
// where the method is not registered.
func (h *Handlers) {{.Pascal}}Callback() http.HandlerFunc {
	if h.{{.Field}} == nil {
		return nil
	}

	return h.{{.Field}}.Callback()
}

// register{{.Pascal}} registers the {{.Name}} registration on the Auth, before the first
// request, where the provider is configured, and answers nil where it is not: the issuer
// is a placement value of the first environment and its pull requests, so a later
// environment never registers the method. The provider is a standard OpenID Connect
// issuer and the application a public client: no secret, and PKCE binds the code to the
// login that asked for it. The application is the authority for role membership
// (signin.DisableRoleSync): the provider says which persona signed in, and the cast the
// migrate command seeded says what the persona may do. The provider attests (Attesting): the person signed in to the provider's host to
// pick a persona, which is the whole proof in a test environment, so a persona sign-in is
// never asked for the sign-in code. The account rule is invited users
// only: the persona's identity (its login as the subject) is linked to the account seeded
// under that login (cast.json, SeedCast), and a persona the seed does not know is refused.
func register{{.Pascal}}(authSession *Session, accessClient *access.Client, settings *Settings) *oidc.Method {
	_ = accessClient
	if settings.{{.Pascal}}.Issuer == "" {
		return nil
	}

	return oidc.SignIn(authSession, {{.Registration}}, nil, &oidc.Config{
		Issuer:      settings.{{.Pascal}}.Issuer,
		ClientID:    settings.{{.Pascal}}.ClientID,
		RedirectURL: settings.{{.Pascal}}.RedirectURL,
		Connection:  {{.Registration}},
		Attesting:   true,
		RoleSync:    {{.RoleSyncExpr}},
		Accounts:    {{.AccountsExpr}},
		LoginURL:    settings.LoginURL,
	})
}
`

// castTemplate renders the persona method's cast.go: the cast file embedded, read for the
// deploy's registration and seeded by the migrate command.
const castTemplate = `package {{.Auth}}

import (
	"context"
	_ "embed" // the cast rides in the binary

	"github.com/cccteam/access"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/impulse/persona"
	"github.com/cccteam/httpio"
	"github.com/cccteam/session"
	"github.com/go-playground/errors/v5"
)

// castFile is the cast of the persona sign-in: the personas a test environment seeds and
// the persona provider offers, declared beside this package in cast.json (login, name,
// address and roles, each role one the role file defines). The deploy registers the cast
// with the provider after traffic (bedrock deploy persona register), and the migrate command
// seeds its accounts in the environments that take the seed (SeedCast).
//
//go:embed cast.json
var castFile []byte

// Cast reads the cast file.
func Cast() (*persona.Cast, error) {
	cast, err := persona.ReadCast(castFile)
	if err != nil {
		return nil, errors.Wrap(err, "persona.ReadCast(): cast.json")
	}

	return cast, nil
}

// SeedCast seeds the cast: a password-less user under each persona's login, with the
// persona's address, seated in the roles the cast names, a global role in the global
// partition and a domain role in every tenant domain, as the first administrator's roles
// are. A persona whose user exists is left as it is, so every deploy seeds the cast again
// harmlessly and a persona added to the cast arrives at the next one. The memberships are
// written first, since writing one that exists is a no-op, then the user: a run that fails
// between the two leaves no user, so the next deploy finishes the job. The persona sign-in
// links the provider's identity to the user of the same login at the first sign-in
// (invited users only), so a persona the seed never made is refused there. It answers how
// many users it created.
func (a *Auth) SeedCast(ctx context.Context) (int, error) {
	cast, err := Cast()
	if err != nil {
		return 0, err
	}
	roles, err := Roles().Parse()
	if err != nil {
		return 0, errors.Wrap(err, "access.RoleFile.Parse()")
	}
	created := 0
	for i := range cast.Personas {
		p := &cast.Personas[i]
		if _, err := a.handlers.API().UserByUsername(ctx, p.Login); err == nil {
			continue
		} else if !httpio.HasNotFound(err) {
			return created, errors.Wrapf(err, "session.AuthAPI.UserByUsername(): %s", p.Login)
		}
		if err := a.seatPersona(ctx, roles, p); err != nil {
			return created, err
		}
		req := &session.CreateUserRequest{Username: p.Login}
		if p.Email != "" {
			req.Email = &p.Email
		}
		if _, err := a.handlers.API().CreateSessionUser(ctx, req); err != nil {
			return created, errors.Wrapf(err, "session.AuthAPI.CreateSessionUser(): %s", p.Login)
		}
		created++
	}

	return created, nil
}

// seatPersona writes a persona's memberships, each role where the role file holds it: a
// global role in the global partition, a domain role in every tenant domain. A role the
// file does not define is refused, since the seed names nothing the release does not.
func (a *Auth) seatPersona(ctx context.Context, roles *access.RoleConfig, p *persona.Persona) error {
	var global, everyDomain []accesstypes.Role
	for _, name := range p.Roles {
		role := accesstypes.Role(name)
		switch {
		case definesRole(roles.Roles.Global, role):
			global = append(global, role)
		case definesRole(roles.Roles.Domain, role):
			everyDomain = append(everyDomain, role)
		default:
			return errors.Newf("cast.json: the persona %s holds the role %q, which the %s auth's role file does not define", p.Login, name, Name)
		}
	}
	manager := a.access.UserManager()
	if len(global) > 0 {
		if err := manager.AddUserRoles(ctx, accesstypes.GlobalPolicyScope(), accesstypes.User(p.Login), global...); err != nil {
			return errors.Wrapf(err, "access.UserManager.AddUserRoles(): %s in the global partition", p.Login)
		}
	}
	if len(everyDomain) > 0 {
		if err := manager.AddUserRoles(ctx, accesstypes.EveryDomainPolicyScope(), accesstypes.User(p.Login), everyDomain...); err != nil {
			return errors.Wrapf(err, "access.UserManager.AddUserRoles(): %s in every domain", p.Login)
		}
	}

	return nil
}

// definesRole reports whether the role file defines the role at the scope.
func definesRole(defined []*access.Role, role accesstypes.Role) bool {
	for _, r := range defined {
		if r.Name == role {
			return true
		}
	}

	return false
}
`
