package resource

import (
	"context"
	"fmt"
	"iter"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/httpio"
	"github.com/cccteam/spxscan"
	"github.com/cccteam/spxscan/spxapi"
	"github.com/go-playground/errors/v5"
	"google.golang.org/api/iterator"
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
func (c *SpannerClient) SpannerReadOnlyTransaction() spxapi.Querier {
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

// ReadOnlyTransaction returns a read-only transaction that can be used for multiple reads from the database.
// You must call Close() when the ReadOnlyTransaction is no longer needed to release resources on the server.
func (c *SpannerClient) ReadOnlyTransaction() ReadOnlyTransactionCloser {
	return newSpannerReadOnlyTransaction(c.spanner)
}

// PostgresReadOnlyTransaction panics because it is not implemented for the SpannerClient.
func (c *SpannerClient) PostgresReadOnlyTransaction() any {
	panic("SpannerClient.PostgresReadOnlyTransaction() should never be called.")
}

var _ Reader[nilResource] = (*spannerReader[nilResource])(nil)

// spannerReader is a reader for Spanner.
type spannerReader[Resource Resourcer] struct {
	readTxn func() spxapi.Querier
}

// DBType returns the database type.
func (c *spannerReader[Resource]) DBType() DBType {
	return SpannerDBType
}

// Read reads a single resource from the database.
func (c *spannerReader[Resource]) Read(ctx context.Context, stmt *Statement) (*Row[Resource], error) {
	var res Resource
	if stmt.maskedNamesColumn != "" {
		row, err := c.readMasked(ctx, stmt)
		if err != nil {
			return nil, err
		}
		if row == nil {
			return nil, httpio.NewNotFoundMessagef("%s (%s) not found", res.Resource(), stmt.resolvedWhereClause)
		}

		return row, nil
	}

	row := new(Row[Resource])
	if err := spxscan.Get(ctx, c.readTxn(), &row.Data, stmt.SpannerStatement()); err != nil {
		if errors.Is(err, spxapi.ErrNotFound) {
			return nil, httpio.NewNotFoundMessagef("%s (%s) not found", res.Resource(), stmt.resolvedWhereClause)
		}

		return nil, errors.Wrap(err, "spxscan.Get()")
	}

	return row, nil
}

// readMasked reads the first row of a masking statement; a nil row with no
// error means no row matched.
func (c *spannerReader[Resource]) readMasked(ctx context.Context, stmt *Statement) (*Row[Resource], error) {
	it := c.readTxn().Query(ctx, stmt.SpannerStatement())
	defer it.Stop()

	spannerRow, err := it.Next()
	if err != nil {
		if errors.Is(err, iterator.Done) {
			return nil, nil
		}

		return nil, errors.Wrap(err, "spanner.RowIterator.Next()")
	}

	return scanMaskedRow[Resource](spannerRow, stmt.maskedNamesColumn)
}

// List reads a list of resources from the database.
func (c *spannerReader[Resource]) List(ctx context.Context, stmt *Statement) iter.Seq2[*Row[Resource], error] {
	if stmt.maskedNamesColumn != "" {
		return c.listMasked(ctx, stmt)
	}

	return func(yield func(*Row[Resource], error) bool) {
		for r, err := range spxscan.SelectSeq[Resource](ctx, c.readTxn(), stmt.SpannerStatement()) {
			if err != nil {
				yield(nil, errors.Wrap(err, "spxscan.SelectSeq()"))

				return
			}
			if !yield(&Row[Resource]{Data: *r}, nil) {
				return
			}
		}
	}
}

// listMasked iterates a masking statement, scanning each row's data and its
// reserved masked-names column into the Row envelope.
func (c *spannerReader[Resource]) listMasked(ctx context.Context, stmt *Statement) iter.Seq2[*Row[Resource], error] {
	return func(yield func(*Row[Resource], error) bool) {
		it := c.readTxn().Query(ctx, stmt.SpannerStatement())
		defer it.Stop()

		for {
			spannerRow, err := it.Next()
			if err != nil {
				if !errors.Is(err, iterator.Done) {
					yield(nil, errors.Wrap(err, "spanner.RowIterator.Next()"))
				}

				return
			}

			row, err := scanMaskedRow[Resource](spannerRow, stmt.maskedNamesColumn)
			if err != nil {
				yield(nil, err)

				return
			}
			if !yield(row, nil) {
				return
			}
		}
	}
}

// scanMaskedRow scans one masking-statement row: the resource columns into the
// envelope's data (leniently, so the reserved column is skipped) and the
// reserved column into the envelope's mask list.
func scanMaskedRow[Resource Resourcer](spannerRow *spanner.Row, maskedNamesColumn string) (*Row[Resource], error) {
	row := new(Row[Resource])
	if err := spannerRow.ToStructLenient(&row.Data); err != nil {
		return nil, errors.Wrap(err, "spanner.Row.ToStructLenient()")
	}
	if err := spannerRow.ColumnByName(maskedNamesColumn, &row.masked); err != nil {
		return nil, errors.Wrap(err, "spanner.Row.ColumnByName()")
	}

	return row, nil
}

var _ ReadOnlyTransactionCloser = (*SpannerReadOnlyTransaction)(nil)

// SpannerReadOnlyTransaction represents a database transaction that can only be used for reads.
type SpannerReadOnlyTransaction struct {
	txn              *spanner.ReadOnlyTransaction
	resourceRowIndex map[string]int
}

// newSpannerReadOnlyTransaction creates a new SpannerReadOnlyTransaction from a spanner.Client
func newSpannerReadOnlyTransaction(client *spanner.Client) ReadOnlyTransactionCloser {
	return &SpannerReadOnlyTransaction{
		txn:              client.ReadOnlyTransaction(),
		resourceRowIndex: make(map[string]int),
	}
}

// Close closes the readonly transaction
func (c *SpannerReadOnlyTransaction) Close() {
	c.txn.Close()
}

// SpannerReadOnlyTransaction returns a read-only transaction for the Spanner client.
func (c *SpannerReadOnlyTransaction) SpannerReadOnlyTransaction() spxapi.Querier {
	return c.txn
}

// PostgresReadOnlyTransaction panics because it is not implemented for the SpannerReadOnlyTransaction.
func (c *SpannerReadOnlyTransaction) PostgresReadOnlyTransaction() any {
	panic("SpannerReadOnlyTransaction.PostgresReadOnlyTransaction() should never be called.")
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
func (c *SpannerReadWriteTransaction) SpannerReadOnlyTransaction() spxapi.Querier {
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
