package main

import (
	"context"

	"github.com/cccteam/ccc/resource/generation"
	"github.com/go-playground/errors/v5"
)

// newGenerator declares the console site's resource generator: the package it reads, the
// schema it reads them against, and what it emits (resource types, handlers, routes,
// authorization matrix, and TypeScript client). The generate program (main.go) runs it and
// warnings_test.go runs it in-process, so the two never drift.
func newGenerator(ctx context.Context) (generation.Generator, error) {
	generator, err := generation.NewResourceGenerator(
		ctx,
		"apps/console/pkg/resources",
		[]string{"file://schema/migrations"},
		generation.GenerateHandlers("apps/console/app"),
		// The router is generated from the outlet declarations: the console is the default
		// outlet, the staff auth's password sessions under /api with its browser application
		// at /.
		generation.GenerateRouter(),
		generation.GenerateRoutes("apps/console/pkg/router", "api",
			generation.Auth("github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/sites/pkg/auth/staff", generation.Password),
			generation.WebApp("/"),
			// The oldest release of the console this outlet still answers. The console sends the
			// release it was built from in X-Api-Version, and the server answers releases from
			// this one up to its own (APP_VERSION) and refuses the rest with 412, which the
			// console turns into a reload onto the current build. The first release is the
			// default, written out so the option is seen: raise it to the release that removed
			// what older builds of the console still read (a field, a method, a former name kept
			// by @formerly), and ThisRelease answers the server's own release alone, for a
			// release deployed inside a maintenance window.
			generation.OldestAnswered("0.0.1"),
		),
		generation.GenerateHandlerTests("apps/console/test/authz"),
		// Tenant-scoped resources and RPC methods are served under the tenant segment
		// pair, /api/tenants/{tenantID}/..., which the generator derives from the tenant
		// record: Tenant, the resource struct annotated @tenant in this site's resource
		// package. The tenant is the permission domain. Tenant existence is concealed: a
		// tenant the caller holds no grant in answers exactly like a tenant that does not
		// exist.
		generation.WithConcealedDomains(),
		generation.WithConsolidatedHandlers("resources", true),
		generation.WithSpannerEmulatorVersion("1.5.56"),
		generation.GenerateTypescript("apps/console/web/src/app/core/service",
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
