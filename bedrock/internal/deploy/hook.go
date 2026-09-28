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

// Hook runs the application's hook for the stage in the checkout, as the build's deploy
// identity, with every fact of the environment file and every substitution of the build
// in its environment: the script (infrastructure/hooks/<stage>.sh), run with bash; or,
// when program names one, the hooks program the image build took out of the image, run
// with the stage as its argument. A hook before the build may add build arguments by
// appending NAME=value lines to the build arguments file (BUILD_ARGS_FILE names it). No
// script, nothing runs; a failing hook stops the build at its stage. after-down runs only
// on a teardown, and the others never do.
func Hook(ctx context.Context, clients *Clients, w Workspace, stage hook.Stage, program string, out io.Writer) error {
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
	c, ok, err := hookCommand(w, stage, program, out)
	if err != nil || !ok {
		return err
	}
	vars := make([]string, 0, len(env)+1)
	for name, value := range env {
		vars = append(vars, name+"="+value)
	}
	sort.Strings(vars)
	vars = append(vars, "BUILD_ARGS_FILE="+filepath.Join(string(w), BuildArgsFile))
	c.Dir, c.Env = string(w), vars
	if err := clients.Exec.Run(ctx, c, out); err != nil {
		return errors.Newf("the %s hook failed (%s): %v", stage, c, err)
	}

	return nil
}

// hookCommand is what runs the stage's hook: the hooks program at program, or the
// script; ok is false, said on out, when there is no script.
func hookCommand(w Workspace, stage hook.Stage, program string, out io.Writer) (c Command, ok bool, err error) {
	if program != "" {
		if _, err := os.Stat(program); err != nil {
			return Command{}, false, errors.Newf("no hooks program at %s: the image build takes /hooks out of the image (build-image --hooks), and the Dockerfile builds it (go build -o /build/hooks ./cmd/deployment/hooks)", program)
		}
		fmt.Fprintf(out, "=== Hook %s: the hooks program ===\n", stage)

		return Command{Name: program, Args: []string{string(stage)}}, true, nil
	}
	script := stage.Script()
	if _, err := os.Stat(filepath.Join(string(w), filepath.FromSlash(script))); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(out, "No hook at %s: nothing to run at %s.\n", script, stage)

			return Command{}, false, nil
		}

		return Command{}, false, errors.Wrap(err, "os.Stat()")
	}
	fmt.Fprintf(out, "=== Hook %s: %s ===\n", stage, script)

	return Command{Name: "bash", Args: []string{script}}, true, nil
}
