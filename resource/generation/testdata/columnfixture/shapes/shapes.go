// Package shapes is a package the generator does not load: its declared type is read
// through the @typescript reader's on-demand load.
package shapes

import "encoding/json"

// Tag is a type declared in a package the generator does not load, with its TypeScript
// type declared here.
//
// @typescript(Tag, from: "tags")
type Tag json.RawMessage

// MarshalJSON writes the raw JSON.
func (t Tag) MarshalJSON() ([]byte, error) {
	return json.RawMessage(t).MarshalJSON()
}

// UnmarshalJSON keeps the raw JSON.
func (t *Tag) UnmarshalJSON(b []byte) error {
	return (*json.RawMessage)(t).UnmarshalJSON(b)
}
