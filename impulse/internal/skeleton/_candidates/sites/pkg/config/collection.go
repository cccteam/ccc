package config

import (
	"github.com/cccteam/access"
	consolerouter "github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/sites/apps/console/pkg/router"
	portalrouter "github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/sites/apps/portal/pkg/router"
	"github.com/go-playground/errors/v5"
)

// Collection is the application's whole permission registry: the union of every site's
// generated collection, which the data level hands the staff auth for its role file to
// validate against. The sites share one policy store, a login is one identity and a role
// is one set of powers across the application, so the roles are validated against
// everything any site registers. A resource both sites serve is declared identically in
// both and appears once; access.UnionCollection refuses sites that disagree on one.
func Collection() (access.PermissionCollection, error) {
	collection, err := access.UnionCollection(consolerouter.Collection(), portalrouter.Collection())
	if err != nil {
		return nil, errors.Wrap(err, "access.UnionCollection(console, portal)")
	}

	return collection, nil
}
