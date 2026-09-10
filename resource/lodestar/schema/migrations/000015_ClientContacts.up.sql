CREATE TABLE ClientContacts (
  Id STRING(36) NOT NULL,
  UserId STRING(320) NOT NULL,
  ClientId STRING(36) NOT NULL,
  DisplayName STRING(MAX) NOT NULL,

  CONSTRAINT CK_ClientContacts_Id CHECK (REGEXP_CONTAINS(Id, r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')),
  CONSTRAINT FK_ClientContacts_ClientId FOREIGN KEY (ClientId) REFERENCES Clients(Id),
) PRIMARY KEY (Id);

CREATE UNIQUE INDEX ClientContactsByUserId ON ClientContacts(UserId);
