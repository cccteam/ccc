// Package main implements a code generator for resource types and handlers.
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
		generation.GenerateRoutes("pkg/router", "api"),
		// The droids outlet is the machine channel: structs annotated with @outlet
		// naming droids are served under /droids, which the router composes behind
		// API-key authentication instead of a browser session.
		generation.WithRouterOutlet("droids", "droids"),
		// The portal outlet is the clients' browser app: structs annotated with @outlet
		// naming portal are served under /portal/api behind the members auth, with their
		// own permission-digest and user-domains routes (ServesSessions).
		generation.WithRouterOutlet("portal", "portal/api", generation.ServesSessions()),
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
