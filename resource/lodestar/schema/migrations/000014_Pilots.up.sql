CREATE TABLE Pilots (
  Id STRING(36) NOT NULL,
  UserId STRING(320) NOT NULL,
  DisplayName STRING(MAX) NOT NULL,
  Clearance INT64 NOT NULL,
  FeeLimit NUMERIC NOT NULL,

  CONSTRAINT CK_Pilots_Id CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
) PRIMARY KEY (Id);

CREATE UNIQUE INDEX PilotsByUserId ON Pilots(UserId);
