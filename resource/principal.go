package resource

import (
	"context"
	"slices"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/logger"
	"github.com/cccteam/session/sessioninfo"
	"github.com/go-playground/errors/v5"
)

// SessionPermissions composes the request's UserPermissions from the session in
// ctx. It picks the checker by the session's principal — forUser for a user
// principal (an ordinary session, or an impersonated user), forRole for a role
// principal — lists the session's domains as the application's roster
// filtered by the checker's HasGrants, and applies the session's permission
// mask. It is the per-request accessor for an application with impersonated
// sessions:
//
//	return resource.SessionPermissions(r.Context(), a.access.ForUser, a.access.ForRole, a.tenants.Domains)
//
// The roster is the application's: access holds no tenant list, so the tenant
// picker's question — which tenants may this session see — is the roster
// asked, tenant by tenant, whether the session holds a grant there. An
// application without tenants passes nil.
//
// A role checker has no User(): a role is not anyone. For a role principal the
// composed UserPermissions reports the session's effective identity — its
// Username, which is the actor who established the session — so a row
// condition's subject binds to the real person and nobody's identity is
// borrowed. An application that does not operate sessions as roles passes
// RolePrincipalsUnsupported as forRole: a role principal then fails closed —
// every check Denied, an empty digest, no footholds, no domains — with the
// same User(), so a deployment that mints role principals without a checker
// is loud rather than permissive.
func SessionPermissions[U UserPermissionChecker, R RolePermissions](ctx context.Context, forUser func(accesstypes.User) U, forRole func(accesstypes.Role) R, roster DomainRoster) UserPermissions {
	principal := sessioninfo.PrincipalFromCtx(ctx)

	var perms UserPermissions
	if role, ok := principal.Role(); ok {
		var rolePerms RolePermissions
		if forRole != nil {
			rolePerms = forRole(role)
		}
		effective := accesstypes.User(sessioninfo.FromCtx(ctx).Username)
		if rolePerms == nil {
			perms = unsupportedRolePermissions{user: effective, role: role}
		} else {
			perms = rosterPermissions{UserPermissionChecker: rolePrincipalPermissions{RolePermissions: rolePerms, user: effective}, roster: roster}
		}
	} else {
		user, _ := principal.User()
		perms = rosterPermissions{UserPermissionChecker: forUser(user), roster: roster}
	}

	return Masked(perms, sessioninfo.MaskFromCtx(ctx))
}

// RolePrincipalsUnsupported is the forRole argument for SessionPermissions in
// an application that does not operate sessions as roles. It returns nil,
// which SessionPermissions replaces with a fail-closed checker for the role.
func RolePrincipalsUnsupported(accesstypes.Role) RolePermissions {
	return nil
}

// rolePrincipalPermissions completes a role checker into the checker every
// decoder consumes: User() is the session's effective identity — the actor
// who established the role-principal session — which is what a row
// condition's subject binds to at render time.
type rolePrincipalPermissions struct {
	RolePermissions
	user accesstypes.User
}

// User returns the session's effective identity.
func (r rolePrincipalPermissions) User() accesstypes.User {
	return r.user
}

// rosterPermissions completes a checker into UserPermissions by answering
// Domains from the application's roster: each tenant the roster lists where
// the checker has a foothold, sorted.
type rosterPermissions struct {
	UserPermissionChecker
	roster DomainRoster
}

// Domains lists the roster's domains where the checker holds a grant, sorted;
// never nil.
func (p rosterPermissions) Domains(ctx context.Context) ([]accesstypes.Domain, error) {
	domains := []accesstypes.Domain{}
	if p.roster == nil {
		return domains, nil
	}
	all, err := p.roster(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "resource.DomainRoster")
	}
	for _, domain := range all {
		has, err := p.HasGrants(ctx, accesstypes.DomainScope(domain))
		if err != nil {
			return nil, errors.Wrap(err, "resource.UserPermissionChecker.HasGrants()")
		}
		if has {
			domains = append(domains, domain)
		}
	}
	slices.Sort(domains)

	return slices.Compact(domains), nil
}

// Masked returns perms attenuated by mask: a permission the mask does not allow
// is Denied for every resource before policy is consulted — and evidenced as a
// mask denial — and the permission digest omits it. A foothold is answered
// through the masked digest, so a session masked to nothing has none and
// lists no domain. User passes through. The unrestricted mask returns perms
// itself.
func Masked(perms UserPermissions, mask accesstypes.PermissionMask) UserPermissions {
	if mask.IsZero() {
		return perms
	}

	return &maskedPermissions{UserPermissions: perms, mask: mask}
}

