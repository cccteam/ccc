-- The declaration the tenant record carries about who may enter a station: the
-- directory-only flag on the record and the directories a station admits its people
-- through, one row per declaration, as an application's @directoryOnly column and
-- @tenantDirectory table are.

ALTER TABLE Stations ADD COLUMN DirectoryOnly BOOL NOT NULL DEFAULT (FALSE);

CREATE TABLE StationDirectories (
  Id STRING(36) NOT NULL,
  StationId STRING(64) NOT NULL,
  Method STRING(16) NOT NULL,
  Connection STRING(MAX) NOT NULL,

  CONSTRAINT FK_StationDirectories_StationId FOREIGN KEY (StationId) REFERENCES Stations(Id),
) PRIMARY KEY (Id);

CREATE UNIQUE INDEX StationDirectoriesByStationIdMethodConnection ON StationDirectories(StationId, Method, Connection);
