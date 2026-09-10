// Package main implements a code generator for resource types and handlers. The
// program declares the served router too (GenerateRouter): each outlet says how it
// authenticates and which browser application it serves, and the generator emits the
// router with its middleware chain documented and proven.
//
// Demonstrates: GenerateRouter.
package main

import (
	"context"
	"log"

	"github.com/cccteam/ccc/resource/generation"
	"github.com/go-playground/errors/v5"
)

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	generator, err := generation.NewResourceGenerator(
		ctx,
		"pkg/resources",
		[]string{"file://schema/migrations"},
		[]string{
			"github.com/cccteam/ccc/resource/lodestar/pkg/resources",
			"github.com/cccteam/ccc/resource/lodestar/pkg/router",
			"github.com/cccteam/ccc/resource/lodestar/pkg/rpc",
			"github.com/cccteam/ccc/resource/lodestar/pkg/virtualresources",
			"github.com/cccteam/ccc/resource/lodestar/pkg/computedresources",
		},
		generation.GenerateHandlers("app"),
		// The router is generated: the default outlet is the console, the crew auth's
		// password sessions under /api with the console's browser application at /.
		generation.GenerateRouter(),
		generation.GenerateRoutes("pkg/router", "api",
			generation.Auth("github.com/cccteam/ccc/resource/lodestar/pkg/auth/crew", generation.Password),
			generation.WebApp("/"),
		),
		// The droids outlet is the machine channel: structs annotated with @outlet
		// naming droids are served under /droids, which the router composes behind
		// API-key authentication (the App's DroidsAuth) instead of a browser session.
		generation.WithRouterOutlet("droids", "droids", generation.APIKey()),
		// The portal outlet is the clients' browser app: structs annotated with @outlet
		// naming portal are served under /portal/api behind the members auth, whose
		// people sign in through their company's Google directory, with their own
		// permission-digest and user-domains routes, and the portal's browser
		// application at /portal.
		generation.WithRouterOutlet("portal", "portal/api",
			generation.Auth("github.com/cccteam/ccc/resource/lodestar/pkg/auth/members", generation.OIDCGoogle),
			generation.WebApp("/portal"),
		),
		// Sector-scoped resources and methods are served under /api/sectors/{sectorID}/;
		// a sector the caller holds no grant in answers like one that does not exist.
		generation.WithDomainRoute("sectors"),
		generation.WithConcealedDomains(),
		generation.WithRPC("pkg/rpc"),
		generation.WithVirtualResources("pkg/virtualresources"),
		generation.WithComputedResources("pkg/computedresources"),
		generation.GenerateHandlerTests("test/authz"),
		// Client (global) and Hangar (sector-scoped) keep standalone PATCH surfaces
		// beside the consolidated handler.
		generation.WithConsolidatedHandlers("resources", true, "Client", "Hangar"),
		generation.WithSpannerEmulatorVersion("1.5.56"),
		generation.GenerateTypescript("web/console/src/app/core/service",
			generation.GenerateMetadata(),
			generation.GeneratePermissions(),
			generation.GenerateEnums(),
		),
		generation.GenerateTypescript("web/portal/src/app/core/service",
			generation.ForOutlet("portal"),
			generation.GenerateMetadata(),
			generation.GeneratePermissions(),
			generation.GenerateEnums(),
		),
	)
	if err != nil {
		return errors.Wrap(err, "generation.NewResourceGenerator()")
	}
	defer generator.Close()

	if err := generator.Generate(); err != nil {
		return errors.Wrap(err, "generation.Generator.Generate()")
	}

	return nil
}
