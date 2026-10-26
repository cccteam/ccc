package resource

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/httpio"
	"github.com/ettle/strcase"
	"github.com/go-playground/errors/v5"
)

// The user-management surface. For every Auth the generated router binds, the library
// serves the Auth's users as resources in the application's collection, beside the
// application's own: who may sign in, which roles they hold, and the operator's
// actions on an account (a temporary password, an unlinked identity). The rows are
// composed in Go from two stores that need not share a database, the session library's
// users and identities and the access library's memberships, so a grant's condition is
// evaluated here over the composed row (condition.Evaluate) rather than lowered into a
// query. Everything else is the collection's: the role file grants the resources with
// the same grammar as the application's resources, the permission digest tells a page
// what the caller may do, and the capability envelope rides every row.
//
// Each Auth gets one partition per permission scope, since a resource has one scope and
// a global role grants only global resources:
//
//   - The global partition, <Auth>Users, <Auth>Seats, <Auth>Roles and
//     <Auth>Invitations: every user, the seats held in the global scope and in every
//     tenant, the roles those seats may take, and the invitations that stand into
//     them. With them Set<Auth>UserPassword and Unlink<Auth>UserIdentity, which target
//     <Auth>Users.
//   - The tenant partition under the tenant segment, <Auth><Tenant>Users,
//     <Auth><Tenant>Seats, <Auth><Tenant>Roles and <Auth><Tenant>Invitations: the
//     users seated in the tenant or everywhere, their seats there, the roles a tenant
//     seat may take, and the invitations that stand into the tenant. With them
//     Set<Auth><Tenant>UserPassword and Unlink<Auth><Tenant>UserIdentity.
//
// A seat is a row of its own, (user, role), so a role file limits what a delegate may
// hand out with the post-image: Create on <Auth><Tenant>Seats under
// new.role IN ('Member', 'Clerk'); an invitation carries its roles, and a condition on
// role is judged once per role it carries, so the same grant limits an invitation. The
// rules no role file writes are the library's: nobody deactivates, deletes or unseats
// themselves; a user seated outside the tenant is managed from the global partition,
// so the tenant partition refuses the account-level actions on them; a user whose roles
// a directory writes (a registration declared SyncsRoles) is not seated by hand; a
// person whose last seat anywhere is removed is deleted with their identity links (the
// orphan rule), and a tenant's delete of a user removes their seats there, the account
// going only when no seat remains anywhere.
//
// Two doors lead into a tenant: an invitation, mailed to an address with the roles its
// acceptance grants (the session library's invitation, accepted by whoever signs in or
// signs up at it; SeatInvited is the acceptance hook that writes the seats), and the
// direct seat of a person the tenant's own directory has vouched for: a user who signed
// in through a directory the tenant declares (WithTenantDirectories) may be seated in
// the tenant without holding a seat there first.

// UserAuth marks one Auth whose users the library manages. The generated router
// package declares a type per Auth whose method returns the declaration the generator
// derived from the Auth's methods; the surface, its rows and its handlers are
// instantiated with that type, so each Auth's resources are distinct Go types carrying
// their own names into the collection.
type UserAuth interface {
	UserSurfaceDeclaration() UserSurfaceDeclaration
}

// UserSurfaceDeclaration is what the generator derives for one Auth: the names its
// resources are built from, the tenant route's parameter, and the shape of the surface.
type UserSurfaceDeclaration struct {
	// Auth is the Auth's name in PascalCase, the resource names' prefix: Crew gives
	// CrewUsers.
	Auth string
	// Tenant is the application's tenant record in PascalCase (Sector), the tenant
	// partition's infix: CrewSectorUsers. Empty where the application has no tenancy,
	// which leaves the surface its global partition alone.
	Tenant string
	// DomainParam is the route parameter the tenant partition reads the tenant from,
	// the one the generated router's domain segment declares.
	DomainParam httpio.ParamType
	// Shape is what the Auth's sign-in methods give the surface.
	Shape UserSurfaceShape
}

