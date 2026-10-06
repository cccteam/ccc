// Package resource stands in for the resource module in the module cache: the check
// reads FeatureFlagsDDL from its source, so this fixture carries the function in the
// library's shape and nothing else.
package resource

// DBType is the database kind.
type DBType string

// The database kinds.
const (
	SpannerDBType  DBType = "spanner"
	PostgresDBType DBType = "postgres"
)

// FeatureFlagsDDL is the fixture's copy of the library's statements.
func FeatureFlagsDDL(dbType DBType) []string {
	switch dbType {
	case SpannerDBType:
		return []string{
			`CREATE TABLE FeatureFlags (
  Name STRING(64) NOT NULL,
  Description STRING(MAX) NOT NULL,
  Enabled BOOL NOT NULL,
  UpdatedAt TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
  UpdatedBy STRING(MAX) NOT NULL,
) PRIMARY KEY (Name)`,
			`CREATE TABLE FeatureFlagChanges (
  Name STRING(64) NOT NULL,
  ChangedAt TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
  Enabled BOOL NOT NULL,
  ChangedBy STRING(MAX) NOT NULL,
) PRIMARY KEY (Name, ChangedAt)`,
		}
	case PostgresDBType:
		return []string{`CREATE TABLE "FeatureFlags" ("Name" VARCHAR(64) NOT NULL, PRIMARY KEY ("Name"))`}
	default:
		return nil
	}
}
