package resource

import (
	"context"
	"time"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
)

type DataChangeEvent struct {
	TableName   accesstypes.Resource `spanner:"TableName"`
	RowID       string               `spanner:"RowId"`
	EventTime   time.Time            `spanner:"EventTime"`
	EventSource string               `spanner:"EventSource"`
	ChangeSet   spanner.NullJSON     `spanner:"ChangeSet"`
}

type DiffElem struct {
	Old any
	New any
}

type jsonFieldName string

type cacheEntry struct {
	index        int
	dbColumnName string
}

type TypescriptData struct {
	Permissions           []accesstypes.Permission
	ResourcePermissions   []accesstypes.Permission
	Resources             []accesstypes.Resource
	RPCMethods            []accesstypes.Resource
	ResourceTags          map[accesstypes.Resource][]accesstypes.Tag
	ResourcePermissionMap permissionMap
	Domains               []accesstypes.PermissionScope
}

// SortDirection defines the sort direction for a field.
type SortDirection string

const (
	SortAscending  SortDirection = "asc"
	SortDescending SortDirection = "desc"
)

// SortField represents a field to sort by, including the field name and sort direction.
type SortField struct {
	Field     string
	Direction SortDirection
}

var _ FieldDefaultFunc = CommitTimestamp

func CommitTimestamp(_ context.Context, _ TxnBuffer) (any, error) {
	return spanner.CommitTimestamp, nil
}

var _ FieldDefaultFunc = CommitTimestampPtr

func CommitTimestampPtr(_ context.Context, _ TxnBuffer) (any, error) {
	return &spanner.CommitTimestamp, nil
}

var _ FieldDefaultFunc = DefaultFalse

func DefaultFalse(_ context.Context, _ TxnBuffer) (any, error) {
	return false, nil
}

var _ FieldDefaultFunc = DefaultTrue

func DefaultTrue(_ context.Context, _ TxnBuffer) (any, error) {
	return true, nil
}

var _ FieldDefaultFunc = DefaultString("test")

func DefaultString(v string) FieldDefaultFunc {
	return func(_ context.Context, _ TxnBuffer) (any, error) {
		return v, nil
	}
}
