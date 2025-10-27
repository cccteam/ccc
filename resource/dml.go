package resource

import "cloud.google.com/go/spanner"

// DBType represents the type of database, such as Spanner or PostgreSQL.
type DBType string

const (
	// SpannerDBType represents the Google Cloud Spanner database.
	SpannerDBType DBType = "spanner"

	// PostgresDBType represents the PostgreSQL database.
	PostgresDBType DBType = "postgres"

	// MockDBType represents a database type for mocking
	MockDBType DBType = "mock"
)

// dbTypes should return DBType constants for all supported databases
func dbTypes() []DBType {
	return []DBType{SpannerDBType, PostgresDBType}
}

// Statement is a generic container for a SQL statement and its parameters,
// supporting both Spanner and PostgreSQL.
type Statement struct {
	// resolvedWhereClause is used to carry contextual information for error messages
	// and is not used in the query.
	resolvedWhereClause string
	SQL                 string
	Params              map[string]any
}

// SpannerStatement converts the generic Statement into a Spanner-specific Statement.
func (s *Statement) SpannerStatement() spanner.Statement {
	return spanner.Statement{
		SQL:    s.SQL,
		Params: s.Params,
	}
}

// Columns is a string representing a comma-separated list of database column names.
type Columns string

// Config holds database-specific configuration for a resource.
type Config struct {
	ChangeTrackingTable string
	TrackChanges        bool
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
