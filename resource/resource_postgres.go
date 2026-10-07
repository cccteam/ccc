package resource

import (
	"context"
	"fmt"
	"iter"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/httpio"
	"github.com/cccteam/spxscan/spxapi"
	"github.com/go-playground/errors/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	// postgresMaxAttempts bounds how often ExecuteFunc runs a transaction that lost to a
	// concurrent one before it gives up and returns the conflict. Spanner's client keeps
	// running an aborted transaction until the context ends; a bound keeps a function that
	// can never win from holding its request until then.
	postgresMaxAttempts = 10

	// postgresRetryBackoff is how long ExecuteFunc waits before the second attempt; each
	// later attempt waits twice as long, up to postgresRetryBackoffCap, each wait spread
	// over its half to its full length so transactions that keep losing to each other do
	// not run in step.
	postgresRetryBackoff    = 5 * time.Millisecond
	postgresRetryBackoffCap = 250 * time.Millisecond
)

var _ Client = (*PostgresClient)(nil)

// PostgresClient is a wrapper around the database.
type PostgresClient struct {
	postgres *pgxpool.Pool
	// stores are the application's file stores by name, where the upload frames
	// stream, the file routes open, and a committed transaction's released objects are
	// deleted from.
	stores *fileStores
	// retryBackoff is the first wait between attempts of a transaction that lost to a
	// concurrent one; a test shortens it.
	retryBackoff time.Duration
}

// NewPostgresClient creates a new Client. WithFileStore and WithNamedFileStore hand it
// the stores the frames drive and a committed transaction's released file objects are
// deleted from; a store wired twice, or two stores on one location, panics here.
func NewPostgresClient(db *pgxpool.Pool, opts ...ClientOption) *PostgresClient {
	return &PostgresClient{
		postgres:     db,
		stores:       applyClientOptions(opts),
		retryBackoff: postgresRetryBackoff,
	}
}

// DBType returns the database type.
func (c *PostgresClient) DBType() DBType {
	return PostgresDBType
}

// FileStore returns the store wired under name, nil when none is.
func (c *PostgresClient) FileStore(name StoreName) FileStore {
	return c.stores.get(name)
}

// Close closes the database connection.
func (c *PostgresClient) Close() {
	c.postgres.Close()
}

// PostgresReadOnlyTransaction returns the pool, which runs each query on its own
// connection outside any transaction, as Spanner's Single() does.
func (c *PostgresClient) PostgresReadOnlyTransaction() PostgresQuerier {
	return c.postgres
}

// ExecuteFunc executes a function within a read-write transaction, serializable as
// Spanner's are. A transaction that loses to a concurrent one (a serialization failure
// or a deadlock) runs the function again, up to postgresMaxAttempts times, as Spanner's
// client runs it again when the transaction aborts.
//
// As with Spanner's, the writes a function buffers are applied when it returns and
// before the commit, in the order they were buffered, and the function's own reads do not
// see them: the code that buffers a patch after reading the row it changes depends on it.
// A refusal the database can state in a constraint (a referential refusal, a duplicate
// key or unique value, a violated CHECK constraint, an update of a row that does not
// exist) answers as the 4xx translatePostgresError composes from the patches the
// transaction buffered; an error the function itself returns passes through unchanged,
// and a commit that fails without the server's answer is reported as one whose outcome
// is unknown (see commitOutcomeUnknown).
//
// Before the writes are applied, a transaction whose patches release a file object of a
// store the client holds no store for is refused naming the store: the rows would go and
// the object would stay with nothing to delete it. Once the commit lands, the file
// objects the transaction's patches released (a deleted row's @file keys, the old key of
// a row pointed at another object) are deleted from their stores, synchronously, before
// ExecuteFunc returns, detached from the request's cancellation; a failed delete is
// logged naming the keys and the call still returns nil, since the rows are gone. A
// transaction that does not commit, for any reason, releases nothing.
func (c *PostgresClient) ExecuteFunc(ctx context.Context, f func(ctx context.Context, txn ReadWriteTransaction) error) error {
	var err error
	for attempt := 1; attempt <= postgresMaxAttempts; attempt++ {
		var retry bool
		if retry, err = c.executeOnce(ctx, f); !retry {
			return err
		}
		if attempt < postgresMaxAttempts {
			select {
			case <-ctx.Done():
				return errors.Wrap(ctx.Err(), "context")
			case <-time.After(c.backoff(attempt)):
			}
		}
	}

	return err
}

// backoff is how long to wait after the given attempt lost: the base doubled for each
// earlier attempt, capped, and spread over its half to its full length.
func (c *PostgresClient) backoff(attempt int) time.Duration {
	wait := min(c.retryBackoff<<(attempt-1), postgresRetryBackoffCap)
	if wait <= 1 {
		return wait
	}

	return wait/2 + rand.N(wait/2+1) //nolint:gosec // a spread, not a secret
}

