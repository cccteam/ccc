// Command hasher is a CLI for the github.com/cccteam/ccc/securehash package.
// It generates and verifies password hashes using bcrypt or argon2.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/cccteam/ccc/securehash/cmd/hasher/internal/cli"
)

func main() {
	cmd := cli.NewRootCmd()
	if err := cmd.Execute(); err != nil {
		var exitErr *cli.ExitError
		if errors.As(err, &exitErr) {
			if exitErr.Message != "" {
				fmt.Fprintln(os.Stderr, "Error:", exitErr.Message)
			}
			os.Exit(exitErr.Code)
		}
		os.Exit(1)
	}
}
