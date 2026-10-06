// exec.go is the deploy sequence's seam to the programs a step runs: tofu for a pull
// request's stack, docker for the image build, bash for an application's hook script. A
// step builds the command; the Runner runs it, the operating system's in the pipeline and
// a fake in tests.

package deploy

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/go-playground/errors/v5"
)

// Command is one program run: its directory, the variables it gets beyond the process's
// own, and its name and arguments.
type Command struct {
	Dir  string
	Env  []string
	Name string
	Args []string
}

// String is the command as a log shows it.
func (c Command) String() string {
	return strings.Join(append([]string{c.Name}, c.Args...), " ")
}

// Runner runs programs.
type Runner interface {
	// Run runs the command with its output and errors on out.
	Run(ctx context.Context, c Command, out io.Writer) error
	// Output runs the command and answers what it wrote to standard output; its
	// errors go to errOut.
	Output(ctx context.Context, c Command, errOut io.Writer) ([]byte, error)
}

// OSRunner runs programs on the operating system, found on PATH.
type OSRunner struct{}

// Run runs the command.
func (OSRunner) Run(ctx context.Context, c Command, out io.Writer) error {
	cmd := command(ctx, c)
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Run(); err != nil {
		return errors.Newf("%s failed: %v", c.Name, err)
	}

	return nil
}

// Output runs the command and answers its standard output.
func (OSRunner) Output(ctx context.Context, c Command, errOut io.Writer) ([]byte, error) {
	var stdout bytes.Buffer
	cmd := command(ctx, c)
	cmd.Stdout, cmd.Stderr = &stdout, errOut
	if err := cmd.Run(); err != nil {
		return nil, errors.Newf("%s failed: %v", c.Name, err)
	}

	return stdout.Bytes(), nil
}

func command(ctx context.Context, c Command) *exec.Cmd {
	cmd := exec.CommandContext(ctx, c.Name, c.Args...)
	cmd.Dir = c.Dir
	cmd.Env = append(os.Environ(), c.Env...)

	return cmd
}
