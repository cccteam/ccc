package resource

import (
	"context"
	"iter"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/spxscan"
	"github.com/go-playground/errors/v5"
)

var _ Client = (*MockClient)(nil)

// MockClient is a wrapper around the database.
type MockClient struct {
	dbType        DBType
	readOnlyMocks []any
	txnReadMocks  []any
	txnMock       ReadWriteTransaction
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
func NewMockClient(txnMock ReadWriteTransaction, readOnlyMocks, txnReadMocks []any) *MockClient {
	return &MockClient{
		dbType:        SpannerDBType,
		readOnlyMocks: readOnlyMocks,
		txnReadMocks:  txnReadMocks,
		txnMock:       txnMock,
	}
}

// Close closes the database connection.
func (c *MockClient) Close() {
}

// ReadOnlyMocks returns the read-only mocks for the Mock client.
func (c *MockClient) ReadOnlyMocks() []any {
	return c.readOnlyMocks
}

// SpannerReadOnlyTransaction returns a read-only transaction for the Mock client.
func (c *MockClient) SpannerReadOnlyTransaction() spxscan.Querier {
	return nil
}

// ExecuteFunc executes a function within a read-write transaction.
func (c *MockClient) ExecuteFunc(ctx context.Context, f func(ctx context.Context, txn ReadWriteTransaction) error) error {
	if err := f(ctx, NewMockReadWriteTransaction(c.txnMock, c.txnReadMocks...)); err != nil {
		return errors.Wrap(err, "f()")
	}

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
}

// NewMockReadWriteTransaction creates a new MockReadWriteTransaction.
func NewMockReadWriteTransaction(mock ReadWriteTransaction, txnReaderMocks ...any) ReadWriteTransaction {
	return &MockReadWriteTransaction{
		txnReaderMocks: txnReaderMocks,
		txnMock:        mock,
	}
}

// DBType returns the database type.
func (c *MockReadWriteTransaction) DBType() DBType {
	return c.txnMock.DBType()
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
func (c *MockReadWriteTransaction) SpannerReadOnlyTransaction() spxscan.Querier {
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

// MockIterSeq2 is used for mocking iter.Seq2[Resourcer, error] type. If both err and resource are
// provided, it will yield all elements in resource first and then err
func MockIterSeq2[Resource Resourcer](err error, resource ...*Resource) iter.Seq2[*Resource, error] {
	return func(yield func(*Resource, error) bool) {
		for _, r := range resource {
			if !yield(r, nil) {
				return
			}
		}
		if err != nil {
			yield(nil, err)

			return
		}
	}
}
