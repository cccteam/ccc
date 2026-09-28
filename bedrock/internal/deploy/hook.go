// hook.go runs an application's hook at its stage.

package deploy

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/hook"
)

// Hook runs the application's script for the stage (infrastructure/hooks/<stage>.sh) in
// the checkout, as the build's deploy identity, with every fact of the environment file
// and every substitution of the build in its environment. A hook before the build may
// add build arguments by appending NAME=value lines to the build arguments file
// (BUILD_ARGS_FILE names it). No script, nothing runs; a failing script stops the build
// at its stage. after-down runs only on a teardown, and the others never do.
func Hook(ctx context.Context, clients *Clients, w Workspace, stage hook.Stage, out io.Writer) error {
	if !stage.Valid() {
		return errors.Newf("%q is not a hook stage (the stages are %v)", stage, hook.Stages)
	}
	env, err := w.Environment()
	if err != nil {
		return err
	}
	if stage == hook.AfterDown {
		if env[downFact] != trueValue {
			fmt.Fprintln(out, "Not a teardown: nothing to run after down.")

			return nil
		}
	} else if env[skipDeploy] == trueValue {
		fmt.Fprintln(out, tornDown)

		return nil
	}
	script := stage.Script()
	if _, err := os.Stat(filepath.Join(string(w), filepath.FromSlash(script))); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(out, "No hook at %s: nothing to run at %s.\n", script, stage)

			return nil
		}

		return errors.Wrap(err, "os.Stat()")
	}
	fmt.Fprintf(out, "=== Hook %s: %s ===\n", stage, script)
	vars := make([]string, 0, len(env)+1)
	for name, value := range env {
		vars = append(vars, name+"="+value)
	}
	sort.Strings(vars)
	vars = append(vars, "BUILD_ARGS_FILE="+filepath.Join(string(w), BuildArgsFile))
	if err := clients.Exec.Run(ctx, Command{Dir: string(w), Env: vars, Name: "bash", Args: []string{script}}, out); err != nil {
		return errors.Newf("the %s hook failed (%s): %v", stage, script, err)
	}

	return nil
}
