-- Callsigns are the handles a squadron answers to on the open channel, filed by the sector
-- marshal: a nullable ARRAY<STRING(16)> column typed by the plain []string in the resource
-- struct, where NULL (the squadron has not filed yet) and an empty array (it flies silent
-- and says so) are different answers. Nullable so the two can be told apart; a Go slice
-- has one form, so the struct's field takes its nullability from this column, the
-- generated request struct carries nullable:"true", and a null in a PATCH writes the
-- column back to NULL. Spanner cannot index an ARRAY column, so the field carries no
-- allow_filter and no list filters or sorts by it.
ALTER TABLE Squadrons ADD COLUMN Callsigns ARRAY<STRING(16)>;
