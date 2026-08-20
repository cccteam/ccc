// Package stations provides the starport's tenancy source: the stations that serve as
// permission domains for domain-scoped resources and RPC methods. The demo uses a
// fixed directory; a real application backs this interface with its tenant table.
package stations

import (
	"context"
	"slices"

	"github.com/cccteam/access"
)

var _ access.Domains = &Directory{}

// Directory is the fixed list of demo stations.
type Directory struct {
	ids []string
}

// NewDirectory constructs the demo station directory.
func NewDirectory() *Directory {
	return &Directory{ids: []string{"station-alpha", "station-beta"}}
}

// DomainIDs implements access.Domains.
func (d *Directory) DomainIDs(_ context.Context) ([]string, error) {
	return slices.Clone(d.ids), nil
}

// DomainExists implements access.Domains.
func (d *Directory) DomainExists(_ context.Context, domain string) (bool, error) {
	return slices.Contains(d.ids, domain), nil
}
