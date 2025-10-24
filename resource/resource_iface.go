package resource

import (
	"context"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
)

// UserPermissions is an interface that provides methods to check user permissions and retrieve user information, and is used
// in the PatchSet and QuerySet types to enforce user permissions on resources.
type UserPermissions interface {
	Check(ctx context.Context, perm accesstypes.Permission, resources ...accesstypes.Resource) (ok bool, missing []accesstypes.Resource, err error)
	Domain() accesstypes.Domain
	User() accesstypes.User
}

// SpannerQuerier is an interface for querying Spanner.
type SpannerQuerier interface {
	Query(ctx context.Context, statement spanner.Statement) *spanner.RowIterator
}

// Reader is an interface that wraps methods for reading resources from a database.
type Reader interface {
	DBType() DBType
	SpannerReadTransaction() SpannerQuerier
	PostgresReadTransaction()
}

// Buffer is an interface for types that can buffer their mutations
// into a transaction. This is used for batching operations.
type Buffer interface {
	Buffer(ctx context.Context, txn *ReadWriteTransaction, eventSource ...string) error
}

// TxnRunner will have its Execute() method called inside the ReadWriteTransaction
type TxnRunner interface {
	Execute(ctx context.Context, txn *ReadWriteTransaction) error
}
