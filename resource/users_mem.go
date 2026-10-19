package resource

import (
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/httpio"
	"github.com/go-playground/errors/v5"
)

// MemUserStore is an in-memory account and seat store: a UserAccounts and a UserSeats
// over maps, for a test harness that builds a user-management surface
// (NewUserSurface) with no session database and no permission engine behind it. The
// generated authorization matrix needs one for every Auth the router binds, since the
// surface's handlers are mounted with the rest. It is safe for concurrent use.
type MemUserStore struct {
	mu         sync.Mutex
	users      []UserAccount
	identities map[ccc.UUID][]UserIdentity
	// seats holds each scope's memberships by user; roles the roles that exist in each
	// scope, which a seat may take.
	seats map[accesstypes.PolicyScope]map[accesstypes.User][]accesstypes.Role
	roles map[accesstypes.PolicyScope][]accesstypes.Role
}

// NewMemUserStore returns an empty store.
func NewMemUserStore() *MemUserStore {
	return &MemUserStore{
		identities: make(map[ccc.UUID][]UserIdentity),
		seats:      make(map[accesstypes.PolicyScope]map[accesstypes.User][]accesstypes.Role),
		roles:      make(map[accesstypes.PolicyScope][]accesstypes.Role),
	}
}

// AddUser adds an account under the username, with a password the person has already
// changed when withPassword is set, and returns its id.
func (s *MemUserStore) AddUser(username string, withPassword bool) ccc.UUID {
	id, err := ccc.NewUUID()
	if err != nil {
		panic(err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.users = append(s.users, UserAccount{ID: id, Username: username, HasPassword: withPassword, CreatedAt: time.Now()})

	return id
}

// DeclareRoles makes the roles exist in the scope, as a role file's roles exist in a
// permission engine's.
func (s *MemUserStore) DeclareRoles(scope accesstypes.PolicyScope, roles ...accesstypes.Role) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.declare(scope, roles...)
}

func (s *MemUserStore) declare(scope accesstypes.PolicyScope, roles ...accesstypes.Role) {
	for _, role := range roles {
		if !slices.Contains(s.roles[scope], role) {
			s.roles[scope] = append(s.roles[scope], role)
		}
	}
	slices.Sort(s.roles[scope])
}

// Seat seats the user in the roles in the scope, declaring the roles.
func (s *MemUserStore) Seat(scope accesstypes.PolicyScope, user accesstypes.User, roles ...accesstypes.Role) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.declare(scope, roles...)
	s.seat(scope, user, roles...)
}

func (s *MemUserStore) seat(scope accesstypes.PolicyScope, user accesstypes.User, roles ...accesstypes.Role) {
	held, ok := s.seats[scope]
	if !ok {
		held = make(map[accesstypes.User][]accesstypes.Role)
		s.seats[scope] = held
	}
	for _, role := range roles {
		if !slices.Contains(held[user], role) {
			held[user] = append(held[user], role)
		}
	}
	slices.Sort(held[user])
}

// Users lists every account, ordered by username.
func (s *MemUserStore) Users(context.Context) ([]UserAccount, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	users := slices.Clone(s.users)
	slices.SortFunc(users, func(a, b UserAccount) int {
		return strings.Compare(strings.ToLower(a.Username), strings.ToLower(b.Username))
	})

	return users, nil
}

func (s *MemUserStore) find(id ccc.UUID) (*UserAccount, error) {
	for i := range s.users {
		if s.users[i].ID == id {
			return &s.users[i], nil
		}
	}

	return nil, httpio.NewNotFoundMessagef("user %s does not exist", id)
}

// User returns one account; a NotFound error where the store holds none under the id.
func (s *MemUserStore) User(_ context.Context, id ccc.UUID) (*UserAccount, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	user, err := s.find(id)
	if err != nil {
		return nil, err
	}
	copied := *user

	return &copied, nil
}

// CreateUser adds an account; a password given marks it for the forced change, as the
// session library does. A username in use is a Conflict.
func (s *MemUserStore) CreateUser(_ context.Context, username string, password *string, disabled bool) (ccc.UUID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.users {
		if strings.EqualFold(s.users[i].Username, username) {
			return ccc.NilUUID, httpio.NewConflictMessagef("username %q is taken", username)
		}
	}
	id, err := ccc.NewUUID()
	if err != nil {
		return ccc.NilUUID, errors.Wrap(err, "ccc.NewUUID()")
	}
	s.users = append(s.users, UserAccount{ID: id, Username: username, HasPassword: password != nil, PasswordChangeRequired: password != nil, Disabled: disabled, CreatedAt: time.Now()})

	return id, nil
}

