CREATE TABLE Berths (
  Id STRING(36) NOT NULL,
  Designation STRING(MAX) NOT NULL,
  SizeClass INT64 NOT NULL,
  Occupied BOOL NOT NULL,

  CONSTRAINT CK_Berths_Id CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
) PRIMARY KEY (Id);

CREATE UNIQUE INDEX BerthsByDesignation ON Berths(Designation);
