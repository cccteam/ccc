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
			// The oldest release of the console the server still answers. A console build
			// sends the release it was built from in X-Api-Version, and the generated router
			// answers releases from this one up to the server's own (APP_VERSION through
			// ServerVersion), refusing the rest with 412 before any handler runs; a build
			// with no header, and a dev build or a dev server, is never refused. This is the
			// first release, which is also what an outlet without the option answers from,
			// written out so the place to raise it is on record: a developer removes a
			// @formerly annotation or a field and raises this to the release that stopped
			// sending the old name, and consoles built before it are told to reload.
			//
			// Demonstrates: api.version-refusal.
			generation.OldestAnswered("0.1.0"),
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
		// Every request body the generated router serves is bounded at 2 MiB, set
		// here once (4 MiB when an application sets nothing): the resource, session,
		// consolidated and plain JSON routes through the router, an RPC method through
		// its handler unless it declares its own (IssueBulletin's @rpc(max: 64KB)),
		// while uploads and live routes bound their own bodies beside it.
		//
		// Demonstrates: generation.body-limit.
		generation.WithBodyLimit(2<<20),
		// The beacon pulses, mounted by hand through hooks.Root under /beacons/
		// (pkg/router/hooks.go), are asked all day by every droid, so the prefix is
		// declared quiet here, once: every request under it writes its request log on
		// event (a pulse answered 200 writes no entry; one answered 404 does) and records
		// no span. The generated router hands the prefix to the request logger and the
		// tracer, and the release file lists it for the stack's own log exclusion.
		// Everything else keeps today's behavior: an entry for every request, spans
		// following the front end, since nothing declares otherwise; IngestDroidReports
		// declares its own word on @rpc.
		//
		// Demonstrates: generation.request-log, generation.traces.
		generation.WithMountedRoutes("/beacons/", generation.LogOnEvent(), generation.TracesOff()),
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
