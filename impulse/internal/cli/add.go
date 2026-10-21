package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"github.com/go-playground/errors/v5"
	"github.com/spf13/cobra"

	"github.com/cccteam/ccc/impulse/app"
	"github.com/cccteam/ccc/impulse/ci"
	"github.com/cccteam/ccc/impulse/internal/authpkg"
	"github.com/cccteam/ccc/impulse/internal/check"
	"github.com/cccteam/ccc/impulse/internal/handoff"
	"github.com/cccteam/ccc/impulse/internal/ledger"
	"github.com/cccteam/ccc/impulse/internal/skeleton"
	transition_ "github.com/cccteam/ccc/impulse/internal/transition"
)

func newAdd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add an option to the application: files and generator edits, then a handoff for the wiring",
		Long: `add makes the deterministic half of an option (files laid in, the generator program and
the seams edited at the source level, go generate run), stages it, and runs impulse check. The checks that fail are
exactly the obligations of the option just enabled, and add hands them to an f.agent the
way impulse handoff does: a brief at ` + handoff.File + ` with what changed, what the
option means, the failing checks verbatim, and a rendered reference application; --f.agent
launches Claude Code on it and verifies the guardrails when it returns.`,
	}
	cmd.AddCommand(newAddOutlet())
	cmd.AddCommand(newAddTenancy())
	cmd.AddCommand(newAddAuth())
	cmd.AddCommand(newAddMethod())
	cmd.AddCommand(newAddSite())
	cmd.AddCommand(newAddFeature())
	cmd.AddCommand(newAddFiles())
	cmd.AddCommand(newAddJob())

	return cmd
}

func newAddJob() *cobra.Command {
	var (
		f        transitionFlags
		schedule string
	)

	cmd := &cobra.Command{
		Use:   "job <name>",
		Short: "Add a job: a command of the job process, its work to write, and optionally the schedule that starts it",
		Long: `job scaffolds a command of the application's job process, the second binary (cmd/jobs)
bedrock deploys as the Cloud Run job beside the service: pkg/jobs gains the job's work,
a function named for the job (prune-reports becomes PruneReports) written empty, which
refuses until the work is written, with the command's name the job process runs it
under; cmd/jobs/<name> gains the command, a cobra command whose Run runs the work over
the resource client of the data level; cmd/jobs/main.go, the root that opens the data
level once and registers every command, is written by the first job and extended by the
rest; the lint configuration admits the command tree's library (github.com/spf13/cobra)
and go.mod requires it. A name carrying a group (files/cleanup) puts the command under a
group command of that word.

--schedule "<cron>" also writes the method marked @rpc and @schedule in the rpc package
that starts the job on the schedule through the job process's starter (the service
starts its job, never a schedule of the job's own), with the starter's wiring where the
application lacks it (the rpc package and its Client, the site level's scheduler guard
and job driver, the App's SchedulerAuth and RPCClient, the test configurers' fakes,
WithRPC in the generator program), as impulse add files wires it for the cleanup; then go
generate emits the method's handler and route. Without a schedule nothing starts the job
until a scheduled method or a caller does.

The work itself, the suite that proves it, and the schedule when none was given are
handed to the agent.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTransition(cmd, &f, transition_.Job{Name: args[0], Schedule: schedule}, "")
		},
	}
	f.bind(cmd)
	cmd.Flags().StringVar(&schedule, "schedule", "", "the cron expression Cloud Scheduler starts the job on, in UTC (\"0 3 * * *\"); a method marked @schedule is written to start it")

	return cmd
}

func newAddFiles() *cobra.Command {
	var f transitionFlags

	cmd := &cobra.Command{
		Use:   "files",
		Short: "Wire the file store: APP_FILE_STORE, the development directory, the cleanup job and its schedule",
		Long: `files wires the framework's file store into the application (resource/filestore): the
data level reads APP_FILE_STORE, opens the store it names when it is set (a directory in
development, a bucket on Cloud Run) and builds the resource client over it, so the
generated handlers keep uploaded files in it and a committed transaction's released
objects are deleted from it; .envrc.template sets file://uploads and .gitignore ignores
the directory; the bootstrap empties a directory store before it seeds; cmd/jobs gains
the orphaned-file cleanup command (pkg/jobs, filestore.Cleanup over the generated
FileHolders()); and the rpc package gains CleanUpFiles, a method marked @schedule that
starts the job process's cleanup each day at 09:00 UTC through the job driver the site
configuration opens from the template job the stack sets in APP_JOBS_TEMPLATE, the
setting its environment embeds, and the version (resource/jobs/cloudrun), behind the
scheduler guard it builds from APP_SCHEDULER_INVOKER (resource/scheduled). An application without an rpc
package gains one, with WithRPC in the generator program and the Client the generated
handlers ask the App for; the test configurers gain a nil guard and a fake starter.

Which resources record files (@file on a key column, an @upload method) is the
application's; until one does, the store is wired and idle.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runTransition(cmd, &f, transition_.Files{}, "")
		},
	}
	f.bind(cmd)

	return cmd
}

