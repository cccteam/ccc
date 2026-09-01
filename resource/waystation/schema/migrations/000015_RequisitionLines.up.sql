CREATE TABLE RequisitionLines (
  Id STRING(36) NOT NULL,
  LineNumber INT64 NOT NULL,
  CatalogItemId STRING(36) NOT NULL,
  Quantity INT64 NOT NULL,
  UnitCostSnapshot NUMERIC NOT NULL,

  CONSTRAINT FK_RequisitionLines_Id FOREIGN KEY (Id) REFERENCES Requisitions(Id),
  CONSTRAINT FK_RequisitionLines_CatalogItemId FOREIGN KEY (CatalogItemId) REFERENCES CatalogItems(Id),
) PRIMARY KEY (Id, LineNumber),
  INTERLEAVE IN PARENT Requisitions ON DELETE CASCADE;
