-- Frame is the telemetry frame as the droid's firmware sent it, a JSON document the
-- service does not model, stored verbatim: typed in Go by telemetry.Frame, a type declared
-- over json.RawMessage in the droid link's own package, whose JSON and Spanner methods the
-- generator writes there (WithTypes). Nullable: the seeded readings and a droid on old
-- firmware send none.
ALTER TABLE DroidReports ADD COLUMN Frame JSON;
