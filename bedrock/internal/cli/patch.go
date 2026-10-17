package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/spf13/cobra"

	"github.com/cccteam/ccc/bedrock/internal/deploy"
	"github.com/cccteam/ccc/bedrock/internal/patch"
	"github.com/cccteam/ccc/bedrock/internal/where"
)

// patchUse names the command group; patchAlias is its former name, which still runs it.
const (
	patchUse   = "patch"
	patchAlias = "hotfix"
)

// patchRule is what every patch release is told at its start: how it deploys, and what an
// environment ahead of it costs.
const patchRule = `A patch release deploys through the environments like any release: tst, then stg, then prd, each after the one before holds it live. An environment that ran a later release may hold a migration or seed file the patch release does not carry: the pipeline's release check refuses the patch release there and names the file; the environment is restored to the patch release first, from an empty database or from production's backup, since a restore run replaces the database and skips the check. At production's door the patch release must be on the line production runs. The restore is bedrock restore <env> <release>, started from GitHub.`

// newPatch is the command group over patch lines.
func newPatch(d deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:     patchUse,
		Aliases: []string{patchAlias},
		Short:   "Patch lines: patch releases of the version production runs (bedrock hotfix is this command's old name)",
		Long: `patch is the new name of bedrock hotfix, which still runs it: a patch release of the version
production runs, whatever the reason (a fix a person makes because something is broken, routine
maintenance a team makes in its window, a security update by severity). patch holds the commands
over patch lines. A line is the branch patch/<major>.<minor>.x, started at a release's commit, on
which release-please releases changes as the line's next patch versions while the default branch
moves on. The window, the actor, who approves and when are the team's configuration, never the
framework's: the placement carries maintenanceWindow for the team, and status prints it.`,
	}
	cmd.AddCommand(newPatchStatus(d), newPatchStart(d), newPatchMerge(d))

	return cmd
}

