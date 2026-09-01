CREATE TABLE Facilities (
  Id STRING(36) NOT NULL,
  ModuleId STRING(36) NOT NULL,
  Name STRING(MAX) NOT NULL,
  Kind STRING(MAX) NOT NULL,

  CONSTRAINT CK_Facilities_Id CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
  CONSTRAINT FK_Facilities_ModuleId FOREIGN KEY (ModuleId) REFERENCES Modules(Id),
) PRIMARY KEY (Id);
