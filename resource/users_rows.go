package resource

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/go-playground/errors/v5"
)

// The rows of the user surface and their composition. Each row type is generic over
// the Auth so its Resource is the Auth's own name; the global and tenant partitions have
// distinct types because a resource has one permission scope.

// UserRow is one user of the global partition (<Auth>Users): the account, its sign-in
// means, and the roles it holds globally and in every tenant.
type UserRow[A UserAuth] struct {
	ID                     ccc.UUID       `json:"id"                     spanner:"Id"`
	Username               string         `json:"username"               spanner:"Username"`
	Disabled               bool           `json:"disabled"               spanner:"Disabled"`
	HasPassword            bool           `json:"hasPassword"            spanner:"HasPassword"`
	PasswordChangeRequired bool           `json:"passwordChangeRequired" spanner:"PasswordChangeRequired"`
	CreatedAt              time.Time      `json:"createdAt"              spanner:"CreatedAt"`
	LastSignInAt           *time.Time     `json:"lastSignInAt"           spanner:"LastSignInAt"`
	Organization           *string        `json:"organization"           spanner:"Organization"`
	Identities             []UserIdentity `json:"identities"             spanner:"Identities"`
	// Roles are the roles held in the partition's own scope, EverywhereRoles the ones
	// held in every tenant, and RolesSynced that a directory writes them.
	Roles           []string `json:"roles"           spanner:"Roles"`
	EverywhereRoles []string `json:"everywhereRoles" spanner:"EverywhereRoles"`
	RolesSynced     bool     `json:"rolesSynced"     spanner:"RolesSynced"`
}

// Resource is the Auth's name for the resource.
func (UserRow[A]) Resource() accesstypes.Resource {
	return namesOf[A]().Global.Users
}

// DefaultConfig tracks nothing: the rows are composed, not stored.
func (UserRow[A]) DefaultConfig() Config {
	return Config{}
}

// TenantUserRow is one user of the tenant partition (<Auth><Tenant>Users): a user
// seated in the tenant or everywhere, with the seats held there.
type TenantUserRow[A UserAuth] struct {
	ID                     ccc.UUID       `json:"id"                     spanner:"Id"`
	Username               string         `json:"username"               spanner:"Username"`
	Disabled               bool           `json:"disabled"               spanner:"Disabled"`
	HasPassword            bool           `json:"hasPassword"            spanner:"HasPassword"`
	PasswordChangeRequired bool           `json:"passwordChangeRequired" spanner:"PasswordChangeRequired"`
	CreatedAt              time.Time      `json:"createdAt"              spanner:"CreatedAt"`
	LastSignInAt           *time.Time     `json:"lastSignInAt"           spanner:"LastSignInAt"`
	Organization           *string        `json:"organization"           spanner:"Organization"`
	Identities             []UserIdentity `json:"identities"             spanner:"Identities"`
	Roles                  []string       `json:"roles"                  spanner:"Roles"`
	EverywhereRoles        []string       `json:"everywhereRoles"        spanner:"EverywhereRoles"`
	RolesSynced            bool           `json:"rolesSynced"            spanner:"RolesSynced"`
}

// Resource is the Auth's name for the resource.
func (TenantUserRow[A]) Resource() accesstypes.Resource {
	return namesOf[A]().Tenant.Users
}

// DefaultConfig tracks nothing: the rows are composed, not stored.
func (TenantUserRow[A]) DefaultConfig() Config {
	return Config{}
}

// SeatRow is one seat of the global partition (<Auth>Seats): a user holding a role
// globally, or in every tenant when the role is of the tenant kind (EveryDomain).
type SeatRow[A UserAuth] struct {
	User        ccc.UUID `json:"user"        spanner:"User"`
	Role        string   `json:"role"        spanner:"Role"`
	Username    string   `json:"username"    spanner:"Username"`
	EveryDomain bool     `json:"everyDomain" spanner:"EveryDomain"`
}

// Resource is the Auth's name for the resource.
func (SeatRow[A]) Resource() accesstypes.Resource {
	return namesOf[A]().Global.Seats
}

// DefaultConfig tracks nothing: the rows are composed, not stored.
func (SeatRow[A]) DefaultConfig() Config {
	return Config{}
}

