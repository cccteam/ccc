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

// SpannerReadOnlyTransaction returns a read-only transaction for the Spanner client.
func (c *SpannerClient) SpannerReadOnlyTransaction() spxscan.Querier {
	return c.spanner.Single()
}

// ExecuteFunc executes a function within a read-write transaction.
func (c *SpannerClient) ExecuteFunc(ctx context.Context, f func(ctx context.Context, txn ReadWriteTransaction) error) error {
	_, err := c.spanner.ReadWriteTransaction(ctx, func(ctx context.Context, txn *spanner.ReadWriteTransaction) error {
		if err := f(ctx, NewSpannerReadWriteTransaction(txn)); err != nil {
			return errors.Wrap(err, "f()")
		}

		return nil
	})
	if err != nil {
		return errors.Wrap(err, "c.db.ReadWriteTransaction()")
	}

	return nil
}

// PostgresReadOnlyTransaction panics because it is not implemented for the SpannerClient.
func (c *SpannerClient) PostgresReadOnlyTransaction() any {
	panic("SpannerClient.PostgresReadOnlyTransaction() should never be called.")
}

var _ Reader[nilResource] = (*SpannerReader[nilResource])(nil)

// SpannerReader is a reader for Spanner.
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

// NewSpannerReadWriteTransaction creates a new SpannerReadWriteTransaction from a spanner.ReadWriteTransaction
func NewSpannerReadWriteTransaction(txn *spanner.ReadWriteTransaction) ReadWriteTransaction {
	return &SpannerReadWriteTransaction{
		txn:              txn,
		resourceRowIndex: make(map[string]int),
	}
}

// DBType returns the database type.
func (c *SpannerReadWriteTransaction) DBType() DBType {
	return SpannerDBType
}

// DataChangeEventIndex provides a sequence number for data change events on the same Resource inside the same transaction.
func (c *SpannerReadWriteTransaction) DataChangeEventIndex(res accesstypes.Resource, rowID string) int {
	indexID := fmt.Sprintf("%s_%s", res, rowID)
	c.resourceRowIndex[indexID]++

	return c.resourceRowIndex[indexID]
}

// SpannerReadOnlyTransaction returns a read-only transaction for the Spanner client.
func (c *SpannerReadWriteTransaction) SpannerReadOnlyTransaction() spxscan.Querier {
	return c.txn
}

// BufferMap buffers a map of changes to be applied to the database.
func (c *SpannerReadWriteTransaction) BufferMap(r PatchSetMetadata, patch map[string]any) error {
	var m *spanner.Mutation

	switch r.PatchType() {
	case CreatePatchType:
		m = spanner.InsertMap(string(r.Resource()), patch)
	case UpdatePatchType:
		m = spanner.UpdateMap(string(r.Resource()), patch)
	case DeletePatchType:
		m = spanner.Delete(string(r.Resource()), r.PrimaryKey().KeySet())
	case CreateOrUpdatePatchType:
		m = spanner.InsertOrUpdateMap(string(r.Resource()), patch)
	default:
		panic(fmt.Sprintf("unsupported operation: %s", r.PatchType()))
	}

	if err := c.txn.BufferWrite([]*spanner.Mutation{m}); err != nil {
		return errors.Wrap(err, "spanner.ReadWriteTransaction.BufferWrite()")
	}

	return nil
}

// BufferStruct buffers a struct of changes to be applied to the database.
func (c *SpannerReadWriteTransaction) BufferStruct(patch PatchSetMetadata) error {
	var m *spanner.Mutation
	var err error

	switch patch.PatchType() {
	case CreatePatchType:
		m, err = spanner.InsertStruct(string(patch.Resource()), patch)
		if err != nil {
			return errors.Wrap(err, "spanner.InsertStruct()")
		}
	case UpdatePatchType:
		m, err = spanner.UpdateStruct(string(patch.Resource()), patch)
		if err != nil {
			return errors.Wrap(err, "spanner.UpdateStruct()")
		}
	case CreateOrUpdatePatchType:
		m, err = spanner.InsertOrUpdateStruct(string(patch.Resource()), patch)
		if err != nil {
			return errors.Wrap(err, "spanner.InsertOrUpdateStruct()")
		}
	default:
		panic(fmt.Sprintf("unsupported operation: %s", patch.PatchType()))
	}

	if err := c.txn.BufferWrite([]*spanner.Mutation{m}); err != nil {
		return errors.Wrap(err, "spanner.ReadWriteTransaction.BufferWrite()")
	}

	return nil
}

// PostgresReadOnlyTransaction panics because it is not implemented for the SpannerReadWriteTransaction.
func (c *SpannerReadWriteTransaction) PostgresReadOnlyTransaction() any {
	panic("SpannerReadWriteTransaction.PostgresReadOnlyTransaction() should never be called.")
}
