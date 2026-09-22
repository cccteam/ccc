-- Two JSON columns typed by application types: Provenance, a plain struct whose
-- TypeScript interface the generator derives, and Position, a GeoJSON Point whose
-- TypeScript type the Go type declares with @typescript. Both nullable: a document filed
-- before the origin was recorded has none, and a call relayed by voice names no point.
ALTER TABLE MissionDocuments ADD COLUMN Provenance JSON;
ALTER TABLE DistressCalls ADD COLUMN Position JSON;
