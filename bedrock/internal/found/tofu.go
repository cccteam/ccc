// tofu.go is the founding's first apply of the new organization's 0-bootstrap: what a
// person does by hand for an organization bootstrapped by hand (0-bootstrap/README.md,
// "Apply with local state", then "Migrate the state into the bucket"), done once by the
// founding identity against the state bucket the seed made, with no local state to
// migrate: tofu init at the layer's prefix, then an apply.

package found

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/deploy"
	"github.com/cccteam/ccc/bedrock/internal/org"
)

const (
	identityEnv    = "GOOGLE_IMPERSONATE_SERVICE_ACCOUNT"
	noInput        = "-input=false"
	noColor        = "-no-color"
	bootstrapLayer = "0-bootstrap"
)

// firstApply applies the rendered 0-bootstrap in the directory as the founding identity,
// against the organization's own state bucket. The layer adopts the folders, the project
// and the boot identity the founding made (its import blocks name them through the seeded
// tfvars) and makes the rest: the identities, the custom roles, the grants on the shared
// secrets, the connection from the recorded authorization, the repository link and the
// two triggers.
func (f *founding) firstApply(ctx context.Context, dir string, h *org.Placement) error {
	identity := f.req.Root.FoundingIdentity()
	layer := filepath.Join(dir, bootstrapLayer)
	env := []string{identityEnv + "=" + identity}
	fmt.Fprintf(f.out, "=== first apply of %s's %s as %s, state in gs://%s/%s ===\n", h.Prefix, bootstrapLayer, identity, h.StateBucket, bootstrapLayer)
	init := deploy.Command{Dir: layer, Env: env, Name: "tofu", Args: []string{"init", noInput, noColor, "-backend-config=prefix=" + bootstrapLayer, "-backend-config=impersonate_service_account=" + identity}}
	if err := f.clients.Exec.Run(ctx, init, f.out); err != nil {
		return errors.Wrapf(err, "the init of %s's %s failed; the log says why", h.Prefix, bootstrapLayer)
	}
	apply := deploy.Command{Dir: layer, Env: env, Name: "tofu", Args: []string{"apply", noInput, noColor, "-auto-approve"}}
	if err := f.clients.Exec.Run(ctx, apply, f.out); err != nil {
		return errors.Wrapf(err, "the first apply of %s's %s failed; the log says why", h.Prefix, bootstrapLayer)
	}
	fmt.Fprintf(f.out, "Applied %s's %s: its triggers exist, and its own apply trigger applies every layer from the first push on.\n", h.Prefix, bootstrapLayer)

	return nil
}
