// hand.go drives tofu in a layer's directory as the person running the command, with
// their own sign-in and no identity impersonated: bedrock org destroy's two layers
// (destroy.go), and the apply of 0-bootstrap bedrock upgrade makes when the pin changes
// kind, since the triggers' steps change shape with it and 0-bootstrap is the one layer
// no trigger applies.

package layers

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/deploy"
)

// HandApply is what ApplyByHand did: the plan's summary line (Plan: 0 to add, 2 to
// change, 0 to destroy.) and the apply's (Apply complete! Resources: ...).
type HandApply struct {
	PlanSummary  string
	ApplySummary string
}

// ApplyByHand applies the layer in the checkout as the person running the command: the
// init at the layer's own backend, the plan into a saved plan, and the apply of that
// plan, with tofu's output on out. The data directory is under the command's working
// files, so the checkout's own init stays as it was.
func ApplyByHand(ctx context.Context, exec deploy.Runner, dir, layer string, out io.Writer) (*HandApply, error) {
	if exec == nil {
		return nil, errors.New("no program runner is wired")
	}
	work, err := os.MkdirTemp("", "bedrock-apply-"+layer+"-")
	if err != nil {
		return nil, errors.Wrap(err, "os.MkdirTemp()")
	}
	defer os.RemoveAll(work)
	h := &handTofu{exec: exec, layer: layer, dir: filepath.Join(dir, layer), data: filepath.Join(work, "data"), out: out}
	if err := h.init(ctx); err != nil {
		return nil, err
	}
	plan := filepath.Join(work, planFile)
	planned, err := h.plan(ctx, plan)
	if err != nil {
		return nil, err
	}
	applied, err := h.applyPlan(ctx, plan)
	if err != nil {
		return nil, err
	}

	return &HandApply{PlanSummary: summaryOf(planned), ApplySummary: lastSummary(applied)}, nil
}

// handTofu drives tofu in one layer's directory as the person running the command: no
// identity is impersonated, the data directory is under the command's working files so
// that the checkout's own init stays as it was (empty for a copy, which keeps its own),
// and the environment carries the GitHub token for 1-org.
type handTofu struct {
	exec  deploy.Runner
	layer string
	dir   string
	data  string
	env   []string
	out   io.Writer
}

// command is one tofu command in the layer's directory.
func (h *handTofu) command(args ...string) deploy.Command {
	env := slices.Clone(h.env)
	if h.data != "" {
		env = append(env, dataDirEnv+"="+h.data)
	}

	return deploy.Command{Dir: h.dir, Env: env, Name: "tofu", Args: args}
}

// run runs one tofu command with its output on the command's.
func (h *handTofu) run(ctx context.Context, args ...string) error {
	return h.exec.Run(ctx, h.command(args...), h.out)
}

// output runs one tofu command and answers what it printed.
func (h *handTofu) output(ctx context.Context, args ...string) ([]byte, error) {
	return h.exec.Output(ctx, h.command(args...), h.out)
}

// init initializes the layer at its own backend, again when the data directory holds an
// earlier init of another shape.
func (h *handTofu) init(ctx context.Context) error {
	if err := h.run(ctx, "init", noInput, noColor, "-reconfigure"); err != nil {
		return errors.Wrapf(err, "the init of %s failed; the log says why", h.layer)
	}

	return nil
}

// stateList is every address in the layer's state.
func (h *handTofu) stateList(ctx context.Context) ([]string, error) {
	printed, err := h.output(ctx, "state", "list")
	if err != nil {
		return nil, errors.Wrapf(err, "listing %s's state failed; the log says why", h.layer)
	}
	var addresses []string
	for _, line := range strings.Split(string(printed), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			addresses = append(addresses, line)
		}
	}

	return addresses, nil
}

// plan plans the layer into the saved plan at path, with the output on the command's
// too, and answers what tofu printed.
func (h *handTofu) plan(ctx context.Context, path string) (string, error) {
	var printed bytes.Buffer
	if err := h.exec.Run(ctx, h.command(planCommand, noInput, noColor, "-out="+path), io.MultiWriter(&printed, h.out)); err != nil {
		return "", errors.Wrapf(err, "the plan of %s failed; the log says why", h.layer)
	}

	return printed.String(), nil
}

// applyPlan applies the saved plan at path, with the output on the command's too, and
// answers what tofu printed.
func (h *handTofu) applyPlan(ctx context.Context, path string) (string, error) {
	var printed bytes.Buffer
	if err := h.exec.Run(ctx, h.command(applyCommand, noInput, noColor, path), io.MultiWriter(&printed, h.out)); err != nil {
		return "", errors.Wrapf(err, "the apply of %s failed; the log says why", h.layer)
	}

	return printed.String(), nil
}
