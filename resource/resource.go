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
	case *MockClient:
		return selectMock[Resource](t.ReadOnlyMocks())
	case *MockReadWriteTransaction:
		return selectMock[Resource](t.TxnReadMocks())
	default:
		panic(fmt.Sprintf("unsupported Client type: %T", t))
	}
}

func selectMock[Resource Resourcer](mocks []any) Reader[Resource] {
	for _, mock := range mocks {
		if m, ok := mock.(Reader[Resource]); ok {
			return m
		}
	}

	panic(fmt.Sprintf("mock for type %T not found", new(Resource)))
}
