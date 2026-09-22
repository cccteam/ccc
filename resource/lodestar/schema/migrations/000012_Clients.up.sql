CREATE TABLE Clients (
  Id STRING(36) NOT NULL,
  Name STRING(MAX) NOT NULL,
  ContactName STRING(MAX) NOT NULL,
  ContactEmail STRING(MAX) NOT NULL,
  Trusted BOOL NOT NULL,

  CONSTRAINT CK_Clients_Id CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
) PRIMARY KEY (Id);

CREATE UNIQUE INDEX ClientsByName ON Clients(Name);
