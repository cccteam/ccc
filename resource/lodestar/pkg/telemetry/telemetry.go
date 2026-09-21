// Package telemetry is the droid link's own: the frames the droid firmware sends, kept as
// the firmware wrote them. Nothing in it is a resource. The generator writes into it all
// the same, because cmd/generate names it with WithTypes: the methods Frame needs to
// cross the wire and the JSON column land beside the type (zz_gen_json.go,
// zz_gen_storage.go) instead of the type moving into the resources package, which would
// put the link's model where the service's rows live, or growing a wrapper converted at
// every boundary.
package telemetry

import "encoding/json"

// Frame is one telemetry frame as the droid's firmware sent it: a JSON document whose
// schema the firmware owns and the service does not model, passed through verbatim from
// the ingest call to the column and back out of the droid channel's list. The type is
// its declaration and its annotation, and nothing else: json.RawMessage itself cannot
// sit on a Spanner column (the client would store it as BYTES), so the frame is a type
// declared over it, typed unknown, and the generator writes the JSON pair that keeps it
// JSON on the wire and the Spanner pair that stores it in the JSON column.
//
// Demonstrates: typescript.types-package.
//
// @typescript(unknown)
type Frame json.RawMessage
