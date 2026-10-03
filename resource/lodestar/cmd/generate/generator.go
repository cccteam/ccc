package generate

import (
	"context"

	"github.com/cccteam/ccc/resource/generation"
	"github.com/go-playground/errors/v5"
)

// NewGenerator declares the application's resource generator: the packages it reads,
// the schema it reads them against, and the served router (GenerateRouter), where each
// outlet says how it authenticates and which browser application it serves. The
// generate program (./resourcegenerator) runs it, and the schema-warning test runs it
// in-process, so the two never drift.
//
// Demonstrates: GenerateRouter, WebApp.several, typescript.types-package.
func NewGenerator(ctx context.Context) (generation.Generator, error) {
	generator, err := generation.NewResourceGenerator(
		ctx,
		"pkg/resources",
		[]string{"file://schema/migrations"},
		generation.GenerateHandlers("app"),
		// The router is generated: the default outlet is the console, the crew auth's
		// password sessions under /console/api with the console's browser application
		// at /console. No application is mounted at /: an installed browser application
		// owns every URL under its start, so beside the portal the console sits under
		// its own path, and the generated router answers the root alone with a redirect
		// to it. Every session outlet's API sits under its application's mount path.
		generation.GenerateRouter(),
		generation.GenerateRoutes("pkg/router", "console/api",
			generation.Auth("github.com/cccteam/ccc/resource/lodestar/pkg/auth/crew", generation.Password),
			generation.WebApp("/console"),
		),
		// The droids outlet is the machine channel: structs annotated with @outlet
		// naming droids are served under /droids, which the router composes behind
		// API-key authentication (the App's DroidsAuth) instead of a browser session.
		generation.WithRouterOutlet("droids", "droids", generation.APIKey()),
		// The portal outlet is the clients' browser app: structs annotated with @outlet
		// naming portal are served under /portal/api behind the members auth, whose
		// people sign in through their company's Google directory, with their own
		// permission-digest and user-domains routes, and the portal's browser
		// application at /portal.
		generation.WithRouterOutlet("portal", "portal/api",
			generation.Auth("github.com/cccteam/ccc/resource/lodestar/pkg/auth/members", generation.OIDCGoogle),
			generation.WebApp("/portal"),
		),
		// Sector-scoped resources and methods are served under /console/api/sectors/{sectorID}/,
		// the segment and the parameter derived from the @tenant record (Sector, keyed by
		// ID); a sector the caller holds no grant in answers like one that does not exist.
		generation.WithConcealedDomains(),
		generation.WithRPC("pkg/rpc"),
		generation.WithVirtualResources("pkg/virtualresources"),
		generation.WithComputedResources("pkg/computedresources"),
		// The droid link's package declares the frame type a DroidReports column
		// holds; naming it lets the generator write the type's JSON and Spanner
		// methods beside it, so the type stays with the link it models.
		generation.WithTypes("pkg/telemetry"),
		generation.GenerateHandlerTests("test/authz"),
		// Client (global) and Hangar (sector-scoped) keep standalone PATCH surfaces
		// beside the consolidated handler.
		generation.WithConsolidatedHandlers("resources", true, "Client", "Hangar"),
		generation.WithSpannerEmulatorVersion("1.5.56"),
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
		return nil, errors.Wrap(err, "generation.NewResourceGenerator()")
	}

	return generator, nil
}
