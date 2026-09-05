// Package computedresources provides Lodestar's computed resources: read-only
// resources whose rows come from application-written query logic instead of a table
// or a subquery. The generated handlers check permissions eagerly at decode time
// (there is no library execution underneath to defer to) and then call this
// package's List and Read functions.
package computedresources

import (
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/go-playground/errors/v5"
)

// Client carries application dependencies into computed-resource query logic. The
// type exists to exercise the generated ComputedClient wiring.
type Client struct{}

// NewClient constructs a Client.
func NewClient() *Client {
	return &Client{}
}

// sectorOf returns the sector the request was checked in. The generated computed
// handlers check permissions in the URL sector's partition and hand the List and Read
// functions the QuerySet that carries that scope, so a sector-scoped computed resource
// partitions its rows on exactly the value the permission check ran against. A global
// scope here is a wiring error: the resource is declared @domain.
func sectorOf[Resource resource.Resourcer](qSet *resource.QuerySet[Resource]) (accesstypes.Domain, error) {
	sector, ok := qSet.Scope().Domain()
	if !ok {
		return "", errors.Newf("%s is sector-scoped but was checked in the global scope", qSet.Resource())
	}

	return sector, nil
}