// executeOnce runs the function in one transaction and reports whether it lost to a
// concurrent transaction and may run again.
func (c *PostgresClient) executeOnce(ctx context.Context, f func(ctx context.Context, txn ReadWriteTransaction) error) (retry bool, _ error) {
	var (
		buffered = newBufferedPatches()
		released = newReleasedKeys()
		touched  = newTouchedRows()
	)

	tx, err := c.postgres.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return false, errors.Wrap(err, "pgxpool.Pool.BeginTx()")
	}
	// The rollback runs detached from the request's cancellation: a canceled context
	// would leave the connection's transaction open until the server notices.
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	txn := newPostgresReadWriteTransaction(tx, buffered, released, touched)
	if err := f(ctx, txn); err != nil {
		return retryablePostgresError(err), errors.Wrap(err, "f()")
	}
	if err := refuseUnwiredRelease(c.stores, released.list()); err != nil {
		return false, err
	}
	if err := txn.flush(ctx); err != nil {
		return retryablePostgresError(err), translatePostgresError(err, buffered)
	}
	if err := tx.Commit(ctx); err != nil {
		err = errors.Wrap(err, "pgx.Tx.Commit()")
		if retryablePostgresError(err) {
			return true, err
		}

		return false, translatePostgresError(markUnknownOutcome(err), buffered)
	}

	releaseFiles(ctx, c.stores, released.list())
	collectTouched(ctx, touched.list())

	return false, nil
}

// markUnknownOutcome marks a commit failure the server did not state: it may have
// committed. An error carrying the server's own answer (a deferred constraint refused at
// commit) is definite and returned as it is.
func markUnknownOutcome(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return err
	}

	return &commitOutcomeUnknownError{err: err}
}

// ReadOnlyTransaction returns a ReadOnlyTransaction that can be used for multiple reads from the database.
// You must call Close() when the ReadOnlyTransaction is no longer needed to release resources on the server.
func (c *PostgresClient) ReadOnlyTransaction() ReadOnlyTransactionCloser {
	return newPostgresReadOnlyTransaction(c.postgres)
}

// SpannerReadOnlyTransaction panics because it is not implemented for the PostgresClient.
func (c *PostgresClient) SpannerReadOnlyTransaction() spxapi.Querier {
	panic("PostgresClient.SpannerReadOnlyTransaction() should never be called.")
}

var _ Reader[nilResource] = (*postgresReader[nilResource])(nil)

// postgresReader is a reader implementation for Postgres.
type postgresReader[Resource Resourcer] struct {
	readTxn func() PostgresQuerier
}

// DBType returns the database type.
func (c *postgresReader[Resource]) DBType() DBType {
	return PostgresDBType
}

// Read reads a single resource from the database.
func (c *postgresReader[Resource]) Read(ctx context.Context, stmt *Statement) (*Row[Resource], error) {
	row, err := readPostgresRow[Resource](ctx, c.readTxn(), stmt)
	if err != nil {
		return nil, err
	}
	if row == nil {
		var res Resource

		return nil, httpio.NewNotFoundMessagef("%s (%s) not found", res.Resource(), stmt.resolvedWhereClause)
	}

	return row, nil
}

// List reads a list of resources from the database.
func (c *postgresReader[Resource]) List(ctx context.Context, stmt *Statement) iter.Seq2[*Row[Resource], error] {
	return listPostgresRows[Resource](ctx, c.readTxn(), stmt)
}

// Count runs a COUNT(*) statement and returns its one value.
func (c *postgresReader[Resource]) Count(ctx context.Context, stmt *Statement) (int64, error) {
	return countPostgresRows(ctx, c.readTxn(), stmt)
}

var (
	_ ReadOnlyTransactionCloser = (*PostgresReadOnlyTransaction)(nil)
	_ PostgresQuerier           = (*PostgresReadOnlyTransaction)(nil)
)

// PostgresReadOnlyTransaction represents a database transaction that can only be used for reads.
// It is a repeatable-read, read-only transaction, begun with its first query so a
// transaction that is never read from takes no connection.
type PostgresReadOnlyTransaction struct {
	pool *pgxpool.Pool

	mu sync.Mutex
	tx pgx.Tx
}

// newPostgresReadOnlyTransaction creates a new PostgresReadOnlyTransaction
func newPostgresReadOnlyTransaction(pool *pgxpool.Pool) ReadOnlyTransactionCloser {
	return &PostgresReadOnlyTransaction{pool: pool}
}

// Query runs the query in the transaction, beginning it first when this is its first.
func (c *PostgresReadOnlyTransaction) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	tx, err := c.begin(ctx)
	if err != nil {
		return nil, err
	}

	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, errors.Wrap(err, "pgx.Tx.Query()")
	}

	return rows, nil
}

// begin returns the transaction, beginning it on first use.
func (c *PostgresReadOnlyTransaction) begin(ctx context.Context) (pgx.Tx, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.tx == nil {
		tx, err := c.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
		if err != nil {
			return nil, errors.Wrap(err, "pgxpool.Pool.BeginTx()")
		}
		c.tx = tx
	}

	return c.tx, nil
}

