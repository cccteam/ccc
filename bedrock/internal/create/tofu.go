// tofu.go is the creation's first applies of the new organization's two foundation
// layers: what a person does by hand for an organization bootstrapped by hand
// (0-bootstrap/README.md, "Apply with local state", then "Migrate the state into the
// bucket"; 1-org/README.md, "Applying"), done once by the creating identity against the
// state bucket the seed made, with no local state to migrate: tofu init at the layer's
// prefix, then an apply. 1-org's apply makes the organization's folders, projects and
// identities, and its outputs record them in the placement, as a person records
// project_ids and project_numbers by hand.

package create

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/deploy"
	"github.com/cccteam/ccc/bedrock/internal/org"
)

const (
	tofuProgram    = "tofu"
	identityEnv    = "GOOGLE_IMPERSONATE_SERVICE_ACCOUNT"
	githubEnv      = "GITHUB_TOKEN"
	noInput        = "-input=false"
	noColor        = "-no-color"
	bootstrapLayer = "0-bootstrap"
	orgLayer       = "1-org"
)

// The 1-org outputs the creation records (1-org/outputs.tf): the projects' ids and
// numbers, the folders' ids, and the public-invoker tag key and value of each environment
// project, which the root organization names in its policies.
const (
	projectIDsOutput     = "project_ids"
	projectNumbersOutput = "project_numbers"
	folderIDsOutput      = "folder_ids"
	tagKeyOutput         = "public_invoker_tag_key"
	tagValueOutput       = "public_invoker_tag_value"
)

// recordedOutputs are the outputs the creation reads, in this order.
var recordedOutputs = []string{projectIDsOutput, projectNumbersOutput, folderIDsOutput, tagKeyOutput, tagValueOutput}

// firstApply applies the rendered layer in the directory as the creating identity,
// against the organization's own state bucket, with env in the programs' environment
// beside the identity. 0-bootstrap adopts the folders, the project and the boot identity
// the creation made (its import blocks name them through the seeded tfvars) and makes
// the rest: the identities, the custom roles, the grants on the shared secrets, the
// connection from the recorded authorization, the repository link and the two triggers.
// 1-org makes the folders, the projects, their identities and policies.
func (f *creation) firstApply(ctx context.Context, dir string, h *org.Placement, layer string, env []string) error {
	identity := f.req.Root.CreatingIdentity()
	layerDir := filepath.Join(dir, layer)
	env = append([]string{identityEnv + "=" + identity}, env...)
	fmt.Fprintf(f.out, "=== first apply of %s's %s as %s, state in gs://%s/%s ===\n", h.Prefix, layer, identity, h.StateBucket, layer)
	init := deploy.Command{Dir: layerDir, Env: env, Name: tofuProgram, Args: []string{"init", noInput, noColor, "-backend-config=prefix=" + layer, "-backend-config=impersonate_service_account=" + identity}}
	if err := f.clients.Exec.Run(ctx, init, f.out); err != nil {
		return errors.Wrapf(err, "the init of %s's %s failed; the log says why", h.Prefix, layer)
	}
	apply := deploy.Command{Dir: layerDir, Env: env, Name: tofuProgram, Args: []string{"apply", noInput, noColor, "-auto-approve"}}
	if err := f.clients.Exec.Run(ctx, apply, f.out); err != nil {
		return errors.Wrapf(err, "the first apply of %s's %s failed; the log says why", h.Prefix, layer)
	}

	return nil
}

// organize is 1-org's first apply, with the infrastructure app's token for the layer's
// GitHub provider, and the record of what it made: the projects' ids and numbers go into
// the placement (projects, projectNumbers), with the folders' ids (folders) and each
// environment project's public-invoker tag (publicInvokerTags), which the root
// organization's policies name; the folders' ids come back for the withdrawal too.
func (f *creation) organize(ctx context.Context, dir string, h *org.Placement) (map[string]string, error) {
	env := []string{githubEnv + "=" + f.req.Token}
	if err := f.firstApply(ctx, dir, h, orgLayer, env); err != nil {
		return nil, err
	}
	outputs, err := f.outputs(ctx, filepath.Join(dir, orgLayer), h, env)
	if err != nil {
		return nil, err
	}
	for key, id := range outputs[projectIDsOutput] {
		h.Projects[key] = id
	}
	for key, number := range outputs[projectNumbersOutput] {
		h.ProjectNumbers[key] = number
	}
	h.Folders = outputs[folderIDsOutput]
	h.PublicInvokerTags = map[string]org.PublicInvokerTag{}
	for env, key := range outputs[tagKeyOutput] {
		value, ok := outputs[tagValueOutput][env]
		if !ok {
			return nil, errors.Newf("the outputs of %s's %s name a public-invoker tag key for %s and no value", h.Prefix, orgLayer, env)
		}
		h.PublicInvokerTags[env] = org.PublicInvokerTag{Key: key, Value: value}
	}
	fmt.Fprintf(f.out, "Applied %s's %s and recorded its %d projects, %d folders and %d public-invoker tags in the placement (projects, projectNumbers, folders, publicInvokerTags).\n", h.Prefix, orgLayer, len(outputs[projectIDsOutput]), len(outputs[folderIDsOutput]), len(h.PublicInvokerTags))

	return outputs[folderIDsOutput], nil
}

// outputs reads the outputs of 1-org the creation records, each a map by key.
func (f *creation) outputs(ctx context.Context, layerDir string, h *org.Placement, env []string) (map[string]map[string]string, error) {
	env = append([]string{identityEnv + "=" + f.req.Root.CreatingIdentity()}, env...)
	data, err := f.clients.Exec.Output(ctx, deploy.Command{Dir: layerDir, Env: env, Name: tofuProgram, Args: []string{"output", noColor, "-json"}}, f.out)
	if err != nil {
		return nil, errors.Wrapf(err, "reading the outputs of %s's %s", h.Prefix, orgLayer)
	}
	// Every output comes back, strings and maps alike; the recorded ones are maps by key,
	// and the rest are left as they are.
	var raw map[string]struct {
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, errors.Wrapf(err, "json.Unmarshal(): the outputs of %s's %s", h.Prefix, orgLayer)
	}
	outputs := map[string]map[string]string{}
	for _, name := range recordedOutputs {
		var value map[string]string
		if len(raw[name].Value) > 0 {
			if err := json.Unmarshal(raw[name].Value, &value); err != nil {
				return nil, errors.Wrapf(err, "json.Unmarshal(): the output %s of %s's %s, a map by key", name, h.Prefix, orgLayer)
			}
		}
		if len(value) == 0 {
			return nil, errors.Newf("the outputs of %s's %s lack %s, which the creation records", h.Prefix, orgLayer, name)
		}
		outputs[name] = value
	}

	return outputs, nil
}
