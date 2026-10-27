// Package staff is the staff auth: the people who sign in to the console with a password, or with any Google account to create their own tenant, who hold their roles in the
// staff permission store.
//
// An auth is a package. It owns its session manager, one Auth session with the sign-in
// methods registered on it (here password, google), with its own session, user, identity, auth
// event, pending sign-in, sign-in attempt and mail link tables and cookie; its permission
// store, with its own table prefix, whose engine signals policy changes to the
// application's other instances through the live service; and its role file, embedded
// beside it, so the release's default roles travel with the binary. Each method is its
// own file in this package, named for its registration.
// A site or an outlet binds to an auth by composing its handlers, and two auths on one
// database and one host never collide, because everything an auth names carries its
// name. The same name in this auth and in another is two unrelated principals. The
// package is named for the population, never for how it signs in: the staff can gain
// or lose a sign-in method without the package moving.
//
// The auth mails: an administrator invites a person by address, with the roles the
// invitation seats, and the invitee follows the link to accept it; a signed-in person
// adds an address and proves it by following the link sent to it.
// The links open the login page of the surface that binds to this auth
// (Settings.MailLoginURL), through the sender the data level hands in (Settings.Mail).
package staff

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
	"github.com/cccteam/session/google"
	"github.com/cccteam/session/mail"
	"github.com/cccteam/session/sessionstorage"
	"github.com/cccteam/session/signin"
	"github.com/go-playground/errors/v5"
)

