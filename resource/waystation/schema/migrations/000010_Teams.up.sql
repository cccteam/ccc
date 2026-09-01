CREATE TABLE Teams (
  Id STRING(36) NOT NULL,
  WaystationId STRING(64) NOT NULL,
  Name STRING(MAX) NOT NULL,
  Specialty STRING(MAX) NOT NULL,

  CONSTRAINT CK_Teams_Id CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
  CONSTRAINT FK_Teams_WaystationId FOREIGN KEY (WaystationId) REFERENCES Waystations(Id),
) PRIMARY KEY (Id);

CREATE INDEX TeamsByName ON Teams(Name);
