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
			"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/outlets/pkg/resources",
			"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/outlets/pkg/router",
		},
		generation.GenerateHandlers("app"),
		generation.GenerateRoutes("pkg/router", "api"),
		// The portal outlet is a second browser surface: structs annotated with @outlet
		// naming portal are served under /portal/api behind the same session handling
		// the console uses, and ServesSessions registers the permission-digest and
		// user-domains routes there so the portal's generated client can bootstrap.
		generation.WithRouterOutlet("portal", "portal/api", generation.ServesSessions()),
		// The machines outlet is the machine REST API: structs annotated with @outlet
		// naming machines are served under /machines, which the router composes behind
		// API-key authentication instead of a browser session.
		generation.WithRouterOutlet("machines", "machines"),
		generation.GenerateHandlerTests("test/authz"),
		// Tenant-scoped resources and RPC methods are served under the tenant segment
		// pair: /api/tenants/{tenantID}/... . The tenant is the permission domain, and
		// Tenant is the tenant-record resource.
		generation.WithDomainRoute("tenants"),
		// Tenant existence is concealed: a tenant the caller holds no grant in answers
		// exactly like a tenant that does not exist.
		generation.WithConcealedDomains(),
		generation.WithConsolidatedHandlers("resources", true),
		generation.WithSpannerEmulatorVersion("1.5.56"),
		generation.GenerateTypescript("web/console/src/app/core/service",
			generation.GenerateMetadata(),
			generation.GeneratePermissions(),
			generation.GenerateEnums(),
		),
		// The portal's client: the portal outlet's members only, addressed under its
		// prefix.
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
