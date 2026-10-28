// Package members is the members auth: the people who sign in through the organization's
// directory (OpenID Connect against Google) and hold their roles in the members permission
// store.
//
// The directory proves who someone is and says what they may do. Role membership is the
// directory's (google.RoleSync): every sign-in reconciles the person's roles to the
// Google Groups the directory places them in and removes any it does not name, so nothing
// in the application assigns roles in this store and the bootstrap seeds none. A global
// role is held in the global partition and a domain role in every sector, so one
// membership reaches every sector, the ones seeded today and the ones created later. The
// application-run alternative, google.DisableRoleSync, leaves role membership to the
// application, so one registration is never both.
//
// An auth is a package. It owns its session manager, one Auth session with the sign-in
// methods registered on it, here the directory alone, with its own session, user,
// identity, auth event and pending sign-in tables and cookie; its permission store, with
// its own table prefix; and its role configuration. A site or an outlet binds to an auth
// by composing its handlers, and two auths on one database and one host never collide,
// because everything an auth names carries its name. The same name in this auth and in
// another is two unrelated principals.
//
// A client is invited too: the console's administrators and the portal's invite an
// address into the members' global partition or into a sector, and the directory
// sign-in that follows the mailed link seats the person with the roles the invitation
// carries (resource.SeatInvited), beside the ones the directory's groups name. A
// member's address is the one the directory verified, held beside the username.
//
// Every member session carries a second factor (pkg/auth: auth.RequireSecondFactor):
// the company's directory attests for its people when the deployment says the fleet
// administers it, and otherwise the login page runs the sign-in code the auth mails.
//
// Demonstrates: auth.directory-roles, auth.two-populations, auth.skipauth-directory, auth.login-refusal-code, auth.invitation, auth.second-factor, auth.attested-directory.
package members

import (
	"context"
	_ "embed" // the role file rides in the binary
	"net/http"
	"time"

	cloudspanner "cloud.google.com/go/spanner"
	"github.com/cccteam/access"
	"github.com/cccteam/access/spannerstore"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/lodestar/pkg/auth"
	"github.com/cccteam/session"
	"github.com/cccteam/session/google"
	"github.com/cccteam/session/sessionstorage"
	"github.com/cccteam/session/signin"
	"github.com/go-playground/errors/v5"
)

const (
	// Name is the auth's name: the cookie its sessions ride in.
	Name = "members"
	// TablePrefix is the name's PascalCase form, which prefixes every table the auth owns.
	TablePrefix = "Members"

	// XSRFCookie is the cookie the auth issues its XSRF token in. It carries the auth's name
	// so two auths on one host never overwrite each other's token; the browser echoes it in
	// the X-XSRF-TOKEN header, so the web app that binds to this auth names the same cookie.
	XSRFCookie = Name + "-xsrf"
	// TrustedDeviceCookie is the cookie a browser a member asked to be remembered after
	// the sign-in code holds its token in; it carries the auth's name, since the crew's
	// cookie lives on the same host.
	TrustedDeviceCookie = Name + "-trusted"

	// DirectoryRegistration is the name the directory is registered under on the Auth:
	// the segment of its login and callback routes (user/directory/login). In
	// development the simulated person is the shared APP_USERNAME, since the
	// registration sets no pair of its own.
	DirectoryRegistration = "directory"

	// The auth's tables, which schema/migrations creates: the sessions and users of the
	// Auth session (a user keyed by the directory's immutable subject through the
	// identities table, so a renamed account stays the same person), the auth events
	// each session records, the pending sign-ins that wait for a confirmation or an
	// MFA step, the sign-in attempts the library counts per name (the mail it sends is
	// counted there too, under the address), the mail links (the invitations and
	// address verifications the auth mails, each a single-use token), and the second
	// factor's: the sign-in codes mailed for a pending sign-in and the browsers people
	// asked to be remembered. The names are spelled out in full so the tool's
	// session-tables check, which reads the literals, finds their migrations.
	sessionsTable       = "MembersSessions"
	usersTable          = "MembersSessionUsers"
	identitiesTable     = "MembersSessionIdentities"
	eventsTable         = "MembersSessionAuthEvents"
	pendingTable        = "MembersSessionPendingSignIns"
	attemptsTable       = "MembersSessionSignInAttempts"
	mailLinksTable      = "MembersSessionMailLinks"
	factorCodesTable    = "MembersSessionFactorCodes"
	trustedDevicesTable = "MembersSessionTrustedDevices"

	// Application is the fleet's name as the mail the auth sends says it.
	Application = "Lodestar"
)

