CREATE TABLE WorkOrders (
  Id STRING(36) NOT NULL,
  WaystationId STRING(64) NOT NULL,
  AssetId STRING(36) NOT NULL,
  Title STRING(MAX) NOT NULL,
  Summary STRING(MAX),
  Priority INT64 NOT NULL,
  StatusId STRING(64) NOT NULL,
  CreatedBy STRING(320) NOT NULL,
  AssignedTeamId STRING(36),
  DueAt TIMESTAMP,
  UpdatedAt TIMESTAMP OPTIONS (allow_commit_timestamp = true),

  CONSTRAINT CK_WorkOrders_Id CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
  CONSTRAINT FK_WorkOrders_WaystationId FOREIGN KEY (WaystationId) REFERENCES Waystations(Id),
  CONSTRAINT FK_WorkOrders_AssetId FOREIGN KEY (AssetId) REFERENCES Assets(Id),
  CONSTRAINT FK_WorkOrders_StatusId FOREIGN KEY (StatusId) REFERENCES WorkOrderStatuses(Id),
  CONSTRAINT FK_WorkOrders_AssignedTeamId FOREIGN KEY (AssignedTeamId) REFERENCES Teams(Id),
) PRIMARY KEY (Id);
