package main

import (
	"context"

	"github.com/cccteam/ccc/resource/generation"
	"github.com/go-playground/errors/v5"
)

// newGenerator declares the portal site's resource generator: the package it reads, the
// schema it reads them against, and what it emits (resource types, handlers, routes,
// authorization matrix, and TypeScript client). The generate program (main.go) runs it and
// warnings_test.go runs it in-process, so the two never drift.
func newGenerator(ctx context.Context) (generation.Generator, error) {
	generator, err := generation.NewResourceGenerator(
		ctx,
		"apps/portal/pkg/resources",
		[]string{"file://schema/migrations"},
		[]string{
			"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/sites/apps/portal/pkg/resources",
			"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/sites/apps/portal/pkg/router",
		},
		generation.GenerateHandlers("apps/portal/app"),
		// The router is generated from the outlet declarations: the portal is this site's
		// default outlet, the staff auth's password sessions under /api with its browser
		// application at /.
		generation.GenerateRouter(),
		generation.GenerateRoutes("apps/portal/pkg/router", "api",
			generation.Auth("github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/sites/pkg/auth/staff", generation.Password),
			generation.WebApp("/"),
		),
		generation.GenerateHandlerTests("apps/portal/test/authz"),
		// Tenant-scoped resources and RPC methods are served under the tenant segment
		// pair: /api/tenants/{tenantID}/... . The tenant is the permission domain.
		generation.WithDomainRoute("tenants"),
		// Tenant existence is concealed: a tenant the caller holds no grant in answers
		// exactly like a tenant that does not exist.
		generation.WithConcealedDomains(),
		generation.WithConsolidatedHandlers("resources", true),
		generation.WithSpannerEmulatorVersion("1.5.56"),
		generation.GenerateTypescript("apps/portal/web/src/app/core/service",
			generation.GenerateMetadata(),
			generation.GeneratePermissions(),
			generation.GenerateEnums(),
		),
	)
	if err != nil {
		return nil, errors.Wrap(err, "generation.NewResourceGenerator()")
	}

	return generator, nil
}