// newPatchStart is patch start <tag>.
func newPatchStart(d deps) *cobra.Command {
	var dirFlag, placementFlag string
	cmd := &cobra.Command{
		Use:   "start <tag>",
		Short: "Start the patch line of a release",
		Long: `start creates the branch patch/<major>.<minor>.x at the commit of the release tagged <tag>, a
release on the default branch, in the repository the command runs in. Fixes are pull requests
against that branch; release-please releases each as the line's next patch version, and the tag
runs through the environments like any release. When the default branch has already cut a later
patch of the same line, the branch starts at a commit on the release's commit whose message
names the line's next release for release-please (its Release-As footer: the patch after the
highest one cut), so the line's first release skips to it; the manifest stays the release's,
since release-please counts the line's commits from the manifest's release on the branch. A line
that already has its branch is reported and left as it is.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			rc, err := d.repository(dirFlag, placementFlag)
			if err != nil {
				return err
			}
			client, err := d.github(ctx)
			if err != nil {
				return err
			}
			req := patch.Request{Owner: rc.owner, Repo: rc.repo, DefaultBranch: rc.placement.DefaultBranch, Tag: args[0]}
			result, err := patch.Start(ctx, client, req)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			where := rc.owner + "/" + rc.repo
			switch {
			case result.Existed && !result.Carries:
				return errors.Newf("%s already exists in %s at %s, which does not carry %s: the line started from an earlier release and serves the patches after it, so a patch release of %s has no line to start on while that branch stands (the repository rules forbid deleting a patch branch)", result.Branch, where, short(result.BranchCommit), result.Tag, result.Tag)
			case result.Existed:
				fmt.Fprintf(out, "%s already exists in %s at %s: open the fix pull request against it.\n", result.Branch, where, short(result.BranchCommit))
			case result.Skipped != "":
				fmt.Fprintf(out, "Created %s in %s at a commit on %s (%s) whose message names the line's next release, %s, since %s has already cut %s.\n",
					result.Branch, where, short(result.Commit), result.Tag, result.Next, rc.placement.DefaultBranch, result.Skipped)
			default:
				fmt.Fprintf(out, "Created %s in %s at %s (%s); the line's next release is %s.\n", result.Branch, where, short(result.Commit), result.Tag, result.Next)
			}
			if !result.Existed {
				fmt.Fprintf(out, "Next: open the fix pull request against %s and merge it; release-please releases the branch as %s, and the tag runs through the environments like any release. When the patch release is out, bring it to %s with bedrock patch merge %s: a branch at the release's commit and its pull request into %s, squash-merged; %s is never the pull request's head.\n",
					result.Branch, result.Next, rc.placement.DefaultBranch, result.Next, rc.placement.DefaultBranch, result.Branch)
			}
			if result.Latest != result.Tag {
				fmt.Fprintf(out, "Warning: %s is not the repository's latest release (%s is). A patch release is based on the release production runs; make sure production runs %s before fixing on %s. GitHub does not know what production runs; production's deployment record does.\n",
					result.Tag, result.Latest, result.Tag, result.Branch)
			}
			fmt.Fprintln(out, patchRule)

			return nil
		},
	}
	cmd.Flags().StringVar(&dirFlag, "dir", "", "the stack directory holding the placement (default: the application's stack, found from the working directory)")
	cmd.Flags().StringVar(&placementFlag, "placement", "", "placement file (default: placement.json in the stack directory)")

	return cmd
}

// newPatchStatus is patch status: the known read of the line's state.
func newPatchStatus(d deps) *cobra.Command {
	var dirFlag, placementFlag string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "What the line of production's release holds: the live releases, a pending patch release, the merge-backs owed",
		Long: `status reads what a team needs before making a patch release, so nobody interprets the records
bucket or the repository by hand: the maintenance window the placement declares; the release each
environment runs, from its newest live deployment record; the patch line of the release production
runs (patch/<major>.<minor>.x), whether its branch exists, and whether a patch release is pending on
it (the branch has commits past its newest release: a release pull request in flight, or changes no
release carries yet); and every patch release the default branch does not carry, with its merge-back
pull request, merged or owed. The last line says whether a patch release may start: the same release
live in every environment, nothing pending on the line, and nothing owed.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			rc, err := d.repository(dirFlag, placementFlag)
			if err != nil {
				return err
			}
			app, err := d.applicationName(dirFlag)
			if err != nil {
				return err
			}
			client, err := d.github(ctx)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			p := rc.placement
			if p.MaintenanceWindow != "" {
				fmt.Fprintf(out, "Maintenance window: %s (placement.json maintenanceWindow; the team's to act on, bedrock acts on it nowhere).\n", p.MaintenanceWindow)
			} else {
				fmt.Fprintln(out, "Maintenance window: none declared (placement.json maintenanceWindow).")
			}
			live, err := deploy.LiveReleases(ctx, d.deploy, p, app)
			if err != nil {
				return err
			}
			notReady := liveLines(out, live)
			production := live[len(live)-1]
			if production.Release == "" {
				fmt.Fprintf(out, "Production (%s) runs nothing live, so there is no line to read yet.\n", production.Env)
			} else {
				st, err := patch.State(ctx, client, rc.owner, rc.repo, production.Release)
				if err != nil {
					return err
				}
				notReady = append(notReady, lineLine(out, st, production.Release)...)
			}
			backs, err := patch.MergeBacks(ctx, client, rc.owner, rc.repo, p.DefaultBranch)
			if err != nil {
				return err
			}
			notReady = append(notReady, mergeBackLines(out, backs, p.DefaultBranch)...)
			if len(notReady) == 0 {
				fmt.Fprintln(out, "Ready for a patch release: yes.")
			} else {
				fmt.Fprintf(out, "Ready for a patch release: no; %s.\n", strings.Join(notReady, ", "))
			}

			return nil
		},
	}
	cmd.Flags().StringVar(&dirFlag, "dir", "", "the stack directory holding the placement (default: the application's stack, found from the working directory)")
	cmd.Flags().StringVar(&placementFlag, "placement", "", "placement file (default: placement.json in the stack directory)")

	return cmd
}

// liveLines prints what each environment runs and answers the reason a patch release may
// not start, when the same release is not live everywhere.
func liveLines(out io.Writer, live []deploy.LiveRelease) []string {
	production := live[len(live)-1]
	same := true
	for _, l := range live {
		switch {
		case !l.Applied:
			fmt.Fprintf(out, "%s: no stack applied; nothing runs there.\n", l.Env)
			same = false
		case l.Release == "":
			fmt.Fprintf(out, "%s: nothing live.\n", l.Env)
			same = false
		default:
			fmt.Fprintf(out, "%s: %s live since %s (build %s).\n", l.Env, l.Release, l.Since, l.Build)
			if l.Release != production.Release {
				same = false
			}
		}
	}
	if same {
		return nil
	}

	return []string{"the same release is not live in every environment"}
}

// lineLine prints the line of production's release and answers the reason a patch
// release may not start, when one is pending on it.
func lineLine(out io.Writer, st *patch.LineState, production string) []string {
	switch {
	case !st.Exists:
		fmt.Fprintf(out, "Line: %s does not exist yet; bedrock patch start %s makes it at %s's commit.\n", st.Line, production, production)
	case st.Pending:
		fmt.Fprintf(out, "Line: %s at %s, past its newest release %s (%s): a patch release is pending (a release pull request in flight, or changes no release carries yet).\n", st.Line, short(st.Tip), st.Tag, short(st.TagCommit))

		return []string{"a patch release is pending on " + st.Line}
	default:
		fmt.Fprintf(out, "Line: %s at %s, which is its newest release %s; nothing pending.\n", st.Line, short(st.Tip), st.Tag)
	}

	return nil
}

