CREATE TABLE DistressCalls (
  Id STRING(36) NOT NULL,
  SectorId STRING(64) NOT NULL,
  Summary STRING(MAX) NOT NULL,
  Severity INT64 NOT NULL,
  CallerContact STRING(MAX),
  Transcript STRING(MAX),
  CaseNumber STRING(MAX) NOT NULL,
  FiledBy STRING(320) NOT NULL,

  CONSTRAINT CK_DistressCalls_Id CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
  CONSTRAINT FK_DistressCalls_SectorId FOREIGN KEY (SectorId) REFERENCES Sectors(Id),
) PRIMARY KEY (Id);

CREATE UNIQUE INDEX DistressCallsByCaseNumber ON DistressCalls(CaseNumber);
