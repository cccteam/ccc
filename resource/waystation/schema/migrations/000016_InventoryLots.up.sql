CREATE TABLE InventoryLots (
  Id STRING(36) NOT NULL,
  WaystationId STRING(64) NOT NULL,
  CatalogItemId STRING(36) NOT NULL,
  Quantity INT64 NOT NULL,
  ExpiresOn DATE,
  BinLocation STRING(MAX) NOT NULL,

  CONSTRAINT CK_InventoryLots_Id CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
  CONSTRAINT FK_InventoryLots_WaystationId FOREIGN KEY (WaystationId) REFERENCES Waystations(Id),
  CONSTRAINT FK_InventoryLots_CatalogItemId FOREIGN KEY (CatalogItemId) REFERENCES CatalogItems(Id),
) PRIMARY KEY (Id);
