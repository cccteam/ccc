package resource

import (
	"fmt"

	"github.com/cccteam/spxscan"
)

// rewReader creates a new Reader for the given transaction.
func rewReader[Resource Resourcer](txn ReadOnlyTransaction) Reader[Resource] {
	switch t := txn.(type) {
	case *SpannerClient, *SpannerReadWriteTransaction:
		return &spannerReader[Resource]{
			readTxn: func() spxscan.Querier { return txn.SpannerReadOnlyTransaction() },
		}
	case *PostgresClient, *PostgresReadWriteTransaction:
		return &postgresReader[Resource]{
			readTxn: func() any { return txn.PostgresReadOnlyTransaction() },
		}
	case *MockClient:
		return selectMock[Resource](t.ReadOnlyMocks())
	case *MockReadWriteTransaction:
		return selectMock[Resource](t.TxnReadMocks())
	default:
		panic(fmt.Sprintf("unsupported Client type: %T", t))
	}
}

func selectMock[Resource Resourcer](mocks []any) Reader[Resource] {
	var foundMock Reader[Resource]
	var found bool
	for _, mock := range mocks {
		if m, ok := mock.(Reader[Resource]); ok {
			if found {
				panic(fmt.Sprintf("found multiple mocks for type %T, only one is allowed", mock))
			}
			foundMock = m
			found = true
		}
	}
	if found {
		return foundMock
	}

	panic(fmt.Sprintf("mock for type %T not found", *new(Resource)))
}
