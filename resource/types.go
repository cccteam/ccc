package resource

import (
	"time"

	"github.com/cccteam/ccc/accesstypes"
)

type DataChangeEvent struct {
	TableName   accesstypes.Resource `spanner:"TableName"`
	RowID       string               `spanner:"RowId"`
	EventTime   time.Time            `spanner:"EventTime"`
	EventSource string               `spanner:"EventSource"`
	ChangeSet   string               `spanner:"ChangeSet"`
}

type DiffElem struct {
	Old any
	New any
}

type cacheEntry struct {
	index int
	tag   string
}
