CREATE TABLE Consignments (
  Id STRING(36) NOT NULL,
  SectorId STRING(64) NOT NULL,
  ClientId STRING(36) NOT NULL,
  BondCode STRING(MAX) NOT NULL,
  Description STRING(MAX) NOT NULL,
  Mass FLOAT64 NOT NULL,
  ExpiresOn DATE NOT NULL,
  ReleasedAt TIMESTAMP,

  CONSTRAINT CK_Consignments_Id CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
  CONSTRAINT FK_Consignments_SectorId FOREIGN KEY (SectorId) REFERENCES Sectors(Id),
  CONSTRAINT FK_Consignments_ClientId FOREIGN KEY (ClientId) REFERENCES Clients(Id),
) PRIMARY KEY (Id);

CREATE UNIQUE INDEX ConsignmentsByBondCode ON Consignments(BondCode);
