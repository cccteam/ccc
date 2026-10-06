-- The tenant roster tests' fixture: a tenant record's table, keyed by the slug every
-- tenant-scoped URL carries, as an application's @tenant table is.

CREATE TABLE Stations (
  Id STRING(64) NOT NULL,
  Name STRING(MAX) NOT NULL,
) PRIMARY KEY (Id);
