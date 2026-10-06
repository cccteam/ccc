CREATE TABLE Squadrons (
  Id STRING(36) NOT NULL,
  WingId STRING(36) NOT NULL,
  Name STRING(MAX) NOT NULL,

  CONSTRAINT CK_Squadrons_Id CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
  CONSTRAINT FK_Squadrons_WingId FOREIGN KEY (WingId) REFERENCES Wings(Id),
) PRIMARY KEY (Id);

CREATE INDEX SquadronsByName ON Squadrons(Name);
