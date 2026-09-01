CREATE TABLE Modules (
  Id STRING(36) NOT NULL,
  WaystationId STRING(64) NOT NULL,
  Name STRING(MAX) NOT NULL,
  Zone STRING(MAX) NOT NULL,
  PressureRated BOOL NOT NULL,

  CONSTRAINT CK_Modules_Id CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
  CONSTRAINT FK_Modules_WaystationId FOREIGN KEY (WaystationId) REFERENCES Waystations(Id),
) PRIMARY KEY (Id);
