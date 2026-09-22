package check

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/impulse/internal/app"
)

// readingProgram is a generator program in the skeletons' runner shape: the declaration in
// generator.go, the runner in main.go reading Warnings() after a clean run.
const (
	readingDeclaration = `package main

import (
	"context"

	"github.com/cccteam/ccc/resource/generation"
)

func newGenerator(ctx context.Context) (generation.Generator, error) {
	return generation.NewResourceGenerator(ctx, "pkg/resources", []string{"file://schema/migrations"}, []string{"example.com/harbor/pkg/resources"},
		generation.GenerateHandlers("app"),
		generation.GenerateRoutes("pkg/router", "api"),
	)
}
`
	readingRunner = `package main

import (
	"context"
	"fmt"
	"os"
)

func main() {
	generator, err := newGenerator(context.Background())
	if err != nil {
		panic(err)
	}
	if err := generator.Generate(); err != nil {
		panic(err)
	}
	for _, warning := range generator.Warnings() {
		fmt.Fprintf(os.Stderr, "Warning: %s\n", warning)
	}
}
`
	// declaringRunner is the runner of a program declared in a package of its own
	// (cmd/generate), the layout an application takes when tests run the declaration
	// in-process.
	declaringRunner = `package main

import (
	"context"
	"fmt"
	"os"

	"example.com/harbor/cmd/generate"
)

func main() {
	generator, err := generate.NewGenerator(context.Background())
	if err != nil {
		panic(err)
	}
	if err := generator.Generate(); err != nil {
		panic(err)
	}
	for _, warning := range generator.Warnings() {
		fmt.Fprintf(os.Stderr, "Warning: %s\n", warning)
	}
}
`
)

func TestGeneratorProgram(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		files       map[string]string
		wantStatus  Status
		wantSummary string
		wantDetails []string
	}{
		{
			name:       "no program",
			files:      map[string]string{"pkg/resources/doc.go": "package resources\n"},
			wantStatus: Fail, wantSummary: "no generator program found (no file calls generation.NewResourceGenerator)",
		},
		{
			name: "the runner shape reads the warnings",
			files: map[string]string{
				"cmd/generate/generate.go":                    "package generate\n\n//go:generate go run ./resourcegenerator\n",
				"cmd/generate/resourcegenerator/generator.go": readingDeclaration,
				"cmd/generate/resourcegenerator/main.go":      readingRunner,
			},
			wantStatus: Pass, wantSummary: "1 generator program(s) read completely",
		},
		{
			name: "a declaring package whose runner reads the warnings",
			files: map[string]string{
				"cmd/generate/generator.go":              declaredProgram("pkg/resources", `generation.GenerateHandlers("app"),`, `generation.GenerateRoutes("pkg/router", "api"),`),
				"cmd/generate/resourcegenerator/main.go": declaringRunner,
			},
			wantStatus: Pass, wantSummary: "1 generator program(s) read completely",
		},
		{
			name:       "a program that never reads the warnings",
			files:      map[string]string{"cmd/generate/main.go": program("pkg/resources", `generation.GenerateHandlers("app"),`, `generation.GenerateRoutes("pkg/router", "api"),`)},
			wantStatus: Warn, wantSummary: "1 generator program(s) read completely; 1 never read Warnings()",
			wantDetails: []string{
				"cmd/generate/main.go: the program never reads Warnings(): the schema warnings a generation raises go unseen; print them after Generate() as the skeletons' runner does, and pin the accepted set in its warnings test",
			},
		},
		{
			name:       "a problem fails the check and lists the unread warnings beneath it",
			files:      map[string]string{"cmd/generate/main.go": program("pkg/resources", `generation.GenerateHandlers(dir),`)},
			wantStatus: Fail, wantSummary: "1 generator program problem(s)",
			wantDetails: []string{
				"cmd/generate/main.go:13: GenerateHandlers argument dir is not a literal",
				"cmd/generate/main.go: the program never reads Warnings(): the schema warnings a generation raises go unseen; print them after Generate() as the skeletons' runner does, and pin the accepted set in its warnings test",
			},
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
			for rel, content := range tt.files {
				write(rel, content)
			}
			a, err := app.Discover(root)
			if err != nil {
				t.Fatalf("app.Discover() error = %v", err)
			}
			got := generatorProgram{}.Run(context.Background(), &Env{App: a})
			want := Result{Name: "generator-program", Status: tt.wantStatus, Summary: tt.wantSummary, Details: tt.wantDetails}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("Run() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