func newAddFeature() *cobra.Command {
	var (
		f    transitionFlags
		site string
	)

	cmd := &cobra.Command{
		Use:   "feature <name>",
		Short: "Add a feature flag: its constant, its development seed row, regeneration, and a handoff for the gate",
		Long: `feature declares a feature flag: a release switch whose code ships before the feature is
turned on, differs per environment, and is removed once the feature is permanent or
abandoned. The resources package gains a resource.Feature constant named after the flag
(cargo_manifest becomes CargoManifest) with a doc stub, in ` + app.FeaturesFile + `; the development
seed gains the flag's row, off, in schema/devseed (set Enabled to TRUE there to start
development and the test environments with the feature on); and go generate runs, so the
generated Features() lists the flag for the deploy's MigrateFeatures and the browser's
Feature union carries it. In the sites layout --site names the site whose resources
package declares it.

What the flag gates (@feature(<Constant>) on a resource, a field or a method, or a read of
it in Go or in the browser), the description, and where the flags dialog's link lives are
handed to the agent; the feature-flags check fails while the flag gates nothing and is
read nowhere.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTransition(cmd, &f, transition_.AddFeature{Name: args[0], Site: site}, "")
		},
	}
	f.bind(cmd)
	cmd.Flags().StringVar(&site, "site", "", "in the sites layout, the site whose resources package declares the flag")

	return cmd
}

func newAddSite() *cobra.Command {
	var (
		f        transitionFlags
		existing string
	)

	cmd := &cobra.Command{
		Use:   "site <name>",
		Short: "Add a site: a stand-alone application on a host of its own under apps/<name>/",
		Long: `site adds a site, a stand-alone application on a host of its own: its own main package,
handlers, router, resources (empty to start), authorization suite, and browser workspace
under apps/<name>/, copied from the first site with the imports renamed; its generator
program and directive; its serve and browser processes on the next ports; its TypeScript
target in the shared generator; and its router collection in the union the roles are
reconciled against. The name is one word of lowercase letters and digits starting with a
letter, at most 25 characters, since it is the second segment of every per-site resource
name bedrock renders and of the site's default hostname label (<code>-<site>): the Cloud
Run service name <prefix>-<env>-<region code>-<code>-<site>-app fits 49 characters with 25
left for the site at the organization's longest prefix, environment, region code and
application code. A name outside the rule is refused, --existing's too.

On a flat application the first site added promotes the layout to sites, the one
non-additive transition: the existing site moves under apps/<existing>/ (every import of
its packages changes), its generator becomes cmd/generate/<existing>generator, the site
level's bundle variable becomes APP_DIST (set per site process, in the Procfile), the
deployment's collection becomes the union of the sites' router collections, and a shared
generator is laid in over an empty pkg/sharedresources. --existing names what the existing
site becomes and is asked when not given, since the name is the site's directory for good.
Everything existing belongs to that site.

The new site's resources, its place in the integration suite, its browser application's
own titles and pages, and the deployment configuration outside the repository are handed
to the agent.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if existing == "" {
				a, err := app.Discover(f.appDir)
				if err != nil {
					return err
				}
				if a.Profile().Layout == app.LayoutFlat {
					answer, err := askExistingSite(cmd, a)
					if err != nil {
						return err
					}
					existing = answer
				}
			}

			return runTransition(cmd, &f, transition_.Site{Name: args[0], Existing: existing}, transition_.SitesReference)
		},
	}
	f.bind(cmd)
	cmd.Flags().StringVar(&existing, "existing", "", "on a flat application, the name the existing site takes under apps/ (asked when not given)")

	return cmd
}

