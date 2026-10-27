// Package crew is the crew auth: the people who sign in to the console, with a password
// or through the fleet's Google directory, and hold their roles in the crew permission
// store.
//
// An auth is a package. It owns its session manager, one Auth session with the sign-in
// methods registered on it, with its own session, user, identity, auth event and pending
// sign-in tables and cookie; its permission store, with its own table prefix; and its
// role configuration. A site or an outlet binds to an auth by composing its handlers, and
// two auths on one database and one host never collide, because everything an auth names
// carries its name. The package is named for the population, never for the method: the
// crew gained a directory sign-in beside the password without the package moving.
//
// Three ways in, one population. The password method checks a persona's password, and the
// personas' roles are the bootstrap's (cmd/bootstrap/users.json); it is registered with
// open sign-up, so a stranger with an address may create a crew login with a password
// from a mailed link, holding no role, as the Google sign-up registration admits one with
// a Google account. The Google method
// provisions whoever the directory verifies under the verified email, holding no role
// until an operator seats them: the crew's roles are the application's on every method
// (google.DisableRoleSync), since a registration whose directory writes the roles stands
// alone on its auth, and the crew auth has three ways in. The two methods meet in one
// users table, one session table and one cookie, so a session opened either way is a
// crew session. In development the directory is simulated and APP_GOOGLE_USERNAME in
// .envrc says who the Google sign-in is. The sign-up registration is the third way, for a
// person on no manifest and in no directory: any Google account, open sign-up, so the
// first sign-in makes a crew login holding no role and no sector. Such a person's one way
// to a sector is to chart their own, the Sector record's self-service creation shape; in
// development APP_SIGN_UP_USERNAME says who the simulated sign-up is. The directory as
// the authority for roles is the members auth's shape (pkg/auth/members), where the
// directory's registration stands alone.
//
// Invitations are the way an administrator brings a person in: the user-management
// surface mails a link carrying the roles, into the global partition or into a sector,
// and whoever follows it and signs in, by any method or by creating a password login,
// is seated with those roles by the acceptance hook (resource.SeatInvited). A person's
// email address is held apart from their username, added and verified by the person
// through a mailed link, or entered unverified by an operator.
//
// Demonstrates: auth.password, auth.two-populations, auth.multi-method, auth.directory-roles, auth.open-sign-up, auth.invitation, auth.email-address, auth.password-sign-up.
package crew

import (
	"context"
	"crypto/sha256"
	_ "embed" // the role file rides in the binary
	"net/http"
	"time"

	cloudspanner "cloud.google.com/go/spanner"
	"github.com/cccteam/access"
	"github.com/cccteam/access/spannerstore"
	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/lodestar/pkg/auth"
	"github.com/cccteam/httpio"
	"github.com/cccteam/session"
	"github.com/cccteam/session/google"
	"github.com/cccteam/session/sessionstorage"
	"github.com/cccteam/session/signin"
	"github.com/go-playground/errors/v5"
)

