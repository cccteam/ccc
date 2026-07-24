CREATE TABLE DockingBays (
  Id STRING(36) NOT NULL,
  Name STRING(MAX) NOT NULL,
  DeckLevel INT64 NOT NULL,
  MaxTonnage INT64 NOT NULL,

  CONSTRAINT CK_DockingBays_Id CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
) PRIMARY KEY (Id);

CREATE UNIQUE INDEX DockingBaysByName ON DockingBays(Name);