type maskedPermissions struct {
	UserPermissions
	mask accesstypes.PermissionMask
}

// Check answers Denied for every resource when the mask does not allow perm,
// without consulting policy; otherwise it delegates.
func (m *maskedPermissions) Check(
	ctx context.Context, env accesstypes.Environment, scope accesstypes.Scope, perm accesstypes.Permission, resources ...accesstypes.Resource,
) (accesstypes.Decisions, error) {
	if !m.mask.Allows(perm) {
		logger.FromCtx(ctx).Warnf("impersonation: %s denied by mask (%s) for %s on %v", perm, m.mask, m.User(), resources)

		return deniedDecisions(resources), nil
	}

	decisions, err := m.UserPermissions.Check(ctx, env, scope, perm, resources...)
	if err != nil {
		return nil, errors.Wrap(err, "resource.UserPermissions.Check()")
	}

	return decisions, nil
}

// PermissionDigest delegates and then drops every permission the mask does not
// allow, so the frontend's digest agrees with what Check will enforce; a
// resource left with no permissions is dropped (absence means denied).
func (m *maskedPermissions) PermissionDigest(ctx context.Context, scope accesstypes.Scope) (accesstypes.PermissionDigest, error) {
	digest, err := m.UserPermissions.PermissionDigest(ctx, scope)
	if err != nil {
		return nil, errors.Wrap(err, "resource.UserPermissions.PermissionDigest()")
	}

	masked := make(accesstypes.PermissionDigest, len(digest))
	for res, perms := range digest {
		kept := make(map[accesstypes.Permission]accesstypes.DigestState, len(perms))
		for perm, state := range perms {
			if m.mask.Allows(perm) {
				kept[perm] = state
			}
		}
		if len(kept) > 0 {
			masked[res] = kept
		}
	}

	return masked, nil
}

// HasGrants answers through the masked digest: the session has a foothold in
// scope exactly when something the mask allows is granted there.
func (m *maskedPermissions) HasGrants(ctx context.Context, scope accesstypes.Scope) (bool, error) {
	digest, err := m.PermissionDigest(ctx, scope)
	if err != nil {
		return false, err
	}

	return len(digest) > 0, nil
}

// Domains lists the delegate's domains where the masked session still has a
// foothold; never nil.
func (m *maskedPermissions) Domains(ctx context.Context) ([]accesstypes.Domain, error) {
	domains, err := m.UserPermissions.Domains(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "resource.UserPermissions.Domains()")
	}
	kept := make([]accesstypes.Domain, 0, len(domains))
	for _, domain := range domains {
		has, err := m.HasGrants(ctx, accesstypes.DomainScope(domain))
		if err != nil {
			return nil, err
		}
		if has {
			kept = append(kept, domain)
		}
	}

	return kept, nil
}

// unsupportedRolePermissions is the fail-closed checker for a role principal
// when no role checker is configured. User() is still the session's effective
// identity, so evidence names the real person even while nothing is granted.
type unsupportedRolePermissions struct {
	user accesstypes.User
	role accesstypes.Role
}

func (u unsupportedRolePermissions) Check(
	ctx context.Context, _ accesstypes.Environment, _ accesstypes.Scope, perm accesstypes.Permission, resources ...accesstypes.Resource,
) (accesstypes.Decisions, error) {
	logger.FromCtx(ctx).Warnf("impersonation: role principal %s has no role-bound checker; %s denied on %v", u.role, perm, resources)

	return deniedDecisions(resources), nil
}

func (unsupportedRolePermissions) PermissionDigest(context.Context, accesstypes.Scope) (accesstypes.PermissionDigest, error) {
	return accesstypes.PermissionDigest{}, nil
}

func (unsupportedRolePermissions) HasGrants(context.Context, accesstypes.Scope) (bool, error) {
	return false, nil
}

func (unsupportedRolePermissions) Domains(context.Context) ([]accesstypes.Domain, error) {
	return []accesstypes.Domain{}, nil
}

func (u unsupportedRolePermissions) User() accesstypes.User {
	return u.user
}

// deniedDecisions is the fail-closed answer for resources: every one Denied.
func deniedDecisions(resources []accesstypes.Resource) accesstypes.Decisions {
	decisions := make(accesstypes.Decisions, len(resources))
	for _, res := range resources {
		decisions[res] = accesstypes.Denied()
	}

	return decisions
}
