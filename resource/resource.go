package resource

import (
	"fmt"

	"github.com/cccteam/spxscan"
)

// NewReader creates a new Reader for the given transaction.
func NewReader[Resource Resourcer](c Client) Reader[Resource] {
	switch c.DBType() {
	case SpannerDBType:
		return &SpannerReader[Resource]{
			dbType:  SpannerDBType,
			readTxn: func() spxscan.Querier { return c.Single() },
		}
	case PostgresDBType:
		panic(fmt.Sprintf("unimplemented database type: %s", c.DBType()))
	default:
		panic(fmt.Sprintf("unsupported database type: %s", c.DBType()))
	}
}

// NewReadWriter
func NewReadWriter[Resource Resourcer](txn Txn) Reader[Resource] {
	switch txn.DBType() {
	case SpannerDBType:
		return &SpannerReader[Resource]{
			dbType:  SpannerDBType,
			readTxn: txn,
		}
	case PostgresDBType:
		panic(fmt.Sprintf("unimplemented database type: %s", txn.DBType()))
	default:
		panic(fmt.Sprintf("unsupported database type: %s", txn.DBType()))
	}
}
