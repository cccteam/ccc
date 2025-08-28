package main

import (
	"fmt"
	"os"
	"runtime/debug"

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

	if version {
		fmt.Printf("ccclint version: %s\n", buildInfo.Main.Version)
		return nil
	}

	if verbose {
		fmt.Printf("plugin version: %s\n", pluginVersion)
	}

	c := &ccclint{
		pluginVersion: pluginVersion,
		verbose:       verbose,
	}

	return c.run()
}
