package resource

import (
	"context"
	"time"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/spxscan/spxapi"
)

// UserPermissions is an interface that provides methods to check user permissions and retrieve user information, and is used
// in the PatchSet and QuerySet types to enforce user permissions on resources.
type UserPermissions interface {
	Check(ctx context.Context, perm accesstypes.Permission, resources ...accesstypes.Resource) (ok bool, missing []accesstypes.Resource, err error)
	Domain() accesstypes.Domain
	User() accesstypes.User
}

// SpannerCommitter is an interface that abstracts the Spanner client's transaction functionality.
// It is used by PatchSet.SpannerApply() to allow for mocking the Spanner client in tests.
// It is satisfied by *spanner.Client.
type SpannerCommitter interface {
	ReadWriteTransaction(ctx context.Context, f func(context.Context, *spanner.ReadWriteTransaction) error) (commitTimestamp time.Time, err error)
}

// TxnBuffer is an interface that abstracts a Spanner read-write transaction,
// allowing mutations to be buffered and queries to be executed within the transaction.
// It is satisfied by *spanner.ReadWriteTransaction.
type TxnBuffer interface {
	BufferWrite(ms []*spanner.Mutation) error
	spxapi.Querier
}

// SpannerBuffer is an interface for types that can buffer their Spanner mutations
// into a transaction via the SpannerBuffer method. This is used for batching
// operations.
type SpannerBuffer interface {
	SpannerBuffer(ctx context.Context, txn TxnBuffer, eventSource ...string) error
}

// TxnRunner will have its Execute() method called inside the *spanner.ReadWriteTransaction provided by TxnBuffer
type TxnRunner interface {
	Execute(ctx context.Context, txn TxnBuffer) error
}

// TxnFuncRunner provides a way to execute a function within a read-write transaction.
// This is useful for batching operations that need to be committed together.
type TxnFuncRunner interface {
	ExecuteFunc(ctx context.Context, runnerFn func(ctx context.Context, txn TxnBuffer) error) error
}

// RunnerFunc is a function that converts a TxnFuncRunner into a TxnRunner
type RunnerFunc func(ctx context.Context, txn TxnBuffer) error

// Execute runs the function.
func (fn RunnerFunc) Execute(ctx context.Context, txn TxnBuffer) error {
	return fn(ctx, txn)
}
