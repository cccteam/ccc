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
			"github.com/cccteam/ccc/resource/waystation/pkg/resources",
			"github.com/cccteam/ccc/resource/waystation/pkg/router",
			"github.com/cccteam/ccc/resource/waystation/pkg/rpc",
			"github.com/cccteam/ccc/resource/waystation/pkg/virtualresources",
			"github.com/cccteam/ccc/resource/waystation/pkg/computedresources",
		},
		generation.GenerateHandlers("app"),
		generation.GenerateRoutes("pkg/router", "api"),
		// The automation outlet is the machine REST API: structs annotated with
		// @outlet naming automation are served under /automation, which the router
		// composes behind API-key authentication instead of the browser session.
		// SensorReading and IngestSensorBatch are automation-ONLY.
		generation.WithRouterOutlet("automation", "automation"),
		generation.GenerateHandlerTests("handlertests"),
		// Domain-scoped resources and RPC methods are served under the waystation
		// segment pair: /api/waystations/{waystationID}/... . The waystation is the
		// permission domain, and Waystation is the tenant-record resource.
		generation.WithDomainRoute("waystations"),
		// Waystation existence is concealed: a station the caller holds no grant
		// in answers exactly like a station that does not exist.
		generation.WithConcealedDomains(),
		generation.WithRPC("pkg/rpc"),
		generation.WithVirtualResources("pkg/virtualresources"),
		generation.WithComputedResources("pkg/computedresources"),
		// Supplier (global) and Module (domain-scoped) are excluded from
		// consolidation so both standalone PATCH surfaces stay exercised alongside
		// the consolidated /api/resources handler.
		generation.WithConsolidatedHandlers("resources", true, "Supplier", "Module"),
		// NOTE: uncountable resource names (Staff, Equipment) cannot be expressed
		// today — a WithPluralOverrides entry whose plural equals its singular
		// generates colliding list/read method names. Finding filed; Asset and
		// StaffMember are the countable stand-ins.
		generation.WithSpannerEmulatorVersion("1.5.56"),
		generation.GenerateTypescript("gui/src/app/core/service",
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
