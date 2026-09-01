CREATE TABLE Requisitions (
  Id STRING(36) NOT NULL,
  WaystationId STRING(64) NOT NULL,
  RequestedBy STRING(320) NOT NULL,
  Justification STRING(MAX),
  NeededBy DATE NOT NULL,
  TotalCost NUMERIC NOT NULL,
  StatusId STRING(64) NOT NULL,

  CONSTRAINT CK_Requisitions_Id CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
  CONSTRAINT FK_Requisitions_WaystationId FOREIGN KEY (WaystationId) REFERENCES Waystations(Id),
  CONSTRAINT FK_Requisitions_StatusId FOREIGN KEY (StatusId) REFERENCES RequisitionStatuses(Id),
) PRIMARY KEY (Id);