// rolesFile is the auth's default roles: what a client may do, in the lowercase names the
// directory's groups carry. The file travels with the binary and is handed to the
// permission engine at New; the store holds no row for these roles, and who holds them is
// the directory's business.
//
//go:embed roles.json
var rolesFile []byte

// Roles returns the auth's role file.
func Roles() access.RoleFile {
	return access.RoleFile(rolesFile)
}

// Settings are the auth's environment-derived settings.
type Settings struct {
	// CookieKey signs session cookies: a Base64-encoded string of at least 32 bytes of
	// cryptographically secure random data.
	CookieKey string
	// SessionTimeout is the idle timeout of a browser session.
	SessionTimeout time.Duration
	// LoginURL is the browser page a refused login returns to, with the reason as a code
	// in the query (?code=, a sessioninfo.LoginRefusalCode, never text): the login page of
	// the surface that binds to this auth, which holds the sentence for each code.
	LoginURL string
	// Directory identifies the application to the directory that verifies its logins.
	Directory auth.Directory
	// Collection is the generated permission collection the role file validates against:
	// the resources, fields and conditions the release declares. The router package
	// generates it and imports this one, so the configuration passes it in.
	Collection access.PermissionCollection
	// ChangeSignal carries the engine's policy-change hints between the application's
	// instances (auth.PolicySignal over the live service): the engine announces after
	// every policy write it makes (the directory sync's at login) and rereads on every
	// hint it receives, the heartbeat left as the backstop. Required.
	ChangeSignal access.ChangeSignal
	// TenantDeclarations reads what a sector declares about who may enter it, in the
	// sign-in's transaction: the generated reader over the Sector record and its
	// directories (app.NewSectorDeclarationReader), which the sign-in rule decides with.
	// Required; New refuses nil.
	TenantDeclarations signin.DeclarationReader[*cloudspanner.ReadWriteTransaction]
	// Mail is how the auth sends its invitations and address verifications: the
	// configuration's sender, and the portal's login page as an absolute URL, where
	// every link leads. Required; New refuses a nil sender.
	Mail auth.Mail
}

// Session is the auth's session manager: one Auth session over the Spanner store, whose
// hooks are typed to the store's transaction.
type Session = session.Auth[session.NoCustomData, session.NoCustomData, *cloudspanner.ReadWriteTransaction]

// Auth is the members auth: its permission store and its session manager with the
// directory registered on it.
type Auth struct {
	access   *access.Client
	handlers *Handlers
}

// Handlers is the auth's served surface, the shape the generated router names for the
// portal outlet (router.PortalSession): the Auth's middleware and handlers, what the
// login page is told, the pending sign-in handlers, and the directory's handlers under
// its registration name.
type Handlers struct {
	*Session
	directory *google.Method
}

// DirectoryLogin sends the browser to the company's directory.
func (h *Handlers) DirectoryLogin() http.HandlerFunc {
	return h.directory.Login()
}

// DirectoryCallback receives the browser back from the directory, provisions or finds
// the person, reconciles their roles and starts the session.
func (h *Handlers) DirectoryCallback() http.HandlerFunc {
	return h.directory.Callback()
}

// InvitationOpen opens the invitation the portal's login page was sent to ({"token"}):
// it answers the address and the sector the invitation names, and sets the cookie the
// directory sign-in that follows carries, so that sign-in accepts it.
func (h *Handlers) InvitationOpen() http.HandlerFunc {
	return h.Invitation().Open()
}

// InvitationAccept accepts the opened invitation as the signed-in client; the
// acceptance hook seats them with the roles it carries.
func (h *Handlers) InvitationAccept() http.HandlerFunc {
	return h.Invitation().Accept()
}

