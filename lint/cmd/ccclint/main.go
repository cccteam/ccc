package main

import (
	"fmt"
	"go/build"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

func main() {
	// Read the .golangci.yml file
	file, err := os.Open(".golangci.yml")
	if err != nil {
		fmt.Printf("Error: %+v\n", err)
		os.Exit(1)
	}

	// Decode the yaml file into a Config struct
	var config Config
	if err := yaml.NewDecoder(file).Decode(&config); err != nil {
		fmt.Printf("Error: %+v\n", err)
		os.Exit(1)
	}

	// Get the custom linter path and extract the version
	parts := strings.Split(config.Linters.Custom.CCCLint.Path, string(filepath.Separator))

	if len(parts) < 2 {
		fmt.Printf("Error: invalid path %s\n", config.Linters.Custom.CCCLint.Path)
		os.Exit(1)
	}

	version := parts[len(parts)-2]

	pluginFile := fmt.Sprintf("tmp/ccc-lint/%s/ccc-lint.so", version)

	// Check if file already exists and exit early if it does
	fi, err := os.Stat(pluginFile)
	if err == nil && fi != nil {
		fmt.Printf("File already exists: %s\n", pluginFile)
		return
	}

	// Running the go get command just makes sure the .so file will exist in the cache
	execCommand("go", "get", fmt.Sprintf("github.com/cccteam/ccc/lint@%s", version))

	if err := os.MkdirAll(fmt.Sprintf("tmp/ccc-lint/%s", version), 0o744); err != nil {
		fmt.Printf("Error: %+v\n", err)
		os.Exit(1)
	}

	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		gopath = build.Default.GOPATH
	}

	cachePath := fmt.Sprintf("%s/pkg/mod/github.com/cccteam/ccc/lint@%s", gopath, version)

	// Build the plugin from the cache
	execCommand("go", "build", "-buildmode=plugin", "-o", pluginFile, filepath.Join(cachePath, "lint.go"))

	// Tidy is just here to make sure the go.mod file is cleaned up afterwards
	execCommand("go", "mod", "tidy")
}

func execCommand(command string, args ...string) {
	out, err := exec.Command(command, args...).CombinedOutput()
	if err != nil {
		fmt.Printf("Error: %+v\n", err)
		fmt.Println(string(out))
		os.Exit(1)
	}
}
