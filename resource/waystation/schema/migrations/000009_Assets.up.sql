CREATE TABLE Assets (
  Id STRING(36) NOT NULL,
  FacilityId STRING(36) NOT NULL,
  SerialNumber STRING(MAX) NOT NULL,
  Name STRING(MAX) NOT NULL,
  CommissionedOn DATE NOT NULL,
  LastServicedAt TIMESTAMP OPTIONS (allow_commit_timestamp=true),

  CONSTRAINT CK_Assets_Id CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
  CONSTRAINT FK_Assets_FacilityId FOREIGN KEY (FacilityId) REFERENCES Facilities(Id),
) PRIMARY KEY (Id);

CREATE UNIQUE INDEX AssetsBySerialNumber ON Assets(SerialNumber);
