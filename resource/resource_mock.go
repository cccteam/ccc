package resource

import (
	"context"
	"iter"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/spxscan/spxapi"
	"github.com/go-playground/errors/v5"
)

var _ Client = (*MockClient)(nil)

// MockClient is a wrapper around the database.
type MockClient struct {
	dbType        DBType
	readOnlyMocks []any
	txnReadMocks  []any
	txnMock       ReadWriteTransaction
	// store is the FileStore a committed transaction's released file objects are
	// deleted from, as on the Spanner client; nil when none was given.
	store FileStore
}

// NewMockClient creates a new MockClient for testing resource database interactions.
//
// It uses the following mocks:
// - txnMock: Mocks Buffer calls.
// - readOnlyMocks: Mocks Read/List calls outside a transaction.
// - txnReadMocks: Mocks Read/List calls inside a transaction.
//
// IMPORTANT: For readOnlyMocks and txnReadMocks, provide only one mock per
// resource type (e.g., Read[MyResource]). Multiple calls for the same Resource
// must be configured on that single mock.
//
// WithFileStore hands it the store ExecuteFunc deletes released file objects from after
// the function returns, so a test can assert what a patch released.
func NewMockClient(txnMock ReadWriteTransaction, readOnlyMocks, txnReadMocks []any, opts ...ClientOption) *MockClient {
	options := applyClientOptions(opts)

	return &MockClient{
		dbType:        SpannerDBType,
		readOnlyMocks: readOnlyMocks,
		txnReadMocks:  txnReadMocks,
		txnMock:       txnMock,
		store:         options.store,
	}
}

// DBType returns the database type the mock stands in for.
func (c *MockClient) DBType() DBType {
	return c.dbType
}

// Close closes the database connection.
func (c *MockClient) Close() {
}

// ReadOnlyMocks returns the read-only mocks for the Mock client.
func (c *MockClient) ReadOnlyMocks() []any {
	return c.readOnlyMocks
}

// SpannerReadOnlyTransaction returns a read-only transaction for the Mock client.
func (c *MockClient) SpannerReadOnlyTransaction() spxapi.Querier {
	return nil
}

// ExecuteFunc executes a function within a read-write transaction. As the Spanner
// client's does, it deletes the file objects the function's patches released from the
// client's FileStore once the function returns nil, and releases nothing when it errors.
func (c *MockClient) ExecuteFunc(ctx context.Context, f func(ctx context.Context, txn ReadWriteTransaction) error) error {
	txn := newMockReadWriteTransaction(c.txnMock, newReleasedKeys(), c.txnReadMocks...)
	if err := f(ctx, txn); err != nil {
		return errors.Wrap(err, "f()")
	}

	releaseFiles(ctx, c.store, txn.Released())

	return nil
}

// ReadOnlyTransaction returns a ReadOnlyTransaction that can be used for multiple reads from the database.
// You must call Close() when the ReadOnlyTransaction is no longer needed to release resources on the server.
func (c *MockClient) ReadOnlyTransaction() ReadOnlyTransactionCloser {
	return c
}

// PostgresReadOnlyTransaction panics because it is not implemented for the MockClient.
func (c *MockClient) PostgresReadOnlyTransaction() any {
	panic("MockClient.PostgresReadOnlyTransaction() should never be called.")
}

var _ ReadWriteTransaction = (*MockReadWriteTransaction)(nil)

// MockReadWriteTransaction represents a database transaction that can be used for both reads and writes.
type MockReadWriteTransaction struct {
	txnReaderMocks []any
	txnMock        ReadWriteTransaction
	released       *releasedKeys
}

// NewMockReadWriteTransaction creates a new MockReadWriteTransaction.
func NewMockReadWriteTransaction(mock ReadWriteTransaction, txnReaderMocks ...any) *MockReadWriteTransaction {
	return newMockReadWriteTransaction(mock, newReleasedKeys(), txnReaderMocks...)
}

// newMockReadWriteTransaction wraps the mocks over the record the released file keys
// are noted in; the Mock client's ExecuteFunc reads it back when the function returns.
func newMockReadWriteTransaction(mock ReadWriteTransaction, released *releasedKeys, txnReaderMocks ...any) *MockReadWriteTransaction {
	return &MockReadWriteTransaction{
		txnReaderMocks: txnReaderMocks,
		txnMock:        mock,
		released:       released,
	}
}

// DBType returns the database type.
func (c *MockReadWriteTransaction) DBType() DBType {
	return c.txnMock.DBType()
}

// recordReleased notes file keys the transaction's patches let go of.
func (c *MockReadWriteTransaction) recordReleased(keys ...string) {
	c.released.record(keys...)
}

// Released returns the file object keys the transaction's patches released so far, as
// SpannerReadWriteTransaction.Released does.
func (c *MockReadWriteTransaction) Released() []string {
	return c.released.list()
}

// DataChangeEventIndex provides a sequence number for data change events on the same Resource inside the same transaction.
func (c *MockReadWriteTransaction) DataChangeEventIndex(res accesstypes.Resource, rowID string) int {
	return c.txnMock.DataChangeEventIndex(res, rowID)
}

// TxnReadMocks returns the read mocks inside a transaction for the MockReadWriteTransaction.
func (c *MockReadWriteTransaction) TxnReadMocks() []any {
	return c.txnReaderMocks
}

// SpannerReadOnlyTransaction returns a read-only transaction for the MockReadWriteTransaction.
func (c *MockReadWriteTransaction) SpannerReadOnlyTransaction() spxapi.Querier {
	return nil
}

// BufferMap buffers a map of changes to be applied to the database.
func (c *MockReadWriteTransaction) BufferMap(r PatchSetMetadata, p map[string]any) error {
	if err := c.txnMock.BufferMap(r, p); err != nil {
		return errors.Wrap(err, "c.txnMock.BufferMap()")
	}

	return nil
}

// BufferStruct buffers a struct of changes to be applied to the database.
func (c *MockReadWriteTransaction) BufferStruct(p PatchSetMetadata) error {
	if err := c.txnMock.BufferStruct(p); err != nil {
		return errors.Wrap(err, "c.txnMock.BufferStruct()")
	}

	return nil
}

// PostgresReadOnlyTransaction panics because it is not implemented for the MockReadWriteTransaction.
func (c *MockReadWriteTransaction) PostgresReadOnlyTransaction() any {
	panic("MockReadWriteTransaction.PostgresReadOnlyTransaction() should never be called.")
}

// MockIterSeq2 is used for mocking the iter.Seq2[*Row[Resource], error] type returned by List.
// Each resource is wrapped in the Row envelope. If both err and resource are provided, it will
// yield all elements in resource first and then err
func MockIterSeq2[Resource Resourcer](err error, resource ...*Resource) iter.Seq2[*Row[Resource], error] {
	return func(yield func(*Row[Resource], error) bool) {
		for _, r := range resource {
			if !yield(&Row[Resource]{Data: *r}, nil) {
				return
			}
		}
		if err != nil {
			yield(nil, err)

			return
		}
	}
}
