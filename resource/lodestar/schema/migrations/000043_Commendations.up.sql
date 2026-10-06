-- Commendations is the commendations desk: a citation on a pilot's record for a sortie
-- flown well, filed by headquarters. It is the one resource behind a feature flag
-- (pkg/resources/features.go, Commendations): the table and the grants ship with the
-- release whether or not the flag is on, and the flag decides whether the desk exists
-- for the API and the browser. AwardedAt is the commit timestamp of the create. PilotId
-- leads an index with the award time, so one pilot's citations list in award order off
-- the index.
CREATE TABLE Commendations (
  Id STRING(36) NOT NULL,
  PilotId STRING(36) NOT NULL,
  Citation STRING(MAX) NOT NULL,
  AwardedBy STRING(320) NOT NULL,
  AwardedAt TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),

  CONSTRAINT CK_Commendations_Id CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
  CONSTRAINT FK_Commendations_PilotId FOREIGN KEY (PilotId) REFERENCES Pilots(Id),
) PRIMARY KEY (Id);

CREATE INDEX CommendationsByPilotIdAwardedAt ON Commendations(PilotId, AwardedAt DESC);
