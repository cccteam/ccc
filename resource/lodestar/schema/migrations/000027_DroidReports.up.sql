CREATE TABLE DroidReports (
  Id STRING(36) NOT NULL,
  SectorId STRING(64) NOT NULL,
  ShipId STRING(36) NOT NULL,
  Subsystem STRING(MAX) NOT NULL,
  Reading FLOAT64 NOT NULL,
  RecordedAt TIMESTAMP NOT NULL,

  CONSTRAINT CK_DroidReports_Id CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
  CONSTRAINT FK_DroidReports_SectorId FOREIGN KEY (SectorId) REFERENCES Sectors(Id),
  CONSTRAINT FK_DroidReports_ShipId FOREIGN KEY (ShipId) REFERENCES Ships(Id),
) PRIMARY KEY (Id);
