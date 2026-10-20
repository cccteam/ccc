CREATE TABLE DataChangeEvents (
  TableName STRING(MAX) NOT NULL,
  RowId STRING(MAX) NOT NULL,
  Sequence INT64 NOT NULL,
  EventTime TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp=true),
  EventSource STRING(MAX) NOT NULL,
  ChangeSet JSON,
) PRIMARY KEY (TableName, RowId, Sequence, EventTime);

-- The pipeline's migration guard reads this database's migration version through the
-- migrations_reader database role, which holds select on the migrations table alone,
-- and the environment layer grants the test environment's deploy identity the role.
-- Created once, by this migration, after the runner has made the table. The emulator
-- accepts the statements and keeps no role.
CREATE ROLE migrations_reader;
GRANT SELECT ON TABLE SchemaMigrations TO ROLE migrations_reader;
