package resource

import "cloud.google.com/go/spanner"

type DBType string

const (
	SpannerDBType  DBType = "spanner"
	PostgresDBType DBType = "postgres"
)

type Statement struct {
	SQL              string
	SpannerParams    map[string]any
	PostgreSQLParams []any
}

type SpannerStatement struct {
	// resolvedWhereClause is used to carry contextual information for error messages
	// and is not used in the query.
	resolvedWhereClause string

	spanner.Statement
}

type PostgresStatement struct {
	// resolvedWhereClause is used to carry contextual information for error messages
	// and is not used in the query.
	resolvedWhereClause string

	SQL              string
	PostgreSQLParams []any
}

type Columns string

type Config struct {
	DBType              DBType
	ChangeTrackingTable string
	TrackChanges        bool
}

func (c Config) SetDBType(dbType DBType) Config {
	c.DBType = dbType

	return c
}

func (c Config) SetChangeTrackingTable(changeTrackingTable string) Config {
	c.ChangeTrackingTable = changeTrackingTable

	return c
}

func (c Config) SetTrackChanges(trackChanges bool) Config {
	c.TrackChanges = trackChanges

	return c
}

type Configurer interface {
	Config() Config
}
