-- CargoBays is the tonnage each of a ship's cargo bays takes, in order: an ARRAY<INT64>
-- column, which the generator types number[] in the console's interface and metadata, one
-- member of the display-type vocabulary the client's union lists. NOT NULL with an empty
-- array as the default: a ship with no bays carries [], never NULL, a create that says
-- nothing about bays gets none, and the struct's []int64 matches the column as the
-- nullability check reads it (pointers and Null wrappers only). Spanner has no array
-- equality and cannot index an ARRAY column, so the field carries no allow_filter and no
-- list filters or sorts by it.
ALTER TABLE Ships ADD COLUMN CargoBays ARRAY<INT64> NOT NULL DEFAULT (ARRAY<INT64>[]);