// mergeBackLines prints the merge-backs the default branch holds or owes and answers
// the reasons a patch release may not start, one per release not merged back.
func mergeBackLines(out io.Writer, backs []patch.MergeBack, defaultBranch string) []string {
	if len(backs) == 0 {
		fmt.Fprintf(out, "Merge-backs: none owed; %s carries every release.\n", defaultBranch)

		return nil
	}
	var owed []string
	for _, b := range backs {
		switch {
		case b.Merged:
			fmt.Fprintf(out, "Merge-back: %s (%s) is merged back by #%d: %s\n", b.Tag, b.Line, b.Number, b.URL)
		case b.Number > 0:
			fmt.Fprintf(out, "Merge-back owed: %s (%s) has pull request #%d from %s open, not merged: %s\n", b.Tag, b.Line, b.Number, patch.MergeBranch(b.Tag), b.URL)
			owed = append(owed, b.Tag+" is not merged back")
		default:
			fmt.Fprintf(out, "Merge-back owed: %s (%s) has no pull request from %s; bedrock patch merge %s opens it.\n", b.Tag, b.Line, patch.MergeBranch(b.Tag), b.Tag)
			owed = append(owed, b.Tag+" is not merged back")
		}
	}

	return owed
}

// applicationName is the application the stack is for: the one application of the
// repository layout, read the way teardown reads it.
func (d deps) applicationName(dirFlag string) (string, error) {
	infraRoot, _, err := d.infrastructureRoot(dirFlag)
	if err != nil {
		return "", err
	}

	return where.SingleApplication(infraRoot)
}

// short is the first seven characters of a commit, as git prints them.
func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}

	return sha
}

// newPatchMerge is patch merge <tag>.
func newPatchMerge(d deps) *cobra.Command {
	var dirFlag, placementFlag string
	cmd := &cobra.Command{
		Use:   "merge <tag>",
		Short: "Bring a released patch to the default branch by pull request",
		Long: `merge creates the branch merge-back/<tag> at the commit of the patch release tagged <tag>, a
release on a patch line, and opens a pull request from it into the default branch, titled
fix: <tag> (the conventional-commit line the squash merge carries and release-please reads; edit
it if the fix deserves a better line) with the release's notes as its body. Conflicts, such as
release-please's manifest and changelog when the default branch has released since the line's
base, are resolved by commits on that branch; the squash merge deletes it, and the patch line is
never the pull request's head, so it is never deleted and never receives a conflict commit. A
release that is not on a patch line, or that the default branch already carries, is refused; an
existing merge-back branch or pull request for the release is reported and left as it is.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			rc, err := d.repository(dirFlag, placementFlag)
			if err != nil {
				return err
			}
			client, err := d.github(ctx)
			if err != nil {
				return err
			}
			req := patch.MergeRequest{Owner: rc.owner, Repo: rc.repo, DefaultBranch: rc.placement.DefaultBranch, Tag: args[0]}
			result, err := patch.Merge(ctx, client, req)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			where := rc.owner + "/" + rc.repo
			switch {
			case result.Existed && result.Merged:
				fmt.Fprintf(out, "%s already has its pull request into %s, #%d, merged: %s. Nothing was created.\n", result.Branch, rc.placement.DefaultBranch, result.Number, result.URL)

				return nil
			case result.Existed:
				fmt.Fprintf(out, "%s already has its pull request into %s, #%d, open: %s. Nothing was created.\n", result.Branch, rc.placement.DefaultBranch, result.Number, result.URL)
			case result.BranchExisted:
				fmt.Fprintf(out, "%s already existed in %s at %s; opened pull request #%d into %s: %s\n", result.Branch, where, short(result.BranchCommit), result.Number, rc.placement.DefaultBranch, result.URL)
			default:
				fmt.Fprintf(out, "Created %s in %s at %s (%s) and opened pull request #%d into %s: %s\n", result.Branch, where, short(result.Commit), result.Tag, result.Number, rc.placement.DefaultBranch, result.URL)
			}
			fmt.Fprintf(out, "Its title, %q, is the commit line the squash merge carries and release-please reads; edit it if the fix deserves a better line. Resolve any conflict with commits on %s (release-please's manifest and changelog when %s has released since the line's base, or code %s has reworked), then squash-merge: the merge deletes %s, and %s stays as it is.\n",
				result.Title, result.Branch, rc.placement.DefaultBranch, rc.placement.DefaultBranch, result.Branch, result.Line)

			return nil
		},
	}
	cmd.Flags().StringVar(&dirFlag, "dir", "", "the stack directory holding the placement (default: the application's stack, found from the working directory)")
	cmd.Flags().StringVar(&placementFlag, "placement", "", "placement file (default: placement.json in the stack directory)")

	return cmd
}
