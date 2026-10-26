// Package ledger records the steps that take an application from the framework pins an
// earlier impulse skeleton named to the pins the current one names. A step is a coherent
// pin set, the resource, access, session and accesstypes versions the skeleton's go.mod
// named, together with the recipes that take an application from the step before it, or a
// note that none is needed. impulse upgrade walks the ledger step by step, reading where
// the application stands from the framework pins in its go.mod and committing each step as
// it goes; the pins are the checkpoint, and nothing else records progress: the framework
// pins say which step's set the application builds against, and the tool pin, moved after
// each step, tells apart the steps that move no framework pin. The impulse tool
// pin moves with its step: a step names the impulse release that added it, the walk moves
// the pin to that release after the step's framework pins, and the step is rendered and
// checked by it. The tool directive puts impulse's own requirements into the application's
// build list, so a pin moved ahead of a step would drag the framework pins past the steps
// between and the walk would read a position it never reached. A release that changes
// nothing an application builds against adds no step, and the walk moves the pin to it
// last, by itself.
//
// A step is appended when the skeleton's pins move or a recipe is needed, and never
// otherwise; the ledger's test holds the last step's pins to the skeleton's, so a pin bump
// without its step fails the build, and holds impulse's own requirements to the last
// step's pins, so a release whose tool directive would carry an application past the
// ledger fails the build until its step is recorded. A step's pins hold together: no
// module a step pins requires a sibling the step pins beyond the step's pin for it, read
// from each pinned release's own go.mod (Consistent), since Go builds one list and the
// walk moves a step's pins in one go get, which refuses a set no list holds. Two pull
// requests released together record one step, not two whose first cannot be built. A
// breaking change in resource, access, session or accesstypes is not done until the step
// that carries it records a recipe for it here, or says none is needed.
//
// A step pins the web library beside the Go modules: the versions of @cccteam/resource,
// @cccteam/resource-angular and @cccteam/resource-firestore the skeleton's web roots named
// when the step's release shipped (Web). The server libraries and the browser library of
// one release are written for each other, so the walk moves them together: at a step, every
// web root of the application (each directory holding an angular.json whose package.json
// names the library) has its three pins rewritten in place and bun install run there, so
// bun.lock records the registry versions, and the step's commit carries both files. A web
// pin at or beyond the step's stays, as a Go pin does. The ledger's test holds the last
// step's web pins to the candidates' web roots, so a web repin without its step fails the
// build, and the web-pins check holds an application's web roots to the step it stands at.
package ledger

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/go-playground/errors/v5"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"

	"github.com/cccteam/ccc/impulse/app"
	"github.com/cccteam/ccc/impulse/internal/check"
	"github.com/cccteam/ccc/impulse/internal/recipe"
	"github.com/cccteam/ccc/impulse/internal/transition"
)

// FrameworkPrefix is the module path prefix of the Impulse Framework libraries, whose pins
// in an application's go.mod say which step the application stands at.
const FrameworkPrefix = "github.com/cccteam/"

// WebPackages are the web library's packages a step pins in every web root's
// package.json: the client, its Angular components and its Firestore live service.
var WebPackages = []string{"@cccteam/resource", "@cccteam/resource-angular", "@cccteam/resource-firestore"}

// Step is one step the ledger records.
type Step struct {
	// Pins are the framework modules' versions the skeleton named at the step, by module
	// path: what an application at this step builds against.
	Pins map[string]string
	// Web are the web library's versions the skeleton's web roots named at the step, by
	// package (WebPackages): what an application at this step's browser applications
	// install. The walk moves every web root's pins to them with the step's Go pins. nil
	// only on a step recorded before the web library was pinned; once a step names them,
	// every later step does.
	Web map[string]string
	// Recipes are the code changes the step needs of an application at the step before
	// it, applied in order; none when the pin bump is the whole step.
	Recipes []Recipe
	// Note says in one line what the step changes for an application, for the plan and
	// the commit; "no code change is needed" when the pins are all that move.
	Note string
	// Pending marks a step whose pins name pushed commits (pseudo-versions) because the
	// wave's releases do not exist yet: the skeleton needed a change of a sibling module
	// not yet released, so it pins the commit. The release's repin moves the pins to the
	// tags and clears the mark. impulse does not release with a pending last step: the
	// release pins check refuses it, and this ledger's validation holds the mark to the
	// pins both ways.
	Pending bool
	// Impulse is the impulse release that added the step, "v0.2.0": the walk moves the
	// tool pin to it after the step's framework pins and renders and checks the step with
	// it (go tool impulse), so what the tool directive puts into the application's build
	// list never runs past the step's set. The release's own pull request adds the step,
	// so a pending step names the release that pull request will take, as its pins name
	// the pushed commits. "" on the steps recorded before the pin moved with its step;
	// once a step names a release, every later step does.
	Impulse string
}

// Recipe is one code change a step needs: a detector that names what in the application
// is in the old form, and an edit that moves only what it finds, so the recipe is safe to
// run twice and safe on an application whose pins were bumped by hand ahead of its code.
type Recipe interface {
	// Name is the recipe's short name (paging).
	Name() string
	// Detect names what in the application is in the old form, one line each (file:line:
	// what); none when the recipe has nothing to do here.
	Detect(ctx context.Context, a *app.App) ([]string, error)
	// Apply edits the application where the old form is found and reports what it did and
	// what it left to the agent.
	Apply(ctx context.Context, a *app.App, exec check.Execer) (*transition.Change, error)
	// Meaning says what the change means in this framework, for the brief.
	Meaning() string
}

