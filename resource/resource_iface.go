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

type SpannerBuffer interface {
	SpannerBuffer(ctx context.Context, txn *spanner.ReadWriteTransaction, eventSource ...string) error
}

type Queryer[T Resourcer] interface {
	Query() *QuerySet[T]
}