package resources

import (
	"time"

	"github.com/cccteam/ccc"
)

type AddressType struct {
	ID          string `spanner:"Id"`
	Description string `spanner:"description"`
}

func (a AddressType) method() {}

func doer() error {
	return nil
}

type Status struct {
	ID          ccc.UUID `spanner:"Id"`
	Description string   `spanner:"description"`
}

type enum int

const (
	e1 enum = iota
	e2
)

type alias = struct{} // line comment on a type alias

// this is not a doc comment
type ExampleStruct struct {
	Foo int
}

type (
	// Doc Comments on type specs must be in the type keyword's parentheses
	FileRecordSet struct {
		// Floating comment in FileRecordSet (not parsed)

		// ID doc comment
		ID ccc.UUID `spanner:"Id"` // ID line comment

		// FileID has its own DocComment
		FileID ccc.UUID `spanner:"FileId" index:"true"`

		// ManyIDs doc comment
		ManyIDs      []FileID            `spanner:"FileIdArray"`
		Status       FileRecordSetStatus `spanner:"Status"`
		ErrorDetails *string             `spanner:"ErrorDetails"`
		UpdatedAt    *time.Time          `spanner:"UpdatedAt" conditions:"immutable"`
	} // This is a Line Comment on FileRecordSet

	// Floating comment in this type spec (not parsed)
)

type FileID string

type FileStatus string

const (
	ErrorProcessingFileStatus FileStatus = "Error Processing"
)

type FileRecordSetStatus string

const (
	ErrorRecordSetStatus FileRecordSetStatus = "Error"
)
