package resource

import (
	"fmt"

	"github.com/cccteam/spxscan"
)

// NewReader creates a new Reader for the given transaction.
func NewReader[Resource Resourcer](txn ReadOnlyTransaction) Reader[Resource] {
	switch t := txn.(type) {
	case *SpannerClient, *SpannerReadWriteTransaction:
		return &SpannerReader[Resource]{
			readTxn: func() spxscan.Querier { return txn.SpannerReadOnlyTransaction() },
		}
	case *PostgresClient, *PostgresReadWriteTransaction:
		return &PostgresReader[Resource]{
			readTxn: func() any { return txn.PostgresReadOnlyTransaction() },
		}
	default:
		panic(fmt.Sprintf("unsupported Client type: %T", t))
	}
}
