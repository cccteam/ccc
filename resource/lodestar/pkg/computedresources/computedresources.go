// Package computedresources provides Lodestar's computed resources: read-only
// resources whose rows come from application-written query logic instead of a table
// or a subquery. The generated handlers check permissions eagerly at decode time
// (there is no library execution underneath to defer to) and then call this
// package's List and Read functions.
package computedresources

import (
	"context"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource/lodestar/pkg/router"
	"github.com/go-chi/chi/v5"
)

// Client carries application dependencies into computed-resource query logic. The
// type exists to exercise the generated ComputedClient wiring.
type Client struct{}

// NewClient constructs a Client.
func NewClient() *Client {
	return &Client{}
}

// requestDomain reads the sector the request was made in. The generated computed
// handlers check permissions in the URL sector's partition but hand the List and Read
// functions no domain (the QuerySet does not expose its scope), so a sector-scoped
// computed resource reads the route parameter back off the request context — the
// same value the handler checked — to partition its own rows. Recorded as a finding:
// structural tenancy for computed resources is the framework's to supply.
func requestDomain(ctx context.Context) accesstypes.Domain {
	return accesstypes.Domain(chi.URLParamFromCtx(ctx, string(router.Domain)))
}
