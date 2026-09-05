package resource

import (
	"context"
	"iter"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/spxscan/spxapi"
)

// UserPermissions is an interface that provides methods to check user permissions and retrieve user information, and is used
// in the PatchSet and QuerySet types to enforce user permissions on resources.
//
// The canonical implementation is the access package's request-bound checker
// (Client.ForUser), which satisfies this interface structurally — neither
// package imports the other.
type UserPermissions interface {
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

	// Domains lists the domains where the user holds at least one grant,
	// sorted — the payload the generated user-domains endpoint serves and
	// the tenant picker's membership question. Same foothold predicate as
	// concealed tenancy's visibility check, so the two never disagree; the
	// global scope is never a domain.
	Domains(ctx context.Context) ([]accesstypes.Domain, error)

	User() accesstypes.User
}

// RolePermissions is the permission surface of a session that operates as a
// role principal: UserPermissions without User(), because a role is not
// anyone. SessionPermissions completes it into the UserPermissions every
// decoder consumes by supplying the session's effective identity as User().
//
// The canonical implementation is the access package's request-bound role
// checker (Client.ForRole), which satisfies this interface structurally —
// neither package imports the other.
type RolePermissions interface {
	// Check returns the Decision for perm on each of resources within scope,
	// evaluated against the role's effective grants. See
	// UserPermissions.Check for the contract every implementation owes:
	// an entry per resource, no short-circuit, one snapshot per call.
	Check(ctx context.Context, env accesstypes.Environment, scope accesstypes.Scope, perm accesstypes.Permission, resources ...accesstypes.Resource) (accesstypes.Decisions, error)

	// PermissionDigest returns the role's structural grant enumeration within
	// scope. See UserPermissions.PermissionDigest.
	PermissionDigest(ctx context.Context, scope accesstypes.Scope) (accesstypes.PermissionDigest, error)

	// Domains lists the domains where the role holds at least one grant,
	// sorted. See UserPermissions.Domains.
	Domains(ctx context.Context) ([]accesstypes.Domain, error)
}

// Client is an interface for the supported database Client's to implement. It is not intended
// for mocking since each database requires an implementation in this package.
type Client interface {
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