// EmailAdd mails a verification link to the address the signed-in client gives
// ({"email"}); a client the directory verified already holds that address verified.
func (h *Handlers) EmailAdd() http.HandlerFunc {
	return h.Email().Add()
}

// EmailVerify marks the address verified from the mailed link's token ({"token"}).
func (h *Handlers) EmailVerify() http.HandlerFunc {
	return h.Email().Verify()
}

// PendingStatus reports the sign-in that waits for a password confirmation or for the
// second factor, if any; a member has no password, so only the factor's wait happens,
// for a member whose directory the fleet does not administer.
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

// PendingSendCode mails the sign-in code to the person whose sign-in waits for the
// second factor: the address they hold, verified or not, which the code then verifies.
func (h *Handlers) PendingSendCode() http.HandlerFunc {
	return h.Pending().SendCode()
}

// PendingVerifyCode completes a waiting sign-in with the code entered ({"code",
// "rememberDevice"}): a wrong code is refused (invalid_code) and the fifth kills the
// code, a code entered while none stands asks for a new one (code_expired), and
// rememberDevice keeps the browser trusted for the period.
func (h *Handlers) PendingVerifyCode() http.HandlerFunc {
	return h.Pending().VerifyCode()
}

// PendingEnrollEmail takes the address a person who holds none gives for the code
// ({"email"}): the code is mailed there, and the address is written on them verified
// once it is entered.
func (h *Handlers) PendingEnrollEmail() http.HandlerFunc {
	return h.Pending().EnrollEmail()
}

