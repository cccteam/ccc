package main

import (
	"cmp"
	"fmt"
	"os"
	"runtime/debug"
	"time"

	"github.com/golangci/golangci-lint/pkg/commands"
	"github.com/golangci/golangci-lint/pkg/exitcodes"
)

var (
	goVersion = "unknown"

	// Populated by goreleaser during build
	version = "unknown"
	commit  = "?"
	date    = ""
)

func main() {
	info := createBuildInfo()

	if err := commands.Execute(info); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Failed executing command with error: %v\n", err)
		os.Exit(exitcodes.Failure)
	}
}

func createBuildInfo() commands.BuildInfo {
	info := commands.BuildInfo{
		Commit:    commit,
		Version:   version,
		GoVersion: goVersion,
		Date:      date,
	}

	buildInfo, available := debug.ReadBuildInfo()
	if !available {
		return info
	}

	info.GoVersion = buildInfo.GoVersion

	if date != "" {
		return info
	}

	for _, dep := range buildInfo.Deps {
		if dep.Path == "github.com/golangci/golangci-lint" {
			info.Version = dep.Version + "-ccc-custom"
			break
		}
	}

	info.Date = cmp.Or(info.Date, time.Now().Format(time.DateOnly))

	info.Commit = fmt.Sprintf("(mod sum: %q)", buildInfo.Main.Sum)

	return info
}
