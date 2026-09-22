package app

import (
	"context"

	"github.com/cccteam/ccc/accesstypes"
)

// TenancyConfigurer is the tenancy seam the configuration provides. Tenant existence is
// concealed (generation.WithConcealedDomains): DomainVisible answers whether the tenant
// exists AND the caller holds at least one grant in it, so a prober cannot confirm a
// tenant exists from the rejection shape.
type TenancyConfigurer interface {
	DomainVisible(ctx context.Context, user accesstypes.User, domain accesstypes.Domain) (bool, error)
}

// DomainVisibleFunc is the seam as the App carries it.
type DomainVisibleFunc = func(ctx context.Context, user accesstypes.User, domain accesstypes.Domain) (bool, error)

// DomainVisible reports whether the tenant exists and the user holds at least one grant
// in it; the generated DomainGuard middleware and the consolidated dispatcher answer "no"
// with the same not-found an unknown tenant gets.
func (a *App) DomainVisible(ctx context.Context, user accesstypes.User, domain accesstypes.Domain) (bool, error) {
	return a.domainVisible(ctx, user, domain)
}