// Steps are the recorded steps, oldest first. The first is the first published impulse
// release's pin set (impulse v0.1.0). A step's pins are written one module per line, the
// module named without the framework prefix as Short prints it.
var Steps = []Step{
	{
		Pins: pins(`
			access v0.10.1
			ccc v0.3.3
			ccc/accesstypes v0.6.0
			ccc/resource v0.11.0
			ccc/tracer v0.1.7
			db-initiator v0.4.1
			httpio v0.7.19
			logger v0.1.27
			session v0.12.0
		`),
		Web: web(`
			@cccteam/resource 0.0.5
			@cccteam/resource-angular 0.0.5
			@cccteam/resource-firestore 0.0.3
		`),
		Note: "the first impulse release's pins; an application created by it stands here already",
	},
	{
		Pins: pins(`
			access v0.10.1
			ccc v0.3.3
			ccc/accesstypes v0.6.0
			ccc/cloud v0.1.0
			ccc/resource v0.12.0
			ccc/tracer v0.2.0
			db-initiator v0.4.1
			httpio v0.7.19
			logger v0.1.27
			session v0.12.0
		`),
		Web: web(`
			@cccteam/resource 0.0.5
			@cccteam/resource-angular 0.0.5
			@cccteam/resource-firestore 0.0.3
		`),
		Recipes: []Recipe{recipe.CloudDriver{}},
		Note:    "the cloud driver builds the logs and traces, and the generated router installs tracing and the request logger",
		Impulse: release("v0.2.0"),
	},
	{
		Pins: pins(`
			access v0.10.3
			ccc v0.3.3
			ccc/accesstypes v0.6.0
			ccc/cloud v0.1.0
			ccc/resource v0.13.0
			ccc/tracer v0.2.0
			db-initiator v0.4.1
			httpio v0.7.21
			logger v0.1.27
			session v0.12.2
		`),
		Web: web(`
			@cccteam/resource 0.0.5
			@cccteam/resource-angular 0.0.5
			@cccteam/resource-firestore 0.0.3
		`),
		Note:    "request bodies are bounded in one place: the generated router applies the application's limit (WithBodyLimit, 4 MiB when unset) and an RPC method may declare its own with @rpc(max:); regeneration carries it, no code change is needed",
		Impulse: release("v0.3.0"),
	},
	{
		Pins: pins(`
			access v0.10.3
			ccc v0.3.3
			ccc/accesstypes v0.6.0
			ccc/cloud v0.2.1
			ccc/resource v0.13.0
			ccc/tracer v0.2.0
			db-initiator v0.4.1
			httpio v0.7.21
			logger v0.1.27
			session v0.12.2
		`),
		Web: web(`
			@cccteam/resource 0.0.5
			@cccteam/resource-angular 0.0.5
			@cccteam/resource-firestore 0.0.3
		`),
		Note:    "cloud v0.2.1 adds the settings declaration of cloud/gcp, which impulse's env-template check reads; impulse v0.3.1 requires it, so its tool pin carries it, and no code change is needed",
		Impulse: release("v0.3.1"),
	},
	{
		Pins: pins(`
			access v0.10.3
			ccc v0.3.3
			ccc/accesstypes v0.6.0
			ccc/cloud v0.2.1
			ccc/resource v0.14.0
			ccc/tracer v0.3.0
			db-initiator v0.4.1
			httpio v0.7.21
			logger v0.1.30
			session v0.12.2
		`),
		Web: web(`
			@cccteam/resource 0.0.5
			@cccteam/resource-angular 0.0.5
			@cccteam/resource-firestore 0.0.3
		`),
		Recipes: []Recipe{recipe.ProviderDrivers{}},
		Note:    "the database, the live service and the job starter open through provider drivers (resource/database/spanner, resource/live/firestore, resource/jobs/cloudrun) whose settings the configuration embeds under the variable names already in use, each bound under a neutral import alias (database, liveservice, jobstarter, cloud) so moving to another provider is the import line; the recipe moves pkg/config onto them and renames its wrappers (DatabaseSettings, LiveSettings); request log words and trace settings are declared per surface: WithRequestLog sets the application default, OutletRequestLog and OutletTraces an outlet's, WithMountedRoutes a prefix mounted by hand, and @rpc(log:), @rpc(trace:), @file and @schedule(log:) a route's own; the generated router applies them, the tracer samples by them, and the release file lists the surfaces; an application that declares nothing keeps its behavior, and regeneration carries the generated test",
		Impulse: release("v0.4.0"),
	},
	{
		Pins: pins(`
			access v0.11.1
			ccc v0.3.5
			ccc/accesstypes v0.7.0
			ccc/cloud v0.2.1
			ccc/resource v0.15.0
			ccc/tracer v0.3.0
			db-initiator v0.4.3
			httpio v0.7.22
			logger v0.1.30
			session v0.13.1
		`),
		Web: web(`
			@cccteam/resource 0.0.8
			@cccteam/resource-angular 0.0.9
			@cccteam/resource-firestore 0.0.6
		`),
		Recipes: []Recipe{recipe.MigrationsReader{}},
		Impulse: release("v0.5.1"),
		Note:    "the skeletons' login pages compose the library's login component (ccc-login-methods from @cccteam/resource-angular 0.0.7), which renders every method the outlet registers from the methods route and owns the sign-in, so a page names no path and adds a method's button by itself (an application's own page moves to it by hand, the skeleton's page being the pattern); the skeletons' consoles compose the library's user-management component (ccc-user-management from @cccteam/resource-angular 0.0.8) on a Users page over the auth's user surface, drawn where the digest grants List on its users resource and listed in the navigation behind the same grant (the tenanted console draws the selected tenant's partition beside the global one, the outlets portal its members'); an application's own console adds the page by hand, the skeleton's being the pattern; the schema sequence creates the migrations_reader database role, which the pipeline's migration guard reads each environment's migration version through (a migration added at the sequence's next free index where none does); an auth signs in through the methods registered on one session Auth (session.NewAuth): the skeletons' auth packages construct it over the sessions, users, identities, auth events, pending sign-ins and sign-in attempts tables and register each method in a file of its own; the auth's migration creates the sign-in attempts table, the attempt limiter's count of failed password attempts per sign-in name, and the auth names it (session.WithSignInAttemptsTableName), so two auths never count in the library's default table; the generator's Auth option takes method declarations (generation.PasswordMethod(), AzureMethod(name), GoogleMethod(name)) and the router serves the methods, session and pending routes the login page renders from; no recipe moves an application off the single-method session types, which the library keeps under session/legacy: an application on them is re-created from the skeleton; the generator serves each auth's users, seats and roles as resources (resource.UserSurface), and the skeletons' configurer hands the App the stores each surface serves over (StaffUserStores) for the generated constructor; a deployed environment's first administrator is seeded by the migrate command (pkg/deploy SeedAdministrators) while an auth's users table holds no rows: a password auth gets administrator with the password the stack generates (APP_<AUTH>_ADMINISTRATOR_PASSWORD, secret:\"generated\"), an auth whose roles the application assigns gets the administrator role memberships under the sign-in name the placement names (APP_<AUTH>_ADMINISTRATOR), both the data level's to declare; an application adds foothold.go to each auth package and the step to its migrate command by hand; every tenant-scoped page of a tenanted console lives under the tenant segment in the URL (`tenants/:tenantID`, which the generated client exports as DomainRoutePath beside DomainRouteParam, whenever anything is served under the tenant), mirroring the API, with a guard on the segment as the one writer of the tenant service's current (from the URL, checked against the session's tenant list), the header's picker opening the same page under the chosen tenant, a refusal page under the tenant's URL for a tenant the session cannot reach, and the user surface's tenant partition served as the tenant's own Users page with the route's tenant as the component's domain; add tenancy copies the guard and the refusal page beside the service, and an application's own console takes the shape by hand, the skeleton's being the pattern; the tenant record declares who creates tenants, apart from who enters one: @tenant alone keeps it to an administrator through the record's create path (the default, unchanged), @tenant(creation: self-service) lets a person who signed in holding no tenant create their own through the generated create-<record> route and become its administrator, and @tenant(creation: both) keeps both ways; self-service needs a registration admitting open sign-up on the outlet's Auth (GoogleMethod(name).OpenSignUp() or AzureMethod(name).OpenSignUp(), which impulse add method --accounts open declares) and the App's <Record>AdministratorRoles(), and an application that wants it annotates the record, declares the registration, names the roles and adds the create-your-tenant page to its console by hand, the tenanted skeleton's being the pattern; the skeletons' web pins move to @cccteam/resource 0.0.8, @cccteam/resource-angular 0.0.9 and @cccteam/resource-firestore 0.0.6 (the login page's sentences for the attempt limit with its wait and for the tenant refusals, and the config-driven pages' row links and after-create navigation under the page's tenant); resource.SessionPermissions takes the application's tenant declarations beside its roster (resource.TenantDeclarations: what a tenant declares about who may enter it, read by domain; nil while the application keeps none, and every tenant admits every session), so a tenant is offered to a session only when the session may enter it and the generated domain guard and consolidated dispatcher ask the session's reach (resource.UserPermissions.Reaches) before the foothold; the skeletons pass nil, and an application adds the argument to its SessionPermissions call by hand; the tenant record declares who may enter a tenant, as data on the record: the directories the tenant admits its people through (a child record annotated @tenantDirectory, one row per directory with a bare @domain column, a @directoryMethod column and a @directoryConnection column, edited under its own grants with the change events as the audit) and whether they are the only way in (a boolean column annotated @directoryOnly on the record); the generator emits New<Record>DeclarationReader, the reader of a tenant's declaration in the sign-in's transaction, and the roster keeps every tenant's declaration beside its key (Declarations, reloaded by the records' generated write paths and handed to SessionPermissions in place of nil); every auth's New sets the library's sign-in rule over the reader its settings carry (signin.TenantPolicy with the memberships read through resource.SignInRoles, and signin.AdmitWithoutTenant() where the record is created by its own people), and the console lists, declares and withdraws a tenant's directories and sets directory-only on a Sign-in page under the tenant's routes; a Google registration admitting any Google account names any Workspace beside it (google.Workspaces(google.AnyWorkspace(), google.AnyGoogleAccount())) so a Workspace account's link carries its domain, what a declaration names a Google directory by; an application adds the directories record, the column, the migration, the reader in each auth's settings, the rule in each auth's New, the Declarations argument and the page by hand, the tenanted skeleton's being the pattern, and declares each directory method it registers on its development tenants in the seed, or the people who sign in through it reach no tenant",
	},
	{
		Pins: pins(`
			access v0.11.1
			ccc v0.3.5
			ccc/accesstypes v0.7.0
			ccc/cloud v0.2.1
			ccc/resource v0.15.2
			ccc/tracer v0.3.0
			db-initiator v0.4.3
			httpio v0.7.22
			logger v0.1.30
			session v0.13.4
		`),
		Web: web(`
			@cccteam/resource 0.0.8
			@cccteam/resource-angular 0.0.9
			@cccteam/resource-firestore 0.0.6
		`),
		Impulse: release("v0.5.4"),
		Note:    "every auth mails, and a person is reached by address: an administrator invites a person by email with the roles the invitation seats (the generated <Auth>Invitations resource, on the global surface and under each tenant, which the role file grants), the invitee follows the mailed link and accepts it signed in or signing up with a new password, and the roles are seated once the transaction that consumed the invitation has committed (SetInvitationAccepted over resource.SeatInvited, set in each auth's New); a signed-in person adds an email address and proves it by following the link sent to it, and the address is trusted for nothing until then (the users table's Email, NormalizedEmail and EmailVerifiedAt columns, which the auth's users migration creates, and the email and emailVerifiedAt fields of the users resource); the password method may admit strangers who sign up from a mailed link (session.OpenSignUp() on the registration, .OpenSignUp() on the declaration, which impulse add auth and add method --accounts open write, and the router's sign-up routes) and may take the address as the sign-in name (session.UsernameIsEmail(), .UsernameIsEmail(), --username-is-email), when a verified change of address renames the user with its seats (SetUserRenamed over resource.SeatsFollowRename); the auth's sessions migration creates the <Prefix>SessionMailLinks table the mailed links live in and the auth names it (session.WithMailLinksTableName); the data level opens the application's one mail sender (session/mail: the log in development, the Google Workspace SMTP relay, session/mail/workspace, where the deployment names a mailbox, APP_MAIL_MAILBOX with the secret APP_MAIL_APP_PASSWORD) and hands every auth its settings, Mail and MailLoginURL, the login page of the surface bound to the auth under the application's public address (APP_PUBLIC_URL, required, which bedrock derives from the site's canonical hostname and the development template sets to the dev proxy); the users surface takes the tenant declarations (resource.WithTenantDirectories) so a tenant seats a person its own directory vouches for by sign-in name; removing a person's last seat anywhere deletes the account with its identity links; the session library's invitation acceptance runs after the consuming transaction commits, outside it; an application adds the mail links and email migrations under each auth's prefix, the Mail and MailLoginURL settings and the hooks in each auth's New, the sender and the variables in its data level, the invitation handlers on its Handlers and the invitation grants in its role files by hand, the skeletons' being the pattern; the skeletons' and Lodestar's web pins moved to @cccteam/resource 0.0.9, @cccteam/resource-angular 0.0.10 and @cccteam/resource-firestore 0.0.7 with impulse v0.5.5, which added no step, so this step's web pins are the ones v0.5.4 shipped and the v0.5.6 step carries the move (the login page's invitation, sign-up and address-verification flows, the user-management page's addresses and invitations, and the email-address component), and the consoles' dashboards carry the signed-in person's address through that component, added, changed and confirmed by a mailed link",
	},
	{
		Pins: pins(`
			access v0.11.1
			ccc v0.3.5
			ccc/accesstypes v0.7.0
			ccc/cloud v0.2.1
			ccc/resource v0.15.4
			ccc/tracer v0.3.0
			db-initiator v0.4.3
			httpio v0.7.22
			logger v0.1.30
			session v0.13.4
		`),
		Web: web(`
			@cccteam/resource 0.0.10
			@cccteam/resource-angular 0.0.11
			@cccteam/resource-firestore 0.0.8
		`),
		Impulse: release("v0.5.6"),
		Note:    "every answer of an outlet names the server's release in X-Api-Version: the generated router puts resource.ReleaseHeader on every outlet ahead of the session handling (the API-key and scheduled outlets included), so the methods list a login page reads before any sign-in carries the release, a browser application shows which release it talks to, and curl shows it; the version check (resource.CheckAPIVersion) refuses as before and keeps naming the release on its refusal; an application regenerates its router (go generate ./...) and changes nothing by hand; the skeletons' and Lodestar's web pins move to @cccteam/resource 0.0.10, @cccteam/resource-angular 0.0.11 and @cccteam/resource-firestore 0.0.8 (the client keeps the release the server names, and the release line, ccc-release from @cccteam/resource-angular/ui-release, stands under every login card and in the footer of every page, which an application adds by hand as the skeletons do)",
	},
	{
		Pins: pins(`
			access v0.11.1
			ccc v0.3.5
			ccc/accesstypes v0.7.0
			ccc/cloud v0.2.1
			ccc/resource v0.15.4
			ccc/tracer v0.3.0
			db-initiator v0.4.3
			httpio v0.7.22
			logger v0.1.30
			session v0.13.4
		`),
		Web: web(`
			@cccteam/resource 0.0.10
			@cccteam/resource-angular 0.0.12
			@cccteam/resource-firestore 0.0.8
		`),
		Impulse: release("v0.5.7"),
		Note:    "signing out is a route: the skeletons' and Lodestar's web pins move to @cccteam/resource-angular 0.0.12, whose sign-out page (ccc-sign-out from @cccteam/resource-angular/auth-sign-out) stops the idle watch, ends the session and forwards the browser to the login page, and the skeletons serve it at /logout, the header's Sign out a link to it and the idle watch sending the browser there when the session times out; the login page ends no session on arrival, so a mailed link, a bookmark or the back button signs nobody out, and the library's login component offers a session it finds standing Continue or Sign out in the form's place (the change form to one marked for a password change); an application adds the route, points its sign-out control at it as a link and drops the logout from its login page's constructor by hand, the skeletons' being the pattern",
	},
	{
		Pins: pins(`
			access v0.11.1
			ccc v0.3.5
			ccc/accesstypes v0.7.0
			ccc/cloud v0.2.1
			ccc/resource v0.15.4
			ccc/tracer v0.3.0
			db-initiator v0.4.3
			httpio v0.7.22
			logger v0.1.30
			session v0.13.5
		`),
		Web: web(`
			@cccteam/resource 0.0.10
			@cccteam/resource-angular 0.0.13
			@cccteam/resource-firestore 0.0.8
		`),
		Impulse: release("v0.5.9"),
		Note:    "the auths' mail may come from an address other than the account that logs in to the relay, so a no-reply alias of a real account serves as the sender without a licensed mailbox of its own: pkg/config/data.go declares MailFrom (APP_MAIL_FROM, optional) beside the mailbox and hands the driver &workspace.Settings{Mailbox: env.MailMailbox, From: env.MailFrom, AppPassword: env.MailAppPassword, Name: …}, since workspace.New now takes a pointer to its settings; .envrc.template lists the variable; the stack carries mail_from beside mail_mailbox (bedrock)",
	},
	{
		Pins: pins(`
			access v0.11.1
			ccc v0.3.5
			ccc/accesstypes v0.7.0
			ccc/cloud v0.2.1
			ccc/resource v0.15.4
			ccc/tracer v0.3.0
			db-initiator v0.4.3
			httpio v0.7.22
			logger v0.1.30
			session v0.13.6
		`),
		Web: web(`
			@cccteam/resource 0.0.10
			@cccteam/resource-angular 0.0.13
			@cccteam/resource-firestore 0.0.8
		`),
		Impulse: release("v0.5.10"),
		Note:    "the Workspace mail driver introduces itself to the relay with the mailbox's domain (session v0.13.6): Google's relay closed the connection on the Go client's default EHLO localhost, so every send from a deployed application answered 'the message could not be sent'; nothing to change in the application",
	},
	{
		Pins: pins(`
			access v0.11.1
			ccc v0.3.5
			ccc/accesstypes v0.7.0
			ccc/cloud v0.2.1
			ccc/resource v0.15.4
			ccc/tracer v0.3.0
			db-initiator v0.4.3
			httpio v0.7.22
			logger v0.1.30
			session v0.13.7
		`),
		Web: web(`
			@cccteam/resource 0.0.11
			@cccteam/resource-angular 0.0.15
			@cccteam/resource-firestore 0.0.9
		`),
		Recipes: []Recipe{recipe.SyncsRoles{}},
		Impulse: release("v0.5.11"),
		Note:    "a directory method whose registration writes the person's roles from the directory at every sign-in (RoleSync: a Microsoft Entra token's application roles, or the Google Groups the group prefix names) declares .SyncsRoles() on every outlet's Auth option, as impulse add auth and add method now write it beside the method file's RoleSync, so the generated user-management surface shows such a person's roles as the directory's and never writes them (it offered them as seats an administrator may remove, which the directory put back at the next sign-in); the recipe chains the flag on every declaration that lacks it where the method file syncs roles, the router regenerates, and impulse check (auths-wired) holds the declaration and the registration to the same rule from here on; and session v0.13.7: the mailed messages (invitation, address confirmation, sign-up and the two notices) read as instructions, say how long the link is good for as a duration instead of a UTC deadline, and end with what to do if it was not the reader; nothing to change in the application",
	},
	{
		Pins: pins(`
			access v0.11.1
			ccc v0.3.5
			ccc/accesstypes v0.7.0
			ccc/cloud v0.2.1
			ccc/resource v0.15.4
			ccc/tracer v0.3.0
			db-initiator v0.4.3
			httpio v0.7.22
			logger v0.1.30
			session v0.13.8
		`),
		Web: web(`
			@cccteam/resource 0.0.12
			@cccteam/resource-angular 0.0.16
			@cccteam/resource-firestore 0.0.10
		`),
		Impulse: release("v0.5.12"),
		Note:    "session v0.13.8, and the web pins move with the step to @cccteam/resource 0.0.12, @cccteam/resource-angular 0.0.16 and @cccteam/resource-firestore 0.0.10 (the walk rewrites them in every web root's package.json and runs bun install there): the login page keeps showing an invitation after a refused directory sign-in (the methods listing names the invitation the browser carries), offers only the directory buttons that could admit the invited address, puts \"Create an account with a password\" last, and says in plain words why a sign-in was refused; an invitation to an address no sign-in method of the auth could admit is refused when it is sent, with a sentence naming the domain; a sign-in that is refused leaves the invitation it carried standing; nothing to change in the application by hand",
	},
	{
		Pins: pins(`
			access v0.11.1
			ccc v0.3.5
			ccc/accesstypes v0.7.0
			ccc/cloud v0.2.1
			ccc/resource v0.15.4
			ccc/tracer v0.3.0
			db-initiator v0.4.3
			httpio v0.7.22
			logger v0.1.30
			session v0.13.9
		`),
		Web: web(`
			@cccteam/resource 0.0.12
			@cccteam/resource-angular 0.0.16
			@cccteam/resource-firestore 0.0.10
		`),
		Impulse: release("v0.5.13"),
		Note:    "session v0.13.9: a directory sign-in resolves to the user who holds its confirmed address when no user is under the sign-in name (a password user is held for the password confirmation, which links the identity to that user; a user without a password is linked outright), and a new user is never provisioned over an address another user holds confirmed; nothing to change in the application by hand",
	},
	{
		Pins: pins(`
			access v0.11.1
			ccc v0.3.5
			ccc/accesstypes v0.7.0
			ccc/cloud v0.2.1
			ccc/resource v0.15.10
			ccc/tracer v0.3.0
			db-initiator v0.4.3
			httpio v0.7.22
			logger v0.1.30
			session v0.13.10
		`),
		Web: web(`
			@cccteam/resource 0.0.12
			@cccteam/resource-angular 0.0.16
			@cccteam/resource-firestore 0.0.10
		`),
		Impulse: release("v0.5.16"),
		Note:    "session v0.13.10 and resource v0.15.10, which retires APIKey(): a machine outlet declares the auth whose Auth issues its bearer tokens, Tokens(importPath), and the generated router carries the token check, the device flow's endpoints under the outlet's prefix and the token routes on the issuing auth's session outlet; an application regenerates its router, declares Tokens on each machine outlet, gives its auth package the device and token handlers, the tokens and device codes tables and the outlet's registration (session.RegisterMachineOutlet beside the sign-in methods, as the outlets skeleton's staff package shows), and replaces its hand-written <Outlet>Auth middleware with the <Outlet>Tokens getter returning that registration; a configured machine key becomes a token the application issues",
	},
	{
		Pins: pins(`
			access v0.11.1
			ccc v0.3.5
			ccc/accesstypes v0.7.0
			ccc/cloud v0.2.1
			ccc/resource v0.15.11
			ccc/tracer v0.3.0
			db-initiator v0.4.3
			httpio v0.7.22
			logger v0.1.30
			session v0.13.10
		`),
		Web: web(`
			@cccteam/resource 0.0.13
			@cccteam/resource-angular 0.0.17
			@cccteam/resource-firestore 0.0.11
		`),
		Impulse: release("v0.5.18"),
		Note:    "the console carries the token page and the device approval page: the generated descriptor of a console whose auth issues tokens names the five routes and the machine outlets it issues for, and the auth names its device page (Settings.DevicePage, passed as session.WithDeviceVerificationURL; the data level names \"<mount>/device\", which the login-pages check holds). An application with a machine outlet mounts <ccc-tokens /> at <mount>/tokens (a navigation item without a permission) and <ccc-device-approval /> at <mount>/device, both from @cccteam/resource-angular/ccc-tokens, by hand per the outlets-flavor skeleton; the web pins move with this step to the ccc-lib release that carries the entry.",
	},
}

