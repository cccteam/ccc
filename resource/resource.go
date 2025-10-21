package resource

import (
	"context"
	"fmt"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/go-playground/errors/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Client is a wrapper around the database.
type Client struct {
	dbType   DBType
	spanner  *spanner.Client
	postgres *pgxpool.Pool
}

// NewSpannerClient creates a new Client.
func NewSpannerClient(db *spanner.Client) *Client {
	return &Client{
		dbType:  SpannerDBType,
		spanner: db,
	}
}

// Close closes the database connection.
func (c *Client) Close() {
	switch c.dbType {
	case SpannerDBType:
		c.spanner.Close()
	case PostgresDBType:
		c.postgres.Close()
	default:
		panic(fmt.Sprintf("unsupported db type: %s", c.dbType))
	}
}

// ExecuteFunc executes a function within a read-write transaction.
func (c *Client) ExecuteFunc(ctx context.Context, f func(ctx context.Context, txn *ReadWriteTransaction) error) error {
	if err := c.Execute(ctx, runnerFunc(f)); err != nil {
		return errors.Wrap(err, "resource.Client.Execute()")
	}

	return nil
}

// Execute executes a runner within a read-write transaction.
func (c *Client) Execute(ctx context.Context, runner TxnRunner) error {
	switch c.dbType {
	case SpannerDBType:
		_, err := c.spanner.ReadWriteTransaction(ctx, func(ctx context.Context, txn *spanner.ReadWriteTransaction) error {
			if err := runner.Execute(ctx, newSpannerReadWriteTransaction(txn)); err != nil {
				return errors.Wrap(err, "runner()")
			}

			return nil
		})
		if err != nil {
			return errors.Wrap(err, "c.db.ReadWriteTransaction()")
		}

		return nil
	case PostgresDBType:
		panic(fmt.Sprintf("operation not implemented for database type: %s", c.dbType))
	case mockDBType:
		return runner.Execute(ctx, newMockReadWriteTransaction())
	default:
		panic(fmt.Sprintf("unsupported db type: %s", c.dbType))
	}
}

// DBType returns the database type.
func (c *Client) DBType() DBType {
	return c.dbType
}

// SpannerReadTransaction returns a transaction for querying resources
func (c *Client) SpannerReadTransaction() SpannerQuerier {
	return c.spanner.Single()
}

// PostgresReadTransaction returns a transaction for querying resources
func (c *Client) PostgresReadTransaction() {}

// ReadWriteTransaction represents a database transaction that can be used for both reads and writes.
type ReadWriteTransaction struct {
	dbType           DBType
	spanner          *spanner.ReadWriteTransaction
	resourceRowIndex map[string]int
}

func newSpannerReadWriteTransaction(txn *spanner.ReadWriteTransaction) *ReadWriteTransaction {
	return &ReadWriteTransaction{
		dbType:           SpannerDBType,
		spanner:          txn,
		resourceRowIndex: make(map[string]int),
	}
}

func newMockReadWriteTransaction() *ReadWriteTransaction {
	return &ReadWriteTransaction{
		dbType:           mockDBType,
		resourceRowIndex: make(map[string]int),
	}
}

func (r *ReadWriteTransaction) dataChangeEventIndex(res accesstypes.Resource, rowId string) int {
	indexId := fmt.Sprintf("%s_%s", res, rowId)
	r.resourceRowIndex[indexId]++

	return r.resourceRowIndex[indexId]
}

// DBType returns the database type.
func (r *ReadWriteTransaction) DBType() DBType {
	return r.dbType
}

// SpannerReadTransaction returns a transaction for querying resources
func (r *ReadWriteTransaction) SpannerReadTransaction() SpannerQuerier {
	return r.spanner
}

// PostgresReadTransaction returns a transaction for querying resources
func (r *ReadWriteTransaction) PostgresReadTransaction() {}

// runnerFunc is a function that converts a TxnFuncRunner into a TxnRunner
type runnerFunc func(ctx context.Context, txn *ReadWriteTransaction) error

// Execute runs the function.
func (fn runnerFunc) Execute(ctx context.Context, txn *ReadWriteTransaction) error {
	return fn(ctx, txn)
}
