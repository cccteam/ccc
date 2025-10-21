package resource

import "cloud.google.com/go/spanner"

// DBType represents the type of database, such as Spanner or PostgreSQL.
type DBType string

const (
	// SpannerDBType represents the Google Cloud Spanner database.
	SpannerDBType DBType = "spanner"

	// PostgresDBType represents the PostgreSQL database.
	PostgresDBType DBType = "postgres"

	mockDBType DBType = "mock"
)

// Statement is a generic container for a SQL statement and its parameters,
// supporting both Spanner and PostgreSQL.
type Statement struct {
	SQL              string
	SpannerParams    map[string]any
	PostgreSQLParams []any
}

// SpannerStatement wraps a `spanner.Statement` and includes a resolved WHERE clause for debugging.
type SpannerStatement struct {
	// resolvedWhereClause is used to carry contextual information for error messages
	// and is not used in the query.
	resolvedWhereClause string

	spanner.Statement
}

// PostgresStatement holds a SQL string and its parameters for a PostgreSQL query.
type PostgresStatement struct {
	// resolvedWhereClause is used to carry contextual information for error messages
	// and is not used in the query.
	resolvedWhereClause string

	SQL    string
	Params []any
}

// Columns is a string representing a comma-separated list of database column names.
type Columns string

// Config holds database-specific configuration for a resource.
type Config struct {
	DBType              DBType
	ChangeTrackingTable string
	TrackChanges        bool
}

// SetDBType returns a new Config with the DBType set.
func (c Config) SetDBType(dbType DBType) Config {
	c.DBType = dbType

	return c
}

// SetChangeTrackingTable returns a new Config with the change tracking table name set.
func (c Config) SetChangeTrackingTable(changeTrackingTable string) Config {
	c.ChangeTrackingTable = changeTrackingTable

	return c
}

// SetTrackChanges returns a new Config with the change tracking flag set.
func (c Config) SetTrackChanges(trackChanges bool) Config {
	c.TrackChanges = trackChanges

	return c
}

// Configurer is an interface for types that can provide a resource configuration.
type Configurer interface {
	Config() Config
}
