package check

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/impulse/app"
)

// The fixture sources the env-reads test lays out: the config package reading its level
// through go-envconfig, a shipped program's main package reading its own settings the
// same way, a tool reading the environment with the standard library's lookups, one
// reading it through go-envconfig under another name, a test file reading it, and a
// package of the application named like the library.
const (
	envReadsConfig = `package config

import (
	"context"

	"github.com/sethvargo/go-envconfig"
)

// coreConfig holds the environment every process reads.
type coreConfig struct {
	ServiceName string ` + "`env:\"APP_SERVICE_NAME,required\"`" + `
}

func load(ctx context.Context) (*coreConfig, error) {
	env := &coreConfig{}
	if err := envconfig.ProcessWith(ctx, &envconfig.Config{Target: env, Lookuper: envconfig.OsLookuper()}); err != nil {
		return nil, err
	}

	return env, nil
}
`
	envReadsTagsOnly = `package config

// coreConfig holds the environment every process reads.
type coreConfig struct {
	ServiceName string ` + "`env:\"APP_SERVICE_NAME,required\"`" + `
}
`
	envReadsSensor = `package main

import (
	"context"

	"github.com/sethvargo/go-envconfig"
)

// settings is what the sensor reads from its environment.
type settings struct {
	Outlet string ` + "`env:\"SENSOR_OUTLET,required\"`" + `
}

func main() {
	s := &settings{}
	_ = envconfig.Process(context.Background(), s)
}
`
	envReadsTool = `package main

import (
	"os"
	"syscall"
)

func main() {
	_ = os.Getenv("TOOL_TARGET")
	_, _ = os.LookupEnv("TOOL_TOKEN")
	lookup := os.Getenv
	_ = lookup("TOOL_SOURCE")
	_, _ = syscall.Getenv("TOOL_HOME")
}
`
	envReadsAliased = `package main

import (
	"context"

	env "github.com/sethvargo/go-envconfig"
)

// settings is read with the processor under another name.
type settings struct {
	Target string
}

func main() {
	env.MustProcess(context.Background(), &settings{})
}
`
	envReadsToolTest = `package main

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	_ = os.Getenv("TOOL_TARGET")
	os.Exit(m.Run())
}
`
	envReadsOwnPackage = `package envconfig

// Process is the application's own, not the library's.
func Process(target any) error {
	_ = target

	return nil
}
`
	envReadsOwnCaller = `package main

import "example.com/harbor/internal/envconfig"

func main() {
	_ = envconfig.Process(nil)
}
`
	envReadsNoRead = "package main\n\nfunc main() {}\n"
)