// askExistingSite asks what the existing site is called when a flat application grows its
// second site and --existing did not say.
func askExistingSite(cmd *cobra.Command, a *app.App) (string, error) {
	if !stdinIsTerminal() {
		return "", errors.New("--existing is required on a flat application: the existing site moves under apps/<existing>/ and the name is its directory for good, so it is asked rather than defaulted")
	}
	hint := ""
	if len(a.WebApps) > 0 {
		if projects, err := a.ReadAngular(a.WebApps[0].Dir); err == nil && len(projects) > 0 {
			hint = fmt.Sprintf(" Its browser application is the %s project.", projects[0].Name)
		}
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "The application is flat: adding a second site moves the existing one under apps/<name>/.\nWhat is the existing site called? The name is its directory for good.%s\n> ", hint)
	answer, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	if err != nil && answer == "" {
		return "", errors.Wrap(err, "reading the answer")
	}
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return "", errors.New("no name given: pass --existing <name>")
	}

	return answer, nil
}

// transitionFlags are the flags every add subcommand shares.
type transitionFlags struct {
	appDir       string
	agent        bool
	agentCommand string
	agentArgs    []string
	skipGenerate bool
}

func (f *transitionFlags) bind(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.appDir, "app", ".", "application root (the directory holding go.mod)")
	f.bindAgent(cmd)
}

// bindAgent binds the flags of the handoff alone, for a command whose application root is
// not a flag.
func (f *transitionFlags) bindAgent(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&f.agent, "agent", false, "launch the agent on the brief and verify when it returns")
	cmd.Flags().StringVar(&f.agentCommand, "agent-command", handoff.DefaultCommand, "the agent executable")
	cmd.Flags().StringArrayVar(&f.agentArgs, "agent-arg", nil, "an argument appended to the agent's command line (repeatable), such as --model or --max-budget-usd")
	cmd.Flags().BoolVar(&f.skipGenerate, "skip-generate", false, "skip the regen check (no emulator, no working-tree rewrite)")
}

// transition is one option's deterministic half.
type transition interface {
	Validate(a *app.App) error
	Apply(ctx context.Context, a *app.App, exec check.Execer) (*transition_.Change, error)
	Meaning() string
}