const (
	// Name is the auth's name: the cookie its sessions ride in.
	Name = "crew"
	// TablePrefix is the name's PascalCase form, which prefixes every table the auth owns.
	TablePrefix = "Crew"

	// XSRFCookie is the cookie the auth issues its XSRF token in. It carries the auth's name
	// so two auths on one host never overwrite each other's token; the browser echoes it in
	// the X-XSRF-TOKEN header, so the web app that binds to this auth names the same cookie.
	XSRFCookie = Name + "-xsrf"

	// GoogleRegistration is the name the Google method is registered under on the Auth:
	// the segment of its login and callback routes (user/google/login) and, in
	// development, the registration whose simulated person APP_GOOGLE_USERNAME names.
	GoogleRegistration = "google"
	// SignUpRegistration is the name the open sign-up method is registered under: any
	// Google account, through user/sign-up/login, and in development the registration
	// whose simulated person APP_SIGN_UP_USERNAME names.
	SignUpRegistration = "sign-up"

	// The auth's tables, which schema/migrations creates: the sessions and users of the
	// Auth session, the identity links of its Google sign-ins, the auth events each
	// session records, the pending sign-ins that wait for a confirmation or an MFA step,
	// and the impersonation record the library joins into every session read, so a
	// view-as or act-as-role session carries who established it, the sign-in attempts
	// the password method's attempt limiter counts per sign-in name, and the mail links
	// (the invitations, verifications and sign-ups the auth mails, each a single-use
	// token). The names are spelled out in full (not TablePrefix + "SessionUsers") so
	// the tool's session-tables check, which reads the literals, finds their migrations.
	sessionsTable   = "CrewSessions"
	usersTable      = "CrewSessionUsers"
	identitiesTable = "CrewSessionIdentities"
	eventsTable     = "CrewSessionAuthEvents"
	pendingTable    = "CrewSessionPendingSignIns"
	attemptsTable   = "CrewSessionSignInAttempts"
	mailLinksTable  = "CrewSessionMailLinks"
	// The bearer tokens the auth issues for its machine outlet and the device
	// authorizations command-line clients start, which the token page and the device
	// flow keep.
	tokensTable      = "CrewSessionTokens"
	deviceCodesTable = "CrewSessionDeviceCodes"
	// ImpersonationsTable is exported: the watch desk names it.
	ImpersonationsTable = "CrewSessionImpersonations"

	// DroidsOutlet is the machine outlet the auth issues tokens for: the droids'
	// telemetry channel (/droids), which the generator program declares Tokens of this
	// package, so the generated router composes the auth's token check in front of the
	// outlet's routes, mounts the device flow's endpoints under it, and serves the token
	// routes on the console. A request there carries a bearer token the auth issued: the
	// droid service account's configured key, seeded as its application token
	// (SeedToken), or a token a crew member issued on the console, or one a command-line
	// client obtained with a crew member's approval. New registers the outlet on the
	// Auth before the first request, as it registers the sign-in methods.
	DroidsOutlet = "droids"
	// seededTokenName names the application token SeedToken keeps for a service
	// account's configured key.
	seededTokenName = "configured key"
	// seededTokenLifetime is how long a seeded token lives: the configured key has no
	// expiry of its own, so the token takes a century and is replaced when the key is.
	seededTokenLifetime = 100

	// Application is the fleet's name as the mail the auth sends says it.
	Application = "Lodestar"

	// impersonationTimeout is the configured hard cap on an impersonated session; the
	// mint route shortens every view-as below it with MaxDuration.
	impersonationTimeout = 4 * time.Hour
)

// AdministratorRoles are the roles whose holders administer a partition, by where they
// are held: Headquarters in the global partition, SectorMarshal in a sector (the
// governor holds it in every sector). The user-management surface judges a self-unseat
// by them (resource.WithAdministratorRoles, the last-owner rule): a marshal leaves a
// sector once another marshal holds it, and the last one is refused; and the Sector
// record's create seats the creator with the sector's (app.SectorAdministratorRoles).
//
// Demonstrates: tenancy.creator-seat.
func AdministratorRoles() (global, everyDomain []accesstypes.Role) {
	return []accesstypes.Role{"Headquarters"}, []accesstypes.Role{"SectorMarshal"}
}

