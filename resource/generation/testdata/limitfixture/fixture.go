// Package limitfixture provides parsed-struct fixtures for the sqltype tag and the
// TypeScript maxLength: one table-backed resource with one field per pairing the tag
// decides, over the synthetic table map limitFixtureTables in the tests.
package limitfixture

import (
	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc"
	"github.com/shopspring/decimal"
)

// Code is a named string type: string-kinded, so a STRING(n) column sizes it.
type Code string

// Struct-level annotations only parse from a TypeSpec doc comment, so the struct sits
// in a grouped type block like real project sources do.
type (
	// Parcel carries one field per pairing: a string, a nullable string on MAX, a
	// NullString, a named string type, sized and unbounded bytes, a string array, a
	// decimal, a nullable decimal, a decimal array, an integer, a UUID on STRING(36), an
	// immutable sized string, and an output-only sized string hidden from the patch wire.
	//
	// @resource
	Parcel struct {
		ID       ccc.UUID            `spanner:"Id"`
		Label    string              `spanner:"Label"`
		Notes    *string             `spanner:"Notes"`
		Alias    spanner.NullString  `spanner:"Alias"`
		Kind     Code                `spanner:"Kind"`
		Seal     []byte              `spanner:"Seal"`
		Manifest []byte              `spanner:"Manifest"`
		Tags     []string            `spanner:"Tags"`
		Weight   decimal.Decimal     `spanner:"Weight"`
		Rebate   decimal.NullDecimal `spanner:"Rebate"`
		Fees     []decimal.Decimal   `spanner:"Fees"`
		Count    int64               `spanner:"Count"`
		OwnerID  ccc.UUID            `spanner:"OwnerId"`
		Serial   string              `spanner:"Serial" conditions:"immutable"`
		Stamp    string              `spanner:"Stamp" conditions:"output_only"`
	}
)
