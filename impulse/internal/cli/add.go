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

	"github.com/cccteam/ccc/impulse/internal/app"
	"github.com/cccteam/ccc/impulse/internal/check"
	"github.com/cccteam/ccc/impulse/internal/handoff"
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

	return cmd
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
		f        transitionFlags
		prefix   string
		sessions bool
		apiKey   bool
	)

	cmd := &cobra.Command{
		Use:   "outlet <name>",
		Short: "Add a router outlet: a second URL space on the same host",
		Long: `outlet adds a router outlet to a flat application. A session outlet (--sessions) is a
second browser surface behind the same session handling as the console: the generator
program gains WithRouterOutlet with ServesSessions and a GenerateTypescript target for
the outlet, and the console's browser project is copied to web/<name> with its API
prefix, base path, and ports rewritten and registered in angular.json, the package
scripts, and the Procfile. An API-key outlet (--api-key) is a machine surface: the
generator program gains WithRouterOutlet alone.

Either way go generate emits the outlet's routes, handlers, and client, and the router
mount, the served assets, the configuration, the members, and the tests are handed to
the agent with the failing checks as the obligations.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if sessions == apiKey {
				return errors.New("choose --sessions (a browser surface) or --api-key (a machine surface)")
			}

			return runTransition(cmd, &f, transition_.Outlet{Name: args[0], Prefix: prefix, Sessions: sessions}, transition_.ReferenceCandidate)
		},
	}
	f.bind(cmd)
	cmd.Flags().StringVar(&prefix, "prefix", "", "the outlet's URL prefix without slashes at either end, such as portal/api (required)")
	cmd.Flags().BoolVar(&sessions, "sessions", false, "a session outlet: a browser surface behind the session handling, with its own client and browser project")
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
		Long: `tenancy makes a flat, untenanted application tenanted. The generator program gains
WithDomainRoute (the table's kebab-case name) and WithConcealedDomains; the tenant table
becomes the next schema migration, with two development tenants seeded as a data
migration beside it; the tenant-record struct is added to the resource package as a
global resource; the data level gains the tenant roster read at startup and the
DomainVisible seam (a new file plus a field and the load in DataConfiguration); the app
exposes the seam to the generated code (a new file plus the Configurer and App edits);
and the reference's tenant service is copied into the browser app.

Which resources become tenant-scoped and how their rows are assigned, the bootstrap
order, the harnesses, the tests, and the tenant picker are handed to the agent with the
failing checks as the obligations.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runTransition(cmd, &f, transition_.Tenancy{Table: table}, transition_.TenancyReferenceCandidate)
		},
	}
	f.bind(cmd)
	cmd.Flags().StringVar(&table, "table", "Tenants", "the tenant-record table, PascalCase and plural")

	return cmd
}

func newAddAuth() *cobra.Command {
	var (
		f         transitionFlags
		preauth   bool
		oidcAzure bool
		authority string
	)

	cmd := &cobra.Command{
		Use:   "auth <name>",
		Short: "Add an auth: a population that signs in one way and holds roles in its own store",
		Long: `auth adds an auth package, pkg/auth/<name>, copied from an auth the application already
has with every name substituted, so the new population owns its own session and user
tables, cookie, store prefix, and roles file from the start. Its table migrations are
copied under the new prefix, an empty roles file is written beside the others, and the
data level constructs it beside the auth it was copied from. The default is a password
auth; --preauth swaps the constructor to the preauth flavor (the application proves who
someone is and asks the session library for a session).

--oidc-azure adds an auth whose people sign in through the organization's directory over
OpenID Connect. It is copied from an OIDC auth the application has, or from the reference
skeleton's, with the directory registration read from APP_<NAME>_OIDC_* variables, the
Procfile built with the session library's skipAuth tag (the directory simulated from
APP_USERNAME until the application is registered with one), and the role-membership
authority set by --authority, which is asked when it is not given: "directory" makes the
directory's role claims the authority (session.RoleSync: every login reconciles the
person's roles to them and removes what they do not name), "application" keeps role
assignment in the application (session.DisableRoleSync). There is no default, because the
wrong answer deletes hand-assigned roles at the next login.

Binding a site or an outlet to the new auth, provisioning its roles, its development
identities, and the tests that prove its people are strangers to the other auths are
handed to the agent.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			flavor := transition_.FlavorPassword
			switch {
			case preauth && oidcAzure:
				return errors.New("--preauth and --oidc-azure are two flavors; pick one")
			case preauth:
				flavor = transition_.FlavorPreauth
			case oidcAzure:
				flavor = transition_.FlavorOIDCAzure
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
	if info, err := os.Stdin.Stat(); err != nil || info.Mode()&os.ModeCharDevice == 0 {
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
	out := cmd.OutOrStdout()
	a, err := app.Discover(f.appDir)
	if err != nil {
		return err
	}
	exec := check.OSExec{}
	repo := handoff.For(a, exec)
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
	if err := t.Validate(a); err != nil {
		return err
	}
	change, err := t.Apply(ctx, a, exec)
	if err != nil {
		return err
	}
	writeChange(out, change)

	// The tree changed, so the application is read again for the checks.
	a, err = app.Discover(f.appDir)
	if err != nil {
		return err
	}
	env := &check.Env{App: a, Exec: exec, SkipGenerate: f.skipGenerate, Fix: true, Out: cmd.ErrOrStderr()}
	results := check.Run(ctx, env, check.All())
	if err := repo.StageAll(ctx); err != nil {
		return err
	}
	check.Report(out, results)
	if !check.Failed(results) {
		fmt.Fprintf(out, "\nThe check is clean: the option is wired. Review the diff and open the pull request.\n")

		return nil
	}

	referenceDir, err := renderReference(reference)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "impulse: no reference application: %v\n", err)
	}
	guard, err := handoff.Take(a, handoff.FromTree(a))
	if err != nil {
		return err
	}
	brief := &handoff.Brief{App: a, Change: change.Text(), Meaning: t.Meaning(), Results: results, Reference: referenceDir, Guard: guard}
	ag := handoff.Agent{Command: f.agentCommand, ExtraArgs: f.agentArgs}

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
	fmt.Fprintln(w)
}

// renderReference renders the candidate with the option wired into a temporary directory
// for the f.agent to read, and returns its path.
func renderReference(candidate string) (string, error) {
	dir, err := os.MkdirTemp("", "impulse-reference-"+candidate+"-")
	if err != nil {
		return "", errors.Wrap(err, "os.MkdirTemp()")
	}
	if _, err := skeleton.Render(skeleton.Options{Candidate: candidate, Dir: dir, ModulePath: "example.com/reference/" + candidate}); err != nil {
		return "", err
	}

	return dir, nil
}
