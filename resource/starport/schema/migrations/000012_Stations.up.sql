CREATE TABLE Stations (
  Id STRING(36) NOT NULL,
  Name STRING(MAX) NOT NULL,

  CONSTRAINT CK_Stations_Id CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
) PRIMARY KEY (Id);

CREATE UNIQUE INDEX StationsByName ON Stations(Name);
