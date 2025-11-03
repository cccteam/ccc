package resource

import (
	"context"
	"fmt"
	"iter"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/spxscan"
	"github.com/jackc/pgx/v5/pgxpool"
)

var _ Client = (*PostgresClient)(nil)

// PostgresClient is a wrapper around the database.
type PostgresClient struct {
	postgres *pgxpool.Pool
}

// NewPostgresClient creates a new Client.
func NewPostgresClient(db *pgxpool.Pool) *PostgresClient {
	return &PostgresClient{
		postgres: db,
	}
}

// Close closes the database connection.
func (c *PostgresClient) Close() {
	c.postgres.Close()
}

// PostgresReadOnlyTransaction panics because it is not implemented for the PostgresClient.
func (c *PostgresClient) PostgresReadOnlyTransaction() any {
	panic("PostgresReadOnlyTransaction() not implemented for PostgresClient")
}

// ExecuteFunc executes a function within a read-write transaction.
func (c *PostgresClient) ExecuteFunc(_ context.Context, _ func(ctx context.Context, _ ReadWriteTransaction) error) error {
	_ = newPostgresPostgresReadWriteTransaction(nil)
	panic("ExecuteFunc() not implemented for PostgresClient")
}

// SpannerReadOnlyTransaction panics because it is not implemented for the PostgresClient.
func (c *PostgresClient) SpannerReadOnlyTransaction() spxscan.Querier {
	panic("PostgresClient.SpannerReadOnlyTransaction() should never be called.")
}

var _ Reader[nilResource] = (*postgresReader[nilResource])(nil)

// postgresReader is a reader implementation for Postgres.
type postgresReader[Resource Resourcer] struct {
	readTxn func() any
}

// DBType returns the database type.
func (c *postgresReader[Resource]) DBType() DBType {
	return PostgresDBType
}

// Read reads a single resource from the database.
func (c *postgresReader[Resource]) Read(_ context.Context, _ *Statement) (*Resource, error) {
	panic("Read() not implemented for PostgresReader[Resource]")
}

// List reads a list of resources from the database.
func (c *postgresReader[Resource]) List(_ context.Context, _ *Statement) iter.Seq2[*Resource, error] {
	panic("List() not implemented for PostgresReader[Resource]")
}

var _ ReadWriteTransaction = (*PostgresReadWriteTransaction)(nil)

// PostgresReadWriteTransaction represents a database transaction that can be used for both reads and writes.
type PostgresReadWriteTransaction struct {
	postgres         *pgxpool.Pool
	resourceRowIndex map[string]int
}

func newPostgresPostgresReadWriteTransaction(txn *pgxpool.Pool) *PostgresReadWriteTransaction {
	return &PostgresReadWriteTransaction{
		postgres:         txn,
		resourceRowIndex: make(map[string]int),
	}
}

// Read reads a single resource from the database.
func (c *PostgresReadWriteTransaction) Read(_ context.Context, _ Resourcer, _ any, _ *Statement) error {
	panic("Read() not implemented for PostgresReadWriteTransaction")
}

// List reads a list of resources from the database.
func (c *PostgresReadWriteTransaction) List(_ context.Context, _ Resourcer, _ []any, _ *Statement) error {
	panic("you should never call List() on a PostgresReadWriteTransaction")
}

// DBType returns the database type.
func (c *PostgresReadWriteTransaction) DBType() DBType {
	return PostgresDBType
}

// DataChangeEventIndex provides a sequence number for data change events on the same Resource inside the same transaction.
func (c *PostgresReadWriteTransaction) DataChangeEventIndex(res accesstypes.Resource, rowID string) int {
	indexID := fmt.Sprintf("%s_%s", res, rowID)
	c.resourceRowIndex[indexID]++

	return c.resourceRowIndex[indexID]
}

// PostgresReadOnlyTransaction panics because it is not implemented for the PostgresReadWriteTransaction.
func (c *PostgresReadWriteTransaction) PostgresReadOnlyTransaction() any {
	panic("PostgresReadOnlyTransaction() not implemented for PostgresReadWriteTransaction")
}

// BufferMap panics because it is not implemented for the PostgresReadWriteTransaction.
func (c *PostgresReadWriteTransaction) BufferMap(_ PatchSetMetadata, _ map[string]any) error {
	panic("BufferMap() not implemented for PostgresReadWriteTransaction")
}

// BufferStruct panics because it is not implemented for the PostgresReadWriteTransaction.
func (c *PostgresReadWriteTransaction) BufferStruct(_ PatchSetMetadata) error {
	panic("BufferStruct() not implemented for PostgresReadWriteTransaction")
}

// SpannerReadOnlyTransaction panics because it is not implemented for the PostgresReadWriteTransaction.
func (c *PostgresReadWriteTransaction) SpannerReadOnlyTransaction() spxscan.Querier {
	panic("PostgresReadWriteTransaction.SpannerReadOnlyTransaction() should never be called.")
}
