package resource

import (
	"context"
	"iter"
	"time"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/spxscan"
	"github.com/cccteam/spxscan/spxapi"
)

// UserPermissions is an interface that provides methods to check user permissions and retrieve user information, and is used
// in the PatchSet and QuerySet types to enforce user permissions on resources.
type UserPermissions interface {
	Check(ctx context.Context, perm accesstypes.Permission, resources ...accesstypes.Resource) (ok bool, missing []accesstypes.Resource, err error)
	Domain() accesstypes.Domain
	User() accesstypes.User
}

type Client interface {
	DBType() DBType
	Single() spxscan.Querier
	ReadWriteTransaction(ctx context.Context, f func(context.Context, *spanner.ReadWriteTransaction) error) (commitTimestamp time.Time, err error)
	// Postgres() *pgxpool.Pool
}

type Txn interface {
	DBType() DBType
	spxapi.Querier
	BufferWrite(ms []*spanner.Mutation) error
	// PosgtresTxn()
}

// SpannerQuerier is an interface for querying Spanner.
type SpannerQuerier interface {
	Query(ctx context.Context, statement spanner.Statement) *spanner.RowIterator
}

// Reader is an interface that wraps methods for reading resources from a database.
type Reader[Resource Resourcer] interface {
	DBType() DBType
	Read(ctx context.Context, stmt *Statement) (*Resource, error)
	List(ctx context.Context, stmt *Statement) iter.Seq2[*Resource, error]
}

type Executor[Resource Resourcer] interface {
	Execute(ctx context.Context, runner TxnRunner[Resource]) error
	ExecuteFunc(ctx context.Context, f func(ctx context.Context, txn *SpannerReadWriteTransaction[Resource]) error) error
}

// Buffer is an interface for types that can buffer their mutations
// into a transaction. This is used for batching operations.
type Buffer[Resource Resourcer] interface {
	Buffer(ctx context.Context, txn *SpannerReadWriteTransaction[Resource], eventSource ...string) error
}

// TxnRunner will have its Execute() method called inside the ReadWriteTransaction
type TxnRunner[Resource Resourcer] interface {
	Execute(ctx context.Context, txn *SpannerReadWriteTransaction[Resource]) error
}