// TenantSeatRow is one seat of the tenant partition (<Auth><Tenant>Seats): a user
// holding a role in the tenant, or everywhere (Everywhere), which the tenant partition
// shows and never writes.
type TenantSeatRow[A UserAuth] struct {
	User       ccc.UUID `json:"user"       spanner:"User"`
	Role       string   `json:"role"       spanner:"Role"`
	Username   string   `json:"username"   spanner:"Username"`
	Everywhere bool     `json:"everywhere" spanner:"Everywhere"`
}

// Resource is the Auth's name for the resource.
func (TenantSeatRow[A]) Resource() accesstypes.Resource {
	return namesOf[A]().Tenant.Seats
}

// DefaultConfig tracks nothing: the rows are composed, not stored.
func (TenantSeatRow[A]) DefaultConfig() Config {
	return Config{}
}

// RoleRow is one role a global seat may take (<Auth>Roles): a role of the global kind,
// held in the global scope, or of the domain kind, held in every tenant.
type RoleRow[A UserAuth] struct {
	Name string `json:"name" spanner:"Name"`
	Kind string `json:"kind" spanner:"Kind"`
}

// Resource is the Auth's name for the resource.
func (RoleRow[A]) Resource() accesstypes.Resource {
	return namesOf[A]().Global.Roles
}

// DefaultConfig tracks nothing: the rows are composed, not stored.
func (RoleRow[A]) DefaultConfig() Config {
	return Config{}
}

// TenantRoleRow is one role a tenant seat may take (<Auth><Tenant>Roles).
type TenantRoleRow[A UserAuth] struct {
	Name string `json:"name" spanner:"Name"`
}

// Resource is the Auth's name for the resource.
func (TenantRoleRow[A]) Resource() accesstypes.Resource {
	return namesOf[A]().Tenant.Roles
}

// DefaultConfig tracks nothing: the rows are composed, not stored.
func (TenantRoleRow[A]) DefaultConfig() Config {
	return Config{}
}

// The kinds a role row reports.
const (
	roleKindGlobal = "global"
	roleKindDomain = "domain"
)

// The request structs: the wire shapes of the rows, the fields a List or Read grant may
// name and a write may set. The read shapes mirror the rows; the write shapes carry
// what a create or update takes, the password and the role immutable, so they are the
// create's alone.
type userRequest struct {
	ID                     ccc.UUID       `json:"id"                     index:"true" perm:"-"`
	Username               string         `json:"username"`
	Disabled               bool           `json:"disabled"`
	HasPassword            bool           `json:"hasPassword"`
	PasswordChangeRequired bool           `json:"passwordChangeRequired"`
	CreatedAt              time.Time      `json:"createdAt"`
	LastSignInAt           *time.Time     `json:"lastSignInAt"`
	Organization           *string        `json:"organization"`
	Identities             []UserIdentity `json:"identities"`
	Roles                  []string       `json:"roles"`
	EverywhereRoles        []string       `json:"everywhereRoles"`
	RolesSynced            bool           `json:"rolesSynced"`
}

type userWriteRequest struct {
	Username string  `json:"username"`
	Password *string `json:"password" immutable:"true"`
	Disabled bool    `json:"disabled"`
}

type tenantUserWriteRequest struct {
	Username string  `json:"username"`
	Password *string `json:"password" immutable:"true"`
	Disabled bool    `json:"disabled"`
	// Role is the seat the created user takes in the tenant, which is what makes them
	// the tenant's user.
	Role string `json:"role" immutable:"true"`
}

type seatRequest struct {
	User        ccc.UUID `json:"user"        index:"true" perm:"-"`
	Role        string   `json:"role"        index:"true" perm:"-"`
	Username    string   `json:"username"`
	EveryDomain bool     `json:"everyDomain"`
}

type seatWriteRequest struct {
	User ccc.UUID `json:"user" immutable:"true"`
	Role string   `json:"role" immutable:"true"`
}

type tenantSeatRequest struct {
	User       ccc.UUID `json:"user"       index:"true" perm:"-"`
	Role       string   `json:"role"       index:"true" perm:"-"`
	Username   string   `json:"username"`
	Everywhere bool     `json:"everywhere"`
}

