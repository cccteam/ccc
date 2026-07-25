CREATE TABLE Ships (
  Id STRING(36) NOT NULL,
  RegistryCode STRING(MAX) NOT NULL,
  Name STRING(MAX) NOT NULL,
  DockingBayId STRING(36),
  CargoValue INT64 NOT NULL,
  UpdatedAt TIMESTAMP OPTIONS (allow_commit_timestamp=true),

  CONSTRAINT CK_Ships_Id CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
  CONSTRAINT FK_Ships_DockingBayId FOREIGN KEY (DockingBayId) REFERENCES DockingBays(Id),
) PRIMARY KEY (Id);

CREATE UNIQUE INDEX ShipsByRegistryCode ON Ships(RegistryCode);

CREATE INDEX ShipsByName ON Ships(Name);