// rolesFile is the auth's default roles: the release's own policy for the crew, which
// travels with the binary and is handed to the permission engine at New. The store holds
// no row for these roles; a login holds one by name alone, and the file is the complete
// statement of what each role may do. A global role is held in the global partition and a
// domain role in every sector.
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
	// LoginURL is the browser page a refused or waiting Google sign-in returns to, with
	// the reason as a code in the query (?code=, a sessioninfo.LoginRefusalCode, never
	// text) or the pending step (?pending=): the console's login page, which holds the
	// sentence for each code.
	LoginURL string
	// Directory identifies the application to the fleet's Google directory, the crew's
	// second way in.
	Directory auth.Directory
	// SignUpRedirectURL is the browser-facing callback of the sign-up registration, the
	// crew's third way in: any Google account, under the same client as the directory,
	// returned to its own callback (user/sign-up/callback).
	SignUpRedirectURL string
	// Mail is how the auth sends its invitations, address verifications and sign-up
	// links: the configuration's sender, and the console's login page as an absolute
	// URL, where every link leads. Required; New refuses a nil sender.
	Mail auth.Mail
	// DevicePage is the console page where a signed-in crew member approves a
	// command-line client's request to act as them, which a device authorization tells
	// the client (verification_uri): a path in the application, the console's mount
	// path joined with /device, where the console mounts the library's device approval
	// page.
	DevicePage string
	// Collection is the generated permission collection the role file validates against:
	// the resources, fields and conditions the release declares. The router package
	// generates it and imports this one, so the configuration passes it in.
	Collection access.PermissionCollection
	// ChangeSignal carries the engine's policy-change hints between the application's
	// instances (auth.PolicySignal over the live service): the engine announces after
	// every policy write it makes and rereads on every hint it receives, the heartbeat
	// left as the backstop. Required.
	ChangeSignal access.ChangeSignal
	// TenantDeclarations reads what a sector declares about who may enter it, in the
	// sign-in's transaction: the generated reader over the Sector record and its
	// directories (app.NewSectorDeclarationReader), which the sign-in rule decides with.
	// Required; New refuses nil.
	TenantDeclarations signin.DeclarationReader[*cloudspanner.ReadWriteTransaction]
	// TenantDirectories reads every sector's declaration of one directory with the
	// default seat its row carries, in the sign-in's transaction: the generated reader
	// (app.NewSectorDirectoryReader), which the directory admission decides a stranger's
	// first sign-in with. Required; New refuses nil.
	TenantDirectories signin.DirectoryReader[*cloudspanner.ReadWriteTransaction]
}

// Session is the auth's session manager: one Auth session over the Spanner store, whose
// hooks are typed to the store's transaction.
type Session = session.Auth[session.NoCustomData, session.NoCustomData, *cloudspanner.ReadWriteTransaction]

// Auth is the crew auth: its permission store and its session manager with the sign-in
// methods registered on it.
type Auth struct {
	access   *access.Client
	handlers *Handlers
}

// Handlers is the auth's served surface, the shape the generated router names for the
// console outlet (router.DefaultSession): the Auth's middleware and handlers, what the
// login page is told, the pending sign-in handlers, and the handlers of each sign-in
// method under its registration name. The App embeds it.
type Handlers struct {
	*Session
	store    *sessionstorage.Auth[*cloudspanner.ReadWriteTransaction]
	password *session.PasswordMethod
	google   *google.Method
	signUp   *google.Method
	droids   *session.MachineOutlet
}

// DroidsTokens returns the droids outlet's token check (router.DroidsTokens): the
// machine outlet registered on the Auth under DroidsOutlet, whose ValidateToken binds
// each request on /droids to the account its bearer token acts for.
func (h *Handlers) DroidsTokens() *session.MachineOutlet {
	return h.droids
}

// DeviceAuthorize starts a command-line client's device authorization ({"client_id",
// "outlet"}, form-encoded or JSON): the client is told the code the person types and
// the device code it polls with. The generated router mounts it under the droids
// outlet, in front of the token check.
func (h *Handlers) DeviceAuthorize() http.HandlerFunc {
	return h.Device().Authorize()
}

// DeviceToken answers the client's polls (grant_type device_code) with its access and
// refresh tokens once a crew member approved, and exchanges a refresh token for the
// next pair (grant_type refresh_token); a rotated refresh token presented again revokes
// the grant's family. Mounted beside DeviceAuthorize.
func (h *Handlers) DeviceToken() http.HandlerFunc {
	return h.Device().Token()
}

// DeviceLookup answers the console's approval page which client asks, for which
// outlet, for the code the person typed (?userCode=).
func (h *Handlers) DeviceLookup() http.HandlerFunc {
	return h.Device().Lookup()
}

// DeviceApprove approves the device authorization the signed-in crew member's code
// names ({"userCode"}), as them: the client's next poll receives tokens acting as them.
func (h *Handlers) DeviceApprove() http.HandlerFunc {
	return h.Device().Approve()
}

// DeviceDeny denies the device authorization the code names ({"userCode"}).
func (h *Handlers) DeviceDeny() http.HandlerFunc {
	return h.Device().Deny()
}

