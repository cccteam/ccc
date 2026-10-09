-- The Postgres runtime tests' fixture: a table carrying every column type a resource
-- field reads (the key a UUID, so the column the Decoder reads is not text), a table with
-- a composite key, the constraints a write can break (a foreign key, a unique value, a
-- CHECK), and the change-event table a tracked resource writes beside its patches.
-- Names are the fixture's own.

CREATE TABLE "Gadgets" (
  "Id" UUID NOT NULL,
  "Name" TEXT NOT NULL,
  "Note" TEXT,
  "Weight" BIGINT NOT NULL,
  "Pieces" BIGINT,
  "Ratio" DOUBLE PRECISION NOT NULL,
  "Price" NUMERIC NOT NULL,
  "Fee" NUMERIC,
  "Active" BOOLEAN,
  "Seen" TIMESTAMPTZ NOT NULL,
  "Retired" TIMESTAMPTZ,
  "Made" DATE NOT NULL,
  "Scrapped" DATE,
  "Attrs" JSONB,
  "Tags" TEXT[],
  "Owner" UUID,
  PRIMARY KEY ("Id"),
  CONSTRAINT "Gadgets_Weight_Check" CHECK ("Weight" >= 0),
  CONSTRAINT "Gadgets_Name_Unique" UNIQUE ("Name")
);

CREATE TABLE "Widgets" (
  "Id" TEXT NOT NULL,
  "GadgetId" UUID NOT NULL,
  "Label" TEXT NOT NULL,
  PRIMARY KEY ("Id"),
  CONSTRAINT "Widgets_Gadget_Fk" FOREIGN KEY ("GadgetId") REFERENCES "Gadgets" ("Id")
);

CREATE TABLE "Bolts" (
  "WidgetId" TEXT NOT NULL,
  "Seq" BIGINT NOT NULL,
  "Torque" NUMERIC,
  PRIMARY KEY ("WidgetId", "Seq")
);

CREATE TABLE "DataChangeEvents" (
  "TableName" TEXT NOT NULL,
  "RowId" TEXT NOT NULL,
  "Sequence" BIGINT NOT NULL,
  "EventTime" TIMESTAMPTZ NOT NULL,
  "EventSource" TEXT NOT NULL,
  "ChangeSet" JSONB,
  PRIMARY KEY ("TableName", "RowId", "EventTime", "Sequence")
);

-- The tenant record the roster reads the keys of.
CREATE TABLE "Stations" (
  "Id" TEXT NOT NULL,
  "Name" TEXT NOT NULL,
  PRIMARY KEY ("Id")
);

-- The Execute gate tests' target: a row with an owner, a state, and a tenant key.
CREATE TABLE "enforcementResources" (
  "Id" TEXT NOT NULL,
  "Owner" TEXT NOT NULL,
  "State" TEXT NOT NULL,
  "Station" TEXT NOT NULL,
  PRIMARY KEY ("Id")
);

-- The file release tests' tables: rows that own a stored object (a NOT NULL key and,
-- optionally, a nullable second one), a table that references them so a delete the
-- foreign key refuses can be tried, and a table whose key is typed by a named store.
CREATE TABLE "FileRows" (
  "Id" TEXT NOT NULL,
  "Title" TEXT NOT NULL,
  "StoreKey" TEXT NOT NULL,
  "ThumbKey" TEXT,
  PRIMARY KEY ("Id")
);

CREATE TABLE "FileRefs" (
  "Id" TEXT NOT NULL,
  "FileRowId" TEXT NOT NULL,
  PRIMARY KEY ("Id"),
  CONSTRAINT "FK_FileRefs_FileRows" FOREIGN KEY ("FileRowId") REFERENCES "FileRows" ("Id")
);

CREATE TABLE "TypedFileRows" (
  "Id" TEXT NOT NULL,
  "DocKey" TEXT NOT NULL,
  PRIMARY KEY ("Id")
);
