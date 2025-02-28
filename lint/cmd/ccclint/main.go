package main

import (
	"fmt"
	"go/build"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"

	"github.com/go-playground/errors/v5"
	"github.com/google/uuid"
)

func main() {
	if err := run(); err != nil {
		fmt.Printf("Error: %+v\n", err)
		os.Exit(1)
	}
}

func run() error {
	wd, err := os.Getwd()
	if err != nil {
		return errors.Wrap(err, "os.Getwd()")
	}

	golangiCiLintVersion := "v1.64.5"
	if err := execCommand(wd, "go", "install", fmt.Sprintf("github.com/golangci/golangci-lint/cmd/golangci-lint@%s", golangiCiLintVersion)); err != nil {
		return errors.Wrap(err, "failed to install golangci-lint")
	}

	buildInfo, available := debug.ReadBuildInfo()
	if !available {
		return errors.New("build info not available")
	}

	pluginVersion := buildInfo.Main.Version // TODO: Put back

	cloneDir := filepath.Join(wd, uuid.New().String())

	if err := execCommand(wd, "git", "clone", "https://github.com/cccteam/ccc.git", cloneDir); err != nil {
		return errors.Wrap(err, "git clone")
	}

	defer func() {
		if err := execCommand(wd, "rm", "-rf", cloneDir); err != nil {
			fmt.Printf("failed to remove clone dir: %s\n", cloneDir)
		}
	}()

	if err := execCommand(cloneDir, "git", "checkout", "lint/"+pluginVersion); err != nil {
		return errors.Wrap(err, "git checkout")
	}
	if err := execCommand(cloneDir+"/lint", "golangci-lint", "custom"); err != nil {
		return errors.Wrap(err, "golangci-lint custom")
	}

	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		gopath = build.Default.GOPATH
	}

	if err := execCommand(cloneDir+"/lint", "mv", "custom-gcl", fmt.Sprintf("%s/bin/golangci-lint", gopath)); err != nil {
		return errors.Wrap(err, "mv custom-gcl")
	}

	return nil
}

func execCommand(dir, command string, args ...string) error {
	cmd := exec.Command(command, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return errors.Wrap(errors.Wrap(err, "Output: "+string(out)), "exec.Command.CombineOutput()")
	}

	fmt.Println("Output: " + string(out))

	return nil
}
