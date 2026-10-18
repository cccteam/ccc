package main

import (
	"context"

	"github.com/cccteam/ccc/resource/generation"
	"github.com/go-playground/errors/v5"
)

// newGenerator declares the application's resource generator: the package it reads, the
// schema it reads them against, and what it emits (resource types, handlers, routes,
// authorization matrix, and TypeScript client). The generate program (main.go) runs it and
// warnings_test.go runs it in-process, so the two never drift.
func newGenerator(ctx context.Context) (generation.Generator, error) {
	generator, err := generation.NewResourceGenerator(
		ctx,
		"pkg/resources",
		[]string{"file://schema/migrations"},
		generation.GenerateHandlers("app"),
		// The router is generated from the outlet declarations: the console is the default
		// outlet, the staff auth's directory sessions under /api with its browser application
		// at /; the portal is a second outlet, the members auth's directory and password
		// sessions under /portal/api with its browser application at /portal.
		generation.GenerateRouter(),
		generation.GenerateRoutes("pkg/router", "api",
			generation.Auth("github.com/impulseframework/harbor/pkg/auth/staff", generation.GoogleMethod("directory")),
			generation.WebApp("/"),
		),
		generation.WithRouterOutlet("portal", "portal/api",
			generation.Auth("github.com/impulseframework/harbor/pkg/auth/members", generation.AzureMethod("directory")),
			generation.WebApp("/portal"),
		),
		generation.GenerateHandlerTests("test/authz"),
		generation.WithConsolidatedHandlers("resources", true),
		generation.WithSpannerEmulatorVersion("1.5.56"),
		generation.GenerateTypescript("web/console/src/app/core/service",
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
