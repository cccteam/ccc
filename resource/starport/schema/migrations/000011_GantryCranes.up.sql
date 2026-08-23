CREATE TABLE GantryCranes (
  Id STRING(36) NOT NULL,
  Callsign STRING(MAX) NOT NULL,
  LiftTonnage INT64 NOT NULL,
  Operational BOOL NOT NULL,

  CONSTRAINT CK_GantryCranes_Id CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
) PRIMARY KEY (Id);

CREATE UNIQUE INDEX GantryCranesByCallsign ON GantryCranes(Callsign);