// validatePending holds a step's Pending mark to its pins: a pin at a pseudo-version
// names a pushed commit, which only a pending step may carry, and a pending step carries
// at least one, so the repin that moves the pins to the tags also clears the mark.
func validatePending(s *Step, at string) error {
	var pushed []string
	for _, name := range s.PinNames() {
		if module.IsPseudoVersion(s.Pins[name]) {
			pushed = append(pushed, name+" at "+s.Pins[name])
		}
	}
	switch {
	case len(pushed) > 0 && !s.Pending:
		return errors.Newf("%s pins %s, a pushed commit, and is not marked pending: a step whose pins name commits says so, and the release's repin clears it", at, pushed[0])
	case len(pushed) == 0 && s.Pending:
		return errors.Newf("%s is marked pending and pins no pushed commit: the repin that moved it to the tags clears the mark", at)
	}

	return nil
}

// validateImpulse holds a step's Impulse to a release: "" or a canonical version with no
// pre-release or build part. A pushed commit's pseudo-version is refused: a pending step
// names the release its pull request will take, not the commit its pins name.
func validateImpulse(s *Step, at string) error {
	if s.Impulse == "" {
		return nil
	}
	if !IsRelease(s.Impulse) {
		return errors.Newf("%s names impulse %q, which is not a release: a step names the tagged version of the impulse that added it (v0.2.0), or the version its pull request will take while it is pending", at, s.Impulse)
	}

	return nil
}

