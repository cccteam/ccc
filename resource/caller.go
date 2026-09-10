package resource

import (
	"context"
	"sync"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/go-playground/errors/v5"
)

// Caller is what the generated frame knows about a request's caller once the entry
// check has run: the checker that check ran with, the scope it ran in, and the
// environment it sampled, so a body that arms a write or a read against the
// caller evaluates the same facts the entry check did (the decision instant
// included). The generated RPC handlers stamp it into the context before Execute
// runs; a body reads it back with CallerFrom.
type Caller struct {
	// Permissions is the checker the entry check ran with: the session's user, or
	// the role an act-as-role session assumed, behind the session's mask.
	Permissions UserPermissions
	// Scope is the partition the request was checked in.
	Scope accesstypes.Scope
	// Env is the decision environment the entry check sampled.
	Env accesstypes.Environment
	// collection renders a conditional grant into the live check, when the
	// frame's decoder was wired to one.
	collection *GeneratedCollection
}

type callerKey struct{}

// WithCaller stamps the caller into the context for the body that runs under it.
func WithCaller(ctx context.Context, caller *Caller) context.Context {
	return context.WithValue(ctx, callerKey{}, caller)
}

// CallerFrom returns the caller the frame stamped, or a zero Caller when none
// was: a mutation or query armed with a zero Caller refuses at execution instead
// of running unchecked.
func CallerFrom(ctx context.Context) *Caller {
	if caller, ok := ctx.Value(callerKey{}).(*Caller); ok && caller != nil {
		return caller
	}

	return &Caller{}
}

// IsZero reports whether no frame stamped the caller.
func (c *Caller) IsZero() bool {
	return c == nil || c.Permissions == nil
}

// Check answers, as data, what the caller may do: the decision for perm on each
// resource (a resource, or a field of one as "Resource.field"), evaluated in the
// caller's scope and environment. A body branches on the answers where policy
// should decide what it does, without arming a write.
func (c *Caller) Check(ctx context.Context, perm accesstypes.Permission, resources ...accesstypes.Resource) (accesstypes.Decisions, error) {
	if c.IsZero() {
		return nil, errNoCaller
	}
	decisions, err := c.Permissions.Check(ctx, c.Env, c.Scope, perm, resources...)
	if err != nil {
		return nil, errors.Wrap(err, "resource.UserPermissions.Check()")
	}

	return decisions, nil
}

// As returns the caller acting through another checker in the same scope and
// environment: a body that should act as a named role composes the checker the
// way act-as-role sessions do (RolePrincipalPermissions over access.ForRole) and
// arms its operations with the result. The actor stays who it was.
func (c *Caller) As(perms UserPermissions) *Caller {
	as := *c
	as.Permissions = perms

	return &as
}

// RolePrincipalPermissions is a checker acting as a role on behalf of a real
// actor: the role's permissions answer the checks, and the actor's identity is
// what row conditions bind and change events record — the same composition an
// act-as-role session gets.
func RolePrincipalPermissions(role RolePermissions, actor accesstypes.User) UserPermissions {
	return rolePrincipalPermissions{RolePermissions: role, user: actor}
}

var errNoCaller = errors.New("no caller is stamped on the context: Enforce and Check run under a generated RPC handler, which stamps the caller before Execute, or under a hand-written handler that called resource.WithCaller")

// arm binds the caller's checker, scope, environment, and collection to the query
// set for one operation's evaluation, or records why it cannot.
func (q *QuerySet[Resource]) arm(caller *Caller, rSet *Set[Resource], requiredPermission accesstypes.Permission) {
	if caller.IsZero() {
		q.armError = errNoCaller

		return
	}
	q.env = caller.Env
	q.collection = caller.collection
	q.EnableUserPermissionEnforcement(rSet, caller.Permissions, caller.Scope, requiredPermission)
}

// Enforce arms the query against the caller: Read and List run the read
// permission gate the resource routes run (resource then requested fields, with
// conditional grants riding the query) before touching the database. A zero
// Caller refuses at execution.
func (q *QuerySet[Resource]) Enforce(caller *Caller, rSet *Set[Resource], requiredPermission accesstypes.Permission) *QuerySet[Resource] {
	q.arm(caller, rSet, requiredPermission)

	return q
}

// Enforce arms the mutation against the caller: Apply and Buffer run the full
// pipeline the resource routes run — the static field gate against the Set, the
// fold of conditional grants, the live check against the real row inside the
// transaction, and the tenancy check — before buffering anything. A refusal is
// the same Forbidden the routes answer, naming the grant that said no. A zero
// Caller refuses at execution.
func (p *PatchSet[Resource]) Enforce(caller *Caller, rSet *Set[Resource], requiredPermission accesstypes.Permission) *PatchSet[Resource] {
	p.querySet.arm(caller, rSet, requiredPermission)

	return p
}

// SetCache holds a resource's canonical permission Sets, one per operation, built
// on first use from the generated wire struct that mirrors what the resource
// routes accept. The generated resources package declares one per resource and
// shape, so hand-written operations armed with Enforce meet exactly the field
// permissions the routes enforce.
type SetCache[Resource Resourcer, Request any] struct {
	sets sync.Map
}

// For returns the Set requiring perm on every addressable field. Construction
// errors are programming errors in generated code and panic.
func (c *SetCache[Resource, Request]) For(perm accesstypes.Permission) *Set[Resource] {
	if cached, ok := c.sets.Load(perm); ok {
		if set, ok := cached.(*Set[Resource]); ok {
			return set
		}
	}
	set, err := NewSet[Resource, Request](perm)
	if err != nil {
		panic(errors.Wrapf(err, "resource.NewSet[%s](%s)", (*new(Resource)).Resource(), perm))
	}
	if actual, loaded := c.sets.LoadOrStore(perm, set); loaded {
		if stored, ok := actual.(*Set[Resource]); ok {
			return stored
		}
	}

	return set
}
