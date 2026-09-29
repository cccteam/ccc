package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-playground/errors/v5"
	"github.com/spf13/cobra"

	"github.com/cccteam/ccc/bedrock/internal/derive"
	"github.com/cccteam/ccc/bedrock/internal/protect"
	"github.com/cccteam/ccc/bedrock/internal/where"
)

// repositoryUse and protectUse name the command group and its command.
const (
	repositoryUse = "repository"
	protectUse    = "protect"
)

// newRepository is the command group over the application's GitHub repository itself.
func newRepository(d deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   repositoryUse,
		Short: "Rules on the application's GitHub repository",
		Long: `repository holds the commands that act on the application's GitHub repository: the one the
command runs in, addressed through its origin remote.`,
	}
	cmd.AddCommand(newRepositoryProtect(d))

	return cmd
}

// newRepositoryProtect is repository protect.
func newRepositoryProtect(d deps) *cobra.Command {
	var dirFlag, placementFlag string
	cmd := &cobra.Command{
		Use:   protectUse,
		Short: "Put the release and branch rules on the repository",
		Long: `protect creates, or brings back in line, the three rulesets of an application repository:
release tags (v* and */v*) are created, moved or deleted only by the release app, with no bypass
for the repository's admins; the default branch and the hotfix branches
(hotfix/<major>.<minor>.x) change only by pull request, with no force push and no deletion. A
ruleset that already matches is left alone, and rulesets bedrock did not make are not touched.

The repository is the origin of the repository the command runs in. The default branch and the
release app come from the placement beside the stack. The token is GITHUB_TOKEN when set, else
the one gh holds for its signed-in account; it must belong to an admin of the organization.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			rc, err := d.repository(dirFlag, placementFlag)
			if err != nil {
				return err
			}
			client, err := d.github(ctx)
			if err != nil {
				return err
			}
			req := protect.Request{Owner: rc.owner, Repo: rc.repo, DefaultBranch: rc.placement.DefaultBranch, ReleaseApp: rc.placement.ReleaseApp}
			result, err := protect.Apply(ctx, client, req)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Repository %s/%s, release app %s (app %d):\n", rc.owner, rc.repo, rc.placement.ReleaseApp, result.ReleaseAppID)
			for _, rs := range result.Rulesets {
				fmt.Fprintf(out, "  %-16s %s (id %d): %s\n", rs.Name, rs.Action, rs.ID, rs.Effect)
			}

			return nil
		},
	}
	cmd.Flags().StringVar(&dirFlag, "dir", "", "the stack directory holding the placement (default: the application's stack, found from the working directory)")
	cmd.Flags().StringVar(&placementFlag, "placement", "", "placement file (default: placement.json in the stack directory)")

	return cmd
}

// repositoryContext is the repository a command acts on and the placement that says
// how: the origin of the repository the command runs in, and the placement beside the
// stack.
type repositoryContext struct {
	owner     string
	repo      string
	placement *derive.Placement
}

// repository finds the repository from the working directory and reads the placement:
// --placement as given, else placement.json in the stack (--dir, or the one found).
func (d deps) repository(dirFlag, placementFlag string) (*repositoryContext, error) {
	start := d.cwd
	if start == "" {
		var err error
		start, err = os.Getwd()
		if err != nil {
			return nil, errors.Wrap(err, "os.Getwd()")
		}
	}
	repoRoot, err := where.RepoRoot(start)
	if err != nil {
		return nil, err
	}
	owner, repo, err := where.Remote(repoRoot)
	if err != nil {
		return nil, err
	}
	file := placementFlag
	if file == "" {
		_, stackDir, err := d.stack("", dirFlag)
		if err != nil {
			return nil, err
		}
		file = filepath.Join(stackDir, placementFile)
	}
	p, err := derive.ReadPlacement(file)
	if err != nil {
		return nil, err
	}

	return &repositoryContext{owner: owner, repo: repo, placement: p}, nil
}
