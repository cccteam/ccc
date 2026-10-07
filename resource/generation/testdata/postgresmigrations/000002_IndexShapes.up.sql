-- The PostgreSQL twin of ../migrations/000002_IndexShapes: the same tables and indexes, as
-- PostgreSQL spells them. A partial index (WHERE ... IS NOT NULL) is the counterpart of
-- Spanner's NULL_FILTERED one, INCLUDE of STORING, and no index backs a foreign key.
CREATE TABLE "Tenants" (
  "Id" VARCHAR(36) NOT NULL,
  PRIMARY KEY ("Id")
);

CREATE TABLE "Orders" (
  "Id" VARCHAR(36) NOT NULL,
  "TenantId" VARCHAR(36) NOT NULL,
  "PlacedAt" TIMESTAMPTZ NOT NULL,
  "Reference" TEXT NOT NULL,
  "ExternalRef" TEXT,
  "Note" TEXT,
  PRIMARY KEY ("Id"),
  CONSTRAINT "FK_Orders_TenantId" FOREIGN KEY ("TenantId") REFERENCES "Tenants" ("Id")
);

CREATE INDEX "OrdersByTenantIdPlacedAt" ON "Orders" ("TenantId", "PlacedAt" DESC) INCLUDE ("Note");
CREATE UNIQUE INDEX "OrdersByReference" ON "Orders" ("Reference");
CREATE UNIQUE INDEX "OrdersByExternalRef" ON "Orders" ("ExternalRef") WHERE "ExternalRef" IS NOT NULL;
CREATE INDEX "OrdersByNote" ON "Orders" ("Note") WHERE "Note" IS NOT NULL;

CREATE TABLE "Seats" (
  "OrderId" VARCHAR(36) NOT NULL,
  "UserId" VARCHAR(36) NOT NULL,
  "Row" BIGINT NOT NULL,
  "Note" TEXT,
  PRIMARY KEY ("OrderId", "UserId"),
  CONSTRAINT "FK_Seats_OrderId" FOREIGN KEY ("OrderId") REFERENCES "Orders" ("Id")
);

CREATE UNIQUE INDEX "SeatsByOrderIdRow" ON "Seats" ("OrderId", "Row");
CREATE INDEX "SeatsByNote" ON "Seats" ("Note");
