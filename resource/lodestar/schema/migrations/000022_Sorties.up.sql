CREATE TABLE Sorties (
  Id STRING(36) NOT NULL,
  MissionId STRING(36) NOT NULL,
  ShipId STRING(36) NOT NULL,
  PilotUserId STRING(320) NOT NULL,
  LaunchedAt TIMESTAMP NOT NULL,
  ReturnedAt TIMESTAMP,
  Debrief STRING(MAX),

  CONSTRAINT CK_Sorties_Id CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
  CONSTRAINT FK_Sorties_MissionId FOREIGN KEY (MissionId) REFERENCES Missions(Id),
  CONSTRAINT FK_Sorties_ShipId FOREIGN KEY (ShipId) REFERENCES Ships(Id),
) PRIMARY KEY (Id);
