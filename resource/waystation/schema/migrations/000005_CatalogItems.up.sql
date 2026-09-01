CREATE TABLE CatalogItems (
  Id STRING(36) NOT NULL,
  Sku STRING(MAX) NOT NULL,
  Name STRING(MAX) NOT NULL,
  CategoryId STRING(64) NOT NULL,
  UnitCost NUMERIC NOT NULL,
  Hazardous BOOL NOT NULL,

  CONSTRAINT CK_CatalogItems_Id CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
  CONSTRAINT FK_CatalogItems_CategoryId FOREIGN KEY (CategoryId) REFERENCES ItemCategories(Id),
) PRIMARY KEY (Id);

CREATE UNIQUE INDEX CatalogItemsBySku ON CatalogItems(Sku);
