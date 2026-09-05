// Package cli is the command tree of the impulse tool.
package cli

import (
	"fmt"
	"os"
	"runtime/debug"

	"github.com/go-playground/errors/v5"
	"github.com/spf13/cobra"
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
	root.AddCommand(newCheck())

	return root
}

func version() string {
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" {
		return "(devel)"
	}

	return info.Main.Version
}

// exitError carries a process exit code out of a command without printing anything.
type exitError struct {
	code int
}

func (e exitError) Error() string {
	return fmt.Sprintf("exit %d", e.code)
}
