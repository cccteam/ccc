package main

import (
	"context"

	"github.com/cccteam/ccc/resource/generation"
	"github.com/go-playground/errors/v5"
)

// newGenerator declares the application's resource generator: the package it reads, the
// schema it reads them against, and what it emits (resource types, handlers, routes,
// authorization matrix, and TypeScript clients). The generate program (main.go) runs it
// and warnings_test.go runs it in-process, so the two never drift.
func newGenerator(ctx context.Context) (generation.Generator, error) {
	generator, err := generation.NewResourceGenerator(
		ctx,
		"pkg/resources",
		[]string{"file://schema/migrations"},
		[]string{
			"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/outlets/pkg/resources",
			"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/outlets/pkg/router",
		},
		generation.GenerateHandlers("app"),
		// The router is generated from the outlet declarations: the console is the default
		// outlet, the staff auth's password sessions under /api with its browser application
		// at /.
		generation.GenerateRouter(),
		generation.GenerateRoutes("pkg/router", "api",
			generation.Auth("github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/outlets/pkg/auth/staff", generation.Password),
			generation.WebApp("/"),
		),
		// The portal outlet is a second browser surface: structs annotated with @outlet
		// naming portal are served under /portal/api behind the members auth, whose people
		// sign in through the organization's Azure directory, with their own
		// permission-digest and user-domains routes and the portal's browser application
		// at /portal.
		generation.WithRouterOutlet("portal", "portal/api",
			generation.Auth("github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/outlets/pkg/auth/members", generation.OIDCAzure),
			generation.WebApp("/portal"),
		),
		// The machines outlet is the machine REST API: structs annotated with @outlet
		// naming machines are served under /machines, which the router composes behind
		// API-key authentication (the App's MachinesAuth) instead of a browser session.
		generation.WithRouterOutlet("machines", "machines", generation.APIKey()),
		generation.GenerateHandlerTests("test/authz"),
		// Tenant-scoped resources and RPC methods are served under the tenant segment
		// pair: /api/tenants/{tenantID}/... . The tenant is the permission domain, and
		// Tenant is the tenant-record resource.
		generation.WithDomainRoute("tenants"),
		// Tenant existence is concealed: a tenant the caller holds no grant in answers
		// exactly like a tenant that does not exist.
		generation.WithConcealedDomains(),
		generation.WithConsolidatedHandlers("resources", true),
		generation.WithSpannerEmulatorVersion("1.5.56"),
		generation.GenerateTypescript("web/console/src/app/core/service",
			generation.GenerateMetadata(),
			generation.GeneratePermissions(),
			generation.GenerateEnums(),
		),
		// The portal's client: the portal outlet's members only, addressed under its
		// prefix.
		generation.GenerateTypescript("web/portal/src/app/core/service",
			generation.ForOutlet("portal"),
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
