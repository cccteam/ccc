CREATE TABLE "DataChangeEvents" (
  "TableName" TEXT NOT NULL,
  "RowId" TEXT NOT NULL,
  "Sequence" BIGINT NOT NULL,
  "EventTime" TIMESTAMPTZ NOT NULL,
  "EventSource" TEXT NOT NULL,
  "ChangeSet" JSONB,
  PRIMARY KEY ("TableName", "RowId", "Sequence", "EventTime")
);