type roleRequest struct {
	Name string `json:"name" index:"true" perm:"-"`
	Kind string `json:"kind"`
}

type tenantRoleRequest struct {
	Name string `json:"name" index:"true" perm:"-"`
}

// The Go field names the handlers address.
const (
	userFieldID       accesstypes.Field = "ID"
	userFieldUsername accesstypes.Field = "Username"
	userFieldPassword accesstypes.Field = "Password"
	userFieldDisabled accesstypes.Field = "Disabled"
	userFieldRole     accesstypes.Field = "Role"
	seatFieldUser     accesstypes.Field = "User"
	seatFieldRole     accesstypes.Field = "Role"
	roleFieldName     accesstypes.Field = "Name"
)

// The default orders: users by username then id, seats by username then role, roles by
// name. The lists are total with the key fields the decoder appends.
var (
	usersOrder = Paging{Order: []SortField{{Field: string(userFieldUsername), Direction: SortAscending}}}
	seatsOrder = Paging{Order: []SortField{{Field: "Username", Direction: SortAscending}, {Field: string(seatFieldRole), Direction: SortAscending}}}
	rolesOrder = Paging{Order: []SortField{{Field: string(roleFieldName), Direction: SortAscending}}}
)

// fieldTagsOf reads a request struct's field registrations, the way the generator
// registers a generated resource's.
func fieldTagsOf[Request any]() []FieldTags {
	t := reflect.TypeFor[Request]()
	fields := make([]FieldTags, 0, t.NumField())
	for field := range t.Fields() {
		fields = append(fields, FieldTagsFromStructTag(accesstypes.Field(field.Name), field.Tag))
	}

	return fields
}

// userSnapshot is one request's view of a partition: the accounts, their identities as
// needed, and the seats held in the partition's scopes with the roles that exist there.
// Composed once per request, so a list and the rules it applies see one state.
type userSnapshot struct {
	shape     UserSurfaceShape
	accounts  UserAccounts
	partition *userPartition

	users []UserAccount
	byID  map[ccc.UUID]*UserAccount
	// roles are the roles that exist in each of the partition's scopes, sorted; seats
	// the roles each user holds in each, by the membership store's user key.
	roles map[accesstypes.PolicyScope][]accesstypes.Role
	seats map[accesstypes.User]map[accesstypes.PolicyScope][]accesstypes.Role

	identities map[ccc.UUID][]UserIdentity
}

// snapshot composes the partition's state.
func (s *UserSurface[A]) snapshot(ctx context.Context, p *userPartition) (*userSnapshot, error) {
	users, err := s.accounts.Users(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "resource.UserAccounts.Users()")
	}
	snap := &userSnapshot{
		shape:      s.decl.Shape,
		accounts:   s.accounts,
		partition:  p,
		users:      users,
		byID:       make(map[ccc.UUID]*UserAccount, len(users)),
		roles:      make(map[accesstypes.PolicyScope][]accesstypes.Role, len(p.seatScopes)),
		seats:      make(map[accesstypes.User]map[accesstypes.PolicyScope][]accesstypes.Role),
		identities: make(map[ccc.UUID][]UserIdentity),
	}
	for i := range snap.users {
		snap.byID[snap.users[i].ID] = &snap.users[i]
	}
	for _, scope := range p.seatScopes {
		roles, err := s.seats.Roles(ctx, scope)
		if err != nil {
			return nil, errors.Wrap(err, "resource.UserSeats.Roles()")
		}
		slices.Sort(roles)
		snap.roles[scope] = roles
		for _, role := range roles {
			holders, err := s.seats.RoleUsers(ctx, scope, role)
			if err != nil {
				return nil, errors.Wrap(err, "resource.UserSeats.RoleUsers()")
			}
			for _, holder := range holders {
				held := snap.seats[holder]
				if held == nil {
					held = make(map[accesstypes.PolicyScope][]accesstypes.Role)
					snap.seats[holder] = held
				}
				held[scope] = append(held[scope], role)
			}
		}
	}

	return snap, nil
}

// byUsername finds an account by username, without regard to case.
func (snap *userSnapshot) byUsername(username string) *UserAccount {
	for i := range snap.users {
		if sameUser(snap.users[i].Username, username) {
			return &snap.users[i]
		}
	}

	return nil
}

