CREATE TABLE Shipments (
  Id STRING(36) NOT NULL,
  WaystationId STRING(64) NOT NULL,
  SupplierId STRING(36) NOT NULL,
  ManifestCode STRING(MAX) NOT NULL,
  ArrivedAt TIMESTAMP,

  CONSTRAINT CK_Shipments_Id CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
  CONSTRAINT FK_Shipments_WaystationId FOREIGN KEY (WaystationId) REFERENCES Waystations(Id),
  CONSTRAINT FK_Shipments_SupplierId FOREIGN KEY (SupplierId) REFERENCES Suppliers(Id),
) PRIMARY KEY (Id);

CREATE UNIQUE INDEX ShipmentsByManifestCode ON Shipments(ManifestCode);
