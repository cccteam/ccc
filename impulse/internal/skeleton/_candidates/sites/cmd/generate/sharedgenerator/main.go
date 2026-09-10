// Package main implements the shared generator: it reads pkg/sharedresources and emits
// its TypeScript into every site's web application, so the sites agree on the shared
// vocabulary. It generates no handlers or routes — each site serves its own resources.
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
		"pkg/sharedresources",
		[]string{"file://schema/migrations"},
		[]string{
			"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/sites/pkg/sharedresources",
		},
		generation.WithSpannerEmulatorVersion("1.5.56"),
		// One TypeScript target per site: adding a site means adding its target here,
		// and `impulse check` fails the build if a site is missed.
		generation.GenerateTypescript("apps/console/web/src/app/core/service/shared",
			generation.GenerateEnums(),
		),
		generation.GenerateTypescript("apps/portal/web/src/app/core/service/shared",
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
