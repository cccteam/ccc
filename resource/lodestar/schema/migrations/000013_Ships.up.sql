CREATE TABLE Ships (
  Id STRING(36) NOT NULL,
  HangarId STRING(36) NOT NULL,
  ClassId STRING(36) NOT NULL,
  Registry STRING(MAX) NOT NULL,
  Name STRING(MAX) NOT NULL,
  LastRefitAt TIMESTAMP OPTIONS (allow_commit_timestamp = true),
  UpdatedAt TIMESTAMP OPTIONS (allow_commit_timestamp = true),

  CONSTRAINT CK_Ships_Id CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
  CONSTRAINT FK_Ships_HangarId FOREIGN KEY (HangarId) REFERENCES Hangars(Id),
  CONSTRAINT FK_Ships_ClassId FOREIGN KEY (ClassId) REFERENCES ShipClasses(Id),
) PRIMARY KEY (Id);

CREATE UNIQUE INDEX ShipsByRegistry ON Ships(Registry);
