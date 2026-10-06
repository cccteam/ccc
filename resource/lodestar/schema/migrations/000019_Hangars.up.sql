CREATE TABLE Hangars (
  Id STRING(36) NOT NULL,
  SectorId STRING(64) NOT NULL,
  Name STRING(MAX) NOT NULL,
  Zone STRING(MAX) NOT NULL,

  CONSTRAINT CK_Hangars_Id CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
  CONSTRAINT FK_Hangars_SectorId FOREIGN KEY (SectorId) REFERENCES Sectors(Id),
) PRIMARY KEY (Id);
