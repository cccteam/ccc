CREATE TABLE Waystations (
  Id STRING(64) NOT NULL,
  Name STRING(MAX) NOT NULL,
  OrbitBand STRING(MAX) NOT NULL,
  Commissioned DATE NOT NULL,

  CONSTRAINT CK_Waystations_Id CHECK (REGEXP_CONTAINS(Id, r'^[a-z][a-z0-9-]{1,62}$')),
) PRIMARY KEY (Id);

CREATE UNIQUE INDEX WaystationsByName ON Waystations(Name);