// validateOrder holds a step to the one before it: no pin moving backward, a pin moved
// or a recipe named, and the release it names at or after the step before's, named
// whenever the step before names one.
func validateOrder(s, prev *Step, at string, i int) error {
	for _, name := range s.PinNames() {
		if have, ok := prev.Pins[name]; ok && semver.Compare(s.Pins[name], have) < 0 {
			return errors.Newf("%s pins %s at %s, behind step %d's %s; the ledger walks forward only", at, name, s.Pins[name], i, have)
		}
	}
	if len(s.Moves(prev.Pins)) == 0 && len(s.WebMoves(prev.Web)) == 0 && len(s.Recipes) == 0 && (prev.Impulse == "" || !s.MovesTool(prev.Impulse)) {
		return errors.Newf("%s moves no pin, Go or web, names no recipe and moves the tool pin nowhere: it is not a step", at)
	}
	switch {
	case prev.Impulse == "":
		return nil
	case s.Impulse == "":
		return errors.Newf("%s names no impulse release and step %d names %s: once a step names the release that added it, every later step does", at, i, prev.Impulse)
	case semver.Compare(s.Impulse, prev.Impulse) < 0:
		return errors.Newf("%s names impulse %s, behind step %d's %s; the ledger walks forward only", at, s.Impulse, i, prev.Impulse)
	}

	return nil
}