// identitiesOf loads a user's identities once.
func (snap *userSnapshot) identitiesOf(ctx context.Context, id ccc.UUID) ([]UserIdentity, error) {
	if identities, ok := snap.identities[id]; ok {
		return identities, nil
	}
	identities, err := snap.accounts.Identities(ctx, id)
	if err != nil {
		return nil, errors.Wrap(err, "resource.UserAccounts.Identities()")
	}
	if identities == nil {
		identities = []UserIdentity{}
	}
	snap.identities[id] = identities

	return identities, nil
}

// heldRoles returns the roles a user holds in one scope, sorted, never nil.
func (snap *userSnapshot) heldRoles(username string, scope accesstypes.PolicyScope) []string {
	held := snap.seats[userKey(username)][scope]
	roles := make([]string, 0, len(held))
	for _, role := range held {
		roles = append(roles, string(role))
	}
	slices.Sort(roles)

	return roles
}

// inPartition reports whether a user belongs to the partition: everyone in the global
// partition, in the tenant partition those seated in the tenant or everywhere.
func (snap *userSnapshot) inPartition(username string) bool {
	if snap.partition.scope.IsGlobal() {
		return true
	}
	for _, scope := range snap.partition.seatScopes {
		if len(snap.seats[userKey(username)][scope]) > 0 {
			return true
		}
	}

	return false
}

// userFacts are the composed facts of one user beyond the account: what the identities
// say.
type userFacts struct {
	organization *string
	identities   []UserIdentity
	rolesSynced  bool
}

// factsOf composes a user's identity-derived facts: the organization every link
// agrees on (nil where they do not, or there is none), and whether a directory writes
// the roles.
func (snap *userSnapshot) factsOf(ctx context.Context, account *UserAccount) (*userFacts, error) {
	identities, err := snap.identitiesOf(ctx, account.ID)
	if err != nil {
		return nil, err
	}
	facts := &userFacts{identities: identities, rolesSynced: snap.shape.syncsRoles(identities)}
	for i := range identities {
		org := identities[i].Organization
		if org == nil {
			continue
		}
		if facts.organization == nil {
			facts.organization = org

			continue
		}
		if !strings.EqualFold(*facts.organization, *org) {
			facts.organization = nil

			break
		}
	}

	return facts, nil
}

// userRowOf composes a user's row in the partition.
func (snap *userSnapshot) userRowOf(ctx context.Context, account *UserAccount) (*composedUser, error) {
	facts, err := snap.factsOf(ctx, account)
	if err != nil {
		return nil, err
	}
	own := snap.partition.seatScopes[0]
	row := &composedUser{
		account:         account,
		facts:           facts,
		roles:           snap.heldRoles(account.Username, own),
		everywhereRoles: snap.heldRoles(account.Username, accesstypes.EveryDomainPolicyScope()),
	}

	return row, nil
}

// composedUser is a user row before it is typed for a partition.
type composedUser struct {
	account         *UserAccount
	facts           *userFacts
	roles           []string
	everywhereRoles []string
}

// image is the row's attribute values by binding name, what a condition reads.
func (u *composedUser) image() map[string]any {
	image := map[string]any{
		userAttrUsername:               u.account.Username,
		userAttrDisabled:               u.account.Disabled,
		userAttrHasPassword:            u.account.HasPassword,
		userAttrPasswordChangeRequired: u.account.PasswordChangeRequired,
		userAttrCreatedAt:              u.account.CreatedAt,
		userAttrLastSignInAt:           nil,
		userAttrOrganization:           nil,
		userAttrRolesSynced:            u.facts.rolesSynced,
	}
	if u.account.LastSignInAt != nil {
		image[userAttrLastSignInAt] = *u.account.LastSignInAt
	}
	if u.facts.organization != nil {
		image[userAttrOrganization] = *u.facts.organization
	}

	return image
}

// toUserRow types the composed user as the global partition's row.
func toUserRow[A UserAuth](u *composedUser) *UserRow[A] {
	return &UserRow[A]{
		ID:                     u.account.ID,
		Username:               u.account.Username,
		Disabled:               u.account.Disabled,
		HasPassword:            u.account.HasPassword,
		PasswordChangeRequired: u.account.PasswordChangeRequired,
		CreatedAt:              u.account.CreatedAt,
		LastSignInAt:           u.account.LastSignInAt,
		Organization:           u.facts.organization,
		Identities:             u.facts.identities,
		Roles:                  u.roles,
		EverywhereRoles:        u.everywhereRoles,
		RolesSynced:            u.facts.rolesSynced,
	}
}