// TokensList lists the signed-in crew member's live tokens: the application tokens
// they issued and the tokens of the device grants they approved, never a secret.
func (h *Handlers) TokensList() http.HandlerFunc {
	return h.ApplicationTokens().List()
}

// TokensCreate issues an application token for a program of the signed-in crew
// member's own ({"name", "outlet", "expiresIn"}), the secret shown this once.
func (h *Handlers) TokensCreate() http.HandlerFunc {
	return h.ApplicationTokens().Create()
}

// TokensRevoke revokes one of the signed-in crew member's tokens ({"id"}), a device
// grant's with its family.
func (h *Handlers) TokensRevoke() http.HandlerFunc {
	return h.ApplicationTokens().Revoke()
}

// SeedToken makes secret the application token of the service account username on
// outlet: a machine's configured credential (the droids' APP_DROIDS_API_KEY), kept the
// way a person's own token is, hashed in the store, bound to the outlet, acting as the
// account, so the token check and the permission checks treat the droid like any
// other caller. The account is created without a password when the auth has never
// seen it; a secret the store already holds live is left alone, and a changed one
// replaces the account's seeded token on the outlet, so a rotated key takes effect
// at the next start.
//
// Demonstrates: outlet.tokens, machine-identity.
func (h *Handlers) SeedToken(ctx context.Context, username, outlet, secret string) error {
	hash := sha256.Sum256([]byte(secret))
	_, err := h.store.Token(ctx, hash[:])
	if err == nil {
		return nil
	}
	if !httpio.HasNotFound(err) {
		return errors.Wrap(err, "sessionstorage.Auth.Token()")
	}

	api := h.API()
	var userID ccc.UUID
	user, err := api.UserByUsername(ctx, username)
	switch {
	case err == nil:
		userID = user.ID
	case httpio.HasNotFound(err):
		id, err := api.CreateSessionUser(ctx, &session.CreateUserRequest{Username: username})
		if err != nil {
			return errors.Wrapf(err, "session.AuthAPI.CreateSessionUser(): service account %s", username)
		}
		userID = id
	default:
		return errors.Wrap(err, "session.AuthAPI.UserByUsername()")
	}

	tokens, err := h.store.Tokens(ctx, userID)
	if err != nil {
		return errors.Wrap(err, "sessionstorage.Auth.Tokens()")
	}
	for _, tok := range tokens {
		if tok.Outlet != outlet || tok.Name != seededTokenName {
			continue
		}
		if err := h.store.RevokeUserToken(ctx, userID, tok.ID, sessionstorage.TokenRevokedByHand); err != nil {
			return errors.Wrap(err, "sessionstorage.Auth.RevokeUserToken()")
		}
	}
	id, err := ccc.NewUUID()
	if err != nil {
		return errors.Wrap(err, "ccc.NewUUID()")
	}
	seeded := &sessionstorage.Token{
		ID: id, TokenHash: hash[:], Kind: sessionstorage.TokenApplication, UserID: userID, Outlet: outlet, Name: seededTokenName,
		ExpiresAt: time.Now().AddDate(seededTokenLifetime, 0, 0),
	}
	if err := h.store.InsertToken(ctx, seeded); err != nil {
		return errors.Wrap(err, "sessionstorage.Auth.InsertToken()")
	}

	return nil
}

// PasswordLogin is the password method's login: {"username", "password"} in, the session
// cookie out.
func (h *Handlers) PasswordLogin() http.HandlerFunc {
	return h.password.Login()
}

// PasswordChange is the password method's change: {"currentPassword", "newPassword"} in
// from a signed-in person, who may be one whose operator-set password must change before
// anything else is served. It validates the session itself, so the generated router
// mounts it outside ValidateSession, behind the XSRF guard.
func (h *Handlers) PasswordChange() http.HandlerFunc {
	return h.password.ChangeUserPassword()
}

// GoogleLogin sends the browser to the fleet's Google directory.
func (h *Handlers) GoogleLogin() http.HandlerFunc {
	return h.google.Login()
}

// GoogleCallback receives the browser back from the directory, provisions or finds the
// person, reconciles their roles and starts the session.
func (h *Handlers) GoogleCallback() http.HandlerFunc {
	return h.google.Callback()
}

