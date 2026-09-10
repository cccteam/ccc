// Package main is the lighthouse resource generator program (test fixture).
package main

import (
	"context"
	"log"

	"github.com/cccteam/ccc/resource/generation"
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
			"example.com/lighthouse/pkg/resources",
			"example.com/lighthouse/pkg/rpc",
		},
		generation.GenerateHandlers("app"),
		generation.GenerateRoutes("pkg/router", "api"),
		generation.WithRouterOutlet("portal", "portal", generation.ServesSessions()),
		generation.GenerateHandlerTests("test/authz"),
		generation.WithRPC("pkg/rpc"),
		generation.WithConsolidatedHandlers("resources", true, "Beacon"),
		generation.WithPluralOverrides(map[string]string{"Lens": "Lenses"}),
		generation.CaserInitialismOverrides(map[string]bool{"GPS": true}),
		generation.WithSpannerEmulatorVersion("1.5.56"),
		generation.GenerateTypescript("web/console/src/app/core/service",
			generation.GenerateMetadata(),
			generation.GeneratePermissions(),
			generation.GenerateEnums(),
		),
		generation.GenerateTypescript("web/portal/src/app/core/service",
			generation.ForOutlet("portal"),
			generation.GenerateEnums(),
		),
	)
	if err != nil {
		return err
	}
	defer generator.Close()

	return generator.Generate()
}
