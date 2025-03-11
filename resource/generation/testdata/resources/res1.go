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

type alias = struct{}

type FileRecordSet struct {
	ID           ccc.UUID            `spanner:"Id"`
	FileID       ccc.UUID            `spanner:"FileId" index:"true"`
	ManyIDs      []FileID            `spanner:"FileIdArray"`
	Status       FileRecordSetStatus `spanner:"Status"`
	ErrorDetails *string             `spanner:"ErrorDetails"`
	UpdatedAt    *time.Time          `spanner:"UpdatedAt" conditions:"immutable"`
}

type FileID string

type FileStatus string

const (
	ErrorProcessingFileStatus FileStatus = "Error Processing"
)

type FileRecordSetStatus string

const (
	ErrorRecordSetStatus FileRecordSetStatus = "Error"
)
