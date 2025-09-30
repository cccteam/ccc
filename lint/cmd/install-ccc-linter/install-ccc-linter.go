// Command install-ccc-linter installs the ccc-lint golangci-lint plugin.
package main

import (
	"context"
	_ "embed"
	"fmt"
	"go/build"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"

	"github.com/go-playground/errors/v5"
)

//go:embed custom-gcl.yml.tmpl
var customGclTemplate string

type cccLintInstaller struct {
	pluginVersion       string
	golangciLintVersion string
	verbose             bool
}

func (c *cccLintInstaller) run(ctx context.Context) error {
	cloneDir, err := os.MkdirTemp("", "ccc-lint-")
	if err != nil {
		return errors.Wrap(err, "os.MkdirTemp()")
	}
	defer os.RemoveAll(cloneDir)

	lintDir := filepath.Join(cloneDir, "lint")

	if c.verbose {
		fmt.Printf("Generating custom golangci-lint config in %s\n", lintDir)
	}

	if err := os.MkdirAll(lintDir, 0o750); err != nil {
		return errors.Wrap(err, "os.MkdirAll()")
	}

	if err := c.generateCustomGclFile(lintDir); err != nil {
		return errors.Wrap(err, "generateCustomGclFile()")
	}

	if err := c.execCommand(ctx, lintDir, "golangci-lint", "custom"); err != nil {
		return errors.Wrap(err, "golangci-lint custom")
	}

	return nil
}

func (c *cccLintInstaller) generateCustomGclFile(lintDir string) error {
	tmpl, err := template.New("custom-gcl").Parse(customGclTemplate)
	if err != nil {
		return errors.Wrap(err, "template.Parse()")
	}

	filePath := filepath.Join(lintDir, ".custom-gcl.yml")
	file, err := os.Create(filePath)
	if err != nil {
		return errors.Wrap(err, "os.Create()")
	}
	defer file.Close()

	data := struct {
		Version       string
		Destination   string
		PluginVersion string
	}{
		Version:       c.golangciLintVersion,
		Destination:   goBinPath() + "/",
		PluginVersion: c.pluginVersion,
	}

	if err := tmpl.Execute(file, data); err != nil {
		return errors.Wrap(err, "template.Execute()")
	}

	if c.verbose {
		fmt.Printf("Generated .custom-gcl.yml in %s\n", lintDir)
	}

	return nil
}

func (c *cccLintInstaller) execCommand(ctx context.Context, dir, command string, args ...string) error {
	if c.verbose {
		fmt.Printf("Command(%s): %s", dir, command)
		for _, arg := range args {
			fmt.Print(" ", arg)
		}
		fmt.Println()
	}

	cmd := exec.CommandContext(ctx, command, args...)
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
