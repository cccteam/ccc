CREATE TABLE SupplyCrates (
  Id STRING(36) NOT NULL,
  Label STRING(MAX) NOT NULL,
  Quantity INT64 NOT NULL,
  Priority INT64 NOT NULL,
  Status STRING(MAX) NOT NULL,
  Barcode STRING(MAX) NOT NULL,
  Notes STRING(MAX),
  InspectorBadge STRING(MAX),
  AssignedShipId STRING(36),

  CONSTRAINT CK_SupplyCrates_Id CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
  CONSTRAINT FK_SupplyCrates_AssignedShipId FOREIGN KEY (AssignedShipId) REFERENCES Ships(Id),
) PRIMARY KEY (Id);

CREATE INDEX SupplyCratesByLabel ON SupplyCrates(Label);