// UserSurfaceShape is what an Auth's methods give its surface: a password method gives
// creation with a password, the temporary password and its forced change; the
// registrations give the sign-in means a row shows and, declared invited-only, creation
// without a password; a registration that writes the roles from its directory makes the
// role part read-only for the users it signs in.
type UserSurfaceShape struct {
	Password bool
	// UsernameIsEmail marks a password method declared with the username as the
	// person's email address (session.UsernameIsEmail): the address columns derive
	// from the username, so the surface takes no address on a user and refuses a
	// rename, which the session library refuses too; a verified new address renames
	// the user (SeatsFollowRename keeps the seats with them).
	UsernameIsEmail bool
	// OpenSignUp marks a password method that offers sign-up to anyone with an address
	// (session.OpenSignUp): a fact for the client descriptor, since the login page
	// serves the sign-up; the surface's rows are the same.
	OpenSignUp    bool
	Registrations []UserRegistration
}

// UserRegistration is one external registration on the Auth, as the generator
// declared it.
type UserRegistration struct {
	// Name is the registration's name on the Auth (google, lakeside-entra), Kind the
	// method behind it (google, azure, workos), Label what a page shows for it.
	Name  string
	Kind  string
	Label string
	// Invited marks a registration admitting invited users only: the surface creates
	// the user, without a password, and the person signs in through the directory.
	Invited bool
	// SyncsRoles marks a registration that writes the roles from its directory at every
	// sign-in. Such a registration stands alone on its Auth (Validate refuses another
	// method beside it), so the directory is the authority for everyone the surface
	// holds: every row says rolesSynced, and the seats are shown and never written here.
	SyncsRoles bool
}

// CreatesUsers reports whether the surface creates users: a password method or an
// invited-only registration gives it that; a directory that admits its own members
// alone leaves creation to the first sign-in.
func (s UserSurfaceShape) CreatesUsers() bool {
	if s.Password {
		return true
	}

	return slices.ContainsFunc(s.Registrations, func(r UserRegistration) bool { return r.Invited })
}

// passwordOnCreate says what a create does with a password: required where the
// password is the only way in, optional beside an invited-only registration, refused
// where no password method exists.
type passwordOnCreate int

const (
	passwordRequired passwordOnCreate = iota
	passwordOptional
	passwordRefused
)

func (s UserSurfaceShape) passwordOnCreate() passwordOnCreate {
	switch {
	case !s.Password:
		return passwordRefused
	case slices.ContainsFunc(s.Registrations, func(r UserRegistration) bool { return r.Invited }):
		return passwordOptional
	default:
		return passwordRequired
	}
}

// rolesSynced reports whether a directory writes the roles of everyone the surface
// holds: the Auth's registration syncs roles. Such a registration stands alone on its
// Auth, so the fact is the Auth's, never read from an identity link (a link carries the
// directory's connection, which several registrations may select, and the registration
// that wrote it is not recorded).
func (s UserSurfaceShape) rolesSynced() bool {
	return slices.ContainsFunc(s.Registrations, func(r UserRegistration) bool { return r.SyncsRoles })
}

// Validate refuses a declaration the surface cannot serve: an Auth name that is not
// PascalCase, a tenant without its route parameter, a registration without a name, a
// registration that syncs roles beside another method.
func (d *UserSurfaceDeclaration) Validate() error {
	if !pascalName(d.Auth) {
		return errors.Newf("resource.UserSurfaceDeclaration: Auth %q is not a PascalCase name", d.Auth)
	}
	if d.Tenant != "" && !pascalName(d.Tenant) {
		return errors.Newf("resource.UserSurfaceDeclaration: Tenant %q is not a PascalCase name", d.Tenant)
	}
	if d.Tenant != "" && d.DomainParam == "" {
		return errors.New("resource.UserSurfaceDeclaration: a tenant partition needs the tenant route's parameter (DomainParam)")
	}
	seen := make(map[string]bool, len(d.Shape.Registrations))
	for _, r := range d.Shape.Registrations {
		if r.Name == "" {
			return errors.New("resource.UserSurfaceDeclaration: a registration needs its name on the Auth")
		}
		if seen[r.Name] {
			return errors.Newf("resource.UserSurfaceDeclaration: registration %q is declared twice", r.Name)
		}
		seen[r.Name] = true
	}
	for _, r := range d.Shape.Registrations {
		if r.SyncsRoles && (d.Shape.Password || len(d.Shape.Registrations) > 1) {
			return errors.Newf("resource.UserSurfaceDeclaration: registration %q syncs roles beside another method: a registration whose directory writes the roles stands alone on its Auth", r.Name)
		}
	}

	return nil
}

