package cli

import (
	"github.com/spf13/cobra"

	"github.com/cccteam/ccc/impulse/internal/handoff"
	transition_ "github.com/cccteam/ccc/impulse/internal/transition"
)

func newRemove() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove an option from the application: files deleted and generator edits, then a handoff for the unwiring",
		Long: `remove takes an option out the way add puts one in: the deterministic half is made (the
option's files deleted, the generator program and the registrations edited, go generate
run), staged, and checked, and what still names the option in the hand-written code is
handed to an agent with a brief at ` + handoff.File + `, the rendered reference showing
what the option's wiring looks like so it is recognizable on the way out.`,
	}
	cmd.AddCommand(newRemoveOutlet())
	cmd.AddCommand(newRemoveSite())
	cmd.AddCommand(newRemoveFeature())

	return cmd
}

func newRemoveFeature() *cobra.Command {
	var f transitionFlags

	cmd := &cobra.Command{
		Use:   "feature <name>",
		Short: "Remove a feature flag: its constant, every @feature naming it, and its seed row",
		Long: `feature retires a feature flag, the step that makes a feature permanent or abandons it.
The resource.Feature constant goes (and its file, when it declared nothing else), every
@feature(<Constant>) annotation naming it goes, so the resources, fields and methods it
gated are served unconditionally, the flag's row leaves the development seed, and go
generate runs, so Features() and the browser's Feature union lose the name. The hand-written
Go and TypeScript that still read the flag are listed by file and line: each fails to
compile until it changes, and the remaining code runs unconditionally, so the on branch is
inlined to make the feature permanent or the feature's code is deleted to abandon it.
--agent hands that list to the agent as the other transitions do. Nothing in the database
refuses the removal: the next deploy's MigrateFeatures deletes the row, and the flip
records stay.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTransition(cmd, &f, transition_.RemoveFeature{Name: args[0]}, "")
		},
	}
	f.bind(cmd)

	return cmd
}

func newRemoveOutlet() *cobra.Command {
	var f transitionFlags

	cmd := &cobra.Command{
		Use:   "outlet <name>",
		Short: "Remove a router outlet: its declaration, generated client, and browser project",
		Long: `outlet removes a router outlet from every site that declares it. The generator program
loses WithRouterOutlet and, for a session outlet, the GenerateTypescript target for the
outlet; the outlet's browser project is deleted and taken out of angular.json, the package
scripts, and the Procfile; @outlet lists that name the outlet beside others drop it; and
go generate runs.

The router group that mounted it, the App's handlers for it, its configuration and
environment lines, its tests, and the structs that were on the outlet alone (each moves to
the default outlet or leaves the application, a decision about who may reach it) are handed
to the agent. The auth the outlet was bound to stays.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTransition(cmd, &f, transition_.RemoveOutlet{Name: args[0]}, transition_.ReferenceCandidate)
		},
	}
	f.bind(cmd)

	return cmd
}

func newRemoveSite() *cobra.Command {
	var f transitionFlags

	cmd := &cobra.Command{
		Use:   "site <name>",
		Short: "Remove a site: its tree under apps/<name>/, generator, shared target, union element, and processes",
		Long: `site removes a site from an application in the sites layout: apps/<name>/ is deleted
with its generator program and directive, its TypeScript target leaves the shared
generator, its router collection leaves the union the roles are reconciled against, its
processes leave the Procfile, and go generate runs. The remaining sites stay where they
are: an application left with one site keeps the sites layout, and nothing moves back to
the root. The application's last site is not removed.

The integration suite that served the site, the deployment configuration outside the
repository, and the tables only the site's resources declared are handed to the agent.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTransition(cmd, &f, transition_.RemoveSite{Name: args[0]}, transition_.SitesReference)
		},
	}
	f.bind(cmd)

	return cmd
}
