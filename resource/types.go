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

type cacheEntry struct {
	index int
	tag   string
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

var _ FieldDefaultFunc = (CommitTimestamp)

func CommitTimestamp(_ context.Context, _ TxnBuffer) (any, error) {
	return spanner.CommitTimestamp, nil
}

var _ FieldDefaultFunc = (CommitTimestampPtr)

func CommitTimestampPtr(_ context.Context, _ TxnBuffer) (any, error) {
	return &spanner.CommitTimestamp, nil
}
