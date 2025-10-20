CREATE TABLE DataChangeEvents (
  TableName STRING(MAX) NOT NULL,
  RowId STRING(MAX) NOT NULL,
  Sequence INTEGER NOT NULL,
  EventTime TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp=true),
  EventSource STRING(MAX) NOT NULL,
  ChangeSet JSON,
) PRIMARY KEY (TableName, RowId, Sequence, EventTime);