// Close closes the readonly transaction
func (c *PostgresReadOnlyTransaction) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.tx != nil {
		// A read-only transaction has nothing to commit; the rollback releases its connection.
		_ = c.tx.Rollback(context.Background())
		c.tx = nil
	}
}

// PostgresReadOnlyTransaction returns the transaction, which runs the reader's queries.
func (c *PostgresReadOnlyTransaction) PostgresReadOnlyTransaction() PostgresQuerier {
	return c
}

// SpannerReadOnlyTransaction panics because it is not implemented for the PostgresReadOnlyTransaction.
func (c *PostgresReadOnlyTransaction) SpannerReadOnlyTransaction() spxapi.Querier {
	panic("PostgresReadOnlyTransaction.SpannerReadOnlyTransaction() should never be called.")
}

var _ ReadWriteTransaction = (*PostgresReadWriteTransaction)(nil)

// PostgresReadWriteTransaction represents a database transaction that can be used for both reads and writes.
// Its writes are buffered, as Spanner's mutations are, and applied by ExecuteFunc when the
// function returns. It records the resource and patch type of every patch it buffers, so a
// write Postgres refuses can be answered in terms of what the transaction was asked to do,
// the file objects its patches release, so the executor deletes them once the commit lands,
// and the rows its patches write, so the request publishes them to the live pages once the
// commit lands.
type PostgresReadWriteTransaction struct {
	txn              pgx.Tx
	resourceRowIndex map[string]int
	pending          []*postgresMutation
	buffered         *bufferedPatches
	released         *releasedKeys
	touchedRows      *touchedRows
}

// newPostgresReadWriteTransaction wraps a transaction over the records its buffered
// patches, released file keys, and written rows are noted in; ExecuteFunc holds the same
// records when the commit comes back.
func newPostgresReadWriteTransaction(txn pgx.Tx, buffered *bufferedPatches, released *releasedKeys, touched *touchedRows) *PostgresReadWriteTransaction {
	return &PostgresReadWriteTransaction{
		txn:              txn,
		resourceRowIndex: make(map[string]int),
		buffered:         buffered,
		released:         released,
		touchedRows:      touched,
	}
}

// recordReleased notes file keys of one store the transaction's patches let go of.
func (c *PostgresReadWriteTransaction) recordReleased(store StoreName, keys ...string) {
	c.released.record(store, keys...)
}

// Released returns the file objects the transaction's patches released so far, each with
// its store: a deleted row's @file keys, and the old key of a row pointed at another
// object. ExecuteFunc deletes them from the client's stores after the commit.
func (c *PostgresReadWriteTransaction) Released() []ReleasedKey {
	return c.released.list()
}

// touched returns the rows the transaction's patches wrote so far.
func (c *PostgresReadWriteTransaction) touched() []touchedRow {
	return c.touchedRows.list()
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

// PostgresReadOnlyTransaction returns the transaction, which runs the reader's queries.
// The writes the function has buffered are not applied yet, so its reads do not see them.
func (c *PostgresReadWriteTransaction) PostgresReadOnlyTransaction() PostgresQuerier {
	return c.txn
}

// BufferMap buffers a map of changes to be applied to the database.
func (c *PostgresReadWriteTransaction) BufferMap(r PatchSetMetadata, patch map[string]any) error {
	return c.buffer(r, patch)
}

// BufferStruct buffers a struct of changes to be applied to the database.
func (c *PostgresReadWriteTransaction) BufferStruct(patch PatchSetMetadata) error {
	values, err := structColumnValues(patch)
	if err != nil {
		return err
	}

	return c.buffer(patch, values)
}

// buffer renders the patch as its statement and queues it for flush.
func (c *PostgresReadWriteTransaction) buffer(r PatchSetMetadata, values map[string]any) error {
	mutation, err := renderPostgresMutation(r, values)
	if err != nil {
		return err
	}
	if _, _, err := mutation.stmt.PostgresStatement(); err != nil {
		return err
	}

	c.pending = append(c.pending, mutation)
	c.buffered.record(r)
	c.touchedRows.record(r)

	return nil
}

// flush applies the buffered writes in the order they were buffered. An UPDATE that
// matches no row is refused as Spanner refuses an update of a row that does not exist.
func (c *PostgresReadWriteTransaction) flush(ctx context.Context) error {
	for _, mutation := range c.pending {
		sql, args, err := mutation.stmt.PostgresStatement()
		if err != nil {
			return err
		}
		tag, err := c.txn.Exec(ctx, sql, args)
		if err != nil {
			return errors.Wrapf(err, "pgx.Tx.Exec(%s)", mutation.describe())
		}
		if mutation.mustAffectRow && tag.RowsAffected() == 0 {
			return errors.Wrapf(errPostgresRowNotFound, "%s", mutation.describe())
		}
	}
	c.pending = nil

	return nil
}

// SpannerReadOnlyTransaction panics because it is not implemented for the PostgresReadWriteTransaction.
func (c *PostgresReadWriteTransaction) SpannerReadOnlyTransaction() spxapi.Querier {
	panic("PostgresReadWriteTransaction.SpannerReadOnlyTransaction() should never be called.")
}
