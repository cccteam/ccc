package resource

import (
	"context"
	"time"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/spxscan/spxapi"
)

type SpannerCommitter interface {
	ReadWriteTransaction(ctx context.Context, f func(context.Context, *spanner.ReadWriteTransaction) error) (commitTimestamp time.Time, err error)
}

type TxnBuffer interface {
	BufferWrite(ms []*spanner.Mutation) error
	spxapi.Querier
}

type SpannerBuffer interface {
	SpannerBuffer(ctx context.Context, txn TxnBuffer, eventSource ...string) error
}

type UserPermissions interface {
	Check(ctx context.Context, perm accesstypes.Permission, resources ...accesstypes.Resource) (ok bool, missing []accesstypes.Resource, err error)
	Domain() accesstypes.Domain
	User() accesstypes.User
}
