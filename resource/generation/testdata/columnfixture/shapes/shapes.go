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

// Memo is a type declared over json.RawMessage with no methods of its own, in a package
// the generator does not write into: JSON by declaration, typed as declared, and refused
// for the pair it needs, naming WithTypes among the fixes.
//
// @typescript(Memo, from: "memos")
type Memo json.RawMessage
