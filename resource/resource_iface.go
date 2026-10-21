package resource

import (
	"context"
	"iter"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/session/signin"
	"github.com/cccteam/spxscan/spxapi"
)

// UserPermissions is the permission surface a request's decoders consume: the
// checks, the frontend's advisory enumerations, the identity a row
// condition's subject binds to, and the session's reach into the
// application's tenants. SessionPermissions composes it per request from the
// session's principal, the application's checker for that principal, the
// application's tenant roster and its tenant declarations; applications hand
// the composed value to the PatchSet and QuerySet types, which enforce
// permissions through it.
type UserPermissions interface {
	UserPermissionChecker

	// Domains lists the domains where the user holds at least one grant and
	// that the session reaches, sorted — the payload the generated
	// user-domains endpoint serves and the tenant picker's question: the
	// application's tenant roster filtered by HasGrants and by Reaches, so a
	// domain listed here is exactly a domain whose routes answer the user
	// with ordinary 403s rather than concealed tenancy's 404; the picker and
	// the guard can never disagree. Never nil: an application without
	// tenants lists none.
	Domains(ctx context.Context) ([]accesstypes.Domain, error)

	// Reaches reports whether the session may enter domain: the tenant's
	// declaration (TenantDeclarations) admits the way the session was
	// established, its sign-in's method and connection, or the session's
	// actor is the application's staff, who hold a grant in the global
	// partition and reach every tenant. It is the generated domain guard's
	// question before the foothold, and the attenuation behind every other
	// answer: in a tenant the session does not reach it holds nothing —
	// every check Denied, an empty digest, no foothold. Whether domain is a
	// tenant at all is the roster's question (TenantRoster.Has), which the
	// guard asks first. A session composed without declarations reaches
	// every domain.
	Reaches(ctx context.Context, domain accesstypes.Domain) (bool, error)
}

// UserPermissionChecker is what an application's per-request accessor for a
// user principal hands SessionPermissions: UserPermissions without Domains,
// since a checker holds no tenant list — the roster is the application's,
// and SessionPermissions filters it by HasGrants.
//
// The canonical implementation is the access package's request-bound checker
// (Client.ForUser), which satisfies this interface structurally — neither
// package imports the other.
type UserPermissionChecker interface {
	// Check returns the Decision for perm on each of resources within scope.
	//
	// env is the request's decision context, sampled once at decode; the check
	// folds environment-referencing conditions against it and fails loudly
	// (error, never a silent allow or deny) when a referenced attribute is
	// absent.
	//
	// The returned Decisions must carry an entry for every resource passed. A
	// resource absent from the map reads as the zero Decision — Denied — so a
	// short implementation fails closed, never open. Implementations must not
	// short-circuit on the first denial.
	//
	// Snapshot pinning: a single Check call must evaluate every resource against one
	// consistent authorization snapshot — a concurrent grant or revocation must affect
	// all of the call's results or none of them. Distinct calls may observe different
	// snapshots; callers must not assume pinning across calls.
	Check(ctx context.Context, env accesstypes.Environment, scope accesstypes.Scope, perm accesstypes.Permission, resources ...accesstypes.Resource) (accesstypes.Decisions, error)

	// PermissionDigest returns the user's structural grant enumeration within
	// scope — the payload the generated permission-digest endpoint serves.
	// Advisory UI material only, never consulted for enforcement: denied
	// targets are absent (fail closed) and nothing folds, so a payload is
	// stable for the life of a policy snapshot.
	PermissionDigest(ctx context.Context, scope accesstypes.Scope) (accesstypes.PermissionDigest, error)

	// HasGrants reports whether the user holds at least one grant in scope —
	// the foothold concealed tenancy's visibility check asks, and the
	// predicate the tenant picker filters the application's roster by. A
	// membership that resolves to no grants is not a foothold; a membership
	// held in every tenant domain is a foothold in each.
	HasGrants(ctx context.Context, scope accesstypes.Scope) (bool, error)

	User() accesstypes.User
}

