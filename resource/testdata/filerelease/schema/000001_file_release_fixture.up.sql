-- The file release tests' fixture: a table whose rows own a stored object (a NOT NULL
-- key) and, optionally, a second one (a nullable key), and a table that references it,
-- so a delete the foreign key refuses can be tried. Names are the fixture's own.

CREATE TABLE FileRows (
  Id STRING(36) NOT NULL,
  Title STRING(MAX) NOT NULL,
  StoreKey STRING(36) NOT NULL,
  ThumbKey STRING(36),
) PRIMARY KEY (Id);

CREATE TABLE FileRefs (
  Id STRING(36) NOT NULL,
  FileRowId STRING(36) NOT NULL,
  CONSTRAINT FK_FileRefs_FileRows FOREIGN KEY (FileRowId) REFERENCES FileRows (Id),
) PRIMARY KEY (Id);
