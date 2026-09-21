// Package columnfixture holds one column type per shape the column classifier resolves,
// derives, or refuses: the built-in rows and the generic row, a plain struct on a JSON
// column, a named slice of structs, a type declaring its TypeScript type, and each
// departure from the rules.
package columnfixture

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"cloud.google.com/go/civil"
	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource/generation/testdata/columnfixture/shapes"
	"github.com/cccteam/ccc/resource/generation/testdata/columnfixture/shared"
)

// Kind is an enumeration-style named string.
type Kind string

// NullKind is how an application spells a nullable enumeration column: an alias of the
// generic row.
type NullKind = ccc.NullEnum[Kind]

// Rank is a named int64.
type Rank int64

// Digest is a named slice of bytes with no JSON methods: the bytes leaf, as an unnamed
// []byte is, since encoding/json writes both as one base64 string.
type Digest []byte

// NullRank is the generic row over a number.
type NullRank = ccc.NullEnum[Rank]

// Provenance is a plain struct a JSON column holds: derived, and stored by generated
// methods.
type Provenance struct {
	System     string    `json:"system"`
	Reference  string    `json:"reference,omitempty"`
	ReceivedAt time.Time `json:"receivedAt"`
	Internal   string    `json:"-"`
}

// Attachment is the element of a named slice a JSON column holds.
type Attachment struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

// Attachments is a named slice of structs with hand-written Spanner methods, which it
// keeps.
type Attachments []Attachment

// DecodeSpanner reads the slice back from its column.
func (a *Attachments) DecodeSpanner(val any) error {
	text, ok := val.(string)
	if !ok {
		return fmt.Errorf("expected string, got %T", val)
	}

	return json.Unmarshal([]byte(text), a)
}

// EncodeSpanner stores the slice as JSON.
func (a Attachments) EncodeSpanner() (any, error) {
	return spanner.NullJSON{Value: a, Valid: true}, nil
}

// Manifests is a named slice of structs with no Spanner methods: derived as
// Manifest[], stored by generated methods.
type Manifests []Manifest

// Manifest is the element of Manifests; it reaches a second struct.
type Manifest struct {
	Item  string      `json:"item"`
	Where *Provenance `json:"where,omitempty"`
}

// Untagged has a field with no json tag.
type Untagged struct {
	Name string
}

// Sealed writes its own JSON and declares no TypeScript type.
type Sealed struct {
	V string `json:"v"`
}

// MarshalJSON writes the value alone.
func (s Sealed) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.V)
}

// Position is a GeoJSON Point: its shape lives outside Go, so it declares its type.
//
// @typescript(Point, from: "geojson")
type Position json.RawMessage

// MarshalJSON writes the raw JSON.
func (p Position) MarshalJSON() ([]byte, error) {
	return json.RawMessage(p).MarshalJSON()
}

// UnmarshalJSON keeps the raw JSON.
func (p *Position) UnmarshalJSON(b []byte) error {
	return (*json.RawMessage)(p).UnmarshalJSON(b)
}

// Label is a plain struct whose declaration wins over derivation.
//
// @typescript(Label, from: "labels")
type Label struct {
	Text string `json:"text"`
}

// Code declares a TypeScript built-in over a named string.
//
// @typescript(string)
type Code string

// Doc is a derived struct reaching a declared type of this package, one of a package
// the generator does not load, and a type JSON by declaration, whose pair the struct's
// use brings in.
type Doc struct {
	Where Position   `json:"where"`
	Tag   shapes.Tag `json:"tag"`
	Body  Payload    `json:"body"`
}

// Clash imports Point from another module than Position does.
//
// @typescript(Point, from: "other-geo")
type Clash json.RawMessage

// MarshalJSON writes the raw JSON.
func (c Clash) MarshalJSON() ([]byte, error) {
	return json.RawMessage(c).MarshalJSON()
}

// UnmarshalJSON keeps the raw JSON.
func (c *Clash) UnmarshalJSON(b []byte) error {
	return (*json.RawMessage)(c).UnmarshalJSON(b)
}

// Payload is a defined type over json.RawMessage with no annotation and no methods of
// its own: JSON by declaration, the unknown leaf as json.RawMessage is, never bytes. Its
// JSON pair is generated (zz_gen_json.go, the fixture's copy of what the generator
// writes, so the run that finds it is a fixed point) and so is its Spanner pair.
type Payload json.RawMessage

// Sheet is a defined type over json.RawMessage declaring its TypeScript type, with no
// methods of its own: typed as declared, both pairs generated.
//
// @typescript(Sheet, from: "sheets")
type Sheet json.RawMessage

// Stamp is a defined type over a type with JSON methods that is no byte slice: JSON by
// declaration too, resolving as time.Time does, its pair converting through time.Time.
type Stamp time.Time

