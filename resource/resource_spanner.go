package resource

import (
	"context"
	"fmt"
	"iter"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/httpio"
	"github.com/cccteam/spxscan"
	"github.com/go-playground/errors/v5"
)

var _ Client = (*SpannerClient)(nil)

// SpannerClient is a wrapper around the database.
type SpannerClient struct {
	spanner *spanner.Client
}

// NewSpannerClient creates a new Client.
func NewSpannerClient(db *spanner.Client) *SpannerClient {
	return &SpannerClient{
		spanner: db,
	}
}

// Close closes the database connection.
func (c *SpannerClient) Close() {
	c.spanner.Close()
}

func (c *SpannerClient) SpannerReadOnlyTransaction() spxscan.Querier {
	return c.spanner.Single()
}

// ExecuteFunc executes a function within a read-write transaction.
func (c *SpannerClient) ExecuteFunc(ctx context.Context, f func(ctx context.Context, txn ReadWriteTransaction) error) error {
	_, err := c.spanner.ReadWriteTransaction(ctx, func(ctx context.Context, txn *spanner.ReadWriteTransaction) error {
		if err := f(ctx, newSpannerReadWriteTransaction(txn)); err != nil {
			return errors.Wrap(err, "f()")
		}

		return nil
	})
	if err != nil {
		return errors.Wrap(err, "c.db.ReadWriteTransaction()")
	}

	return nil
}

func (c *SpannerClient) PostgresReadOnlyTransaction() any {
	panic("SpannerClient.PostgresReadOnlyTransaction() should never be called.")
}

var _ Reader[nilResource] = (*SpannerReader[nilResource])(nil)

type SpannerReader[Resource Resourcer] struct {
	readTxn func() spxscan.Querier
}

// DBType returns the database type.
func (c *SpannerReader[Resource]) DBType() DBType {
	return SpannerDBType
}

// Read reads a single resource from the database.
func (c *SpannerReader[Resource]) Read(ctx context.Context, stmt *Statement) (*Resource, error) {
	var res Resource
	dst := new(Resource)
	if err := spxscan.Get(ctx, c.readTxn(), dst, stmt.SpannerStatement()); err != nil {
		if errors.Is(err, spxscan.ErrNotFound) {
			return nil, httpio.NewNotFoundMessagef("%s (%s) not found", res.Resource(), stmt.resolvedWhereClause)
		}

		return nil, errors.Wrap(err, "spxscan.Get()")
	}

	return dst, nil
}

// List reads a list of resources from the database.
func (c *SpannerReader[Resource]) List(ctx context.Context, stmt *Statement) iter.Seq2[*Resource, error] {
	return func(yield func(*Resource, error) bool) {
		for r, err := range spxscan.SelectSeq[Resource](ctx, c.readTxn(), stmt.SpannerStatement()) {
			if err != nil {
				yield(nil, errors.Wrap(err, "spxscan.SelectSeq()"))

				return
			}
			if !yield(r, nil) {
				return
			}
		}
	}
}

var _ ReadWriteTransaction = (*SpannerReadWriteTransaction)(nil)

// SpannerReadWriteTransaction represents a database transaction that can be used for both reads and writes.
type SpannerReadWriteTransaction struct {
	txn              *spanner.ReadWriteTransaction
	resourceRowIndex map[string]int
}

func newSpannerReadWriteTransaction(txn *spanner.ReadWriteTransaction) ReadWriteTransaction {
	return &SpannerReadWriteTransaction{
		txn:              txn,
		resourceRowIndex: make(map[string]int),
	}
}

// DBType returns the database type.
func (c *SpannerReadWriteTransaction) DBType() DBType {
	return SpannerDBType
}

// DataChangeEventIndex() provides a sequence number for data change events on the same Resource inside the same transaction
func (r *SpannerReadWriteTransaction) DataChangeEventIndex(res accesstypes.Resource, rowID string) int {
	indexID := fmt.Sprintf("%s_%s", res, rowID)
	r.resourceRowIndex[indexID]++

	return r.resourceRowIndex[indexID]
}

func (c *SpannerReadWriteTransaction) SpannerReadOnlyTransaction() spxscan.Querier {
	return c.txn
}

func (c *SpannerReadWriteTransaction) BufferMap(patchType PatchType, r ResourcePatch, patch map[string]any) error {
	var m *spanner.Mutation

	switch patchType {
	case CreatePatchType:
		m = spanner.InsertMap(string(r.Resource()), patch)
	case UpdatePatchType:
		m = spanner.UpdateMap(string(r.Resource()), patch)
	case DeletePatchType:
		m = spanner.Delete(string(r.Resource()), r.PrimaryKey().KeySet())
	case CreateOrUpdatePatchType:
		m = spanner.InsertOrUpdateMap(string(r.Resource()), patch)
	default:
		panic(fmt.Sprintf("unsupported operation: %s", patchType))
	}

	if err := c.txn.BufferWrite([]*spanner.Mutation{m}); err != nil {
		return errors.Wrap(err, "spanner.ReadWriteTransaction.BufferWrite()")
	}

	return nil
}

func (c *SpannerReadWriteTransaction) BufferStruct(patchType PatchType, r ResourcePatch, patch any) error {
	var m *spanner.Mutation
	var err error

	switch patchType {
	case CreatePatchType:
		m, err = spanner.InsertStruct(string(r.Resource()), patch)
		if err != nil {
			return errors.Wrap(err, "spanner.InsertStruct()")
		}
	case UpdatePatchType:
		m, err = spanner.UpdateStruct(string(r.Resource()), patch)
		if err != nil {
			return errors.Wrap(err, "spanner.UpdateStruct()")
		}
	case CreateOrUpdatePatchType:
		m, err = spanner.InsertOrUpdateStruct(string(r.Resource()), patch)
		if err != nil {
			return errors.Wrap(err, "spanner.InsertOrUpdateStruct()")
		}
	default:
		panic(fmt.Sprintf("unsupported operation: %s", patchType))
	}

	if err := c.txn.BufferWrite([]*spanner.Mutation{m}); err != nil {
		return errors.Wrap(err, "spanner.ReadWriteTransaction.BufferWrite()")
	}

	return nil
}

func (c *SpannerReadWriteTransaction) PostgresReadOnlyTransaction() any {
	panic("SpannerReadWriteTransaction.PostgresReadOnlyTransaction() should never be called.")
}