// New opens the auth's permission store and session manager over the database. The
// permission engine validates the role file against the collection and refuses to start
// on a file that does not parse or grants what the release does not declare; it then
// blocks until its first policy snapshot is loaded. It announces its policy writes and
// watches the other instances' through the change signal, which is required.
func New(ctx context.Context, db *cloudspanner.Client, settings *Settings) (*Auth, error) {
	if settings.ChangeSignal == nil {
		return nil, errors.New("members.Settings.ChangeSignal is required: the engine announces and watches policy changes through it")
	}
	if settings.TenantDeclarations == nil {
		return nil, errors.New("members.Settings.TenantDeclarations is required: the sign-in rule reads what each sector declares about who may enter it through the generated reader (app.NewSectorDeclarationReader)")
	}
	if settings.Mail.Sender == nil {
		return nil, errors.New("members.Settings.Mail.Sender is required: the auth mails its invitations and address verifications through it (the configuration's driver, or mail.Log in development)")
	}
	store, err := spannerstore.New(db, spannerstore.WithPrefix(TablePrefix))
	if err != nil {
		return nil, errors.Wrap(err, "spannerstore.New()")
	}
	accessClient, err := access.New(store, access.WithDefaultRoles(settings.Collection, Roles()), access.WithChangeSignal(settings.ChangeSignal))
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

	// One Auth session the directory establishes, which mails as the fleet
	// (session.WithMail): the invitations the user-management surfaces send and the
	// verification of an address, each leading to the portal's login page with its
	// token in the query.
	membersSession, err := session.NewAuth[session.NoCustomData, session.NoCustomData](
		sessionstorage.NewSpannerAuth(db, sessionstorage.WithAuthEvents(events)),
		settings.CookieKey,
		session.WithSessionTableName(sessionsTable),
		session.WithUserTableName(usersTable),
		session.WithIdentitiesTableName(identitiesTable),
		session.WithPendingSignInsTableName(pendingTable),
		session.WithSignInAttemptsTableName(attemptsTable),
		session.WithMailLinksTableName(mailLinksTable),
		session.WithFactorCodesTableName(factorCodesTable),
		session.WithTrustedDevicesTableName(trustedDevicesTable),
		session.WithTrustedDeviceCookieName(TrustedDeviceCookie),
		session.WithMail(settings.Mail.Sender, session.MailSettings{Application: Application, LoginURL: settings.Mail.LoginURL}),
		session.WithCookieName(Name),
		session.WithXSRFCookieName(XSRFCookie),
		session.WithSessionTimeout(settings.SessionTimeout),
	)
	if err != nil {
		return nil, errors.Wrap(err, "session.NewAuth()")
	}

	// The directory's groups are the source of role membership, read through the Cloud
	// Identity Groups API with the person's own sign-in token, as far as the configured
	// lookup reaches: the groups they are a direct member of, or with the nested lookup
	// the groups above those too, level by level. Under the session library's skipAuth
	// build tag the lookup is simulated: every sign-in is in the groups APP_ROLES names.
	lookup, err := google.ParseGroupLookup(settings.Directory.GroupLookup)
	if err != nil {
		return nil, errors.Wrap(err, "google.ParseGroupLookup()")
	}

	// The directory, registered before the first request. The directory is the authority
	// for role membership: every sign-in reconciles the person's roles to the Google
	// Groups the directory places them in (those named by the group prefix), removing
	// any it does not name, and a sign-in naming no known role is refused. A global role
	// is written in the global partition and a domain role in every sector, so the sync
	// keeps no list of sectors. Hand-assigned roles do not survive it, so nothing in the
	// application assigns roles in this store.
	// The organization slot is the members' one Workspace, named by the connection
	// setting (the name every link carries) and checked against the hosted domain,
	// declared attesting where the deployment says the fleet administers it
	// (auth.Directory.Administered), so a member's sign-in carries the Workspace's
	// second factor and is never asked for the code. The
	// account rule is the library's standard one, members of the directory: an identity
	// the directory verifies for the first time becomes a member under the verified
	// email, with no password (the directory is their only way in), linked by the
	// directory's immutable subject so a renamed account stays the same person at the
	// next sign-in.
	directory := google.SignIn(membersSession, DirectoryRegistration, nil, &google.Config{
		ClientID:     settings.Directory.ClientID,
		ClientSecret: settings.Directory.ClientSecret,
		RedirectURL:  settings.Directory.RedirectURL,
		Workspaces:   settings.Directory.Workspace(),
		RoleSync:     google.RoleSync(accessClient.UserManager(), settings.Directory.GroupPrefix, lookup),
		LoginURL:     settings.LoginURL,
	})

	// The sign-in rule: the library's tenant policy, the same rule the crew auth sets,
	// so the two populations are admitted alike. A client's sign-in names their roles
	// (the directory's groups), which the rule admits on the library's checks; the rule
	// decides a sign-in the directory names no roles for by the client's sectors'
	// declarations, as the crew's. Which sectors the session then reaches is decided
	// again where its chart is computed (app.UserPermissions): the seed declares the
	// company directory on every sector. The memberships are read outside the sign-in's
	// transaction (resource.SignInRoles): the Spanner client refuses the engine's read
	// under the transaction's context as a nested transaction. The rule's last word is
	// the second factor every member session carries (auth.RequireSecondFactor), which
	// the company's directory attests for where the fleet administers it.
	//
	// Demonstrates: auth.second-factor.
	membersSession.SetSignInPolicy(signin.TenantPolicy(resource.SignInRoles(accessClient.UserManager()), settings.TenantDeclarations).Then(auth.RequireSecondFactor))

	// An accepted invitation seats the client with the roles it carries, in the sector
	// it names or in the global partition, written to the members engine by the
	// library's hook (resource.SeatInvited) once the transaction that consumed the
	// invitation has committed, before the sign-in rule decides. The directory's sync
	// writes beside it at the same sign-in: an invitation's roles are the
	// application's, the groups' the directory's, and a seat the sync does not name is
	// left to it (SyncsRoles) only where the directory names roles at all.
	//
	// Demonstrates: auth.invitation.
	membersSession.SetInvitationAccepted(resource.SeatInvited(accessClient.UserManager()))

	return &Auth{access: accessClient, handlers: &Handlers{Session: membersSession, directory: directory}}, nil
}

// Access returns the auth's permission engine: the handlers check against it, and its
// UserManager writes roles, grants, and role assignments.
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

// Close releases the permission engine.
func (a *Auth) Close() error {
	if err := a.access.Close(); err != nil {
		return errors.Wrap(err, "access.Client.Close()")
	}

	return nil
}