// validateWeb holds a step's web pins to the library: a step naming any names every
// package of WebPackages at a release version and no other package, and once the step
// before names them, so does the step; and to the step before: no web pin moving backward.
func validateWeb(s, prev *Step, at string, i int) error {
	if len(s.Web) == 0 {
		if prev != nil && len(prev.Web) > 0 {
			return errors.Newf("%s names no web pins and step %d does: once a step names the web library's versions, every later step does", at, i)
		}

		return nil
	}
	for _, name := range s.WebNames() {
		switch {
		case !slices.Contains(WebPackages, name):
			return errors.Newf("%s pins %s, which is not a web library package (%s)", at, name, strings.Join(WebPackages, ", "))
		case check.WebVersion(s.Web[name]) == "":
			return errors.Newf("%s pins %s at %q, which is not a release version (0.0.16)", at, name, s.Web[name])
		}
	}
	for _, name := range WebPackages {
		if _, ok := s.Web[name]; !ok {
			return errors.Newf("%s pins no %s: a step names every web library package's version", at, name)
		}
	}
	if prev == nil {
		return nil
	}
	for _, name := range s.WebNames() {
		if have, ok := prev.Web[name]; ok && check.WebBehind(s.Web[name], have) {
			return errors.Newf("%s pins %s at %s, behind step %d's %s; the ledger walks forward only", at, name, s.Web[name], i, have)
		}
	}

	return nil
}

