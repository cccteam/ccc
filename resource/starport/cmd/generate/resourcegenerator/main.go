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
			"github.com/cccteam/ccc/resource/starport/pkg/resources",
			"github.com/cccteam/ccc/resource/starport/pkg/router",
			"github.com/cccteam/ccc/resource/starport/pkg/rpc",
		},
		generation.GenerateHandlers("app"),
		generation.GenerateRoutes("pkg/router", "api"),
		// Domain-scoped resources and RPC methods are served under the station segment
		// pair: /api/stations/{stationID}/... . The station is the permission domain.
		generation.WithDomainRoute("stations", "stationID"),
		generation.WithRPC("pkg/rpc"),
		// CrewMember is excluded from consolidation so the app exercises both mutation
		// surfaces: the consolidated PATCH /api/resources handler and a standalone
		// per-resource PATCH /api/crew-members handler. Berth is excluded because it is
		// domain-scoped: the consolidated handler cannot carry a domain yet, so including
		// it is a generation error until the consolidated payload gains a per-op domain.
		generation.WithConsolidatedHandlers("resources", true, "CrewMember", "Berth"),
		generation.WithSpannerEmulatorVersion("1.5.55"),
		generation.GenerateTypescript("gui/src/app/core/service",
			generation.GenerateMetadata(),
			generation.GeneratePermissions(),
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
