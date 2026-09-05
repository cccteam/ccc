CREATE TABLE Readings (
  Id STRING(36) NOT NULL,
  TenantId STRING(64) NOT NULL,
  Source STRING(MAX) NOT NULL,
  Value FLOAT64 NOT NULL,
  RecordedAt TIMESTAMP NOT NULL,

  CONSTRAINT CK_Readings_Id CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
  CONSTRAINT FK_Readings_TenantId FOREIGN KEY (TenantId) REFERENCES Tenants(Id),
) PRIMARY KEY (Id);
