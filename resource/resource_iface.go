package resource

import (
	"context"
	"iter"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/spxscan"
)

// UserPermissions is an interface that provides methods to check user permissions and retrieve user information, and is used
// in the PatchSet and QuerySet types to enforce user permissions on resources.
type UserPermissions interface {
	Check(ctx context.Context, perm accesstypes.Permission, resources ...accesstypes.Resource) (ok bool, missing []accesstypes.Resource, err error)
	Domain() accesstypes.Domain
	User() accesstypes.User
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
	SpannerReadOnlyTransaction() spxscan.Querier
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
type Reader[Resource Resourcer] interface {
	DBType() DBType
	Read(ctx context.Context, stmt *Statement) (*Resource, error)
	List(ctx context.Context, stmt *Statement) iter.Seq2[*Resource, error]
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
