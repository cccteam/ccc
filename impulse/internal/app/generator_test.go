package app

import (
	"strings"
	"testing"
)

const programHead = `package main

import (
	"context"

	"github.com/cccteam/ccc/resource/generation"
)

func main() {
	_, _ = generation.NewResourceGenerator(
		context.Background(),
		"pkg/resources",
		[]string{"file://schema/migrations"},
		[]string{"example.com/x/pkg/resources"},
`

const programTail = `
	)
}
`

func TestParseGeneratorProblems(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		options      string
		wantProblems []string
	}{
		{
			name:    "clean program",
			options: `generation.GenerateHandlers("app"), generation.WithConcealedDomains(),`,
		},
		{
			name:         "unknown option",
			options:      `generation.WithFrobnicator("x"),`,
			wantProblems: []string{"unknown option generation.WithFrobnicator"},
		},
		{
			name:         "non-literal argument",
			options:      `generation.GenerateHandlers(dir),`,
			wantProblems: []string{"GenerateHandlers argument dir is not a literal"},
		},
		{
			name:         "too few arguments",
			options:      `generation.GenerateRoutes("pkg/router"),`,
			wantProblems: []string{"GenerateRoutes takes 2 argument(s), found 1"},
		},
		{
			name:         "too many arguments",
			options:      `generation.WithRPC("pkg/rpc", "extra"),`,
			wantProblems: []string{"WithRPC takes 1 argument(s), found 2"},
		},
		{
			name:         "wrong literal kind",
			options:      `generation.WithConsolidatedHandlers("resources", "yes"),`,
			wantProblems: []string{`WithConsolidatedHandlers argument "yes" should be a bool literal`},
		},
		{
			name:         "ts option at top level",
			options:      `generation.GenerateEnums(),`,
			wantProblems: []string{"GenerateEnums is a TSOption, but a ResourceOption is expected here"},
		},
		{
			name:         "resource option nested in typescript",
			options:      `generation.GenerateTypescript("gui/src", generation.WithRPC("pkg/rpc")),`,
			wantProblems: []string{"WithRPC is a ResourceOption, but a TSOption is expected here"},
		},
		{
			name:         "not a call",
			options:      `myOption,`,
			wantProblems: []string{"expected a ResourceOption call, found myOption"},
		},
		{
			name:         "call from another package",
			options:      `other.Option(),`,
			wantProblems: []string{"expected a generation.<Option>() call, found other.Option(...)"},
		},
		{
			name:    "manual registrations are composites",
			options: `generation.WithManualResources(generation.ManualRegistration{Resource: "Beacons"}),`,
		},
		{
			name:         "bool map with string value",
			options:      `generation.CaserInitialismOverrides(map[string]bool{"ID": true}), generation.WithPluralOverrides(map[string]bool{"a": true}),`,
			wantProblems: []string{"WithPluralOverrides argument map[string]bool{...} should be a map[string]string literal"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			g, err := parseGenerator("main.go", []byte(programHead+tt.options+programTail))
			if err != nil {
				t.Fatalf("parseGenerator() error = %v", err)
			}
			if g == nil {
				t.Fatal("parseGenerator() = nil, want a generator")
			}
			if len(g.Problems) != len(tt.wantProblems) {
				t.Fatalf("got %d problems, want %d:\n%s", len(g.Problems), len(tt.wantProblems), problems(g))
			}
			for i, want := range tt.wantProblems {
				if !strings.Contains(g.Problems[i].Message, want) {
					t.Errorf("problem %d = %q, want containing %q", i, g.Problems[i].Message, want)
				}
				if !strings.HasPrefix(g.Problems[i].Pos, "main.go:") {
					t.Errorf("problem %d position = %q, want main.go:<line>", i, g.Problems[i].Pos)
				}
			}
		})
	}
}

func TestParseGeneratorNotAProgram(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  string
	}{
		{
			name: "no generation import",
			src:  "package main\n\nfunc main() { NewResourceGenerator() }\nfunc NewResourceGenerator() {}\n",
		},
		{
			name: "import without the call",
			src:  "package main\n\nimport _ \"github.com/cccteam/ccc/resource/generation\"\n\nfunc main() {}\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			g, err := parseGenerator("main.go", []byte(tt.src))
			if err != nil {
				t.Fatalf("parseGenerator() error = %v", err)
			}
			if g != nil {
				t.Errorf("parseGenerator() = %+v, want nil", g)
			}
		})
	}
}

func TestParseEnvTag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		value  string
		want   EnvTag
		wantOK bool
	}{
		{name: "plain", value: "APP_HOST", want: EnvTag{Name: "APP_HOST"}, wantOK: true},
		{name: "required", value: "APP_KEY,required", want: EnvTag{Name: "APP_KEY", Required: true}, wantOK: true},
		{name: "default", value: "APP_PORT,default=8080", want: EnvTag{Name: "APP_PORT", HasDefault: true}, wantOK: true},
		{name: "default with expansion", value: "APP_X, default=$APP_Y", want: EnvTag{Name: "APP_X", HasDefault: true}, wantOK: true},
		{name: "prefix only", value: ",prefix=APP_", wantOK: false},
		{name: "empty", value: "", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := parseEnvTag(tt.value)
			if ok != tt.wantOK || got != tt.want {
				t.Errorf("parseEnvTag(%q) = (%+v, %v), want (%+v, %v)", tt.value, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}