func newAddOutlet() *cobra.Command {
	var (
		f      transitionFlags
		prefix string
		auth   string
		apiKey bool
	)

	cmd := &cobra.Command{
		Use:   "outlet <name>",
		Short: "Add a router outlet: a second URL space on the same host",
		Long: `outlet adds a router outlet to a flat application. A session outlet (--auth <name>) is a
second browser surface bound to the named auth package, pkg/auth/<name>: the generator
program gains WithRouterOutlet with that auth's Auth and WebApp("/<name>") and a
GenerateTypescript target for the outlet, and the console's browser project is copied to
web/<name> with its API prefix, base path, and ports rewritten and registered in
angular.json, the package scripts, and the Procfile. A console that was alone at / moves
to /console with its API at /console/api, since no browser application is mounted at /
beside another (an installed application's scope is every URL under its start), and the
regenerated router answers the root alone with a redirect there; the brief lists the Go
and prose that still name /api. An API-key outlet (--api-key) is a machine surface: the
generator program gains WithRouterOutlet with APIKey(). (An application that kept a
hand-written router gains ServesSessions or nothing, as before, and the brief names the
auth the outlet binds to.)

Either way go generate emits the outlet's routes, handlers, and client, and the
generated router mounts the outlet from its declaration. The App's handlers the router
now requires (the outlet's session getter and browser-application pair, or its API-key
middleware), the configuration, the members, and the tests are handed to the agent with
the failing checks as the obligations.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if (auth != "") == apiKey {
				return errors.New("choose --auth <name> (a browser surface bound to that auth) or --api-key (a machine surface)")
			}

			return runTransition(cmd, &f, transition_.Outlet{Name: args[0], Prefix: prefix, Sessions: auth != "", Auth: auth}, transition_.ReferenceCandidate)
		},
	}
	f.bind(cmd)
	cmd.Flags().StringVar(&prefix, "prefix", "", "the outlet's URL prefix without slashes at either end, such as portal/api (required)")
	cmd.Flags().StringVar(&auth, "auth", "", "a session outlet: a browser surface bound to this auth package (pkg/auth/<name>), with its own client and browser project")
	cmd.Flags().BoolVar(&apiKey, "api-key", false, "an API-key outlet: a machine surface behind an authentication the application defines")
	_ = cmd.MarkFlagRequired("prefix")

	return cmd
}

func newAddTenancy() *cobra.Command {
	var (
		f     transitionFlags
		table string
	)

	cmd := &cobra.Command{
		Use:   "tenancy",
		Short: "Make the application tenanted: a tenant table as the domain universe",
		Long: `tenancy makes a flat, untenanted application tenanted. The tenant table becomes the
next schema migration, with two development tenants seeded as a data migration beside
it; the tenant-record struct is added to the resource package as a global resource
annotated @tenant, from which the generator derives the tenant segment and emits the
constructor of the tenant roster; the generator program gains WithConcealedDomains; the
data level gains the roster, built with that constructor over the live service's tenants
signal and started where the configuration is built (a new file plus a field and the
start in DataConfiguration), so a tenant created on any instance is usable at once,
without a restart; the app exposes the roster to the generated code as TenantRoster()
and hands its Domains to the session permissions (a new file plus the Configurer, App
and UserPermissions edits); and the reference's tenant service, tenant guard and refusal
page are copied into the browser app.

Which resources become tenant-scoped and how their rows are assigned, the bootstrap
order, the harnesses, the tests, the tenant segment in the browser's routes, and the
tenant picker are handed to the agent with the failing checks as the obligations.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runTransition(cmd, &f, transition_.Tenancy{Table: table}, transition_.TenancyReferenceCandidate)
		},
	}
	f.bind(cmd)
	cmd.Flags().StringVar(&table, "tenant-table", "Tenants", "the tenant-record table, PascalCase and plural")

	return cmd
}

