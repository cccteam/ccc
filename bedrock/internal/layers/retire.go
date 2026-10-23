// retire.go is the part of bedrock org retire that touches 1-org's state: a retired
// application's GitHub repository, with the settings 1-org declared on it, taken out of
// the state so that the layer, rendered without the application, deletes nothing on
// GitHub. The repository itself stays, archived by the command before this runs.

package layers

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/deploy"
)

// repositoryResourcePrefix opens the type of every resource 1-org declares on an
// application's repository: the repository (github_repository), its rulesets
// (github_repository_ruleset), its operations environments
// (github_repository_environment) and their deployment policies
// (github_repository_environment_deployment_policy).
const repositoryResourcePrefix = "github_repository"

// RetireRepository takes an application's GitHub repository and the settings 1-org
// declared on it out of 1-org's state, as the person running the command, with the
// GitHub token 1-org's provider takes: the repository is archived by then, so GitHub
// refuses every change to it and its settings, and a plan of the layer rendered without
// the application would otherwise try to delete them (the repository against its
// prevent_destroy). What is taken out stays on GitHub as it was, frozen with the
// archive. It answers the addresses taken out, in the state's order.
func RetireRepository(ctx context.Context, exec deploy.Runner, dir, app, githubToken string, out io.Writer) ([]string, error) {
	if exec == nil {
		return nil, errors.New("no program runner is wired")
	}
	work, err := os.MkdirTemp("", "bedrock-retire-")
	if err != nil {
		return nil, errors.Wrap(err, "os.MkdirTemp()")
	}
	defer os.RemoveAll(work)
	h := &handTofu{exec: exec, layer: orgLayer, dir: filepath.Join(dir, orgLayer), data: filepath.Join(work, orgLayer+".data"), env: []string{githubEnv + "=" + githubToken}, out: out}
	if err := h.init(ctx); err != nil {
		return nil, err
	}
	addresses, err := h.stateList(ctx)
	if err != nil {
		return nil, err
	}
	var taken []string
	for _, address := range addresses {
		if !repositoryResource(address, app) {
			continue
		}
		if err := h.run(ctx, "state", "rm", address); err != nil {
			return taken, errors.Wrapf(err, "taking %s out of %s's state failed; the log says why", address, orgLayer)
		}
		taken = append(taken, address)
	}

	return taken, nil
}

// repositoryResource reports whether a state address names a resource 1-org declares on
// the application's repository: a github_repository type whose instance key is the
// application's code alone (the repository, its release-tag ruleset) or the code
// followed by the separator a derived key carries (the branch rulesets' "code:line", the
// operations environments' "code-environment").
func repositoryResource(address, app string) bool {
	if !strings.HasPrefix(resourceType(address), repositoryResourcePrefix) {
		return false
	}
	key, ok := instanceKey(address)
	if !ok {
		return false
	}

	return key == app || strings.HasPrefix(key, app+":") || strings.HasPrefix(key, app+"-")
}

// instanceKey is the string key of a for_each instance in a state address, the text
// between the quotes of its last ["..."]; false for an address with none.
func instanceKey(address string) (string, bool) {
	start := strings.LastIndex(address, `["`)
	if start < 0 || !strings.HasSuffix(address, `"]`) {
		return "", false
	}

	return address[start+2 : len(address)-2], true
}
