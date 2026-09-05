CREATE TABLE Announcements (
  Id STRING(36) NOT NULL,
  TenantId STRING(64) NOT NULL,
  Title STRING(MAX) NOT NULL,
  Body STRING(MAX) NOT NULL,

  CONSTRAINT CK_Announcements_Id CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
  CONSTRAINT FK_Announcements_TenantId FOREIGN KEY (TenantId) REFERENCES Tenants(Id),
) PRIMARY KEY (Id);
