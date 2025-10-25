package resource

import (
	"context"
	"fmt"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/jackc/pgx/v5/pgxpool"
)

var _ Client = (*PostgresClient)(nil)

// PostgresClient is a wrapper around the database.
type PostgresClient struct {
	dbType   DBType
	postgres *pgxpool.Pool
}

// NewPostgresClient creates a new Client.
func NewPostgresClient(db *pgxpool.Pool) *PostgresClient {
	return &PostgresClient{
		dbType:   PostgresDBType,
		postgres: db,
	}
}

// Close closes the database connection.
func (c *PostgresClient) Close() {
	c.postgres.Close()
}

// // ExecuteFunc executes a function within a read-write transaction.
// func (c *PostgresClient) ExecuteFunc(ctx context.Context, f func(ctx context.Context, txn ReadWriteTransaction) error) error {
// 	if err := c.Execute(ctx, runnerFunc(f)); err != nil {
// 		return errors.Wrap(err, "resource.Client.Execute()")
// 	}

// 	return nil
// }

// // Execute executes a runner within a read-write transaction.
// func (c *PostgresClient) Execute(ctx context.Context, runner TxnRunner) error {
// 	panic(fmt.Sprintf("operation not implemented for database type: %s", PostgresDBType))
// }

// // Read reads a single resource from the database.
// func (c *PostgresClient) Read(ctx context.Context, res Resourcer, dst any, stmt *Statement) error {
// 	return nil
// }

// // List reads a list of resources from the database.
// func (c *PostgresClient) List(ctx context.Context, res Resourcer, dst []any, stmt *Statement) error {
// 	panic("you should never call List() on a PostgresClient")
// }

// // DBType returns the database type.
// func (c *PostgresClient) DBType() DBType {
// 	return c.dbType
// }

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
func (r *PostgresReadWriteTransaction) DBType() DBType {
	return PostgresDBType
}

func (r *PostgresReadWriteTransaction) dataChangeEventIndex(res accesstypes.Resource, rowID string) int {
	indexID := fmt.Sprintf("%s_%s", res, rowID)
	r.resourceRowIndex[indexID]++

	return r.resourceRowIndex[indexID]
}
