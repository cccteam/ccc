package resource

import (
	"time"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc"
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

type Link struct {
	ID       ccc.UUID `json:"id"`
	Resource string   `json:"resource"`
	Name     string   `json:"name"`
}
