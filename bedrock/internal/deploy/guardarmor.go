// guardarmor.go is the Cloud Armor guard: the check-and-plan job's gate for the rule
// bedrock check applies by hand, read against the branch the pull request merges into
// through GitHub, since the job's checkout holds the pull request's commit alone.

package deploy

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/check"
	"github.com/cccteam/ccc/bedrock/internal/github"
)

// guardCloudArmor refuses a pull request that removes an environment's cloud_armor entry
// from the stack's terraform.tfvars in one step while the base branch has the policy on
// ("preview" or "enforce"): the apply would detach the policy from the backend services
// and destroy it in one go, which fails while the policy is attached, so the entry is set
// to "off" first, merged and applied, then removed. The rule is bedrock check's
// (check.CompareCloudArmor); by hand the check compares the working tree with the
// default branch through git, and so says nothing in the job's checkout, a single
// commit, which is why the guard reads the base branch's copy (the default branch, or the
// patch line the pull request is against) through GitHub with the repository's token. A
// refusal is said on the pull request, as a failing plan is. A tag build has no base
// branch and nothing to guard; a base branch without the file, and a token that could
// not be minted, leave nothing to compare, said in the log.
func guardCloudArmor(ctx context.Context, clients *Clients, w Workspace, build *Build, env map[string]string, out io.Writer) error {
	subs := build.Substitutions
	if subs[prNumberSub] == "" {
		return nil
	}
	branch := subs[baseBranchSub]
	if branch == "" {
		branch = subs[defaultBranchSub]
	}
	if env[githubTokenFact] == "" {
		fmt.Fprintf(out, "Cloud Armor guard: without the repository's token the copy of %s on %s is not read, and nothing is compared.\n", stackTfvars, branch)

		return nil
	}
	owner, repo, err := splitRepo(subs[repoFullNameSub])
	if err != nil {
		return err
	}
	now, err := os.ReadFile(filepath.Join(string(w), filepath.FromSlash(stackTfvars)))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return errors.Wrap(err, "os.ReadFile()")
	}
	committed, err := clients.GitHub(env[githubTokenFact]).Contents(ctx, owner, repo, stackTfvars, branch)
	if err != nil {
		if !github.NotFound(err) {
			return err
		}
		fmt.Fprintf(out, "Cloud Armor guard: %s has no %s, so no policy was on there; nothing to compare.\n", branch, stackTfvars)

		return nil
	}
	findings, err := check.CompareCloudArmor(now, committed, stackTfvars, branch)
	if err != nil {
		return err
	}
	if len(findings) == 0 {
		fmt.Fprintf(out, "Cloud Armor guard passed: no policy removed in one step against %s.\n", branch)

		return nil
	}
	problems := make([]string, 0, len(findings))
	for i := range findings {
		problems = append(problems, findings[i].Problem())
		fmt.Fprintln(out, rejected+findings[i].Problem())
	}
	cause := errors.New(rejected + strings.Join(problems, "; "))

	return refuse(ctx, clients, build, env, fmt.Sprintf("The pull-request build stopped before the stack's plans (build %s): %v", build.ID, errors.Cause(cause)), cause, out)
}