func newAddAuth() *cobra.Command {
	var (
		f         transitionFlags
		methods   []string
		authority string
		accounts  string
		outlet    string
	)

	cmd := &cobra.Command{
		Use:   "auth <name>",
		Short: "Add an auth: a population that holds roles in its own store and signs in through the methods on its Auth",
		Long: `auth adds an auth package, pkg/auth/<name>, rendered from the same source as the skeleton's
auths, so the new population owns its own session, user, identity, auth event and pending
sign-in tables, cookie, store prefix, and role file from the start, with one file per
sign-in method. Its session tables are written as migrations under the new prefix and its
access tables copied from an auth the application has; an empty role file is written in
the package (the file its Roles() embeds and hands to the permission engine), and the data
level constructs it beside that auth. The package carries a foothold (foothold.go): the
first administrator the migrate command seeds while the auth's users table holds no rows,
a user named administrator with the password the stack generates for a password auth, or
the administrator role memberships under a sign-in name the placement names for an auth
whose roles the application assigns; its roles, its setting in the data level and its
seeding from pkg/deploy are handed to the agent.

--method names the sign-in methods the Auth starts with, repeatable: password (the default
when none is given), azure:<registration> (Microsoft Entra over OpenID Connect) or
google:<registration> (Google Workspace over OpenID Connect), the registration being the
segment of the method's routes (user/<registration>/login). A directory method added here
takes one organization, and --accounts its standard account rule for a first sign-in, one
rule for every directory method given: members (members of the directory, the default: the
person becomes a user under the name the directory presents), invited (invited users only:
a user created beforehand is linked, anyone else refused) or open (open sign-up, which the
session library allows with any organization or personal accounts alone, so it is refused
with the one organization taken here); impulse add method composes the other slots. The
rule is written to the method file (Accounts) and, for invited, as .InvitedUsers() on the
method's declaration, so the generated users surface creates the invitations; --accounts
is refused with the password method alone, as add method refuses it. Its settings are read
from the environment under the registration's variables (APP_<NAME>_<REGISTRATION>_*), the
Procfile builds with the session library's skipAuth tag (the directory simulated until the
application is registered with one), and the role-membership authority is set by
--authority, which is asked when it is not given: "directory" makes the directory's roles
the authority (every sign-in reconciles the person's roles to them and removes what they
do not name), "application" keeps role assignment in the application. There is no default,
because the wrong answer deletes hand-assigned roles at the next sign-in.

--outlet names the outlet the new auth serves, asked when it is not given: default for the
one GenerateRoutes declares (the console's), or a WithRouterOutlet name such as portal; in
the sites layout the site's name for its default outlet. The outlet must exist (impulse
add outlet adds one, bound to an auth the application has), and an API-key outlet is
refused, since a machine surface has no sign-in. The outlet's declaration in the generator
program is pointed at the new auth with its methods, in place of the auth it declared, so
the regenerated router mounts the auth's session group under the outlet's prefix: its
login routes, the methods route its login page renders from, and the pending sign-in
routes. There is no default, because an auth is reachable only through the outlet bound
to it. Under a hand-written router the program declares no Auth to point, and the brief
says to mount the group in the router.

Checking the auth's roles in the deploy, the App's handlers the generated router now
requires for the outlet, its development identities, and the tests that prove its people
are strangers to the other auths are handed to the agent.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			specs, err := authMethods(cmd, methods, authority, accounts)
			if err != nil {
				return err
			}
			if outlet == "" {
				answer, err := askOutlet(cmd, f.appDir, args[0])
				if err != nil {
					return err
				}
				outlet = answer
			}

			return runTransition(cmd, &f, transition_.Auth{Name: args[0], Methods: specs, Outlet: outlet}, transition_.ReferenceCandidate)
		},
	}
	f.bind(cmd)
	cmd.Flags().StringArrayVar(&methods, "method", nil, "a sign-in method the auth starts with (repeatable): password, azure:<registration>, or google:<registration>; password when none is given")
	cmd.Flags().StringVar(&authority, "authority", "", "who owns role membership for the directory methods: directory (roles synchronized at every sign-in) or application (roles assigned in the application); asked when not given")
	cmd.Flags().StringVar(&accounts, "accounts", "", "the standard account rule for a first sign-in: members (default), invited, or open")
	cmd.Flags().StringVar(&outlet, "outlet", "", "the outlet the auth serves, whose declaration is pointed at it: default for the console's, or a WithRouterOutlet name; asked when not given")

	return cmd
}

// authMethods reads add auth's --method flags and settles the directory methods' authority
// and account rule, which the command takes once for all of them: the authority is asked
// when a directory method is given without it, and either flag is refused with the
// password method alone.
func authMethods(cmd *cobra.Command, methods []string, authority, accounts string) ([]authpkg.Method, error) {
	specs, err := parseMethods(methods)
	if err != nil {
		return nil, err
	}
	directory := false
	for i := range specs {
		if specs[i].Directory() {
			directory = true
		}
	}
	if directory && authority == "" {
		answer, err := askAuthority(cmd)
		if err != nil {
			return nil, err
		}
		authority = answer
	}
	if !directory && authority != "" {
		return nil, errors.New("--authority goes with a directory method (azure or google); the password method's role membership is the application's")
	}
	if !directory && accounts != "" {
		return nil, errors.New("--accounts goes with a directory method (azure or google); the password method's users are always created in the application")
	}
	for i := range specs {
		if specs[i].Directory() {
			specs[i].Authority = authority
			specs[i].Accounts = accounts
		}
	}

	return specs, nil
}

// outletQuestion opens what a person at the terminal is asked when --outlet is not given;
// the application's outlets follow it, one per line.
const outletQuestion = `Which outlet does the %s auth serve? Its declaration in the generator program is
pointed at the auth, so the generated router mounts the auth's login routes under the
outlet's prefix.
`

// askOutlet asks which outlet the auth serves when the flag did not say and a person is
// at the terminal, listing the application's outlets. Without a terminal the flag is
// required: there is no default, because an auth is reachable only through the outlet
// bound to it, and the tool cannot know which population an outlet is for.
func askOutlet(cmd *cobra.Command, appDir, auth string) (string, error) {
	a, err := app.Discover(appDir)
	if err != nil {
		return "", err
	}
	choices := transition_.OutletChoices(a)
	var keys []string
	for i := range choices {
		if !choices[i].Outlet.APIKey {
			keys = append(keys, choices[i].Key)
		}
	}
	if len(keys) == 0 {
		return "", errors.New("no outlet for the auth to serve: the generator programs declare no session outlet (GenerateRoutes, WithRouterOutlet with Auth); impulse add outlet adds one")
	}
	if !stdinIsTerminal() {
		return "", errors.Newf("--outlet is required: the outlet the %s auth serves, one of %s; its declaration is pointed at the auth, and there is no default because an auth is reachable only through the outlet bound to it", auth, strings.Join(keys, ", "))
	}
	w := cmd.ErrOrStderr()
	fmt.Fprintf(w, outletQuestion, auth)
	width := 0
	for i := range choices {
		width = max(width, len(choices[i].Key))
	}
	for i := range choices {
		fmt.Fprintf(w, "  %-*s  %s\n", width, choices[i].Key, outletDescription(&choices[i]))
	}
	fmt.Fprint(w, "[outlet]: ")
	line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	if err != nil && line == "" {
		return "", errors.Wrap(err, "reading the answer")
	}
	answer := strings.TrimSpace(line)
	for i := range choices {
		if choices[i].Key == answer {
			return answer, nil
		}
	}

	return "", errors.Newf("%q is not an outlet: %s", answer, strings.Join(keys, ", "))
}

// outletDescription says what an outlet is for the question: its prefix and the auth it
// binds to, or that it is a machine surface.
func outletDescription(c *transition_.OutletChoice) string {
	o := &c.Outlet
	switch {
	case o.APIKey:
		return fmt.Sprintf("/%s, an API-key outlet: a machine surface with no sign-in", o.Prefix)
	case o.Auth != nil:
		return fmt.Sprintf("/%s, bound to the %s auth", o.Prefix, path.Base(o.Auth.ImportPath))
	default:
		return fmt.Sprintf("/%s, no auth declared", o.Prefix)
	}
}

// parseMethods reads --method flags: password, or <kind>:<registration>.
func parseMethods(flags []string) ([]authpkg.Method, error) {
	if len(flags) == 0 {
		return []authpkg.Method{{Kind: app.MethodPassword}}, nil
	}
	var specs []authpkg.Method
	for _, flag := range flags {
		kind, name, _ := strings.Cut(flag, ":")
		switch kind {
		case app.MethodPassword:
			if name != "" {
				return nil, errors.Newf("--method %s: the password method takes no registration name", flag)
			}
			specs = append(specs, authpkg.Method{Kind: app.MethodPassword})
		case app.MethodAzure, app.MethodGoogle:
			if name == "" {
				name = kind
			}
			specs = append(specs, authpkg.Method{Kind: kind, Name: name})
		default:
			return nil, errors.Newf("--method %s: password, azure:<registration>, or google:<registration>", flag)
		}
	}

	return specs, nil
}

func newAddMethod() *cobra.Command {
	var (
		f         transitionFlags
		authority string
		slot      string
		accounts  string
	)

	cmd := &cobra.Command{
		Use:   "method <auth> <kind> [<registration>]",
		Short: "Add a sign-in method to an auth: a registration on its Auth, in a file of its own",
		Long: `method adds a sign-in method to an auth the application has: password, azure (Microsoft
Entra over OpenID Connect) or google (Google Workspace over OpenID Connect), under the
registration name given (the segment of its routes, user/<registration>/login; the kind's
name when none is given; the password method is always registered as password). The method
gets a file of its own in the auth's package with its registration constant, its settings,
its handlers and the registration New makes; the Auth file's Handlers and Settings gain it;
the data level reads its settings from the environment under the registration's variables
(APP_<AUTH>_<REGISTRATION>_*); every outlet bound to the auth declares it, so the generated
router mounts its routes and the login page, which renders from the methods route, offers
it; and for a directory method the Procfile builds with the session library's skipAuth
tag, which simulates the directory until the application is registered with it.

--slot is the organization slot of a directory method, which organizations' accounts it
admits. For azure: one (one tenant, the default), listed (named tenants), any (any tenant),
personal (Microsoft personal accounts alone), one-personal (one tenant and personal
accounts), any-personal (any tenant and personal accounts). For google: one (one
Workspace, the default), listed (named Workspaces), any-workspace (any Workspace), any
(any Google account), mixed (one Workspace and any Google account).

--accounts is the standard account rule for a first sign-in: members (members of the
directory, the default: the person becomes a user under the name the directory presents),
invited (invited users only: a user created beforehand is linked, anyone else refused) or
open (open sign-up). The session library allows open only with any organization or
personal accounts, and refuses members there, since every organization in the world is
"the directory".

--authority is who owns role membership for a directory method, asked when not given:
directory (the directory's roles are synchronized at every sign-in, allowed with one
organization only) or application (roles are assigned in the application).`,
		Args: cobra.RangeArgs(2, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			spec := authpkg.Method{Kind: args[1], Slot: slot, Accounts: accounts, Authority: authority}
			if len(args) == 3 {
				spec.Name = args[2]
			} else if spec.Kind != app.MethodPassword {
				spec.Name = spec.Kind
			}
			if spec.Kind == app.MethodPassword && (slot != "" || accounts != "" || authority != "") {
				return errors.New("the password method takes no --slot, --accounts or --authority")
			}
			if spec.Directory() && authority == "" {
				answer, err := askAuthority(cmd)
				if err != nil {
					return err
				}
				spec.Authority = answer
			}

			return runTransition(cmd, &f, &transition_.Method{Auth: args[0], Spec: spec}, transition_.ReferenceCandidate)
		},
	}
	f.bind(cmd)
	cmd.Flags().StringVar(&authority, "authority", "", "who owns role membership for a directory method: directory (roles synchronized at every sign-in, one organization only) or application (roles assigned in the application); asked when not given")
	cmd.Flags().StringVar(&slot, "slot", "", "the organization slot of a directory method: azure one|listed|any|personal|one-personal|any-personal, google one|listed|any-workspace|any|mixed (default: one)")
	cmd.Flags().StringVar(&accounts, "accounts", "", "the standard account rule for a first sign-in: members (default), invited, or open")

	return cmd
}

// authorityQuestion is what a person at the terminal is asked when --authority is not
// given for a directory method.
const authorityQuestion = `Who is the authority for this method's role membership?
  directory    the directory's roles are synchronized at every sign-in; roles it does
               not name are removed, and a sign-in naming no known role is refused
  application  roles are assigned in the application: the bootstrap now, an
               administration surface later
[directory/application]: `

// askAuthority asks who owns role membership when the flag did not say and a person is at
// the terminal. Without a terminal the flag is required: there is no default, because the
// wrong answer deletes hand-assigned roles at the next sign-in.
func askAuthority(cmd *cobra.Command) (string, error) {
	if !stdinIsTerminal() {
		return "", errors.New("--authority is required for a directory method: directory (the directory's roles are synchronized at every sign-in and roles it does not name are removed) or application (roles are assigned in the application). It is asked because the wrong answer deletes hand-assigned roles at the next sign-in")
	}
	fmt.Fprint(cmd.ErrOrStderr(), authorityQuestion)
	line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	if err != nil && line == "" {
		return "", errors.Wrap(err, "reading the answer")
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	if answer != transition_.AuthorityDirectory && answer != transition_.AuthorityApplication {
		return "", errors.Newf("%q is not an answer: directory or application", strings.TrimSpace(line))
	}

	return answer, nil
}

// runTransition is the flow every add subcommand shares: a clean tree, the
// deterministic half, go generate through the transition, the check with its fixes,
// staging, and the handoff.
func runTransition(cmd *cobra.Command, f *transitionFlags, t transition, reference string) error {
	ctx := cmd.Context()
	a, err := app.Discover(f.appDir)
	if err != nil {
		return err
	}
	repo := handoff.For(a, check.OSExec{})
	if err := repo.Check(ctx); err != nil {
		return err
	}
	dirty, err := repo.Dirty(ctx)
	if err != nil {
		return err
	}
	if len(dirty) > 0 {
		return errors.Newf("the working tree is not clean (%d path(s)): commit or stash first, so the transition is one reviewable diff", len(dirty))
	}

	return runTransitions(cmd, f, repo, []transition{t}, reference)
}

// runTransitions applies the transitions in order, each on the tree the one before it
// left, then runs the check once, stages everything, and hands what is left to the agent
// in one brief. impulse new composes its options this way; add runs one.
func runTransitions(cmd *cobra.Command, f *transitionFlags, repo handoff.Repo, ts []transition, reference string) error {
	ctx := cmd.Context()
	out := cmd.OutOrStdout()
	exec := check.OSExec{}
	var changes, meanings []string
	// left counts the obligations the changes list themselves (the hand-written uses a
	// removal leaves, a scaffolded job's work), which the check cannot see, so the
	// handoff happens for them even when the check is clean.
	left := 0
	for _, t := range ts {
		// Each transition reads the tree the one before it left.
		a, err := app.Discover(f.appDir)
		if err != nil {
			return err
		}
		if err := t.Validate(a); err != nil {
			return err
		}
		change, err := t.Apply(ctx, a, exec)
		if err != nil {
			return err
		}
		writeChange(out, change)
		changes = append(changes, change.Text())
		meanings = append(meanings, t.Meaning())
		left += len(change.Left)
	}

	// The tree changed, so the application is read again for the checks.
	a, err := app.Discover(f.appDir)
	if err != nil {
		return err
	}
	// The owned files follow the code: a transition that changed the browser workspaces
	// rewrote the CI workflow as part of its change, and this leaves every flow, impulse
	// new's composed options included, with the owned files current before the check.
	owned, err := ci.Write(a)
	if err != nil {
		return err
	}
	if owned.Written {
		fmt.Fprintf(out, "Rewrote %s from the code.\n\n", ci.List(owned.WrittenFiles()))
	}
	env := &check.Env{App: a, Exec: exec, SkipGenerate: f.skipGenerate, Fix: true, Out: cmd.ErrOrStderr(), Ledger: ledger.Current{}}
	results := check.Run(ctx, env, check.All())
	if err := repo.StageAll(ctx); err != nil {
		return err
	}
	check.Report(out, results)
	if !check.Failed(results) && left == 0 {
		if len(ts) == 1 {
			fmt.Fprintf(out, "\nThe check is clean: the option is wired. Review the diff and open the pull request.\n")
		} else {
			fmt.Fprintf(out, "\nThe check is clean: the %d options are wired. Review the diff and open the pull request.\n", len(ts))
		}

		return nil
	}

	referenceDir := ""
	if reference != "" {
		if referenceDir, err = renderReference(reference); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "impulse: no reference application: %s\n", message(err))
		}
	}
	guard, err := handoff.Take(a, handoff.FromTree(a))
	if err != nil {
		return err
	}
	brief := &handoff.Brief{App: a, Change: strings.Join(changes, "\n"), Meaning: strings.Join(meanings, "\n\n"), Results: results, Reference: referenceDir, Guard: guard, Left: left}
	ag := &handoff.Agent{Command: f.agentCommand, ExtraArgs: f.agentArgs}

	return completeHandoff(ctx, out, f.appDir, env, repo, brief, ag, f.agent)
}

// writeChange tells the user what the transition did and left.
func writeChange(w io.Writer, change *transition_.Change) {
	fmt.Fprintf(w, "Changed:\n")
	for _, d := range change.Did {
		fmt.Fprintf(w, "  - %s\n", d)
	}
	if len(change.Skipped) > 0 {
		fmt.Fprintf(w, "Left to the agent:\n")
		for _, s := range change.Skipped {
			fmt.Fprintf(w, "  - %s\n", s)
		}
	}
	if len(change.Left) > 0 {
		fmt.Fprintf(w, "Yours (%s):\n", change.LeftNote)
		for _, u := range change.Left {
			fmt.Fprintf(w, "  - %s\n", u)
		}
	}
	fmt.Fprintln(w)
}

// renderReference renders the candidate with the option wired into a temporary directory
// for the f.agent to read, and returns its path.
func renderReference(candidate string) (string, error) {
	dir, err := os.MkdirTemp("", "impulse-reference-"+candidate+"-")
	if err != nil {
		return "", errors.Wrap(err, "os.MkdirTemp()")
	}
	if _, err := skeleton.Render(&skeleton.Options{Candidate: candidate, Dir: dir, ModulePath: "example.com/reference/" + candidate}); err != nil {
		return "", err
	}

	return dir, nil
}
