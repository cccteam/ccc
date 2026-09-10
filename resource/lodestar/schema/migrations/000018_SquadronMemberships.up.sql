CREATE TABLE SquadronMemberships (
  SquadronId STRING(36) NOT NULL,
  UserId STRING(320) NOT NULL,

  CONSTRAINT FK_SquadronMemberships_SquadronId FOREIGN KEY (SquadronId) REFERENCES Squadrons(Id),
) PRIMARY KEY (SquadronId, UserId);

CREATE INDEX SquadronMembershipsByUserId ON SquadronMemberships(UserId);
