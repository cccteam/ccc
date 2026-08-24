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
			"github.com/cccteam/ccc/resource/starport/pkg/virtualresources",
		},
		generation.GenerateHandlers("app"),
		generation.GenerateRoutes("pkg/router", "api"),
		// Domain-scoped resources and RPC methods are served under the station segment
		// pair: /api/stations/{stationID}/... . The station is the permission domain.
		generation.WithDomainRoute("stations", "stationID"),
		generation.WithRPC("pkg/rpc"),
		// Virtual resources are list-only projections backed by embedded subqueries;
		// their primary keys come from @primarykey annotations instead of the schema.
		generation.WithVirtualResources("pkg/virtualresources"),
		// CrewMember is excluded from consolidation so the app exercises both mutation
		// surfaces: the consolidated PATCH /api/resources handler and a standalone
		// per-resource PATCH /api/crew-members handler. Berth is excluded BY CHOICE so a
		// domain-scoped resource keeps exercising the standalone domain-scoped surface —
		// its routes are load-bearing for the frozen domain-partition suite. GantryCrane
		// is the domain-scoped resource inside the consolidated set: its operations carry
		// the domain in the path (/stations/{stationID}/gantry-cranes/...).
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
