package resource

import (
	"context"
	"fmt"
	"iter"
	"time"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/httpio"
	"github.com/cccteam/spxscan"
	"github.com/go-playground/errors/v5"
)

var (
	_ Client = (*SpannerClient)(nil)
	_ Txn    = (*SpannerClient)(nil)
)

// SpannerClient is a wrapper around the database.
type SpannerClient struct {
	dbType  DBType
	spanner *spanner.Client
}

// NewSpannerClient creates a new Client.
func NewSpannerClient(db *spanner.Client) *SpannerClient {
	return &SpannerClient{
		dbType:  SpannerDBType,
		spanner: db,
	}
}

// Close closes the database connection.
func (c *SpannerClient) Close() {
	c.spanner.Close()
}

// DBType returns the database type.
func (c *SpannerClient) DBType() DBType {
	return c.dbType
}

func (c *SpannerClient) Single() spxscan.Querier {
	return c.spanner.Single()
}

func (c *SpannerClient) ReadWriteTransaction(ctx context.Context, f func(context.Context, *spanner.ReadWriteTransaction) error) (commitTimestamp time.Time, err error) {
	return c.spanner.ReadWriteTransaction(ctx, f)
}

// ExecuteFunc executes a function within a read-write transaction.
func (c *SpannerClient) ExecuteFunc(ctx context.Context, f func(ctx context.Context, txn *SpannerReadWriteTransaction) error) error {
	if err := c.Execute(ctx, runnerFunc(f)); err != nil {
		return errors.Wrap(err, "resource.Client.Execute()")
	}

	return nil
}

// Execute executes a runner within a read-write transaction.
func (c *SpannerClient) Execute(ctx context.Context, runner TxnRunner) error {
	_, err := c.ReadWriteTransaction(ctx, func(ctx context.Context, txn *spanner.ReadWriteTransaction) error {
		if err := runner.Execute(ctx, newSpannerReadWriteTransaction(txn)); err != nil {
			return errors.Wrap(err, "runner()")
		}

		return nil
	})
	if err != nil {
		return errors.Wrap(err, "c.db.ReadWriteTransaction()")
	}

	return nil
}

type SpannerReader[Resource Resourcer] struct {
	dbType  DBType
	readTxn func() spxscan.Querier
}

// DBType returns the database type.
func (c *SpannerReader[Resource]) DBType() DBType {
	return c.dbType
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
			if !yield(r, errors.Wrap(err, "spxscan.SelectSeq()")) {
				return
			}
		}
	}
}

type SpannerReadWriter[Resource Resourcer] struct {
	dbType  DBType
	readTxn func() spxscan.Querier
}

type SpannerExecutor[Resource Resourcer] struct {
	dbType               DBType
	ReadWriteTransaction func(ctx context.Context, f func(context.Context, *spanner.ReadWriteTransaction) error) (commitTimestamp time.Time, err error)
}

// SpannerReadWriteTransaction represents a database transaction that can be used for both reads and writes.
type SpannerReadWriteTransaction struct {
	dbType           DBType
	txn              *spanner.ReadWriteTransaction
	resourceRowIndex map[string]int
}

func newSpannerReadWriteTransaction(txn *spanner.ReadWriteTransaction) *SpannerReadWriteTransaction {
	return &SpannerReadWriteTransaction{
		dbType:           SpannerDBType,
		txn:              txn,
		resourceRowIndex: make(map[string]int),
	}
}

func (c *SpannerReadWriteTransaction) SpannerTxn() spxscan.Querier {
	return c.txn
}

func (r *SpannerReadWriteTransaction) dataChangeEventIndex(res accesstypes.Resource, rowID string) int {
	indexID := fmt.Sprintf("%s_%s", res, rowID)
	r.resourceRowIndex[indexID]++

	return r.resourceRowIndex[indexID]
}

// Read reads a single resource from the database.
func (c *SpannerReadWriteTransaction) Read(ctx context.Context, stmt *Statement) (*Resource, error) {
	var res Resource
	dst := new(Resource)
	if err := spxscan.Get(ctx, c.txn, dst, stmt.SpannerStatement()); err != nil {
		if errors.Is(err, spxscan.ErrNotFound) {
			return nil, httpio.NewNotFoundMessagef("%s (%s) not found", res.Resource(), stmt.resolvedWhereClause)
		}

		return nil, errors.Wrap(err, "spxscan.Get()")
	}

	return dst, nil
}

// List reads a list of resources from the database.
func (c *SpannerReadWriteTransaction) List(ctx context.Context, stmt *Statement) iter.Seq2[*Resource, error] {
	return func(yield func(*Resource, error) bool) {
		for r, err := range spxscan.SelectSeq(ctx, c.txn, stmt.SpannerStatement()) {
			if !yield(r, err) {
				return
			}
		}
	}
}

// DBType returns the database type.
func (r *SpannerReadWriteTransaction) DBType() DBType {
	return r.dbType
}

// runnerFunc is a function that converts a TxnFuncRunner into a TxnRunner
type runnerFunc func(ctx context.Context, txn *SpannerReadWriteTransaction) error

// Execute runs the function.
func (fn runnerFunc) Execute(ctx context.Context, txn *SpannerReadWriteTransaction) error {
	return fn(ctx, txn)
}
