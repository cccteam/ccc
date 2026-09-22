CREATE TABLE Refits (
  Id STRING(36) NOT NULL,
  ShipId STRING(36) NOT NULL,
  StatusId STRING(64) NOT NULL,
  Estimate NUMERIC,
  InspectedAt TIMESTAMP OPTIONS (allow_commit_timestamp = true),
  OpenedBy STRING(320) NOT NULL,
  Notes STRING(MAX),

  CONSTRAINT CK_Refits_Id CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
  CONSTRAINT FK_Refits_ShipId FOREIGN KEY (ShipId) REFERENCES Ships(Id),
  CONSTRAINT FK_Refits_StatusId FOREIGN KEY (StatusId) REFERENCES RefitStatuses(Id),
) PRIMARY KEY (Id);