// pascalName reports whether name opens with an upper-case letter and carries letters
// and digits alone.
func pascalName(name string) bool {
	if name == "" || name[0] < 'A' || name[0] > 'Z' {
		return false
	}
	for _, r := range name {
		if (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}

	return true
}

// Names derives the resource, route and parameter names of the declaration's
// partitions: what the collection registers, the router mounts and the client
// descriptor carries, computed once here so every consumer spells them alike.
func (d *UserSurfaceDeclaration) Names() UserResourceNames {
	names := UserResourceNames{Global: partitionNames(d.Auth, "")}
	if d.Tenant != "" {
		names.Tenant = partitionNames(d.Auth, d.Tenant)
		names.Tenant.Tenanted = true
	}

	return names
}

// UserResourceNames are the names of one Auth's surface: the global partition's and,
// where the application has tenancy, the tenant partition's.
type UserResourceNames struct {
	Global UserPartitionNames
	// Tenant is the zero value where the application has no tenancy (Tenanted false).
	Tenant UserPartitionNames
}

// UserPartitionNames are one partition's names: its four resources and two methods,
// the route segments they are served at under the outlet's prefix (and, tenanted, the
// domain segment), and the parameters of their read routes.
type UserPartitionNames struct {
	Tenanted bool

	Users, Seats, Roles, Invitations, SetPassword, UnlinkIdentity accesstypes.Resource

	UsersRoute, SeatsRoute, RolesRoute, InvitationsRoute, SetPasswordRoute, UnlinkIdentityRoute string

	// UserParam is the users read route's parameter (crewUserID), SeatUserParam and
	// SeatRoleParam the seats read route's, RoleParam the roles read route's,
	// InvitationParam the invitations read route's.
	UserParam, SeatUserParam, SeatRoleParam, RoleParam, InvitationParam httpio.ParamType
}

// partitionNames spells one partition: <Auth><Tenant>Users and so on, their kebab-case
// routes, and the read parameters named the way the generator names every read route's
// (the singular resource and its key field in lowerCamelCase).
func partitionNames(auth, tenant string) UserPartitionNames {
	prefix := auth + tenant
	infix := auth + tenant + "User"
	n := UserPartitionNames{
		Users:           accesstypes.Resource(prefix + "Users"),
		Seats:           accesstypes.Resource(prefix + "Seats"),
		Roles:           accesstypes.Resource(prefix + "Roles"),
		Invitations:     accesstypes.Resource(prefix + "Invitations"),
		SetPassword:     accesstypes.Resource("Set" + infix + "Password"),
		UnlinkIdentity:  accesstypes.Resource("Unlink" + infix + "Identity"),
		UserParam:       httpio.ParamType(strcase.ToGoCamel(prefix + "UserID")),
		SeatUserParam:   httpio.ParamType(strcase.ToGoCamel(prefix + "SeatUser")),
		SeatRoleParam:   httpio.ParamType(strcase.ToGoCamel(prefix + "SeatRole")),
		RoleParam:       httpio.ParamType(strcase.ToGoCamel(prefix + "RoleName")),
		InvitationParam: httpio.ParamType(strcase.ToGoCamel(prefix + "InvitationID")),
	}
	n.UsersRoute = strcase.ToKebab(string(n.Users))
	n.SeatsRoute = strcase.ToKebab(string(n.Seats))
	n.RolesRoute = strcase.ToKebab(string(n.Roles))
	n.InvitationsRoute = strcase.ToKebab(string(n.Invitations))
	n.SetPasswordRoute = strcase.ToKebab(string(n.SetPassword))
	n.UnlinkIdentityRoute = strcase.ToKebab(string(n.UnlinkIdentity))

	return n
}

// Resources lists the partition's resources and methods, the names a role file grants.
func (n *UserPartitionNames) Resources() []accesstypes.Resource {
	return []accesstypes.Resource{n.Users, n.Seats, n.Roles, n.Invitations, n.SetPassword, n.UnlinkIdentity}
}

// Methods lists the partition's methods, which target its users resource.
func (n *UserPartitionNames) Methods() []accesstypes.Resource {
	return []accesstypes.Resource{n.SetPassword, n.UnlinkIdentity}
}

// UserAccount is one user as the Auth's account store holds it.
type UserAccount struct {
	ID       ccc.UUID
	Username string
	// HasPassword reports a password on the account; PasswordChangeRequired that the
	// password is one an operator set, which the person changes before anything else
	// is served.
	HasPassword            bool
	PasswordChangeRequired bool
	Disabled               bool
	CreatedAt              time.Time
	LastSignInAt           *time.Time
	// Email is the person's address, nil for none, and EmailVerifiedAt when the person
	// proved it by following a link sent to it, nil while it is an operator's entry
	// (the session library's two states: verified before trusted).
	Email           *string
	EmailVerifiedAt *time.Time
}

// UserInvitation is one standing invitation as the Auth's account store holds it: an
// address invited to hold roles, in a tenant or on the global surface, until it is
// accepted, revoked or expires.
type UserInvitation struct {
	ID    ccc.UUID
	Email string
	// Tenant is the tenant the invitation seats the person in, "" for the global
	// partition.
	Tenant string
	// Roles are the roles the acceptance grants.
	Roles []string
	// InvitedBy is the user who sent it, null when unknown.
	InvitedBy ccc.NullUUID
	CreatedAt time.Time
	ExpiresAt time.Time
}

// UserInvitationRequest is what the surface asks the account store to invite: the
// address, the tenant ("" for the global partition), the roles, and the caller.
type UserInvitationRequest struct {
	Email     string
	Tenant    string
	Roles     []string
	InvitedBy ccc.NullUUID
}

// UserIdentity is one external identity linked to a user: the way they sign in through a
// registration. It is a field of the user row, so its wire shape is fixed here.
type UserIdentity struct {
	ID ccc.UUID `json:"id"`
	// Method is the method's kind (google, azure, workos), Connection the registration
	// the identity was linked through, Subject the directory's identifier.
	Method     string `json:"method"`
	Connection string `json:"connection"`
	Subject    string `json:"subject"`
	// Organization is the tenant key the registration stored on the link: the Google
	// Workspace, the Entra tenant, the WorkOS organization; nil where it stored none.
	Organization *string `json:"organization"`
	// Email is the address the directory asserted when the identity was linked.
	Email      *string   `json:"email"`
	LinkedAt   time.Time `json:"linkedAt"`
	LastUsedAt time.Time `json:"lastUsedAt"`
}

// UserAccounts is the account store the surface serves: the session library's users,
// identities and passwords of one Auth, as sessionusers.Accounts adapts them. A user
// the store does not hold answers an error httpio.HasNotFound reports.
type UserAccounts interface {
	// Users lists every account, ordered by username.
	Users(ctx context.Context) ([]UserAccount, error)
	User(ctx context.Context, id ccc.UUID) (*UserAccount, error)
	// CreateUser adds an account; a password given is one an operator set, so the
	// account carries the forced-change mark. Nil creates a password-less account that
	// signs in through a registration. An email given is an operator's entry of the
	// person's address, unverified, which reserves the address; nil enters none.
	CreateUser(ctx context.Context, username string, password, email *string, disabled bool) (ccc.UUID, error)
	RenameUser(ctx context.Context, id ccc.UUID, username string) error
	// SetUserEmail is an operator's entry of the person's address, unverified, or its
	// removal (nil). Refused (Conflict) where the username is the address.
	SetUserEmail(ctx context.Context, id ccc.UUID, email *string) error
	// Invite stores an invitation and mails its link; the invitation is returned as
	// stored. Invitations lists the ones that stand, and RevokeInvitation withdraws one
	// (NotFound when it no longer stands).
	Invite(ctx context.Context, req *UserInvitationRequest) (*UserInvitation, error)
	Invitations(ctx context.Context) ([]UserInvitation, error)
	RevokeInvitation(ctx context.Context, id ccc.UUID) error
	SetUserDisabled(ctx context.Context, id ccc.UUID, disabled bool) error
	// DeleteUser removes the account with its identities and sessions.
	DeleteUser(ctx context.Context, id ccc.UUID) error
	// ResetUserPassword sets a temporary password an operator chose: the account
	// takes the forced-change mark and its sessions end.
	ResetUserPassword(ctx context.Context, id ccc.UUID, temporary string) error
	Identities(ctx context.Context, id ccc.UUID) ([]UserIdentity, error)
	UnlinkIdentity(ctx context.Context, identityID ccc.UUID) error
}

// UserSeats is the membership store the surface seats and unseats through: the access
// library's UserManager, structurally.
type UserSeats interface {
	AddUserRoles(ctx context.Context, scope accesstypes.PolicyScope, user accesstypes.User, roles ...accesstypes.Role) error
	DeleteUserRoles(ctx context.Context, scope accesstypes.PolicyScope, user accesstypes.User, roles ...accesstypes.Role) error
	// UserRoles returns the user's memberships by where each is held: every one with no
	// scopes given, those of each given scope otherwise.
	UserRoles(ctx context.Context, user accesstypes.User, scopes ...accesstypes.PolicyScope) (accesstypes.RoleCollection, error)
	RoleUsers(ctx context.Context, scope accesstypes.PolicyScope, role accesstypes.Role) ([]accesstypes.User, error)
	// Roles lists the roles that exist in scope: the default roles of the scope's kind
	// and the custom roles held there.
	Roles(ctx context.Context, scope accesstypes.PolicyScope) ([]accesstypes.Role, error)
}

// UserApp is the application surface the user handlers draw on: the decoder seam, the
// cursor key the lists page with, and the database client, whose type decides where a
// sort places NULL.
type UserApp interface {
	DecoderAccessor
	CursorKey() *CursorKey
	ResourceClient() Client
}

// UserSurface serves one Auth's users: the handlers of both partitions over the Auth's
// account store and membership store. The generated router package builds one per Auth
// (New<Auth>UserSurface) and the application hands it back through <Auth>UserSurface(),
// which the generated handlers call.
type UserSurface[A UserAuth] struct {
	decl     UserSurfaceDeclaration
	names    UserResourceNames
	accounts UserAccounts
	seats    UserSeats
	settings userSurfaceSettings
}

// userSurfaceSettings is what the options set.
type userSurfaceSettings struct {
	declarations TenantDeclarations
}

// UserSurfaceOption configures a surface at construction.
type UserSurfaceOption func(*userSurfaceSettings)

// WithTenantDirectories gives the surface what each tenant declares about who may
// enter it, which opens the second door into a tenant: a user who signed in through a
// directory the tenant declares may be seated in the tenant directly, without holding
// a seat there first (the person entered through the tenant's own directory, was told
// they have no access yet, and asked their administrator). Without it a tenant seat
// goes to the tenant's users alone, and anyone else enters by invitation. A tenanted
// application passes its roster's Declarations.
func WithTenantDirectories(declarations TenantDeclarations) UserSurfaceOption {
	return func(s *userSurfaceSettings) {
		s.declarations = declarations
	}
}

// NewUserSurface builds the surface of the Auth A over its stores. It panics on a
// declaration the surface cannot serve and on a nil store: both are programming errors
// surfaced where the application wires its handlers.
func NewUserSurface[A UserAuth](accounts UserAccounts, seats UserSeats, opts ...UserSurfaceOption) *UserSurface[A] {
	var a A
	decl := a.UserSurfaceDeclaration()
	if err := decl.Validate(); err != nil {
		panic(err)
	}
	if accounts == nil || seats == nil {
		panic("resource.NewUserSurface: the account store and the membership store are required")
	}
	s := &UserSurface[A]{decl: decl, names: decl.Names(), accounts: accounts, seats: seats}
	for _, opt := range opts {
		opt(&s.settings)
	}

	return s
}

// Declaration returns the surface's declaration.
func (s *UserSurface[A]) Declaration() UserSurfaceDeclaration {
	return s.decl
}

// Names returns the surface's names.
func (s *UserSurface[A]) Names() UserResourceNames {
	return s.names
}

// SeatStore returns the membership store the surface seats and unseats through: the
// Auth's engine's user manager. The generated create-your-tenant route seats a tenant's
// first administrator through it, the one membership written outside the surface's
// handlers.
func (s *UserSurface[A]) SeatStore() UserSeats {
	return s.seats
}

// namesOf is the Auth's names, for the row types' Resource methods.
func namesOf[A UserAuth]() UserResourceNames {
	var a A
	decl := a.UserSurfaceDeclaration()

	return decl.Names()
}

// userPartition is the scope one request runs in: the global partition, or one tenant
// of the tenant partition.
type userPartition struct {
	names  UserPartitionNames
	scope  accesstypes.Scope
	domain accesstypes.Domain
	// seatScopes are the policy scopes whose seats the partition shows: the global and
	// every-domain scopes, or the tenant's own and every-domain.
	seatScopes []accesstypes.PolicyScope
}

// ownScope is where a seat made in the partition is held: the global scope for a
// global-kind role, every domain for a domain-kind role, the tenant's domain in the
// tenant partition.
func (p *userPartition) ownScopes() []accesstypes.PolicyScope {
	if p.scope.IsGlobal() {
		return []accesstypes.PolicyScope{accesstypes.GlobalPolicyScope(), accesstypes.EveryDomainPolicyScope()}
	}

	return []accesstypes.PolicyScope{accesstypes.DomainPolicyScope(p.domain)}
}

func (s *UserSurface[A]) globalPartition() *userPartition {
	return &userPartition{
		names:      s.names.Global,
		scope:      accesstypes.GlobalScope(),
		seatScopes: []accesstypes.PolicyScope{accesstypes.GlobalPolicyScope(), accesstypes.EveryDomainPolicyScope()},
	}
}

func (s *UserSurface[A]) tenantPartition(domain accesstypes.Domain) *userPartition {
	return &userPartition{
		names:      s.names.Tenant,
		scope:      accesstypes.DomainScope(domain),
		domain:     domain,
		seatScopes: []accesstypes.PolicyScope{accesstypes.DomainPolicyScope(domain), accesstypes.EveryDomainPolicyScope()},
	}
}

// requireTenant panics where the application has no tenancy: the generator emits the
// tenant handlers only where it does, so reaching here is a wiring error.
func (s *UserSurface[A]) requireTenant() {
	if !s.names.Tenant.Tenanted {
		panic("resource.UserSurface: the application declares no tenant record, so the surface has no tenant partition")
	}
}

// userKey names a user to the membership store: the username, which is the access
// library's user identifier.
func userKey(username string) accesstypes.User {
	return accesstypes.User(username)
}

// sameUser compares usernames the way the account store matches them, without regard
// to case.
func sameUser(a, b string) bool {
	return strings.EqualFold(a, b)
}