// RenameUser changes the account's username.
func (s *MemUserStore) RenameUser(_ context.Context, id ccc.UUID, username string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	user, err := s.find(id)
	if err != nil {
		return err
	}
	user.Username = username

	return nil
}

// SetUserDisabled sets the account's disabled flag.
func (s *MemUserStore) SetUserDisabled(_ context.Context, id ccc.UUID, disabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	user, err := s.find(id)
	if err != nil {
		return err
	}
	user.Disabled = disabled

	return nil
}

// DeleteUser removes the account and its identities.
func (s *MemUserStore) DeleteUser(_ context.Context, id ccc.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.find(id); err != nil {
		return err
	}
	s.users = slices.DeleteFunc(s.users, func(u UserAccount) bool { return u.ID == id })
	delete(s.identities, id)

	return nil
}

// ResetUserPassword marks the account for the forced change.
func (s *MemUserStore) ResetUserPassword(_ context.Context, id ccc.UUID, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	user, err := s.find(id)
	if err != nil {
		return err
	}
	user.HasPassword, user.PasswordChangeRequired = true, true

	return nil
}

// LinkIdentity records an identity on the account, for a test that unlinks one.
func (s *MemUserStore) LinkIdentity(id ccc.UUID, identity *UserIdentity) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.identities[id] = append(s.identities[id], *identity)
}

// Identities lists the account's identities.
func (s *MemUserStore) Identities(_ context.Context, id ccc.UUID) ([]UserIdentity, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return slices.Clone(s.identities[id]), nil
}

// UnlinkIdentity removes the identity from whichever account holds it.
func (s *MemUserStore) UnlinkIdentity(_ context.Context, identityID ccc.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for user, identities := range s.identities {
		s.identities[user] = slices.DeleteFunc(identities, func(i UserIdentity) bool { return i.ID == identityID })
	}

	return nil
}

// AddUserRoles seats the user in the roles in the scope; a role that does not exist
// there is an error, as a permission engine's is.
func (s *MemUserStore) AddUserRoles(_ context.Context, scope accesstypes.PolicyScope, user accesstypes.User, roles ...accesstypes.Role) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing := s.rolesIn(scope)
	for _, role := range roles {
		if !slices.Contains(existing, role) {
			return errors.Newf("role %s does not exist in %s", role, scope)
		}
	}
	s.seat(scope, user, roles...)

	return nil
}

// DeleteUserRoles unseats the user from the roles in the scope.
func (s *MemUserStore) DeleteUserRoles(_ context.Context, scope accesstypes.PolicyScope, user accesstypes.User, roles ...accesstypes.Role) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	held := s.seats[scope]
	held[user] = slices.DeleteFunc(held[user], func(r accesstypes.Role) bool { return slices.Contains(roles, r) })
	if len(held[user]) == 0 {
		delete(held, user)
	}

	return nil
}

// UserRoles returns the user's seats by scope: every scope with none given, the given
// ones otherwise.
func (s *MemUserStore) UserRoles(_ context.Context, user accesstypes.User, scopes ...accesstypes.PolicyScope) (accesstypes.RoleCollection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(accesstypes.RoleCollection)
	if len(scopes) == 0 {
		for scope, held := range s.seats {
			if roles := held[user]; len(roles) > 0 {
				out[scope] = slices.Clone(roles)
			}
		}

		return out, nil
	}
	for _, scope := range scopes {
		out[scope] = slices.Clone(s.seats[scope][user])
	}

	return out, nil
}

// RoleUsers lists the users seated in the role in the scope, sorted.
func (s *MemUserStore) RoleUsers(_ context.Context, scope accesstypes.PolicyScope, role accesstypes.Role) ([]accesstypes.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var users []accesstypes.User
	for user, roles := range s.seats[scope] {
		if slices.Contains(roles, role) {
			users = append(users, user)
		}
	}
	slices.Sort(users)

	return users, nil
}

// Roles lists the roles that exist in the scope, sorted: the ones declared there and, for
// one domain, the ones declared in every domain.
func (s *MemUserStore) Roles(_ context.Context, scope accesstypes.PolicyScope) ([]accesstypes.Role, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.rolesIn(scope), nil
}

func (s *MemUserStore) rolesIn(scope accesstypes.PolicyScope) []accesstypes.Role {
	roles := slices.Clone(s.roles[scope])
	if _, oneDomain := scope.Domain(); oneDomain {
		for _, role := range s.roles[accesstypes.EveryDomainPolicyScope()] {
			if !slices.Contains(roles, role) {
				roles = append(roles, role)
			}
		}
	}
	slices.Sort(roles)

	return roles
}

var (
	_ UserAccounts = (*MemUserStore)(nil)
	_ UserSeats    = (*MemUserStore)(nil)
)