const (
	// Name is the auth's name: the cookie its sessions ride in.
	Name = "staff"
	// TablePrefix is the name's PascalCase form, which prefixes every table the auth owns.
	TablePrefix = "Staff"
	// Application is the application's name as the mail the auth sends says it: the
	// subject and the first line of an invitation name it.
	Application = "Tenanted"

	// XSRFCookie is the cookie the auth issues its XSRF token in. It carries the auth's name
	// so two auths on one host never overwrite each other's token; the browser echoes it in
	// the X-XSRF-TOKEN header, so the web app that binds to this auth names the same cookie.
	XSRFCookie = Name + "-xsrf"

	// The auth's tables, which schema/migrations creates: the sessions and users of the
	// Auth session (a person is one user whichever way they sign in; an identity the
	// directory verifies links to its user through the identities table), the auth events
	// each session records, the pending sign-ins that wait for a confirmation, the
	// sign-in attempts the password method's attempt limiter counts per sign-in name, and
	// the mail links: the invitations, address verifications and sign-ups the auth mails,
	// each a single-use token its follower proves; the tokens: the bearer credentials of
	// the machine outlets whose tokens this auth issues (the application tokens its people
	// issue and the access and refresh tokens of the device grants they approve); and the
	// device codes: the device authorizations command-line clients wait on. The names are
	// spelled out in full so the tool's session-tables check, which reads the literals,
	// finds their migrations.
	sessionsTable    = "StaffSessions"
	usersTable       = "StaffSessionUsers"
	identitiesTable  = "StaffSessionIdentities"
	eventsTable      = "StaffSessionAuthEvents"
	pendingTable     = "StaffSessionPendingSignIns"
	attemptsTable    = "StaffSessionSignInAttempts"
	mailLinksTable   = "StaffSessionMailLinks"
	tokensTable      = "StaffSessionTokens"
	deviceCodesTable = "StaffSessionDeviceCodes"
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
	// Mail sends the auth's mail: the invitation an administrator sends to an address
	// and the link that verifies an address a person adds.
	// The application's one sender, which the data level opens: the Workspace relay
	// (session/mail/workspace) where the deployment names a mailbox, and the log (mail.Log)
	// in development, where nothing is sent and a developer follows the link from the log.
	// Required; New refuses nil.
	Mail mail.Sender
	// MailLoginURL is the absolute address of the login page of the surface that binds
	// to this auth (https://host/login), which every mailed link opens with its token in
	// the query: the application's public address and the surface's login path.
	MailLoginURL string
	// LoginURL is the browser page a refused or waiting sign-in returns to, with the
	// reason as a code in the query (?code=, a sessioninfo.LoginRefusalCode, never text, or
	// ?pending= for a sign-in that waits): the login page of the surface that binds to
	// this auth, which holds the sentence for each code.
	LoginURL string
	// Google is the google registration's settings: how the application is known to
	// Google, read from the environment (APP_STAFF_GOOGLE_CLIENT_ID, APP_STAFF_GOOGLE_CLIENT_SECRET, APP_STAFF_GOOGLE_REDIRECT_URL).
	Google GoogleSettings
	// TenantDeclarations reads what a tenant declares about who may enter it, in the
	// sign-in's transaction, which the sign-in rule decides with: the generated reader
	// over the Tenant record and its directories (app.NewTenantDeclarationReader).
	// Required; New refuses nil.
	TenantDeclarations signin.DeclarationReader[*cloudspanner.ReadWriteTransaction]
	// TenantDirectories reads every tenant's declaration of one directory with the
	// default seat its row carries, in the sign-in's transaction, which the directory
	// admission decides a stranger's first sign-in with: the generated reader
	// (app.NewTenantDirectoryReader). Required; New refuses nil.
	TenantDirectories signin.DirectoryReader[*cloudspanner.ReadWriteTransaction]
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

// Auth is the staff auth: its permission store and its session manager with the
// sign-in methods registered on it, and the database client it reads its own tables
// through (the users table, when the deployment seeds the first administrator).
type Auth struct {
	db       *cloudspanner.Client
	access   *access.Client
	handlers *Handlers
}

// Handlers is the auth's served surface, the shape the generated router names for the
// outlet bound to it: the Auth's middleware and handlers, what the login page is told,
// the pending sign-in handlers, each method's handlers under its registration name
// (in the method's own file).
type Handlers struct {
	*Session
	password *session.PasswordMethod
	google   *google.Method
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
		return nil, errors.New("staff.Settings.Signals is nil: the permission engine signals policy changes through the application's live service, which every application wires")
	}
	if settings.Mail == nil {
		return nil, errors.New("staff.Settings.Mail is nil: the auth mails its invitations and address verifications through the application's sender (the Workspace relay deployed, the log in development), which the data level opens")
	}
	if settings.TenantDeclarations == nil {
		return nil, errors.New("staff.Settings.TenantDeclarations is nil: the sign-in rule reads what each tenant declares about who may enter it through the generated reader (app.NewTenantDeclarationReader), which the data level hands in")
	}
	if settings.TenantDirectories == nil {
		return nil, errors.New("staff.Settings.TenantDirectories is nil: the directory admission reads what the tenants declare about one directory, its default seat included, through the generated reader (app.NewTenantDirectoryReader), which the data level hands in")
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
		session.WithSignInAttemptsTableName(attemptsTable),
		session.WithMailLinksTableName(mailLinksTable),
		session.WithTokensTableName(tokensTable),
		session.WithDeviceCodesTableName(deviceCodesTable),
		session.WithMail(settings.Mail, session.MailSettings{Application: Application, LoginURL: settings.MailLoginURL}),
		session.WithCookieName(Name),
		session.WithXSRFCookieName(XSRFCookie),
		session.WithSessionTimeout(settings.SessionTimeout),
	)
	if err != nil {
		return nil, errors.Wrap(err, "session.NewAuth()")
	}

	handlers := &Handlers{Session: authSession}
	handlers.password = registerPassword(authSession)
	handlers.google = registerGoogle(authSession, accessClient, settings)

	// The sign-in rule: the library's tenant policy. A person's tenants are where they
	// hold a role (the engine's memberships), and what each tenant admits is its
	// declaration, read in the sign-in's transaction: a password is admitted unless
	// every one of the person's tenants admits its people through directories only, a
	// sign-in through a directory when one of their tenants declares it (its method and
	// the connection its identities carry); the application's own staff (a global role,
	// or one held in every tenant) are admitted on the library's checks.
	// A person who holds no tenant is admitted too (signin.AdmitWithoutTenant), since
	// the Tenant record's creation shape lets a stranger sign in first and create
	// their own; an application whose administrator alone creates tenants drops the
	// option, and such a person is refused (no_tenant).
	// Which tenants the session then reaches is decided again, tenant by tenant, where
	// its tenant list is computed (UserPermissions). The memberships are read outside
	// the sign-in's transaction (resource.SignInRoles): the Spanner client refuses the
	// engine's read under the transaction's context as a nested transaction.
	authSession.SetSignInPolicy(signin.TenantPolicy(resource.SignInRoles(accessClient.UserManager()), settings.TenantDeclarations, signin.AdmitWithoutTenant()))

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

	// An invitation accepted seats its roles: once the transaction that consumed the
	// invitation has committed, the hook writes the invitee's memberships through the
	// permission engine, outside the sign-in's transaction (the engine's own read-write
	// transaction would wait on it), and the sign-in rule that follows finds them.
	authSession.SetInvitationAccepted(resource.SeatInvited(accessClient.UserManager()))

	return &Auth{db: db, access: accessClient, handlers: handlers}, nil
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
