-- The semantic differential's fixture world: a checked resource with every
-- comparison type in a NOT NULL and a nullable column, two join paths (one
-- hop, and one hop whose table carries a second hop), a partitioned anchor
-- for subject sets, and a global anchor for subject values. Names are the
-- fixture's own; nothing here is an application's vocabulary.

CREATE TABLE Hubs (
  Id STRING(36) NOT NULL,
  Region STRING(MAX),
) PRIMARY KEY (Id);

CREATE TABLE Carriers (
  Id STRING(36) NOT NULL,
  Code STRING(MAX),
) PRIMARY KEY (Id);

CREATE TABLE Routes (
  Id STRING(36) NOT NULL,
  HubId STRING(36),
  CONSTRAINT FK_Routes_Hub FOREIGN KEY (HubId) REFERENCES Hubs (Id),
) PRIMARY KEY (Id);

CREATE TABLE Parcels (
  Id STRING(36) NOT NULL,
  Depot STRING(MAX) NOT NULL,
  Label STRING(MAX) NOT NULL,
  Note STRING(MAX),
  Weight INT64 NOT NULL,
  Pieces INT64,
  Ratio FLOAT64 NOT NULL,
  Density FLOAT64,
  Price NUMERIC NOT NULL,
  Fee NUMERIC,
  Fragile BOOL NOT NULL,
  Insured BOOL,
  ShippedAt TIMESTAMP NOT NULL,
  DeliveredAt TIMESTAMP,
  ShipDate DATE NOT NULL,
  DueDate DATE,
  CarrierId STRING(36),
  RouteId STRING(36),
  CONSTRAINT FK_Parcels_Carrier FOREIGN KEY (CarrierId) REFERENCES Carriers (Id),
  CONSTRAINT FK_Parcels_Route FOREIGN KEY (RouteId) REFERENCES Routes (Id),
) PRIMARY KEY (Id);

CREATE INDEX ParcelsByDepot ON Parcels(Depot);

CREATE TABLE Memberships (
  Id STRING(36) NOT NULL,
  UserId STRING(MAX) NOT NULL,
  Depot STRING(MAX) NOT NULL,
  Team STRING(MAX),
  Tier INT64,
  HubId STRING(36),
  CONSTRAINT FK_Memberships_Hub FOREIGN KEY (HubId) REFERENCES Hubs (Id),
) PRIMARY KEY (Id);

CREATE INDEX MembershipsByUser ON Memberships(UserId, Depot);

CREATE TABLE Profiles (
  UserId STRING(MAX) NOT NULL,
  Nickname STRING(MAX),
  Quota INT64,
  Rate FLOAT64,
  Allowance NUMERIC,
  Active BOOL,
  ClearedUntil TIMESTAMP,
  StartDate DATE,
  HomeHubId STRING(36),
  CONSTRAINT FK_Profiles_Hub FOREIGN KEY (HomeHubId) REFERENCES Hubs (Id),
) PRIMARY KEY (UserId);
