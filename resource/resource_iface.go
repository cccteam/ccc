package resource

import (
	"context"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
)

type SpannerBuffer interface {
	SpannerBuffer(ctx context.Context, txn *spanner.ReadWriteTransaction, eventSource ...string) error
}

type UserPermissions interface {
	Check(ctx context.Context, perm accesstypes.Permission, resources ...accesstypes.Resource) (ok bool, missing []accesstypes.Resource, err error)
	Domain() accesstypes.Domain
	User() accesstypes.User
}
