CREATE TABLE CargoManifests (
  ShipId STRING(36) NOT NULL,
  LineNumber INT64 NOT NULL,
  Details STRING(MAX) NOT NULL,
  Quantity INT64 NOT NULL,
  DeclaredValue INT64 NOT NULL,

  CONSTRAINT FK_CargoManifests_ShipId FOREIGN KEY (ShipId) REFERENCES Ships(Id),
) PRIMARY KEY (ShipId, LineNumber);