// SignUpLogin sends the browser to Google to sign up with any Google account: the
// crew's third way in, for a person the manifest and the directory do not know.
func (h *Handlers) SignUpLogin() http.HandlerFunc {
	return h.signUp.Login()
}

// SignUpCallback receives the browser back from Google, makes a crew login of a person
// it has never seen (open sign-up) holding no role, and starts the session.
func (h *Handlers) SignUpCallback() http.HandlerFunc {
	return h.signUp.Callback()
}

// InvitationOpen opens the invitation the login page was sent to ({"token"}): it
// answers the address and the sector the invitation names, and sets the cookie the
// sign-in that follows carries, so whichever method the person signs in with accepts it.
func (h *Handlers) InvitationOpen() http.HandlerFunc {
	return h.Invitation().Open()
}

// InvitationAccept accepts the opened invitation as the signed-in person, who proved
// they read the mailbox; the acceptance hook seats them with the roles it carries.
func (h *Handlers) InvitationAccept() http.HandlerFunc {
	return h.Invitation().Accept()
}

// InvitationSignUp creates the invited person's crew login with a password
// ({"username", "password"}), the invitation's address verified, and signs them in with
// the invitation accepted on the way.
func (h *Handlers) InvitationSignUp() http.HandlerFunc {
	return h.Invitation().SignUp()
}

// EmailAdd mails a verification link to the address the signed-in person gives
// ({"email"}); the address is held unverified until the link is followed.
func (h *Handlers) EmailAdd() http.HandlerFunc {
	return h.Email().Add()
}

// EmailVerify marks the address verified from the mailed link's token ({"token"}).
func (h *Handlers) EmailVerify() http.HandlerFunc {
	return h.Email().Verify()
}

// SignUpStart mails a sign-up link to the address a stranger gives ({"email"}): the
// password method's open sign-up, which answers alike whatever the address's state.
func (h *Handlers) SignUpStart() http.HandlerFunc {
	return h.SignUp().Start()
}

// SignUpOpen opens the sign-up link ({"token"}) and answers the address it was sent to.
func (h *Handlers) SignUpOpen() http.HandlerFunc {
	return h.SignUp().Open()
}

// SignUpComplete creates the crew login from the opened link with the username and
// password the person chose ({"token", "username", "password"}), the address verified,
// holding no role and no sector, and signs them in.
func (h *Handlers) SignUpComplete() http.HandlerFunc {
	return h.SignUp().Complete()
}

// PendingStatus reports the sign-in that waits for a password confirmation or an MFA
// step, if any.
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

