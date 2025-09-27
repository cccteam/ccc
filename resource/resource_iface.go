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

// SpannerCommitter is an interface implemented by *spanner.Client and is used by PatchSet to allow mocking of *spanner.Client
type SpannerCommitter interface {
	ReadWriteTransaction(ctx context.Context, f func(context.Context, *spanner.ReadWriteTransaction) error) (commitTimestamp time.Time, err error)
}

// TxnBuffer is an interface implemented by *spanner.ReadWriteTransaction, and is used by PatchSet to allow mocking of *spanner.ReadWriteTransaction
type TxnBuffer interface {
	BufferWrite(ms []*spanner.Mutation) error
	spxapi.Querier
}

// SpannerBuffer is an interface implemented by *PatchSet that buffers its mutations in the *spanner.ReadWriteTransaction provided by TxnBuffer
type SpannerBuffer interface {
	SpannerBuffer(ctx context.Context, txn TxnBuffer, eventSource ...string) error
}

// TxnRunner will have its Execute() method called inside the *spanner.ReadWriteTransaction provided by TxnBuffer
type TxnRunner interface {
	Execute(ctx context.Context, txn TxnBuffer) error
}

// TxnFuncRunner will have its ExecuteFunc() method called inside the *spanner.ReadWriteTransaction provided by TxnBuffer
type TxnFuncRunner interface {
	ExecuteFunc(ctx context.Context, runnerFn func(ctx context.Context, txn TxnBuffer) error) error
}

// RunnerFunc is a function that converts a TxnFuncRunner into a TxnRunner
type RunnerFunc func(ctx context.Context, txn TxnBuffer) error

// Execute runs the function.
func (fn RunnerFunc) Execute(ctx context.Context, txn TxnBuffer) error {
	return fn(ctx, txn)
}
