// Package nullablefixture provides parsed-struct fixtures for the nullability check on
// slice-typed fields and the nullable tag: every slice shape on a nullable and on a NOT
// NULL column, a scalar mismatch beside a slice, and the pointer-to-slice forms the
// column typing refuses, over the synthetic table map nullableFixtureTables in the tests.
package nullablefixture

import "github.com/cccteam/ccc"

// Marks is a named slice type: a slice, whatever its name.
type Marks []string

// NullMarks is a named slice type whose name starts with Null: still a slice, so its
// nullability is its column's, not its name's.
type NullMarks []string

// Struct-level annotations only parse from a TypeSpec doc comment, so the structs sit in
// a grouped type block like real project sources do.
type (
	// Roster carries every slice shape the check admits beside two scalars: a byte slice
	// and a slice of integers on nullable columns, a named slice on a nullable array, a
	// byte slice and a slice of strings on NOT NULL columns, a Null-prefixed named slice
	// on a NOT NULL array, an output-only byte slice on a nullable column, a pointer to a
	// string on a nullable column, and a plain string on a NOT NULL one.
	//
	// @resource
	Roster struct {
		ID     ccc.UUID  `spanner:"Id"`
		Seal   []byte    `spanner:"Seal"`
		Bays   []int64   `spanner:"Bays"`
		Named  Marks     `spanner:"Named"`
		Digest []byte    `spanner:"Digest"`
		Tags   []string  `spanner:"Tags"`
		Nulled NullMarks `spanner:"Nulled"`
		Stamp  []byte    `spanner:"Stamp" conditions:"output_only"`
		Label  *string   `spanner:"Label"`
		Note   string    `spanner:"Note"`
	}

	// Ledger carries a scalar on a nullable column beside a slice on one: the scalar is
	// reported in the mismatch table, the slice is not.
	//
	// @resource
	Ledger struct {
		ID    ccc.UUID `spanner:"Id"`
		Count int64    `spanner:"Count"`
		Bays  []int64  `spanner:"Bays"`
	}

	// Pin carries a pointer to each slice shape, a byte slice, a slice of integers, and a
	// named slice, each refused by the column typing naming the plain slice.
	//
	// @resource
	Pin struct {
		ID    ccc.UUID `spanner:"Id"`
		Seal  *[]byte  `spanner:"Seal"`
		Bays  *[]int64 `spanner:"Bays"`
		Named *Marks   `spanner:"Named"`
	}
)
