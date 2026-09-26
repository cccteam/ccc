package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/cccteam/ccc/bedrock/internal/hotfix"
)

// hotfixUse names the command group.
const hotfixUse = "hotfix"

// newHotfix is the command group over hotfix lines.
func newHotfix(d deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   hotfixUse,
		Short: "Hotfix lines: fixes released from a branch beside the default branch",
		Long: `hotfix holds the commands over hotfix lines. A line is the branch hotfix/<major>.<minor>.x,
started at a release's commit, on which release-please releases fixes as the line's next patch
versions while the default branch moves on.`,
	}
	cmd.AddCommand(newHotfixStart(d))

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
patch of the same line, the branch starts at a commit that sets release-please's manifest to
that patch, so the line's next release skips past it. A line that already has its branch is
reported and left as it is.`,
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
			if result.Existed {
				fmt.Fprintf(out, "%s already exists in %s at %s: open the fix pull request against it.\n", result.Branch, where, short(result.BranchCommit))

				return nil
			}
			if result.Skipped != "" {
				fmt.Fprintf(out, "Created %s in %s at a commit on %s (%s) that sets the manifest to %s, which %s has already cut; the line's next release is %s.\n",
					result.Branch, where, short(result.Commit), result.Tag, result.Skipped[1:], rc.placement.DefaultBranch, result.Next)
			} else {
				fmt.Fprintf(out, "Created %s in %s at %s (%s); the line's next release is %s.\n", result.Branch, where, short(result.Commit), result.Tag, result.Next)
			}
			fmt.Fprintf(out, "Next: open the fix pull request against %s and merge it; release-please releases the branch as %s, and the tag runs through the environments like any release. When the hotfix is out, merge %s back into %s by pull request with a merge commit, so %s carries the fix and counts releases from it.\n",
				result.Branch, result.Next, result.Branch, rc.placement.DefaultBranch, rc.placement.DefaultBranch)

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