// RolePermissions is the permission surface of a session that operates as a
// role principal: UserPermissionChecker without User(), because a role is not
// anyone. SessionPermissions completes it into the UserPermissions every
// decoder consumes by supplying the session's effective identity as User().
//
// The canonical implementation is the access package's request-bound role
// checker (Client.ForRole), which satisfies this interface structurally —
// neither package imports the other.
type RolePermissions interface {
	// Check returns the Decision for perm on each of resources within scope,
	// evaluated against the role's effective grants. See
	// UserPermissionChecker.Check for the contract every implementation owes:
	// an entry per resource, no short-circuit, one snapshot per call.
	Check(ctx context.Context, env accesstypes.Environment, scope accesstypes.Scope, perm accesstypes.Permission, resources ...accesstypes.Resource) (accesstypes.Decisions, error)

	// PermissionDigest returns the role's structural grant enumeration within
	// scope. See UserPermissionChecker.PermissionDigest.
	PermissionDigest(ctx context.Context, scope accesstypes.Scope) (accesstypes.PermissionDigest, error)

	// HasGrants reports whether the role holds at least one grant in scope.
	// See UserPermissionChecker.HasGrants.
	HasGrants(ctx context.Context, scope accesstypes.Scope) (bool, error)
}

// DomainRoster lists the application's tenant domains: the roster the tenant
// picker is filtered from. A tenanted application passes its TenantRoster's
// Domains, which has this signature and answers from the roster's set; an
// application without tenants passes nil to SessionPermissions, and its
// sessions list no domain.
type DomainRoster func(ctx context.Context) ([]accesstypes.Domain, error)

// TenantDeclarations reads what a tenant declares about who may enter it: the
// tenant's declaration (signin.TenantDeclaration) by domain, as the
// application keeps it, which SessionPermissions applies to each tenant the
// session holds a grant in. It answers nil for a tenant that declares
// nothing, which admits a password and refuses a sign-in through a directory,
// as the sign-in policy does (signin.TenantPolicy). An application that keeps
// no declarations passes nil to SessionPermissions, and every tenant admits
// every session.
type TenantDeclarations func(ctx context.Context, domain accesstypes.Domain) (*signin.TenantDeclaration, error)

// Client is an interface for the supported database Client's to implement. It is not intended
// for mocking since each database requires an implementation in this package.
type Client interface {
	// DBType is the application database's type: the placement its lists sort NULL
	// in, which a computed resource's handler follows (NewComputedQueryDecoder).
	DBType() DBType
	// FileStore returns the file store wired under name (WithFileStore for
	// DefaultStore, WithNamedFileStore for a named store's StoreNameFor), or nil when
	// none is: the generated upload frames and file routes read their store here, the
	// one wiring point, and the generated router refuses to start when a store the
	// package uses is not wired (RequireFileStores). The Postgres client holds none.
	FileStore(name StoreName) FileStore
	ReadOnlyTransaction() ReadOnlyTransactionCloser
	ReadOnlyTransaction
	Executor
}

// ReadWriteTransaction is an interface that represents a database transaction that can be used for both reads and writes.
type ReadWriteTransaction interface {
	DBType() DBType
	ReadOnlyTransaction
	BufferMap(res PatchSetMetadata, patch map[string]any) error
	BufferStruct(res PatchSetMetadata) error

	// DataChangeEventIndex provides a sequence number for data change events on the same Resource inside the same transaction
	DataChangeEventIndex(res accesstypes.Resource, rowID string) int
}

// ReadOnlyTransaction is an interface that represents a database transaction that can be used for reads only.
type ReadOnlyTransaction interface {
	SpannerReadOnlyTransaction() spxapi.Querier
	PostgresReadOnlyTransaction() any
}

// ReadOnlyTransactionCloser is an interface that represents a database transaction that can be used for reads only
// and must be closed when it is no longer needed.
type ReadOnlyTransactionCloser interface {
	ReadOnlyTransaction
	Close()
}

// Executor interface exposes ability to run a function inside a transaction.
type Executor interface {
	ExecuteFunc(ctx context.Context, f func(ctx context.Context, txn ReadWriteTransaction) error) error
}

// Reader is an interface that wraps methods for reading resources from a database.
// Read and List return each row wrapped in the Row envelope, which carries the row
// data alongside per-row metadata.
type Reader[Resource Resourcer] interface {
	DBType() DBType
	Read(ctx context.Context, stmt *Statement) (*Row[Resource], error)
	List(ctx context.Context, stmt *Statement) iter.Seq2[*Row[Resource], error]
	// Count runs a statement whose single row and column is a count and returns it.
	Count(ctx context.Context, stmt *Statement) (int64, error)
}

// PatchSetMetadata is an interface that all PatchSet types must implement to allow their mutations to be buffered
type PatchSetMetadata interface {
	PatchType() PatchType
	PrimaryKey() KeySet
	Resource() accesstypes.Resource
}

// Buffer is an interface for types that can buffer their mutations
// into a transaction. This is used for batching operations.
type Buffer interface {
	Buffer(ctx context.Context, txn ReadWriteTransaction, eventSource ...string) error
}
