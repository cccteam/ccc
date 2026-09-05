package cli

import (
	"fmt"
	"os"

	"github.com/go-playground/errors/v5"
	"github.com/spf13/cobra"

	"github.com/cccteam/ccc/impulse/internal/app"
	"github.com/cccteam/ccc/impulse/internal/check"
	"github.com/cccteam/ccc/impulse/internal/handoff"
	"github.com/cccteam/ccc/impulse/internal/skeleton"
	"github.com/cccteam/ccc/impulse/internal/transition"
)

func newAdd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add an option to the application: files and generator edits, then a handoff for the wiring",
		Long: `add makes the deterministic half of an option (files copied in, the generator program
edited, go generate run), stages it, and runs impulse check. The checks that fail are
exactly the obligations of the option just enabled, and add hands them to an f.agent the
way impulse handoff does: a brief at ` + handoff.File + ` with what changed, what the
option means, the failing checks verbatim, and a rendered reference application; --f.agent
launches Claude Code on it and verifies the guardrails when it returns.`,
	}
	cmd.AddCommand(newAddOutlet())

	return cmd
}

// outletFlags are the add outlet command's flags.
type outletFlags struct {
	appDir       string
	prefix       string
	sessions     bool
	apiKey       bool
	agent        bool
	agentCommand string
	agentArgs    []string
	skipGenerate bool
}

func (f *outletFlags) bind(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.appDir, "app", ".", "application root (the directory holding go.mod)")
	cmd.Flags().StringVar(&f.prefix, "prefix", "", "the outlet's URL prefix without slashes at either end, such as portal/api (required)")
	cmd.Flags().BoolVar(&f.sessions, "sessions", false, "a session outlet: a browser surface behind the session handling, with its own client and browser project")
	cmd.Flags().BoolVar(&f.apiKey, "api-key", false, "an API-key outlet: a machine surface behind an authentication the application defines")
	cmd.Flags().BoolVar(&f.agent, "agent", false, "launch the agent on the brief and verify when it returns")
	cmd.Flags().StringVar(&f.agentCommand, "agent-command", handoff.DefaultCommand, "the agent executable")
	cmd.Flags().StringArrayVar(&f.agentArgs, "agent-arg", nil, "an argument appended to the agent's command line (repeatable), such as --model or --max-budget-usd")
	cmd.Flags().BoolVar(&f.skipGenerate, "skip-generate", false, "skip the regen check (no emulator, no working-tree rewrite)")
	_ = cmd.MarkFlagRequired("prefix")
}

func newAddOutlet() *cobra.Command {
	var f outletFlags

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
			if f.sessions == f.apiKey {
				return errors.New("choose --sessions (a browser surface) or --api-key (a machine surface)")
			}
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

			o := transition.Outlet{Name: args[0], Prefix: f.prefix, Sessions: f.sessions}
			if err := o.Validate(a); err != nil {
				return err
			}
			change, err := o.Apply(ctx, a, exec)
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
				fmt.Fprintf(out, "\nThe check is clean: the %s outlet is wired. Review the diff and open the pull request.\n", o.Name)

				return nil
			}

			reference, err := renderReference(transition.ReferenceCandidate)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "impulse: no reference application: %v\n", err)
			}
			guard, err := handoff.Take(a, handoff.FromTree(a))
			if err != nil {
				return err
			}
			brief := &handoff.Brief{App: a, Change: change.Text(), Meaning: o.Meaning(), Results: results, Reference: reference, Guard: guard}
			ag := handoff.Agent{Command: f.agentCommand, ExtraArgs: f.agentArgs}

			return completeHandoff(ctx, out, f.appDir, env, repo, brief, ag, f.agent)
		},
	}

	f.bind(cmd)

	return cmd
}

// writeChange tells the user what the transition did and left.
func writeChange(w interface{ Write([]byte) (int, error) }, change *transition.Change) {
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
