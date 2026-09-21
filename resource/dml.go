package resource

import (
	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
)

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

	// capabilityPlan carries the statement's capability evaluation (§13): how
	// each row's reserved capability property assembles from the scanned
	// group booleans. Nil unless the request opted in.
	capabilityPlan *capabilityPlan

	// maskedNamesColumn names the reserved masked-cell-names output column when
	// the statement renders cell masking; empty otherwise. The readers scan it
	// into the Row envelope instead of the destination struct.
	maskedNamesColumn string

	// cursorColumns are the sort keys the statement selects a second time
	// under reserved aliases for the cursor (see cursorColumnItems); empty
	// otherwise. The readers scan them into the Row envelope.
	cursorColumns []cursorColumn

	SQL    string
	Params map[string]any
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
	// FileKeys are the resource's fields holding a stored file's key, one per @file
	// declaration: the columns whose old values the patch machinery records as
	// released when a row is deleted or pointed at another object, so the
	// transaction's executor deletes the objects after the commit. The generated
	// DefaultConfig sets them from the struct's annotations; an application's own
	// Config that leaves them empty inherits them, since which columns hold file keys
	// is the schema's fact, never the application's choice.
	FileKeys []accesstypes.Field
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

// SetFileKeys returns a new Config naming the fields that hold a stored file's key.
func (c Config) SetFileKeys(fields ...accesstypes.Field) Config {
	c.FileKeys = fields

	return c
}