func TestEnvReads(t *testing.T) {
	t.Parallel()

	const (
		staffAuth  = `generation.Auth("example.com/harbor/pkg/auth/staff", generation.PasswordMethod())`
		tokens     = `generation.Tokens("example.com/harbor/pkg/auth/staff")`
		configFile = "pkg/config/config.go"
		sensorFile = "cmd/sensor/main.go"
		toolFile   = "cmd/tool/main.go"
		twoPlaces  = "reads the environment outside the config package (pkg/config): a read lives there, or in the main package of a shipped program the generator program declares (ShipsProgram)"
	)
	shipping := program("pkg/resources",
		`generation.GenerateHandlers("app"),`,
		`generation.GenerateRouter(),`,
		`generation.GenerateRoutes("pkg/router", "api", `+staffAuth+`, generation.WebApp("/")),`,
		`generation.WithRouterOutlet("machines", "machines", `+tokens+`, generation.ShipsProgram("cmd/sensor", generation.Platforms("linux/amd64"))),`,
	)
	plain := program("pkg/resources",
		`generation.GenerateHandlers("app"),`,
		`generation.GenerateRouter(),`,
		`generation.GenerateRoutes("pkg/router", "api", `+staffAuth+`, generation.WebApp("/")),`,
		`generation.WithRouterOutlet("machines", "machines", `+tokens+`),`,
	)
	tests := []struct {
		name string
		// program is the generator program; files the rest of the tree, by root-relative
		// path.
		program string
		files   map[string]string
		want    Result
	}{
		{
			name:    "reads in the config package and a declared program's main package pass",
			program: shipping,
			files:   map[string]string{configFile: envReadsConfig, sensorFile: envReadsSensor},
			want:    pass(envReads{}.Name(), "2 environment read(s), every one in the config package (pkg/config) or a declared shipped program's main package (cmd/sensor)"),
		},
		{
			name:    "reads in an undeclared main package are refused, naming the file, the call and the two places",
			program: plain,
			files:   map[string]string{configFile: envReadsConfig, toolFile: envReadsTool},
			want: fail(envReads{}.Name(), "4 environment read(s) outside the config package",
				"cmd/tool/main.go:9: os.Getenv "+twoPlaces,
				"cmd/tool/main.go:10: os.LookupEnv "+twoPlaces,
				"cmd/tool/main.go:11: os.Getenv "+twoPlaces,
				"cmd/tool/main.go:13: syscall.Getenv "+twoPlaces,
			),
		},
		{
			name:    "an alias does not escape the check",
			program: plain,
			files:   map[string]string{configFile: envReadsConfig, toolFile: envReadsAliased},
			want: fail(envReads{}.Name(), "1 environment read(s) outside the config package",
				"cmd/tool/main.go:15: env.MustProcess (github.com/sethvargo/go-envconfig) "+twoPlaces,
			),
		},
		{
			name:    "a program no outlet declares carries env tags of its own, so the config package cannot be told",
			program: plain,
			files:   map[string]string{configFile: envReadsConfig, sensorFile: envReadsSensor},
			want:    fail(envReads{}.Name(), "env tags in more than one package (cmd/sensor, pkg/config): the config package holds the application's, and only the main package of a shipped program the generator program declares (ShipsProgram) holds its own, so the config package cannot be told"),
		},
		{
			name:    "a test file may read the environment",
			program: plain,
			files:   map[string]string{configFile: envReadsConfig, toolFile: envReadsNoRead, "cmd/tool/main_test.go": envReadsToolTest},
			want:    pass(envReads{}.Name(), "1 environment read(s), every one in the config package (pkg/config)"),
		},
		{
			name:    "a package of the application named like the library is not a reader",
			program: plain,
			files:   map[string]string{configFile: envReadsConfig, "internal/envconfig/envconfig.go": envReadsOwnPackage, toolFile: envReadsOwnCaller},
			want:    pass(envReads{}.Name(), "1 environment read(s), every one in the config package (pkg/config)"),
		},
		{
			name:    "no read anywhere",
			program: plain,
			files:   map[string]string{configFile: envReadsTagsOnly, toolFile: envReadsNoRead},
			want:    pass(envReads{}.Name(), "no Go file reads the environment"),
		},
		{
			name:    "no env tags, no config package",
			program: plain,
			files:   map[string]string{toolFile: envReadsTool},
			want:    skip(envReads{}.Name(), "no env tags outside a shipped program's main package: no config package to hold the reads"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			write := func(rel, content string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Dir(filepath.Join(root, rel)), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, rel), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			write("go.mod", "module example.com/harbor\n\ngo 1.26.6\n")
			write("cmd/generate/main.go", tt.program)
			for rel, content := range tt.files {
				write(rel, content)
			}

			a, err := app.Discover(root)
			if err != nil {
				t.Fatalf("app.Discover() error = %v", err)
			}
			got := envReads{}.Run(context.Background(), &Env{App: a})
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Run() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestEnvReadsMeaning holds the brief's explanation to the pattern it must teach: where
// a read lives, what the reads are, and the two exemptions.
func TestEnvReadsMeaning(t *testing.T) {
	t.Parallel()

	meaning := Meaning(envReads{}.Name())
	for _, want := range []string{"config package", "`os.Getenv`", "`ShipsProgram(", "test file", "bedrock derives the stack's variables"} {
		if !strings.Contains(meaning, want) {
			t.Errorf("Meaning() lacks %q", want)
		}
	}
}
