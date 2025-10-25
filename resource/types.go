package resource

import (
	"context"
	"time"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
)

const (
	trueStr  = "true"
	eqStr    = "eq"
	neStr    = "ne"
	gtStr    = "gt"
	ltStr    = "lt"
	gteStr   = "gte"
	lteStr   = "lte"
	inStr    = "in"
	notinStr = "notin"
)

// DataChangeEvent represents a record of a change made to a database table.
type DataChangeEvent struct {
	TableName   accesstypes.Resource `spanner:"TableName"`
	RowID       string               `spanner:"RowId"`
	Sequence    int                  `spanner:"Sequence"`
	EventTime   time.Time            `spanner:"EventTime"`
	EventSource string               `spanner:"EventSource"`
	ChangeSet   spanner.NullJSON     `spanner:"ChangeSet"`
}

// DiffElem represents the old and new values of a field that has been changed.
type DiffElem struct {
	Old any
	New any
}

type jsonFieldName string

type dbFieldMetadata struct {
	index      int
	ColumnName string
}

// TypescriptData holds all the collected resource and permission information needed for TypeScript code generation.
type TypescriptData struct {
	Permissions           []accesstypes.Permission
	ResourcePermissions   []accesstypes.Permission
	Resources             []accesstypes.Resource
	ResourceTags          map[accesstypes.Resource][]accesstypes.Tag
	ResourcePermissionMap permissionMap
	Domains               []accesstypes.PermissionScope
}

// SortDirection defines the sort direction for a field.
type SortDirection string

const (
	// SortAscending specifies sorting in ascending order.
	SortAscending SortDirection = "asc"
	// SortDescending specifies sorting in descending order.
	SortDescending SortDirection = "desc"
)

// SortField represents a field to sort by, including the field name and sort direction.
type SortField struct {
	Field     string
	Direction SortDirection
}

var _ FieldDefaultFunc = CommitTimestamp

// CommitTimestamp is a FieldDefaultFunc that returns the Spanner commit timestamp.
func CommitTimestamp(_ context.Context, _ *SpannerReadWriteTransaction) (any, error) {
	return spanner.CommitTimestamp, nil
}

var _ FieldDefaultFunc = CommitTimestampPtr

// CommitTimestampPtr is a FieldDefaultFunc that returns a pointer to the Spanner commit timestamp.
func CommitTimestampPtr(_ context.Context, _ *SpannerReadWriteTransaction) (any, error) {
	return &spanner.CommitTimestamp, nil
}

var _ FieldDefaultFunc = DefaultFalse

// DefaultFalse is a FieldDefaultFunc that returns false.
func DefaultFalse(_ context.Context, _ *SpannerReadWriteTransaction) (any, error) {
	return false, nil
}

var _ FieldDefaultFunc = DefaultTrue

// DefaultTrue is a FieldDefaultFunc that returns true.
func DefaultTrue(_ context.Context, _ *SpannerReadWriteTransaction) (any, error) {
	return true, nil
}

var _ FieldDefaultFunc = DefaultString("test")

// DefaultString returns a FieldDefaultFunc that provides the given string value.
func DefaultString(v string) FieldDefaultFunc {
	return func(_ context.Context, _ *SpannerReadWriteTransaction) (any, error) {
		return v, nil
	}
}