// Wrapped is a defined type over a plain struct: no JSON methods behind it, so no pair,
// and derived from the struct's fields as the struct is.
type Wrapped Provenance

// Token is a plain byte slice declaring a TypeScript type that promises JSON where the
// wire carries base64: refused.
//
// @typescript(Token, from: "tokens")
type Token []byte

// Half implements one JSON method without the other over json.RawMessage: refused.
type Half json.RawMessage

// MarshalJSON writes the raw JSON; the type has no UnmarshalJSON.
func (h Half) MarshalJSON() ([]byte, error) {
	return json.RawMessage(h).MarshalJSON()
}

// Bound declares a built-in with a module, which no module exports.
//
// @typescript(string, from: "strings")
type Bound string

// Loose declares a type outside the built-ins with no module.
//
// @typescript(Money)
type Loose struct {
	Amount string `json:"amount"`
}

// Row carries one column per shape the classifier resolves or derives, the ARRAY
// columns of every leaf among them.
type Row struct {
	ID          ccc.UUID           `spanner:"Id"`
	Kind        NullKind           `spanner:"Kind"`
	Rank        NullRank           `spanner:"Rank"`
	Note        spanner.NullString `spanner:"Note"`
	Blob        spanner.NullJSON   `spanner:"Blob"`
	When        *time.Time         `spanner:"When"`
	Tags        []string           `spanner:"Tags"`
	Counts      []int64            `spanner:"Counts"`
	Flags       []bool             `spanner:"Flags"`
	Stamps      []time.Time        `spanner:"Stamps"`
	Days        []civil.Date       `spanner:"Days"`
	IDs         []ccc.UUID         `spanner:"Ids"`
	Ranks       []*int64           `spanner:"Ranks"`
	Toggles     []*bool            `spanner:"Toggles"`
	Seal        []byte             `spanner:"Seal"`
	Digest      Digest             `spanner:"Digest"`
	Chunks      [][]byte           `spanner:"Chunks"`
	Checksum    [4]byte            `spanner:"Checksum"`
	Provenance  *Provenance        `spanner:"Provenance"`
	Attachments Attachments        `spanner:"Attachments"`
	Manifests   Manifests          `spanner:"Manifests"`
	Position    *Position          `spanner:"Position"`
	Positions   []Position         `spanner:"Positions"`
	Label       Label              `spanner:"Label"`
	Code        Code               `spanner:"Code"`
	Doc         Doc                `spanner:"Doc"`
	Tag         shapes.Tag         `spanner:"Tag"`
	Payload     *Payload           `spanner:"Payload"`
	Payloads    []Payload          `spanner:"Payloads"`
	Sheet       Sheet              `spanner:"Sheet"`
	Stamp       Stamp              `spanner:"Stamp"`
	Wrapped     Wrapped            `spanner:"Wrapped"`
	Memo        shapes.Memo        `spanner:"Memo"`
	Shared      shared.Manifest    `spanner:"Shared"`
}

// Bad carries one column per shape the classifier refuses.
type Bad struct {
	ID       ccc.UUID          `spanner:"Id"`
	Count    sql.NullInt64     `spanner:"Count"`
	Untagged Untagged          `spanner:"Untagged"`
	Sealed   Sealed            `spanner:"Sealed"`
	Matrix   [][]string        `spanner:"Matrix"`
	Any      any               `spanner:"Any"`
	Raw      json.RawMessage   `spanner:"Raw"`
	RawPtr   *json.RawMessage  `spanner:"RawPtr"`
	Raws     []json.RawMessage `spanner:"Raws"`
	Token    Token             `spanner:"Token"`
	Bound    Bound             `spanner:"Bound"`
	Loose    Loose             `spanner:"Loose"`
}

// Partial carries the type implementing one JSON method without the other.
type Partial struct {
	ID   ccc.UUID `spanner:"Id"`
	Half Half     `spanner:"Half"`
}

// Clashing carries both types importing Point.
type Clashing struct {
	ID    ccc.UUID `spanner:"Id"`
	Here  Position `spanner:"Here"`
	There Clash    `spanner:"There"`
}

// Request is an RPC-shaped struct reaching declared types, so the walker's leaf is
// the same imported type, byte slices in every shape the wire carries as base64, the
// JSON values (json.RawMessage and the types declared over it), and lists of the leaves
// whose display name is not their interface type.
type Request struct {
	ID      ccc.UUID
	Where   Position
	Marks   []shapes.Tag
	Doc     Doc
	Seal    []byte
	Digest  Digest
	Sealed  *[]byte
	Chunks  [][]byte
	Hashes  []Digest
	Counts  []int64
	Days    []civil.Date
	IDs     []ccc.UUID
	Raw     json.RawMessage
	Payload Payload
	Sheet   Sheet
	Stamp   Stamp
}
