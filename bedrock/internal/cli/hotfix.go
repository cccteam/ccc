package cli

import (
	"fmt"

	"github.com/go-playground/errors/v5"

	"github.com/spf13/cobra"

	"github.com/cccteam/ccc/bedrock/internal/hotfix"
)

// hotfixUse names the command group.
const hotfixUse = "hotfix"

// hotfixRule is what every hotfix is told at its start: how it deploys, and what an
// environment ahead of it costs.
const hotfixRule = `A hotfix deploys through the environments like any release: tst, then stg, then prd, each after the one before holds it live. An environment that ran a later release may hold a migration or seed file the hotfix does not carry: the pipeline's release check refuses the hotfix there and names the file; the environment is restored to the hotfix first, from an empty database or from production's backup, since a restore run replaces the database and skips the check. At production's door the hotfix must be on the line production runs. The restore is bedrock restore <env> <release>, started from GitHub.`

// newHotfix is the command group over hotfix lines.
func newHotfix(d deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   hotfixUse,
		Short: "Hotfix lines: fixes released from a branch beside the default branch",
		Long: `hotfix holds the commands over hotfix lines. A line is the branch hotfix/<major>.<minor>.x,
started at a release's commit, on which release-please releases fixes as the line's next patch
versions while the default branch moves on.`,
	}
	cmd.AddCommand(newHotfixStart(d), newHotfixMerge(d))

	return cmd
}

// newHotfixStart is hotfix start <tag>.
func newHotfixStart(d deps) *cobra.Command {
	var dirFlag, placementFlag string
	cmd := &cobra.Command{
		Use:   "start <tag>",
		Short: "Start the hotfix line of a release",
		Long: `start creates the branch hotfix/<major>.<minor>.x at the commit of the release tagged <tag>, a
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
			req := hotfix.Request{Owner: rc.owner, Repo: rc.repo, DefaultBranch: rc.placement.DefaultBranch, Tag: args[0]}
			result, err := hotfix.Start(ctx, client, req)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			where := rc.owner + "/" + rc.repo
			switch {
			case result.Existed && !result.Carries:
				return errors.Newf("%s already exists in %s at %s, which does not carry %s: the line started from an earlier release and serves the patches after it, so a hotfix of %s has no line to start on while that branch stands (the repository rules forbid deleting a hotfix branch)", result.Branch, where, short(result.BranchCommit), result.Tag, result.Tag)
			case result.Existed:
				fmt.Fprintf(out, "%s already exists in %s at %s: open the fix pull request against it.\n", result.Branch, where, short(result.BranchCommit))
			case result.Skipped != "":
				fmt.Fprintf(out, "Created %s in %s at a commit on %s (%s) whose message names the line's next release, %s, since %s has already cut %s.\n",
					result.Branch, where, short(result.Commit), result.Tag, result.Next, rc.placement.DefaultBranch, result.Skipped)
			default:
				fmt.Fprintf(out, "Created %s in %s at %s (%s); the line's next release is %s.\n", result.Branch, where, short(result.Commit), result.Tag, result.Next)
			}
			if !result.Existed {
				fmt.Fprintf(out, "Next: open the fix pull request against %s and merge it; release-please releases the branch as %s, and the tag runs through the environments like any release. When the hotfix is out, bring it to %s with bedrock hotfix merge %s: a branch at the release's commit and its pull request into %s, squash-merged; %s is never the pull request's head.\n",
					result.Branch, result.Next, rc.placement.DefaultBranch, result.Next, rc.placement.DefaultBranch, result.Branch)
			}
			if result.Latest != result.Tag {
				fmt.Fprintf(out, "Warning: %s is not the repository's latest release (%s is). A hotfix is based on the release production runs; make sure production runs %s before fixing on %s. GitHub does not know what production runs; production's deployment record does.\n",
					result.Tag, result.Latest, result.Tag, result.Branch)
			}
			fmt.Fprintln(out, hotfixRule)

			return nil
		},
	}
	cmd.Flags().StringVar(&dirFlag, "dir", "", "the stack directory holding the placement (default: the application's stack, found from the working directory)")
	cmd.Flags().StringVar(&placementFlag, "placement", "", "placement file (default: placement.json in the stack directory)")

	return cmd
}

// short is the first seven characters of a commit, as git prints them.
func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}

	return sha
}

// newHotfixMerge is hotfix merge <tag>.
func newHotfixMerge(d deps) *cobra.Command {
	var dirFlag, placementFlag string
	cmd := &cobra.Command{
		Use:   "merge <tag>",
		Short: "Bring a released hotfix to the default branch by pull request",
		Long: `merge creates the branch merge-back/<tag> at the commit of the hotfix release tagged <tag>, a
release on a hotfix line, and opens a pull request from it into the default branch, titled
fix: <tag> (the conventional-commit line the squash merge carries and release-please reads; edit
it if the fix deserves a better line) with the release's notes as its body. Conflicts, such as
release-please's manifest and changelog when the default branch has released since the line's
base, are resolved by commits on that branch; the squash merge deletes it, and the hotfix line is
never the pull request's head, so it is never deleted and never receives a conflict commit. A
release that is not on a hotfix line, or that the default branch already carries, is refused; an
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
			req := hotfix.MergeRequest{Owner: rc.owner, Repo: rc.repo, DefaultBranch: rc.placement.DefaultBranch, Tag: args[0]}
			result, err := hotfix.Merge(ctx, client, req)
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
