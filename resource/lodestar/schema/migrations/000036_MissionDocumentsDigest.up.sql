-- Digest is the SHA-256 of a mission document's stored bytes, recorded by the upload
-- method from the file the frame streamed: a BYTES column, which the generator types as
-- one string in both clients' interfaces (encoding/json carries it as base64) with
-- display type bytes. NOT NULL: every document is digested as it is filed, and nothing
-- else writes the table. Added nullable, then constrained, the two-step form Spanner
-- accepts for a NOT NULL column on an existing table.
ALTER TABLE MissionDocuments ADD COLUMN Digest BYTES(32);
ALTER TABLE MissionDocuments ALTER COLUMN Digest BYTES(32) NOT NULL;
