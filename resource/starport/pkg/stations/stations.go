// Package stations provides the starport's tenancy source: the stations that serve as
// permission domains for domain-scoped resources and RPC methods. The demo uses a
// fixed list; a real application derives its domains from its tenant table. Domains
// are opaque labels to the access engine, so the application owning this list is also
// the one responsible for its validity.
package stations

import (
	"slices"

	"github.com/cccteam/ccc/accesstypes"
)

var directory = []accesstypes.Domain{"station-alpha", "station-beta"}

// Domains returns the demo stations as permission domains: the domain universe the
// bootstrap passes to access.MigrateRoles (the global domain is always included
// implicitly by the engine).
func Domains() []accesstypes.Domain {
	return slices.Clone(directory)
}
