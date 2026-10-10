package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/go-playground/errors/v5"
	"github.com/spf13/cobra"

	"github.com/cccteam/ccc/impulse/app"
	"github.com/cccteam/ccc/impulse/ci"
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
	cmd.AddCommand(newAddSite())
	cmd.AddCommand(newAddFeature())
	cmd.AddCommand(newAddFiles())

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
starts the job process's cleanup each day at 09:00 UTC through the starter the site
configuration builds from the template job the stack sets in APP_JOBS_TEMPLATE and the
version (resource/jobs), behind the scheduler guard it
builds from APP_SCHEDULER_INVOKER (resource/scheduled). An application without an rpc
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
reconciled against.

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
and UserPermissions edits); and the reference's tenant service is copied into the
browser app.

Which resources become tenant-scoped and how their rows are assigned, the bootstrap
order, the harnesses, the tests, and the tenant picker are handed to the agent with the
failing checks as the obligations.`,
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
		f          transitionFlags
		preauth    bool
		oidcAzure  bool
		oidcGoogle bool
		authority  string
	)

	cmd := &cobra.Command{
		Use:   "auth <name>",
		Short: "Add an auth: a population that signs in one way and holds roles in its own store",
		Long: `auth adds an auth package, pkg/auth/<name>, copied from an auth the application already
has with every name substituted, so the new population owns its own session and user
tables, cookie, store prefix, and role file from the start. Its table migrations are
copied under the new prefix, an empty role file is written in the package (the file its
Roles() embeds and hands to the permission engine), and the data level constructs it
beside the auth it was copied from. The default is a password
auth; --preauth swaps the constructor to the preauth flavor (the application proves who
someone is and asks the session library for a session).

--oidc-azure and --oidc-google add an auth whose people sign in through the organization's
directory over OpenID Connect. It is copied from an OIDC auth the application has, or from
the reference skeleton's (rewritten for Google when that is the flavor: a hosted domain in
place of an issuer, a subject-keyed user anchor, no front-channel logout), with the
directory registration read from APP_<NAME>_OIDC_* variables, the Procfile built with the
session library's skipAuth tag (the directory simulated from APP_USERNAME until the
application is registered with one), and the role-membership authority set by
--authority, which is asked when it is not given: "directory" makes the directory's role
claims the authority (session.RoleSync: every login reconciles the person's roles to them
and removes what they do not name), "application" keeps role assignment in the application
(session.DisableRoleSync). There is no default, because the wrong answer deletes
hand-assigned roles at the next login. For Google the directory's authority is its Groups
(session.GoogleRoleSync over the Cloud Identity Groups API, with the person's own sign-in
token), read by a group prefix the registration names, as far as the group lookup it names
reaches (direct, or nested); under skipAuth the lookup is simulated from APP_ROLES.

Binding a site or an outlet to the new auth, checking its roles in the deploy, its
development identities, and the tests that prove its people are strangers to the other
auths are handed to the agent.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			flavor := transition_.FlavorPassword
			chosen := 0
			for _, on := range []bool{preauth, oidcAzure, oidcGoogle} {
				if on {
					chosen++
				}
			}
			switch {
			case chosen > 1:
				return errors.New("--preauth, --oidc-azure, and --oidc-google are flavors; pick one")
			case preauth:
				flavor = transition_.FlavorPreauth
			case oidcAzure, oidcGoogle:
				flavor = transition_.FlavorOIDCAzure
				if oidcGoogle {
					flavor = transition_.FlavorOIDCGoogle
				}
				if authority == "" {
					answer, err := askAuthority(cmd)
					if err != nil {
						return err
					}
					authority = answer
				}
			}

			return runTransition(cmd, &f, transition_.Auth{Name: args[0], Flavor: flavor, Authority: authority}, transition_.ReferenceCandidate)
		},
	}
	f.bind(cmd)
	cmd.Flags().BoolVar(&preauth, "preauth", false, "a preauth auth: the application proves the principal and the session library issues the session")
	cmd.Flags().BoolVar(&oidcAzure, "oidc-azure", false, "an OIDC auth: its people sign in through the organization's Azure directory")
	cmd.Flags().BoolVar(&oidcGoogle, "oidc-google", false, "an OIDC auth: its people sign in through the organization's Google Workspace directory")
	cmd.Flags().StringVar(&authority, "authority", "", "who owns role membership for an OIDC auth: directory (role claims synchronized at every login) or application (roles assigned in the application); asked when not given")

	return cmd
}

// authorityQuestion is what a person at the terminal is asked when --authority is not
// given for an OIDC auth.
const authorityQuestion = `Who is the authority for this auth's role membership?
  directory    the directory's role claims are synchronized at every login; roles it does
               not name are removed, and a login naming no known role is refused
  application  roles are assigned in the application: the bootstrap now, an
               administration surface later
[directory/application]: `

// askAuthority asks who owns role membership when the flag did not say and a person is at
// the terminal. Without a terminal the flag is required: there is no default, because the
// wrong answer deletes hand-assigned roles at the next login.
func askAuthority(cmd *cobra.Command) (string, error) {
	if !stdinIsTerminal() {
		return "", errors.New("--authority is required for an OIDC auth: directory (the directory's role claims are synchronized at every login and roles it does not name are removed) or application (roles are assigned in the application). It is asked because the wrong answer deletes hand-assigned roles at the next login")
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
	// uses counts the hand-written uses a removal left behind, obligations the check
	// cannot see, so the handoff happens for them even when the check is clean.
	uses := 0
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
		uses += len(change.Uses)
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
	if !check.Failed(results) && uses == 0 {
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
	brief := &handoff.Brief{App: a, Change: strings.Join(changes, "\n"), Meaning: strings.Join(meanings, "\n\n"), Results: results, Reference: referenceDir, Guard: guard, Left: uses}
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
	if len(change.Uses) > 0 {
		fmt.Fprintf(w, "Still named by the hand-written code (%s):\n", change.UsesNote)
		for _, u := range change.Uses {
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
