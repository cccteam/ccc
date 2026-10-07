-- The semantic differential's fixture world for PostgreSQL: the tables of
-- ../schema, with every text column ordered byte by byte (COLLATE "C"), as Spanner
-- orders STRING. The condition language compares strings by code point, so a
-- column in a locale collation ("closed" before "Open") would answer a comparison
-- or an order differently from the language's meaning. Names are the fixture's own;
-- nothing here is an application's vocabulary.

CREATE TABLE "Hubs" (
  "Id" TEXT COLLATE "C" NOT NULL,
  "Region" TEXT COLLATE "C",
  PRIMARY KEY ("Id")
);

CREATE TABLE "Carriers" (
  "Id" TEXT COLLATE "C" NOT NULL,
  "Code" TEXT COLLATE "C",
  PRIMARY KEY ("Id")
);

CREATE TABLE "Routes" (
  "Id" TEXT COLLATE "C" NOT NULL,
  "HubId" TEXT COLLATE "C",
  PRIMARY KEY ("Id"),
  CONSTRAINT "FK_Routes_Hub" FOREIGN KEY ("HubId") REFERENCES "Hubs" ("Id")
);

CREATE TABLE "Parcels" (
  "Id" TEXT COLLATE "C" NOT NULL,
  "Depot" TEXT COLLATE "C" NOT NULL,
  "Label" TEXT COLLATE "C" NOT NULL,
  "Note" TEXT COLLATE "C",
  "Weight" BIGINT NOT NULL,
  "Pieces" BIGINT,
  "Ratio" DOUBLE PRECISION NOT NULL,
  "Density" DOUBLE PRECISION,
  "Price" NUMERIC NOT NULL,
  "Fee" NUMERIC,
  "Fragile" BOOLEAN NOT NULL,
  "Insured" BOOLEAN,
  "ShippedAt" TIMESTAMPTZ NOT NULL,
  "DeliveredAt" TIMESTAMPTZ,
  "ShipDate" DATE NOT NULL,
  "DueDate" DATE,
  "CarrierId" TEXT COLLATE "C",
  "RouteId" TEXT COLLATE "C",
  PRIMARY KEY ("Id"),
  CONSTRAINT "FK_Parcels_Carrier" FOREIGN KEY ("CarrierId") REFERENCES "Carriers" ("Id"),
  CONSTRAINT "FK_Parcels_Route" FOREIGN KEY ("RouteId") REFERENCES "Routes" ("Id")
);

CREATE INDEX "ParcelsByDepot" ON "Parcels" ("Depot");

CREATE TABLE "Memberships" (
  "Id" TEXT COLLATE "C" NOT NULL,
  "UserId" TEXT COLLATE "C" NOT NULL,
  "Depot" TEXT COLLATE "C" NOT NULL,
  "Team" TEXT COLLATE "C",
  "Tier" BIGINT,
  "HubId" TEXT COLLATE "C",
  PRIMARY KEY ("Id"),
  CONSTRAINT "FK_Memberships_Hub" FOREIGN KEY ("HubId") REFERENCES "Hubs" ("Id")
);

CREATE INDEX "MembershipsByUser" ON "Memberships" ("UserId", "Depot");

CREATE TABLE "Profiles" (
  "UserId" TEXT COLLATE "C" NOT NULL,
  "Nickname" TEXT COLLATE "C",
  "Quota" BIGINT,
  "Rate" DOUBLE PRECISION,
  "Allowance" NUMERIC,
  "Active" BOOLEAN,
  "ClearedUntil" TIMESTAMPTZ,
  "StartDate" DATE,
  "HomeHubId" TEXT COLLATE "C",
  PRIMARY KEY ("UserId"),
  CONSTRAINT "FK_Profiles_Hub" FOREIGN KEY ("HomeHubId") REFERENCES "Hubs" ("Id")
);
