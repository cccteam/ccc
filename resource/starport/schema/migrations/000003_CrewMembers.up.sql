CREATE TABLE CrewMembers (
  Id STRING(36) NOT NULL,
  ShipId STRING(36) NOT NULL,
  Name STRING(MAX) NOT NULL,
  Rank STRING(MAX) NOT NULL,
  ClearanceLevel INT64 NOT NULL,
  MedicalNotes STRING(MAX),

  CONSTRAINT CK_CrewMembers_Id CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
  CONSTRAINT FK_CrewMembers_ShipId FOREIGN KEY (ShipId) REFERENCES Ships(Id),
) PRIMARY KEY (Id);

CREATE INDEX CrewMembersByShipId ON CrewMembers(ShipId);
