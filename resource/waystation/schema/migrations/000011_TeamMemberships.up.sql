CREATE TABLE TeamMemberships (
  TeamId STRING(36) NOT NULL,
  UserId STRING(320) NOT NULL,

  CONSTRAINT FK_TeamMemberships_TeamId FOREIGN KEY (TeamId) REFERENCES Teams(Id),
) PRIMARY KEY (TeamId, UserId);

CREATE INDEX TeamMembershipsByUserId ON TeamMemberships(UserId);
