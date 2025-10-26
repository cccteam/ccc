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

func (c *PostgresClient) PostgresReadOnlyTransaction() any {
	panic(fmt.Sprintf("operation not implemented for database type: %s", PostgresDBType))
}

// ExecuteFunc executes a function within a read-write transaction.
func (c *PostgresClient) ExecuteFunc(ctx context.Context, f func(ctx context.Context, txn ReadWriteTransaction) error) error {
	panic(fmt.Sprintf("operation not implemented for database type: %s", PostgresDBType))
}

func (c *PostgresClient) SpannerReadOnlyTransaction() spxscan.Querier {
	panic("PostgresClient.SpannerReadOnlyTransaction() should never be called.")
}

var _ Reader[nilResource] = (*PostgresReader[nilResource])(nil)

type PostgresReader[Resource Resourcer] struct {
	readTxn func() any
}

// DBType returns the database type.
func (c *PostgresReader[Resource]) DBType() DBType {
	return PostgresDBType
}

// Read reads a single resource from the database.
func (c *PostgresReader[Resource]) Read(ctx context.Context, stmt *Statement) (*Resource, error) {
	panic(fmt.Sprintf("operation not implemented for database type: %s", SpannerDBType))
}

// List reads a list of resources from the database.
func (c *PostgresReader[Resource]) List(ctx context.Context, stmt *Statement) iter.Seq2[*Resource, error] {
	panic(fmt.Sprintf("operation not implemented for database type: %s", SpannerDBType))
}

var _ ReadWriteTransaction = (*PostgresReadWriteTransaction)(nil)

// PostgresReadWriteTransaction represents a database transaction that can be used for both reads and writes.
type PostgresReadWriteTransaction struct {
	dbType           DBType
	postgres         *pgxpool.Pool
	resourceRowIndex map[string]int
}

func newPostgresPostgresReadWriteTransaction(txn *pgxpool.Pool) *PostgresReadWriteTransaction {
	return &PostgresReadWriteTransaction{
		dbType:           PostgresDBType,
		postgres:         txn,
		resourceRowIndex: make(map[string]int),
	}
}

// Read reads a single resource from the database.
func (c *PostgresReadWriteTransaction) Read(ctx context.Context, res Resourcer, dst any, stmt *Statement) error {
	return nil
}

// List reads a list of resources from the database.
func (c *PostgresReadWriteTransaction) List(ctx context.Context, res Resourcer, dst []any, stmt *Statement) error {
	panic("you should never call List() on a PostgresReadWriteTransaction")
}

// DBType returns the database type.
func (c *PostgresReadWriteTransaction) DBType() DBType {
	return PostgresDBType
}

// DataChangeEventIndex() provides a sequence number for data change events on the same Resource inside the same transaction
func (c *PostgresReadWriteTransaction) DataChangeEventIndex(res accesstypes.Resource, rowID string) int {
	indexID := fmt.Sprintf("%s_%s", res, rowID)
	c.resourceRowIndex[indexID]++

	return c.resourceRowIndex[indexID]
}

func (c *PostgresReadWriteTransaction) PostgresReadOnlyTransaction() any {
	panic("PostgresReadOnlyTransaction() not yet implemented")
}

func (c *PostgresReadWriteTransaction) BufferMap(_ PatchType, _ ResourcePatch, _ map[string]any) error {
	panic(fmt.Sprintf("operation not implemented for database type: %s", c.DBType()))
}

func (c *PostgresReadWriteTransaction) BufferStruct(_ PatchType, _ ResourcePatch, _ any) error {
	panic(fmt.Sprintf("operation not implemented for database type: %s", c.DBType()))
}

func (c *PostgresReadWriteTransaction) SpannerReadOnlyTransaction() spxscan.Querier {
	panic("PostgresReadWriteTransaction.SpannerReadOnlyTransaction() should never be called.")
}