// New opens the auth's permission store and session manager over the database. The
// permission engine validates the role file against the collection and refuses to start
// on a file that does not parse or grants what the release does not declare; it then
// blocks until its first policy snapshot is loaded. It announces its policy writes and
// watches the other instances' through the change signal, which is required.
func New(ctx context.Context, db *cloudspanner.Client, settings *Settings) (*Auth, error) {
	if settings.ChangeSignal == nil {
		return nil, errors.New("crew.Settings.ChangeSignal is required: the engine announces and watches policy changes through it")
	}
	if settings.TenantDeclarations == nil {
		return nil, errors.New("crew.Settings.TenantDeclarations is required: the sign-in rule reads what each sector declares about who may enter it through the generated reader (app.NewSectorDeclarationReader)")
	}
	if settings.TenantDirectories == nil {
		return nil, errors.New("crew.Settings.TenantDirectories is required: the directory admission reads what each sector declares about one directory, its default seat included, through the generated reader (app.NewSectorDirectoryReader)")
	}
	if settings.Mail.Sender == nil {
		return nil, errors.New("crew.Settings.Mail.Sender is required: the auth mails its invitations, address verifications and sign-up links through it (the configuration's driver, or mail.Log in development)")
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

	// The impersonation record rides beside the sessions, so StartImpersonatedSession
	// can mint view-as and act-as-role sessions, and the auth events table records how
	// each session was authenticated.
	impersonation, err := sessionstorage.NewImpersonationTable(ImpersonationsTable)
	if err != nil {
		return nil, errors.Wrap(err, "sessionstorage.NewImpersonationTable()")
	}
	events, err := sessionstorage.NewAuthEventsTable(eventsTable)
	if err != nil {
		return nil, errors.Wrap(err, "sessionstorage.NewAuthEventsTable()")
	}

	// One Auth session that every method establishes, which mails as the fleet
	// (session.WithMail): the invitations the user-management surface sends, the
	// verification of an address a person adds, and the sign-up links, each leading to
	// the console's login page with its token in the query.
	sessionStore := sessionstorage.NewSpannerAuth(db, sessionstorage.WithImpersonation(impersonation), sessionstorage.WithAuthEvents(events))
	crewSession, err := session.NewAuth[session.NoCustomData, session.NoCustomData](
		sessionStore,
		settings.CookieKey,
		session.WithSessionTableName(sessionsTable),
		session.WithUserTableName(usersTable),
		session.WithIdentitiesTableName(identitiesTable),
		session.WithPendingSignInsTableName(pendingTable),
		session.WithSignInAttemptsTableName(attemptsTable),
		session.WithMailLinksTableName(mailLinksTable),
		session.WithTokensTableName(tokensTable),
		session.WithDeviceCodesTableName(deviceCodesTable),
		session.WithMail(settings.Mail.Sender, session.MailSettings{Application: Application, LoginURL: settings.Mail.LoginURL}),
		session.WithCookieName(Name),
		session.WithXSRFCookieName(XSRFCookie),
		session.WithSessionTimeout(settings.SessionTimeout),
		session.WithImpersonationTimeout(impersonationTimeout),
		session.WithDeviceVerificationURL(settings.DevicePage),
	)
	if err != nil {
		return nil, errors.Wrap(err, "session.NewAuth()")
	}

	// The methods, registered before the first request: the password method under the
	// library's name, and the Google method under GoogleRegistration. The Google method's
	// organization slot is the crew's one Workspace, named by the connection setting
	// (the name every directory identity's link carries) and checked against the hosted
	// domain. Its account rule is the library's standard one, members of the directory:
	// a person the directory verifies for the first time becomes a crew member under the
	// verified email, with no password (the directory is their way in) and no role until
	// an operator seats them. The application is the authority for the crew's roles
	// (google.DisableRoleSync): the sign-in neither reads nor writes them. A registration
	// whose directory writes the roles stands alone on its auth, and the crew auth has
	// three ways in; the members auth is the one whose directory ranks its people. A
	// verified email that is a username a password persona already holds links the
	// identity to that persona once their password confirms it (the directory's word
	// alone is not enough), so the development login APP_GOOGLE_USERNAME names is one
	// that is not on the manifest.
	//
	// The password method is registered with open sign-up (session.OpenSignUp): a
	// stranger gives an address, follows the mailed link and chooses a username and a
	// password, and the login holds no role and no sector until they chart their own or
	// an operator seats them, exactly as the Google sign-up registration's does. The
	// generator program declares the same (PasswordMethod().OpenSignUp()), so the router
	// mounts the sign-up routes and the login page is told of them.
	password := session.PasswordSignIn(crewSession, session.OpenSignUp())
	googleMethod := google.SignIn(crewSession, GoogleRegistration, nil, &google.Config{
		ClientID:     settings.Directory.ClientID,
		ClientSecret: settings.Directory.ClientSecret,
		RedirectURL:  settings.Directory.RedirectURL,
		Workspaces:   google.Workspace(settings.Directory.Connection, settings.Directory.HostedDomain),
		RoleSync:     google.DisableRoleSync(),
		LoginURL:     settings.LoginURL,
	})
	// The sign-up registration, under SignUpRegistration: any Google account
	// (google.AnyGoogleAccount), the open sign-up rule (signin.OpenSignUp: a person the
	// auth has never seen becomes a crew login under the verified email), and the
	// application as the authority for roles (google.DisableRoleSync: the sign-in neither
	// reads nor writes roles, so the person holds nothing until they chart a sector of
	// their own or an operator seats them). It shares the directory's client; only the
	// callback is its own.
	signUpMethod := google.SignIn(crewSession, SignUpRegistration, nil, &google.Config{
		ClientID:     settings.Directory.ClientID,
		ClientSecret: settings.Directory.ClientSecret,
		RedirectURL:  settings.SignUpRedirectURL,
		Workspaces:   google.AnyGoogleAccount(),
		RoleSync:     google.DisableRoleSync(),
		Accounts:     signin.OpenSignUp,
		LoginURL:     settings.LoginURL,
	})

	// The sign-in rule: the library's tenant policy. A person's sectors are where they
	// hold a role (the engine's memberships), and what each sector admits is its
	// declaration, read in the sign-in's transaction: a password is admitted unless
	// every one of the person's sectors admits its people through directories only
	// (the bosun, whose only sector is Bastion, is refused directory_only), a sign-in
	// through a directory when one of their sectors declares it; the fleet's own staff
	// (a global role, or one held in every sector) and a sign-in whose directory names
	// the roles (the navigator's, the client's) are admitted on the library's checks;
	// a person who holds no sector is admitted too (signin.AdmitWithoutTenant), since
	// the pioneer signs up first and charts her sector after. Which sectors the session
	// then reaches is decided again, sector by sector, where its chart is computed
	// (app.UserPermissions). The memberships are read outside the sign-in's transaction
	// (resource.SignInRoles): the Spanner client refuses the engine's read under the
	// transaction's context as a nested transaction.
	//
	// Demonstrates: tenancy.declaration, tenancy.directory-only.
	crewSession.SetSignInPolicy(signin.TenantPolicy(resource.SignInRoles(accessClient.UserManager()), settings.TenantDeclarations, signin.AdmitWithoutTenant()))

	// The directory admission: what a fleet member nobody has seated gets at their first
	// sign-in through a directory a sector declares, before the registration's account
	// rule is reached. The directory rows the sectors declare for the identity's
	// connection decide: a row with a default seat creates the person and seats them
	// with it (Cinder seats them SectorCrew, written through the engine once the
	// sign-in's transaction has committed, resource.SeatDeclared, before the sign-in
	// rule decides, so the rule finds the membership); rows with none refuse them
	// (invitation_only), and no account is created, unless they followed an invitation
	// link, which is the way in; a person already seated somewhere is the sector's own
	// and passes. A directory no sector declares falls to the registration's rule as
	// before (the pioneer's sign-up). The crew's roles are the application's, so the
	// admission is set here; an auth whose directory writes the roles (the members'
	// directory) is the authority for everyone it holds and sets none.
	//
	// Demonstrates: tenancy.default-seat.
	crewSession.SetDirectoryAdmission(settings.TenantDirectories, resource.SignInRoles(accessClient.UserManager()), resource.SeatDeclared(accessClient.UserManager()))

	// An accepted invitation seats the person with the roles it carries, in the
	// sector it names or in the global partition, written to the crew engine by the
	// library's hook (resource.SeatInvited) once the transaction that consumed the
	// invitation has committed and before the sign-in rule decides, so the rule sees
	// the new seat: a stranger invited into Bastion signs in admitted. The engine
	// writes through its own client, outside any transaction, keyed by the username
	// the hook is handed.
	//
	// Demonstrates: auth.invitation.
	crewSession.SetInvitationAccepted(resource.SeatInvited(accessClient.UserManager()))

	// The machine outlet the auth issues tokens for, registered before the first
	// request as the methods are: the droids' channel, whose token check the generated
	// router composes in front of its routes. Registration closes with the first use of
	// the Auth, so it happens here rather than where the App is built, which a bootstrap
	// or a test harness may reach after creating users through the API.
	droids := session.RegisterMachineOutlet(crewSession, DroidsOutlet)

	return &Auth{access: accessClient, handlers: &Handlers{Session: crewSession, store: sessionStore, password: password, google: googleMethod, signUp: signUpMethod, droids: droids}}, nil
}

// Access returns the auth's permission engine: the handlers check against it, and its
// UserManager writes roles, grants, and role assignments.
func (a *Auth) Access() *access.Client {
	return a.access
}

// Session returns the auth's session manager, for the bootstrap's user creation and the
// impersonation routes' API.
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
