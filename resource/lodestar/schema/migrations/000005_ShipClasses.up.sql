CREATE TABLE ShipClasses (
  Id STRING(36) NOT NULL,
  Designation STRING(MAX) NOT NULL,
  RoleId STRING(64) NOT NULL,
  Tonnage INT64 NOT NULL,
  Hardened BOOL NOT NULL,

  CONSTRAINT CK_ShipClasses_Id CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
  CONSTRAINT FK_ShipClasses_RoleId FOREIGN KEY (RoleId) REFERENCES ShipRoles(Id),
) PRIMARY KEY (Id);

CREATE UNIQUE INDEX ShipClassesByDesignation ON ShipClasses(Designation);
