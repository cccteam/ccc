CREATE TABLE StaffMembers (
  Id STRING(36) NOT NULL,
  UserId STRING(320) NOT NULL,
  DisplayName STRING(MAX) NOT NULL,
  ApprovalLimit NUMERIC NOT NULL,
  HomeWaystationId STRING(64),

  CONSTRAINT CK_StaffMembers_Id CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
  CONSTRAINT FK_StaffMembers_HomeWaystationId FOREIGN KEY (HomeWaystationId) REFERENCES Waystations(Id),
) PRIMARY KEY (Id);

CREATE UNIQUE INDEX StaffMembersByUserId ON StaffMembers(UserId);
