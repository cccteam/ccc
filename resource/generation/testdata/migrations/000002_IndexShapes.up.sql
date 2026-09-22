CREATE TABLE Tenants (
  Id STRING(36) NOT NULL,
) PRIMARY KEY (Id);

CREATE TABLE Orders (
  Id STRING(36) NOT NULL,
  TenantId STRING(36) NOT NULL,
  PlacedAt TIMESTAMP NOT NULL,
  Reference STRING(MAX) NOT NULL,
  ExternalRef STRING(MAX),
  Note STRING(MAX),

  CONSTRAINT FK_Orders_TenantId FOREIGN KEY (TenantId) REFERENCES Tenants(Id),
) PRIMARY KEY (Id);

CREATE INDEX OrdersByTenantIdPlacedAt ON Orders(TenantId, PlacedAt DESC) STORING (Note);
CREATE UNIQUE INDEX OrdersByReference ON Orders(Reference);
CREATE UNIQUE NULL_FILTERED INDEX OrdersByExternalRef ON Orders(ExternalRef);
CREATE NULL_FILTERED INDEX OrdersByNote ON Orders(Note);

CREATE TABLE Seats (
  OrderId STRING(36) NOT NULL,
  UserId STRING(36) NOT NULL,
  Row INT64 NOT NULL,
  Note STRING(MAX),

  CONSTRAINT FK_Seats_OrderId FOREIGN KEY (OrderId) REFERENCES Orders(Id),
) PRIMARY KEY (OrderId, UserId);

CREATE UNIQUE INDEX SeatsByOrderIdRow ON Seats(OrderId, Row);
CREATE INDEX SeatsByNote ON Seats(Note);
