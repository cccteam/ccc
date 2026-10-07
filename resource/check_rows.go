package resource

import (
	"context"

	"cloud.google.com/go/spanner"
	"github.com/go-playground/errors/v5"
	"google.golang.org/api/iterator"
)

// queryCheckRow runs a check-SELECT in the transaction and returns the first row's
// leading boolean columns, a NULL read as false — a condition permits only on TRUE.
// found is false when the statement returns no row, which the callers answer as a
// NotFound of their own, kept distinguishable from a condition that fails.
func queryCheckRow(ctx context.Context, txn ReadWriteTransaction, stmt *Statement, columns int) (checks []bool, found bool, err error) {
	switch txn.DBType() {
	case SpannerDBType:
		return querySpannerCheckRow(ctx, txn, stmt, columns)
	case PostgresDBType:
		return queryPostgresCheckRow(ctx, txn.PostgresReadOnlyTransaction(), stmt, columns)
	default:
		return nil, false, errors.Newf("check statements are not implemented for %s", txn.DBType())
	}
}

// querySpannerCheckRow reads a check-SELECT's first row through the Spanner transaction.
func querySpannerCheckRow(ctx context.Context, txn ReadWriteTransaction, stmt *Statement, columns int) (checks []bool, found bool, err error) {
	it := txn.SpannerReadOnlyTransaction().Query(ctx, stmt.SpannerStatement())
	defer it.Stop()

	row, err := it.Next()
	if err != nil {
		if errors.Is(err, iterator.Done) {
			return nil, false, nil
		}

		return nil, false, errors.Wrap(err, "spanner.RowIterator.Next()")
	}

	checks = make([]bool, columns)
	for i := range checks {
		var check spanner.NullBool
		if err := row.Column(i, &check); err != nil {
			return nil, false, errors.Wrap(err, "spanner.Row.Column()")
		}
		checks[i] = check.Valid && check.Bool
	}

	return checks, true, nil
}
