// Package shared is an application package the generator writes nothing into unless
// WithTypes names it: its declared type is used on a column, and the pairs the type
// needs are generated here when it is named, refused when it is not.
package shared

import "encoding/json"

// Manifest is a type declared over json.RawMessage with no methods of its own, declaring
// its TypeScript type: the JSON pair and the Spanner pair are the generator's to write.
//
// @typescript(Manifest, from: "manifests")
type Manifest json.RawMessage
