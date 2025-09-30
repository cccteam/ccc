package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime/debug"
	"strings"

	"github.com/go-playground/errors/v5"
	"github.com/spf13/pflag"
)

func main() {
	if err := run(); err != nil {
		fmt.Printf("Error: %+v\n", err)
		os.Exit(1)
	}
}

func run() error {
	buildInfo, available := debug.ReadBuildInfo()
	if !available {
		return errors.New("build info not available")
	}

	var pluginVersion string
	pflag.StringVarP(&pluginVersion, "plugin-version", "p", buildInfo.Main.Version, "Version of the ccc/lint plugin")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	defaultVersion, err := getLatestGolangciLintVersion(ctx)
	if err != nil {
		return errors.Wrap(err, "getLatestGolangciLintVersion()")
	}

	var golangciLintVersion string
	pflag.StringVarP(&golangciLintVersion, "golangci-lint-version", "g", defaultVersion, "Version of golangci-lint to use (default: latest stable)")

	var localInstallPath string
	pflag.StringVarP(&localInstallPath, "local-install-path", "l", "", "Path to install plugin from")

	var verbose bool
	pflag.BoolVarP(&verbose, "verbose", "v", false, "Verbose output")

	var help bool
	pflag.BoolVarP(&help, "help", "h", false, "Print usage")

	var version bool
	pflag.BoolVar(&version, "version", false, "Print version")

	pflag.Parse()

	if help {
		pflag.Usage()

		return nil
	}

	if verbose {
		fmt.Printf("plugin version: %s\n", pluginVersion)
		fmt.Printf("golangci-lint version: %s\n", golangciLintVersion)
	}

	if version {
		fmt.Printf("install-ccc-linter version: %s\n", buildInfo.Main.Version)

		return nil
	}

	c := &cccLintInstaller{
		pluginVersion:       pluginVersion,
		golangciLintVersion: golangciLintVersion,
		verbose:             verbose,
		localInstallPath:    localInstallPath,
	}

	return c.run(ctx)
}

func getLatestGolangciLintVersion(ctx context.Context) (string, error) {
	// Use go list to get the latest v2 version from the Go module proxy
	cmd := exec.CommandContext(ctx, "go", "list", "-m", "github.com/golangci/golangci-lint/v2@latest")
	output, err := cmd.Output()
	if err != nil {
		return "", errors.Wrap(err, "cmd.Output()")
	}

	// Output format: "github.com/golangci/golangci-lint/v2 v2.5.0"
	parts := strings.Fields(strings.TrimSpace(string(output)))
	if len(parts) < 2 {
		return "", errors.New("unexpected output from go list")
	}

	// Return the version (second field)
	return parts[1], nil
}