// IsRelease reports whether the version is a release: canonical, with no pre-release or
// build part, so a pushed commit's pseudo-version is not one.
func IsRelease(version string) bool {
	return semver.IsValid(version) && semver.Canonical(version) == version && semver.Prerelease(version) == ""
}

// release reads a step's impulse release, "v0.2.0". As with pins, the ledger is a table
// the build checks, so a malformed version stops the program at start rather than
// reading as a release.
func release(version string) string {
	if !IsRelease(version) {
		panic(fmt.Sprintf("ledger: malformed impulse release %q", version))
	}

	return version
}

// pins reads a step's pin set, one "ccc/resource v0.12.0" line per module, into the map
// by module path. The ledger is a table the build checks, so a malformed line stops the
// program at start rather than reading as a pin set with a module missing.
func pins(lines string) map[string]string {
	set := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(lines), "\n") {
		name, version, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok || !semver.IsValid(version) {
			panic(fmt.Sprintf("ledger: malformed pin %q", line))
		}
		set[FrameworkPrefix+name] = version
	}

	return set
}

// web reads a step's web pins, one "@cccteam/resource-angular 0.0.16" line per package,
// into the map by package. As with pins, a malformed line stops the program at start.
func web(lines string) map[string]string {
	set := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(lines), "\n") {
		name, version, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok || check.WebVersion(version) == "" {
			panic(fmt.Sprintf("ledger: malformed web pin %q", line))
		}
		set[name] = version
	}

	return set
}

