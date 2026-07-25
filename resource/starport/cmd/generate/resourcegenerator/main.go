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
		generation.WithRPC("pkg/rpc"),
		// CrewMember is excluded from consolidation so the app exercises both mutation
		// surfaces: the consolidated PATCH /api/resources handler and a standalone
		// per-resource PATCH /api/crew-members handler.
		generation.WithConsolidatedHandlers("resources", true, "CrewMember"),
		generation.WithSpannerEmulatorVersion("1.5.55"),
		generation.GenerateTypescript("frontend/src/service",
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
