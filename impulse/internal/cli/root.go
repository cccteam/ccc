// Package cli is the command tree of the impulse tool.
package cli

import (
	"fmt"
	"os"

	"github.com/go-playground/errors/v5"
	"github.com/spf13/cobra"

	"github.com/cccteam/ccc/impulse/internal/check"
)

// Main runs the tool with the arguments and returns the process exit code.
func Main(args []string) int {
	root := newRoot()
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		var exit exitError
		if ok := asExit(err, &exit); ok {
			return exit.code
		}
		// The cause carries the message written for the user; the chain above it holds
		// source positions meant for a developer of the tool.
		fmt.Fprintln(os.Stderr, "impulse:", errors.Cause(err))

		return 2
	}

	return 0
}

func newRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           "impulse",
		Short:         "The command-line tool for Impulse applications",
		Long:          "impulse works on applications built on the cccteam libraries with ccc/resource at the center.\nIt reads the application's own code and configuration; it keeps no record of its own.",
		Version:       version(),
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newNew())
	root.AddCommand(newCheck())
	root.AddCommand(newAudit())
	root.AddCommand(newAdvise())
	root.AddCommand(newRender())
	root.AddCommand(newHandoff())
	root.AddCommand(newAdd())
	root.AddCommand(newSwap())
	root.AddCommand(newRemove())
	root.AddCommand(newUpgrade())

	return root
}

// version is what impulse --version prints: the running impulse's version from its build
// information, the same value the pins check compares go.mod's impulse pin with.
func version() string {
	return check.RunningVersion()
}

// failedExit is the status a command exits with when what it checked failed: impulse
// check with a failing check, impulse handoff with a handoff not clean. A command that
// could not run exits with 2, so a caller running the check as a command (the upgrade
// walk, through go tool impulse) tells a failing check from one that did not run.
const failedExit = 1

// exitError carries a process exit code out of a command without printing anything.
type exitError struct {
	code int
}

func (e exitError) Error() string {
	return fmt.Sprintf("exit %d", e.code)
}
