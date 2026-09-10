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
			"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/tenanted/pkg/resources",
			"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/tenanted/pkg/router",
		},
		generation.GenerateHandlers("app"),
		generation.GenerateRoutes("pkg/router", "api"),
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
