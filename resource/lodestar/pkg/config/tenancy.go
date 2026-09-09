package config

import (
	"context"
	stderrors "errors"

	cloudspanner "cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/go-playground/errors/v5"
	"google.golang.org/api/iterator"
)

// tenantRoster is the tenant list read from the Sectors table once at startup. It is
// cached rather than queried per check: the generated consolidated handler consults
// DomainVisible inside the mutation transaction, where opening another read is illegal
// on the emulator. A process restart picks up new tenants.
type tenantRoster struct {
	domains []accesstypes.Domain
	set     map[accesstypes.Domain]bool
}

// Domains lists the tenants as permission domains, from the roster read at startup.
func (c *DataConfiguration) Domains(_ context.Context) ([]accesstypes.Domain, error) {
	return c.tenants.domains, nil
}

// DomainVisible reports whether the domain is a known sector AND the user holds at least
// one grant in it: existence from the startup roster, foothold from the permission
// engine's in-memory policy snapshot (no store read, so it is safe inside the
// consolidated handler's mutation transaction). The engine is the one of the auth the
// request came through: a client's foothold is in the members store. Sector existence is
// concealed: a caller with no foothold is answered exactly like the sector does not
// exist, which is how Cinder stays dark on most personas' star charts.
//
// Demonstrates: tenancy.concealed.
func (c *DataConfiguration) DomainVisible(ctx context.Context, user accesstypes.User, domain accesstypes.Domain) (bool, error) {
	if !c.tenants.set[domain] {
		return false, nil
	}

	visible, err := c.engine(ctx).UserHasGrants(ctx, user, accesstypes.DomainScope(domain))
	if err != nil {
		return false, errors.Wrap(err, "access.Client.UserHasGrants()")
	}

	return visible, nil
}

// loadTenants reads the tenant roster from the Sectors table.
func (c *DataConfiguration) loadTenants(ctx context.Context) error {
	iter := c.spannerClient.Single().Query(ctx, cloudspanner.Statement{SQL: "SELECT Id FROM Sectors ORDER BY Id"})
	defer iter.Stop()

	c.tenants.set = make(map[accesstypes.Domain]bool)
	for {
		row, err := iter.Next()
		if err != nil {
			if stderrors.Is(err, iterator.Done) {
				return nil
			}

			return errors.Wrap(err, "spanner.RowIterator.Next()")
		}

		var id string
		if err := row.Columns(&id); err != nil {
			return errors.Wrap(err, "spanner.Row.Columns()")
		}
		c.tenants.domains = append(c.tenants.domains, accesstypes.Domain(id))
		c.tenants.set[accesstypes.Domain(id)] = true
	}
}
