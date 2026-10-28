package check

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/impulse/app"
)

func TestShippedPrograms(t *testing.T) {
	t.Parallel()

	const (
		staffAuth = `generation.Auth("example.com/harbor/pkg/auth/staff", generation.PasswordMethod())`
		tokens    = `generation.Tokens("example.com/harbor/pkg/auth/staff")`
		build     = "go build -o " + os.DevNull + " ./cmd/sensor"
		mainFile  = "package main\n\nfunc main() {}\n"
	)
	linux := func(arch string) []string {
		return []string{"GOOS=linux", "GOARCH=" + arch, "CGO_ENABLED=0"}
	}

	tests := []struct {
		name    string
		program string
		// mains are the main packages in the tree, by file.
		mains       map[string]string
		answers     map[string]fakeAnswer
		wantStatus  Status
		wantSummary string
		wantDetails []string
		wantCalls   []string
		wantEnvs    [][]string
	}{
		{
			name: "no outlet ships a program",
			program: program("pkg/resources",
				`generation.GenerateHandlers("app"),`,
				`generation.GenerateRouter(),`,
				`generation.GenerateRoutes("pkg/router", "api", `+staffAuth+`, generation.WebApp("/")),`,
				`generation.WithRouterOutlet("machines", "machines", `+tokens+`),`,
			),
			wantStatus:  Pass,
			wantSummary: "no outlet ships a program",
		},
		{
			name: "a program on a machine outlet builds for each platform it declares",
			program: program("pkg/resources",
				`generation.GenerateHandlers("app"),`,
				`generation.GenerateRouter(),`,
				`generation.GenerateRoutes("pkg/router", "api", `+staffAuth+`, generation.WebApp("/")),`,
				`generation.WithRouterOutlet("machines", "machines", `+tokens+`, generation.ShipsProgram("cmd/sensor", generation.Platforms("linux/amd64", "linux/arm64"))),`,
			),
			mains:       map[string]string{"cmd/sensor/main.go": mainFile},
			answers:     map[string]fakeAnswer{build: {}},
			wantStatus:  Pass,
			wantSummary: "1 shipped program(s) build for their platforms",
			wantCalls:   []string{build, build},
			wantEnvs:    [][]string{linux("amd64"), linux("arm64")},
		},
		{
			name: "a build that fails reports the compiler's output",
			program: program("pkg/resources",
				`generation.GenerateHandlers("app"),`,
				`generation.GenerateRouter(),`,
				`generation.GenerateRoutes("pkg/router", "api", `+staffAuth+`, generation.WebApp("/")),`,
				`generation.WithRouterOutlet("machines", "machines", `+tokens+`, generation.ShipsProgram("cmd/sensor", generation.Platforms("linux/amd64"))),`,
			),
			mains:       map[string]string{"cmd/sensor/main.go": mainFile},
			answers:     map[string]fakeAnswer{build: {out: "# example.com/harbor/cmd/sensor\ncmd/sensor/main.go:3:2: undefined: reading\n", err: errExit}},
			wantStatus:  Fail,
			wantSummary: "1 of 1 shipped program(s) cannot ship as declared",
			wantDetails: []string{
				"cmd/generate/main.go:16: cmd/sensor does not build for linux/amd64:",
				"# example.com/harbor/cmd/sensor",
				"cmd/sensor/main.go:3:2: undefined: reading",
			},
			wantCalls: []string{build},
			wantEnvs:  [][]string{linux("amd64")},
		},
		{
			name: "a program on a session outlet is refused naming Tokens, and nothing is built",
			program: program("pkg/resources",
				`generation.GenerateHandlers("app"),`,
				`generation.GenerateRouter(),`,
				`generation.GenerateRoutes("pkg/router", "api", `+staffAuth+`, generation.WebApp("/"), generation.ShipsProgram("cmd/sensor", generation.Platforms("linux/amd64"))),`,
			),
			mains:       map[string]string{"cmd/sensor/main.go": mainFile},
			wantStatus:  Fail,
			wantSummary: "1 of 1 shipped program(s) cannot ship as declared",
			wantDetails: []string{
				"cmd/generate/main.go:15: outlet default ships cmd/sensor and declares no Tokens: a session outlet serves no programs; a machine outlet, declared with Tokens(importPath), serves the programs the release ships behind its token check",
			},
		},
		{
			name: "a directory holding no main package is refused, and nothing is built",
			program: program("pkg/resources",
				`generation.GenerateHandlers("app"),`,
				`generation.GenerateRouter(),`,
				`generation.GenerateRoutes("pkg/router", "api", `+staffAuth+`, generation.WebApp("/")),`,
				`generation.WithRouterOutlet("machines", "machines", `+tokens+`, generation.ShipsProgram("cmd/sensor", generation.Platforms("linux/amd64"))),`,
			),
			mains:       map[string]string{"cmd/sensor/sensor.go": "package sensor\n"},
			wantStatus:  Fail,
			wantSummary: "1 of 1 shipped program(s) cannot ship as declared",
			wantDetails: []string{
				"cmd/generate/main.go:16: outlet machines ships cmd/sensor, and no main package is there: ShipsProgram names the root-relative directory of the program's main package",
			},
		},
		{
			name: "one program ships and another does not",
			program: program("pkg/resources",
				`generation.GenerateHandlers("app"),`,
				`generation.GenerateRouter(),`,
				`generation.GenerateRoutes("pkg/router", "api", `+staffAuth+`, generation.WebApp("/")),`,
				`generation.WithRouterOutlet("machines", "machines", `+tokens+`, generation.ShipsProgram("cmd/sensor", generation.Platforms("linux/amd64")), generation.ShipsProgram("cmd/gauge", generation.Platforms("linux/amd64"))),`,
			),
			mains:       map[string]string{"cmd/sensor/main.go": mainFile},
			answers:     map[string]fakeAnswer{build: {}},
			wantStatus:  Fail,
			wantSummary: "1 of 2 shipped program(s) cannot ship as declared",
			wantDetails: []string{
				"cmd/generate/main.go:16: outlet machines ships cmd/gauge, and no main package is there: ShipsProgram names the root-relative directory of the program's main package",
			},
			wantCalls: []string{build},
			wantEnvs:  [][]string{linux("amd64")},
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
			for rel, content := range tt.mains {
				write(rel, content)
			}

			a, err := app.Discover(root)
			if err != nil {
				t.Fatalf("app.Discover() error = %v", err)
			}
			exec := &fakeExec{t: t, answers: tt.answers}
			got := shippedPrograms{}.Run(context.Background(), &Env{App: a, Exec: exec})
			want := Result{Name: shippedPrograms{}.Name(), Status: tt.wantStatus, Summary: tt.wantSummary, Details: tt.wantDetails}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("Run() mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantCalls, exec.calls); diff != "" {
				t.Errorf("commands mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantEnvs, exec.envs); diff != "" {
				t.Errorf("command environments mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
