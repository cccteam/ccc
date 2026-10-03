package cli

import (
	"os"
	"path/filepath"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/derive"
	"github.com/cccteam/ccc/bedrock/internal/where"
)

// repositoryContext is the repository a command acts on and the placement that says
// how: the origin of the repository the command runs in, and the placement beside the
// stack. The commands that take it act on the repository through the GitHub API (a
// restore's dispatch, a hotfix line's branches and pull requests); what configures the
// repository is declared in the organization's infrastructure, never by a command.
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
