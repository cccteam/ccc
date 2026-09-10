package accesstypes

// Principal is the authorization subject a session evaluates against: a user
// or a role. Kind is structural — an unexported discriminator set by the
// constructors, never a distinguished User or Role value — so no username can
// ever read as a role and no role name as a user. Whether a session is
// impersonated is a property of the session's impersonation record, not of
// its Principal: an impersonated user and a user acting as themself are the
// same Principal value.
//
// The zero Principal is the zero User's principal, which holds no grants
// (fail closed). Principal is comparable and usable as a map key. It has no
// parsed or serialized form: storage and wire formats carry the kind as a
// separate field, never as a magic string.
type Principal struct {
	user User
	role Role
	kind principalKind
}

type principalKind uint8

const (
	principalUser principalKind = iota
	principalRole
)

// UserPrincipal returns the Principal for user: permission checks resolve
// user's role memberships and evaluate the grants they carry.
func UserPrincipal(user User) Principal {
	return Principal{user: user}
}

// RolePrincipal returns the Principal for role: permission checks evaluate
// role's grants directly, with no membership lookup.
func RolePrincipal(role Role) Principal {
	return Principal{role: role, kind: principalRole}
}

// User returns the user and true for a user principal, or the zero User and
// false for a role principal.
func (p Principal) User() (User, bool) {
	if p.kind != principalUser {
		return "", false
	}

	return p.user, true
}

// Role returns the role and true for a role principal, or the zero Role and
// false for a user principal.
func (p Principal) Role() (Role, bool) {
	if p.kind != principalRole {
		return "", false
	}

	return p.role, true
}

// IsRole reports whether the principal is a role.
func (p Principal) IsRole() bool {
	return p.kind == principalRole
}

// String renders the principal for display only: "user:<user>" or
// "role:<role>". The output must never be parsed back into a Principal; use
// User and Role for logic.
func (p Principal) String() string {
	if p.kind == principalRole {
		return "role:" + string(p.role)
	}

	return "user:" + string(p.user)
}
