package main

import (
	"fmt"
	"go/build"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/go-playground/errors/v5"
)

type ccclint struct {
	pluginVersion string
	verbose       bool
}

func (c *ccclint) run() error {
	cloneDir, err := os.MkdirTemp("", "ccc-lint-")
	if err != nil {
		return errors.Wrap(err, "os.MkdirTemp()")
	}
	defer os.RemoveAll(cloneDir)

	if err := c.execCommand("", "git", "clone", "https://github.com/cccteam/ccc.git", cloneDir); err != nil {
		return errors.Wrap(err, "git clone")
	}

	if err := c.execCommand(cloneDir, "git", "checkout", "lint/"+c.pluginVersion); err != nil {
		return errors.Wrap(err, "git checkout")
	}
	if err := c.execCommand(filepath.Join(cloneDir, "lint"), "golangci-lint", "custom"); err != nil {
		return errors.Wrap(err, "golangci-lint custom")
	}

	// FIXME(jwatson): If we dynamically create .custom-gcl.yml, we can avoid this mv step
	if err := c.execCommand(filepath.Join(cloneDir, "lint"), "mv", "custom-gcl", filepath.Join(goBinPath(), "golangci-lint-ccc")); err != nil {
		return errors.Wrap(err, "mv custom-gcl")
	}

	return nil
}

func (c *ccclint) execCommand(dir, command string, args ...string) error {
	if c.verbose {
		fmt.Printf("Command(%s): %s", dir, command)
		for _, arg := range args {
			fmt.Print(" ", arg)
		}
		fmt.Println()
	}

	cmd := exec.Command(command, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return errors.Wrap(errors.Wrap(err, "Output: "+string(out)), "exec.Command.CombineOutput()")
	}

	if c.verbose && len(out) > 0 {
		fmt.Println("Output: " + string(out) + "\n")
	}

	return nil
}

func goBinPath() string {
	gobin := os.Getenv("GOBIN")
	if gobin == "" {
		gopath := os.Getenv("GOPATH")
		if gopath == "" {
			gopath = build.Default.GOPATH
		}
		gobin = filepath.Join(gopath, "bin")
	}

	return gobin
}
