-- A child that PostgreSQL has no interleave for: its key leads with the parent's, and the
-- foreign key to it cascades or does not.
CREATE TABLE "OrderLines" (
  "Id" VARCHAR(36) NOT NULL,
  "LineNumber" BIGINT NOT NULL,
  "StoreKey" VARCHAR(36),
  PRIMARY KEY ("Id", "LineNumber"),
  CONSTRAINT "FK_OrderLines_Id" FOREIGN KEY ("Id") REFERENCES "Orders" ("Id") ON DELETE CASCADE
);

CREATE TABLE "OrderNotes" (
  "Id" VARCHAR(36) NOT NULL,
  "NoteNumber" BIGINT NOT NULL,
  "Body" TEXT NOT NULL,
  PRIMARY KEY ("Id", "NoteNumber"),
  CONSTRAINT "FK_OrderNotes_Id" FOREIGN KEY ("Id") REFERENCES "Orders" ("Id")
);

CREATE TABLE "Attachments" (
  "Id" VARCHAR(36) NOT NULL,
  "OrderId" VARCHAR(36) NOT NULL,
  "TenantId" VARCHAR(36) NOT NULL,
  "StoreKey" VARCHAR(36),
  PRIMARY KEY ("Id"),
  CONSTRAINT "FK_Attachments_OrderId" FOREIGN KEY ("OrderId") REFERENCES "Orders" ("Id") ON DELETE CASCADE,
  CONSTRAINT "FK_Attachments_TenantId" FOREIGN KEY ("TenantId") REFERENCES "Tenants" ("Id")
);

-- A table of every column type the schema read maps, and a two-hop foreign key.
CREATE TABLE "Shapes" (
  "Id" UUID NOT NULL,
  "Label" VARCHAR(64) NOT NULL,
  "Fixed" CHAR(4),
  "Body" TEXT,
  "Small" SMALLINT,
  "Count" INTEGER,
  "Big" BIGINT,
  "Ratio32" REAL,
  "Ratio64" DOUBLE PRECISION,
  "Price" NUMERIC(12, 2),
  "Flag" BOOLEAN,
  "At" TIMESTAMPTZ,
  "AtLocal" TIMESTAMP,
  "Day" DATE,
  "Raw" BYTEA,
  "Doc" JSONB,
  "Names" TEXT[],
  "Codes" VARCHAR(8)[],
  "Search" TSVECTOR,
  "Derived" BIGINT GENERATED ALWAYS AS ("Count" * 2) STORED,
  "Serial" BIGINT GENERATED ALWAYS AS IDENTITY,
  "Defaulted" TEXT NOT NULL DEFAULT 'x',
  PRIMARY KEY ("Id")
);

CREATE TABLE "ShapeNotes" (
  "Id" VARCHAR(36) NOT NULL,
  "OrderRef" VARCHAR(36),
  PRIMARY KEY ("Id"),
  CONSTRAINT "FK_ShapeNotes_OrderRef" FOREIGN KEY ("OrderRef") REFERENCES "Orders" ("Id")
);

CREATE TABLE "ShapeNoteTags" (
  "Id" VARCHAR(36) NOT NULL,
  "NoteRef" VARCHAR(36),
  PRIMARY KEY ("Id"),
  CONSTRAINT "FK_ShapeNoteTags_NoteRef" FOREIGN KEY ("NoteRef") REFERENCES "ShapeNotes" ("Id")
);

-- An enumeration table, a domain and an enumeration type.
CREATE TABLE "Statuses" (
  "Id" TEXT NOT NULL,
  "Description" TEXT NOT NULL,
  PRIMARY KEY ("Id")
);
INSERT INTO "Statuses" ("Id", "Description") VALUES ('open', 'Open'), ('closed', 'Closed');

CREATE DOMAIN "Slug" AS VARCHAR(40);
CREATE TYPE "Mood" AS ENUM ('calm', 'angry');
CREATE TABLE "Typed" (
  "Id" TEXT NOT NULL,
  "Handle" "Slug" NOT NULL,
  "Feeling" "Mood",
  "Feelings" "Mood"[],
  PRIMARY KEY ("Id")
);
