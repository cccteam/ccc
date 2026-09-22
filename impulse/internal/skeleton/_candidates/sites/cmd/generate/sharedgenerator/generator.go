package main

import (
	"context"

	"github.com/cccteam/ccc/resource/generation"
	"github.com/go-playground/errors/v5"
)

// newGenerator declares the shared vocabulary's resource generator: the package it reads,
// the schema it reads them against, and what it emits (TypeScript enumerations for every
// site). The generate program (main.go) runs it and warnings_test.go runs it in-process,
// so the two never drift.
func newGenerator(ctx context.Context) (generation.Generator, error) {
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
		return nil, errors.Wrap(err, "generation.NewResourceGenerator()")
	}

	return generator, nil
}
