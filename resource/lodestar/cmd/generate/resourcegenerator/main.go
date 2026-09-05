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
			"github.com/cccteam/ccc/resource/lodestar/pkg/resources",
			"github.com/cccteam/ccc/resource/lodestar/pkg/router",
			"github.com/cccteam/ccc/resource/lodestar/pkg/rpc",
			"github.com/cccteam/ccc/resource/lodestar/pkg/virtualresources",
			"github.com/cccteam/ccc/resource/lodestar/pkg/computedresources",
		},
		generation.GenerateHandlers("app"),
		generation.GenerateRoutes("pkg/router", "api"),
		// The portal outlet is the clients' browser app: structs annotated with
		// @outlet naming portal are served under /portal, behind the same PasswordAuth
		// composed around that prefix, with their own permission-digest and
		// user-domains routes (ServesSessions) so the portal's generated client can
		// bootstrap. Its members are Missions, DistressCalls, ClientContacts, and
		// StandDownMission, all @outlet(default, portal).
		generation.WithRouterOutlet("portal", "portal", generation.ServesSessions()),
		// The droids outlet is the machine channel: structs annotated with @outlet
		// naming droids are served under /droids, which the router composes behind
		// API-key authentication instead of the browser session. DroidReport and
		// IngestDroidReports are droids-ONLY; Consignment and ReleaseConsignment are
		// shared with the default outlet.
		generation.WithRouterOutlet("droids", "droids"),
		generation.GenerateHandlerTests("test/authz"),
		// Sector-scoped resources and RPC methods are served under the sector segment
		// pair: /api/sectors/{sectorID}/... . The sector is the permission domain, and
		// Sector is the tenant-record resource, so the parameter derives from its key.
		generation.WithDomainRoute("sectors"),
		// Sector existence is concealed: a sector the caller holds no grant in answers
		// exactly like a sector that does not exist — Cinder is dark on most charts.
		generation.WithConcealedDomains(),
		generation.WithRPC("pkg/rpc"),
		generation.WithVirtualResources("pkg/virtualresources"),
		generation.WithComputedResources("pkg/computedresources"),
		// Client (global) and Hangar (sector-scoped) are excluded from consolidation
		// so both standalone PATCH surfaces stay exercised alongside the consolidated
		// /api/resources handler.
		generation.WithConsolidatedHandlers("resources", true, "Client", "Hangar"),
		generation.WithSpannerEmulatorVersion("1.5.56"),
		// Two TypeScript targets in one run: the crew console on the default outlet,
		// and the client portal filtered to the portal outlet's members.
		generation.GenerateTypescript("web/console/src/app/core/service",
			generation.GenerateMetadata(),
			generation.GeneratePermissions(),
			generation.GenerateEnums(),
		),
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
