CREATE TABLE Sectors (
  Id STRING(64) NOT NULL,
  Name STRING(MAX) NOT NULL,
  Region STRING(MAX) NOT NULL,
  Established DATE NOT NULL,
  -- The sector's people enter through its directories only: a password does not reach a
  -- sector whose flag is true. Half of what the sector declares about who may enter it;
  -- the other half is SectorDirectories.
  DirectoryOnly BOOL NOT NULL DEFAULT (FALSE),

  CONSTRAINT CK_Sectors_Id CHECK (REGEXP_CONTAINS(Id, r'^[a-z][a-z0-9-]{1,62}$')),
) PRIMARY KEY (Id);

CREATE UNIQUE INDEX SectorsByName ON Sectors(Name);

-- The directories a sector admits its people through, one row per declaration: the
-- sign-in method (google, azure, workos) and the connection the method's identities carry
-- (a Google Workspace by the name the registration gives it, a Microsoft Entra tenant by
-- its tenant id). One domain, one directory by default; a second directory is a second row.
CREATE TABLE SectorDirectories (
  Id STRING(36) NOT NULL,
  SectorId STRING(64) NOT NULL,
  Method STRING(16) NOT NULL,
  Connection STRING(MAX) NOT NULL,

  CONSTRAINT CK_SectorDirectories_Id CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
  CONSTRAINT CK_SectorDirectories_Method CHECK (Method IN ('google', 'azure', 'workos')),
  CONSTRAINT FK_SectorDirectories_SectorId FOREIGN KEY (SectorId) REFERENCES Sectors(Id),
) PRIMARY KEY (Id);

CREATE UNIQUE INDEX SectorDirectoriesBySectorIdMethodConnection ON SectorDirectories(SectorId, Method, Connection);
