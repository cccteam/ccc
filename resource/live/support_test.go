package live

import (
	"context"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/spxscan/spxapi"
	"github.com/go-playground/errors/v5"
)

// shipRow is a resource the tests write.
type shipRow struct {
	ID   string `spanner:"Id"`
	Name string `spanner:"Name"`
}

func (shipRow) Resource() accesstypes.Resource {
	return "Ships"
}

// hangarRow is a second resource the tests write in a tenant.
type hangarRow struct {
	ID   string `spanner:"Id"`
	Name string `spanner:"Name"`
}

func (hangarRow) Resource() accesstypes.Resource {
	return "Hangars"
}

// bufferingTxn is the ReadWriteTransaction the Mock client delegates its buffering to.
type bufferingTxn struct{}

func (*bufferingTxn) DBType() resource.DBType {
	return resource.SpannerDBType
}

func (*bufferingTxn) SpannerReadOnlyTransaction() spxapi.Querier {
	return nil
}

func (*bufferingTxn) PostgresReadOnlyTransaction() resource.PostgresQuerier {
	return nil
}

func (*bufferingTxn) BufferMap(resource.PatchSetMetadata, map[string]any) error {
	return nil
}

func (*bufferingTxn) BufferStruct(resource.PatchSetMetadata) error {
	return nil
}

func (*bufferingTxn) DataChangeEventIndex(accesstypes.Resource, string) int {
	return 0
}

// grantAll grants every check.
type grantAll struct{}

func (grantAll) Check(_ context.Context, _ accesstypes.Environment, _ accesstypes.Scope, _ accesstypes.Permission, resources ...accesstypes.Resource) (accesstypes.Decisions, error) {
	decisions := make(accesstypes.Decisions, len(resources))
	for _, res := range resources {
		decisions[res] = accesstypes.Granted()
	}

	return decisions, nil
}

func (grantAll) PermissionDigest(context.Context, accesstypes.Scope) (accesstypes.PermissionDigest, error) {
	return accesstypes.PermissionDigest{}, nil
}

func (grantAll) HasGrants(context.Context, accesstypes.Scope) (bool, error) {
	return true, nil
}

func (grantAll) Domains(context.Context) ([]accesstypes.Domain, error) {
	return nil, nil
}

func (grantAll) User() accesstypes.User {
	return "dispatcher"
}

// bufferRow buffers one update of a row: a Hangar decoded in domain when one is given,
// a Ship built by hand otherwise.
func bufferRow(ctx context.Context, txn resource.ReadWriteTransaction, domain accesstypes.Domain, key string) error {
	if domain != "" {
		p := resource.NewPatchSet(resource.NewMetadata[hangarRow]()).SetPatchType(resource.UpdatePatchType)
		p.SetKey("ID", key)
		p.Set("Name", "renamed")
		p.EnableUserPermissionEnforcement(nil, grantAll{}, accesstypes.DomainScope(domain), accesstypes.Update)
		if err := p.Buffer(ctx, txn); err != nil {
			return errors.Wrap(err, "resource.PatchSet.Buffer()")
		}

		return nil
	}
	p := resource.NewPatchSet(resource.NewMetadata[shipRow]()).SetPatchType(resource.UpdatePatchType)
	p.SetKey("ID", key)
	p.Set("Name", "renamed")
	if err := p.Buffer(ctx, txn); err != nil {
		return errors.Wrap(err, "resource.PatchSet.Buffer()")
	}

	return nil
}