// Short names a framework module without the prefix (ccc/resource, access).
func Short(path string) string {
	return strings.TrimPrefix(path, FrameworkPrefix)
}

// PinNames lists the module paths a step pins, sorted.
func (s *Step) PinNames() []string {
	names := make([]string, 0, len(s.Pins))
	for name := range s.Pins {
		names = append(names, name)
	}
	sort.Strings(names)

	return names
}

// Reached reports whether an application with the pins stands at or beyond the step:
// every module the step pins is required at its version or a later one.
func (s *Step) Reached(pins map[string]string) bool {
	for name, version := range s.Pins {
		have, ok := pins[name]
		if !ok || semver.Compare(have, version) < 0 {
			return false
		}
	}

	return true
}

// ReachedTool reports whether the tool pin stands at or beyond the impulse release that
// added the step: so when the step names none, or go.mod pins no tool (an application
// from before the tool directive stands by its framework pins alone).
func (s *Step) ReachedTool(pin string) bool {
	return s.Impulse == "" || pin == "" || semver.Compare(pin, s.Impulse) >= 0
}

// Moves lists the pins the step moves from the pins given, "ccc/resource v0.12.0" each
// in module order: a module the pins lack or hold at an earlier version. A pin already at
// or beyond the step's is not moved.
func (s *Step) Moves(from map[string]string) []string {
	var moves []string
	for _, name := range s.PinNames() {
		if have, ok := from[name]; !ok || semver.Compare(have, s.Pins[name]) < 0 {
			moves = append(moves, Short(name)+" "+s.Pins[name])
		}
	}

	return moves
}

// WebNames lists the web packages a step pins, sorted.
func (s *Step) WebNames() []string {
	names := make([]string, 0, len(s.Web))
	for name := range s.Web {
		names = append(names, name)
	}
	sort.Strings(names)

	return names
}

// WebMoves lists the web pins the step moves from the specs given, "@cccteam/resource
// 0.0.12" each in package order: a package the specs lack, hold at an earlier version or
// hold at a spec that is no exact release (a range, a tag, a yalc attachment). A pin at or
// beyond the step's is not moved.
func (s *Step) WebMoves(from map[string]string) []string {
	var moves []string
	for _, name := range s.WebNames() {
		if have, ok := from[name]; !ok || check.WebBehind(have, s.Web[name]) {
			moves = append(moves, name+" "+s.Web[name])
		}
	}

	return moves
}