// toTenantUserRow types the composed user as the tenant partition's row.
func toTenantUserRow[A UserAuth](u *composedUser) *TenantUserRow[A] {
	return &TenantUserRow[A]{
		ID:                     u.account.ID,
		Username:               u.account.Username,
		Disabled:               u.account.Disabled,
		HasPassword:            u.account.HasPassword,
		PasswordChangeRequired: u.account.PasswordChangeRequired,
		CreatedAt:              u.account.CreatedAt,
		LastSignInAt:           u.account.LastSignInAt,
		Organization:           u.facts.organization,
		Identities:             u.facts.identities,
		Roles:                  u.roles,
		EverywhereRoles:        u.everywhereRoles,
		RolesSynced:            u.facts.rolesSynced,
	}
}

// composedSeat is a seat row before it is typed for a partition.
type composedSeat struct {
	user  *UserAccount
	role  accesstypes.Role
	scope accesstypes.PolicyScope
}

// everywhere reports whether the seat is held in every tenant.
func (s *composedSeat) everywhere() bool {
	return s.scope.IsEveryDomain()
}

// image is the seat's attribute values by binding name.
func (s *composedSeat) image(tenanted bool) map[string]any {
	image := map[string]any{
		seatAttrUser:     s.user.ID.String(),
		seatAttrUsername: s.user.Username,
		seatAttrRole:     string(s.role),
	}
	if tenanted {
		image[seatAttrEverywhere] = s.everywhere()
	} else {
		image[seatAttrEveryDomain] = s.everywhere()
	}

	return image
}

// seatsOf lists the seats of the partition: every (user, role) held in its scopes, for
// users the account store holds.
func (snap *userSnapshot) seatsOf() []*composedSeat {
	var seats []*composedSeat
	for i := range snap.users {
		user := &snap.users[i]
		for _, scope := range snap.partition.seatScopes {
			for _, role := range snap.seats[userKey(user.Username)][scope] {
				seats = append(seats, &composedSeat{user: user, role: role, scope: scope})
			}
		}
	}

	return seats
}

// seatOf finds one seat of the partition by user and role.
func (snap *userSnapshot) seatOf(user *UserAccount, role accesstypes.Role) *composedSeat {
	for _, scope := range snap.partition.seatScopes {
		if slices.Contains(snap.seats[userKey(user.Username)][scope], role) {
			return &composedSeat{user: user, role: role, scope: scope}
		}
	}

	return nil
}

// roleScope finds the scope a role is seated in from the partition: the partition's
// own scopes in order, so in the global partition a role of the global kind is held
// globally and one of the domain kind in every tenant. ok is false for a role that
// exists in none.
func (snap *userSnapshot) roleScope(role accesstypes.Role) (accesstypes.PolicyScope, bool) {
	for _, scope := range snap.partition.ownScopes() {
		if slices.Contains(snap.roles[scope], role) {
			return scope, true
		}
	}

	return accesstypes.PolicyScope{}, false
}

// composedRole is a role row before it is typed for a partition.
type composedRole struct {
	name  accesstypes.Role
	scope accesstypes.PolicyScope
}

func (r *composedRole) kind() string {
	if r.scope.IsGlobal() {
		return roleKindGlobal
	}

	return roleKindDomain
}

// image is the role's attribute values by binding name.
func (r *composedRole) image(tenanted bool) map[string]any {
	image := map[string]any{roleAttrName: string(r.name)}
	if !tenanted {
		image[roleAttrKind] = r.kind()
	}

	return image
}

// rolesOf lists the roles a seat made in the partition may take: the roles of its own
// scopes.
func (snap *userSnapshot) rolesOf() []*composedRole {
	var roles []*composedRole
	for _, scope := range snap.partition.ownScopes() {
		for _, role := range snap.roles[scope] {
			roles = append(roles, &composedRole{name: role, scope: scope})
		}
	}

	return roles
}
