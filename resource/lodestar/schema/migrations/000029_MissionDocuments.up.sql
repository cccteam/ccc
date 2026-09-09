CREATE TABLE MissionDocuments (
  Id STRING(36) NOT NULL,
  MissionId STRING(36) NOT NULL,
  Title STRING(MAX) NOT NULL,
  FileName STRING(MAX) NOT NULL,
  ContentType STRING(MAX) NOT NULL,
  Size INT64 NOT NULL,
  StoreKey STRING(36) NOT NULL,
  UploadedBy STRING(MAX) NOT NULL,
  UploadedAt TIMESTAMP NOT NULL,

  CONSTRAINT CK_MissionDocuments_Id CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
  CONSTRAINT FK_MissionDocuments_MissionId FOREIGN KEY (MissionId) REFERENCES Missions(Id),
) PRIMARY KEY (Id);

CREATE INDEX MissionDocumentsByMissionId ON MissionDocuments(MissionId);