// MovesTool reports whether the step moves the impulse tool pin from the pin given: the
// step names the release that added it, and the pin is missing or behind it. A pin at or
// beyond the release stays, as a framework pin bumped by hand ahead of its step does.
func (s *Step) MovesTool(pin string) bool {
	return s.Impulse != "" && (pin == "" || semver.Compare(pin, s.Impulse) < 0)
}

// Current is the recorded ledger as the checks ask it (check.Ledger).
type Current struct{}

// AtLastStep reports whether the framework pins in go.mod reach the recorded ledger's last
// step: no step is pending for the application, so moving the tool pin alone is right;
// behind it, impulse upgrade is what moves the pins.
func (Current) AtLastStep(mod *modfile.File) bool {
	return Position(Steps, AppPins(mod), ToolPin(mod)) == len(Steps)-1
}

// WebPins is the web library's versions of the step the application stands at, read from
// go.mod as the walk reads it (Position); nil before the first step, or at a step that
// names none.
func (Current) WebPins(mod *modfile.File) map[string]string {
	i := Position(Steps, AppPins(mod), ToolPin(mod))
	if i < 0 {
		return nil
	}

	return Steps[i].Web
}

// ToolPin reads the impulse tool pin from an application's go.mod: the version it
// requires impulse at, "" when it does not.
func ToolPin(mod *modfile.File) string {
	if mod == nil {
		return ""
	}
	for _, r := range mod.Require {
		if r.Mod.Path == check.ImpulseModule {
			return r.Mod.Version
		}
	}

	return ""
}

// AppPins reads the framework pins from an application's go.mod: every required module
// under the framework prefix but impulse itself, whose pin is the tool's.
func AppPins(mod *modfile.File) map[string]string {
	pins := map[string]string{}
	if mod == nil {
		return pins
	}
	for _, r := range mod.Require {
		if strings.HasPrefix(r.Mod.Path, FrameworkPrefix) && r.Mod.Path != check.ImpulseModule {
			pins[r.Mod.Path] = r.Mod.Version
		}
	}

	return pins
}

// Position is the index of the latest step the application stands at or beyond, -1 when
// it reaches none: every module the step pins is required at its version or later
// (Reached), and the impulse tool pin stands at or beyond the release that added the
// step (ReachedTool). The tool pin tells apart the steps that move no framework pin: a
// step carrying a recipe alone is walked once, and the tool pin the walk moves after it
// is what says so.
func Position(steps []Step, pins map[string]string, toolPin string) int {
	position := -1
	for i := range steps {
		if steps[i].Reached(pins) && steps[i].ReachedTool(toolPin) {
			position = i
		}
	}

	return position
}

// Pending lists the steps after the application's position, in order: none when it
// stands at the last.
func Pending(steps []Step, pins map[string]string, toolPin string) []Step {
	return steps[Position(steps, pins, toolPin)+1:]
}

// Names labels each step by the pins it moves from the step before it (the first by all of
// its pins), for the plan and the tests.
func Names(steps []Step) []string {
	names := make([]string, 0, len(steps))
	var prev map[string]string
	for i := range steps {
		names = append(names, strings.Join(steps[i].Moves(prev), ", "))
		prev = steps[i].Pins
	}

	return names
}

// Validate checks the ledger's shape: at least one step, each with a pin set of framework
// modules at valid versions (impulse's own pin is not among them: a step names the release
// that added it in Impulse, a release or "", never behind the step before's and always
// named once a step before names one) and a note, no pin moving backward from the step
// before, every step after the first moving a framework pin or a web pin, naming a recipe
// or moving the tool pin past the step before's (a change the application makes by hand,
// which the note describes, rides a step of its own), no recipe named twice in one step,
// and the web pins, when a step names them, naming every web library package at a release
// version, none other, never behind the step before's and always named once a step before
// names them. The ledger's test holds Steps to it.
func Validate(steps []Step) error {
	if len(steps) == 0 {
		return errors.New("the ledger records no step; the first is the first impulse release's pins")
	}
	for i := range steps {
		s := &steps[i]
		at := fmt.Sprintf("step %d", i+1)
		switch {
		case len(s.Pins) == 0:
			return errors.Newf("%s pins no framework module", at)
		case strings.TrimSpace(s.Note) == "":
			return errors.Newf("%s has no note", at)
		}
		for _, name := range s.PinNames() {
			switch {
			case name == check.ImpulseModule:
				return errors.Newf("%s pins %s among the framework pins; the step names the impulse release that added it in Impulse, and the walk moves the tool pin to it", at, name)
			case !strings.HasPrefix(name, FrameworkPrefix):
				return errors.Newf("%s pins %s, which is not a framework module (%s...)", at, name, FrameworkPrefix)
			case !semver.IsValid(s.Pins[name]):
				return errors.Newf("%s pins %s at %q, which is not a semantic version", at, name, s.Pins[name])
			}
		}
		if err := validatePending(s, at); err != nil {
			return err
		}
		if err := validateImpulse(s, at); err != nil {
			return err
		}
		var prev *Step
		if i > 0 {
			prev = &steps[i-1]
			if err := validateOrder(s, prev, at, i); err != nil {
				return err
			}
		}
		if err := validateWeb(s, prev, at, i); err != nil {
			return err
		}
		seen := map[string]bool{}
		for _, recipe := range s.Recipes {
			if seen[recipe.Name()] {
				return errors.Newf("%s names the recipe %s twice", at, recipe.Name())
			}
			seen[recipe.Name()] = true
		}
	}

	return nil
}
