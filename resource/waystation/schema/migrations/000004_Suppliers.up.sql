CREATE TABLE Suppliers (
  Id STRING(36) NOT NULL,
  Name STRING(MAX) NOT NULL,
  ContactName STRING(MAX) NOT NULL,
  ContactEmail STRING(MAX) NOT NULL,
  Active BOOL NOT NULL,

  CONSTRAINT CK_Suppliers_Id CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
) PRIMARY KEY (Id);

CREATE UNIQUE INDEX SuppliersByName ON Suppliers(Name);
