package main

import (
	"fmt"
	"go/build"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/go-playground/errors/v5"
	"github.com/google/uuid"
)

type ccclint struct {
	golangiCiLintVersion string
	pluginVersion        string
	verbose              bool
}

func (c *ccclint) run() error {
	wd, err := os.Getwd()
	if err != nil {
		return errors.Wrap(err, "os.Getwd()")
	}

	if err := c.execCommand(wd, "go", "install", fmt.Sprintf("github.com/golangci/golangci-lint/cmd/golangci-lint@%s", c.golangiCiLintVersion)); err != nil {
		return errors.Wrap(err, "failed to install golangci-lint")
	}

	cloneDir := filepath.Join(wd, uuid.New().String())

	if err := c.execCommand(wd, "git", "clone", "https://github.com/cccteam/ccc.git", cloneDir); err != nil {
		return errors.Wrap(err, "git clone")
	}

	defer func() {
		if err := c.execCommand(wd, "rm", "-rf", cloneDir); err != nil {
			fmt.Printf("failed to remove clone dir: %s\n", cloneDir)
		}
	}()

	if err := c.execCommand(cloneDir, "git", "checkout", "lint/"+c.pluginVersion); err != nil {
		return errors.Wrap(err, "git checkout")
	}
	if err := c.execCommand(cloneDir+"/lint", "golangci-lint", "custom"); err != nil {
		return errors.Wrap(err, "golangci-lint custom")
	}

	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		gopath = build.Default.GOPATH
	}

	if err := c.execCommand(cloneDir+"/lint", "mv", "custom-gcl", fmt.Sprintf("%s/bin/golangci-lint", gopath)); err != nil {
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
