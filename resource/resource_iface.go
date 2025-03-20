package resource

import (
	"context"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
)

type SpannerReader interface {
	SpannerRead(ctx context.Context, txn *spanner.ReadOnlyTransaction, dst any) error
	Resource() accesstypes.Resource
	KeySet() KeySet
}

type SpannerLister interface {
	SpannerList(ctx context.Context, txn *spanner.ReadOnlyTransaction, dst any) error
}

type SpannerBufferer interface {
	SpannerBuffer(ctx context.Context, txn *spanner.ReadWriteTransaction, eventSource ...string) error
	Resource() accesstypes.Resource
	PrimaryKey() KeySet
}

type Queryer[Resource Resourcer] interface {
	Query() *QuerySet[Resource]
}

type UserPermissions interface {
	Check(ctx context.Context, perm accesstypes.Permission, resources ...accesstypes.Resource) (ok bool, missing []accesstypes.Resource, err error)
	Domain() accesstypes.Domain
	User() accesstypes.User
}
