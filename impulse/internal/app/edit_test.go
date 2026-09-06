package app

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

const editProgram = `package main

import "github.com/cccteam/ccc/resource/generation"

func run() error {
	g, err := generation.NewResourceGenerator(nil, "pkg/resources", []string{"file://schema/migrations"}, []string{"example.com/beacon/pkg/resources"},
		generation.GenerateHandlers("app"),
		// Routes under /api.
		generation.GenerateRoutes("pkg/router", "api"),
		generation.GenerateTypescript("web/console/src/app/core/service",
			generation.GenerateEnums(),
		),
	)
	_ = g

	return err
}
`

func TestInsertOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		src     string
		after   string
		options []string
		want    string
		wantErr string
	}{
		{
			name:    "after an option in the middle, keeping the comment that follows",
			src:     editProgram,
			after:   "GenerateHandlers",
			options: []string{`generation.WithRPC("pkg/rpc")`},
			want: strings.Replace(editProgram, "\t\tgeneration.GenerateHandlers(\"app\"),\n",
				"\t\tgeneration.GenerateHandlers(\"app\"),\n\t\tgeneration.WithRPC(\"pkg/rpc\"),\n", 1),
		},
		{
			name:  "after the last option, two at once",
			src:   editProgram,
			after: "",
			options: []string{
				`generation.WithRouterOutlet("portal", "portal/api", generation.ServesSessions())`,
				`generation.WithRouterOutlet("machines", "machines")`,
			},
			want: strings.Replace(editProgram, "\t\t\tgeneration.GenerateEnums(),\n\t\t),\n",
				"\t\t\tgeneration.GenerateEnums(),\n\t\t),\n\t\tgeneration.WithRouterOutlet(\"portal\", \"portal/api\", generation.ServesSessions()),\n\t\tgeneration.WithRouterOutlet(\"machines\", \"machines\"),\n", 1),
		},
		{
			name: "an aliased import is honored",
			src: `package main

import gen "github.com/cccteam/ccc/resource/generation"

func run() {
	gen.NewResourceGenerator(nil, "pkg/resources", nil, nil, gen.GenerateHandlers("app"))
}
`,
			after:   "GenerateHandlers",
			options: []string{`generation.GenerateRoutes("pkg/router", "api")`},
			want: `package main

import gen "github.com/cccteam/ccc/resource/generation"

func run() {
	gen.NewResourceGenerator(nil, "pkg/resources", nil, nil, gen.GenerateHandlers("app"),
		gen.GenerateRoutes("pkg/router", "api"))
}
`,
		},
		{
			name:    "no anchor",
			src:     editProgram,
			after:   "WithRPC",
			options: []string{`generation.WithRouterOutlet("portal", "portal/api")`},
			wantErr: "no WithRPC option to insert after",
		},
		{
			name:    "no generator program",
			src:     "package main\n\nfunc main() {}\n",
			after:   "",
			options: []string{`generation.WithRPC("pkg/rpc")`},
			wantErr: "does not import github.com/cccteam/ccc/resource/generation",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := InsertOptions("main.go", []byte(tt.src), tt.after, tt.options)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("InsertOptions() error = %v, want %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("InsertOptions() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, string(got)); diff != "" {
				t.Errorf("InsertOptions() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestOptionText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, option, first string
		want                string
	}{
		{
			name: "the default client target", option: "GenerateTypescript", first: "web/console/src/app/core/service",
			want: "generation.GenerateTypescript(\"web/console/src/app/core/service\",\n\t\t\tgeneration.GenerateEnums(),\n\t\t)",
		},
		{name: "a target the program lacks", option: "GenerateTypescript", first: "web/portal/src/app/core/service"},
		{name: "an option without a string first argument", option: "GenerateEnums", first: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := OptionText("main.go", []byte(editProgram), tt.option, tt.first)
			if err != nil {
				t.Fatalf("OptionText() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("OptionText() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRemoveOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, option, first string
		want                string
		wantN               int
	}{
		{
			name: "an option with the comment that introduces it", option: "GenerateRoutes", first: "pkg/router",
			want:  strings.Replace(editProgram, "\t\t// Routes under /api.\n\t\tgeneration.GenerateRoutes(\"pkg/router\", \"api\"),\n", "", 1),
			wantN: 1,
		},
		{
			name: "the last option, spanning lines", option: "GenerateTypescript", first: "web/console/src/app/core/service",
			want:  strings.Replace(editProgram, "\t\tgeneration.GenerateTypescript(\"web/console/src/app/core/service\",\n\t\t\tgeneration.GenerateEnums(),\n\t\t),\n", "", 1),
			wantN: 1,
		},
		{name: "an option the program lacks", option: "WithRouterOutlet", first: "portal", want: editProgram},
		{name: "the same option under another first argument", option: "GenerateRoutes", first: "pkg/other", want: editProgram},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, n, err := RemoveOptions("main.go", []byte(editProgram), tt.option, tt.first)
			if err != nil {
				t.Fatalf("RemoveOptions() error = %v", err)
			}
			if n != tt.wantN {
				t.Errorf("RemoveOptions() n = %d, want %d", n, tt.wantN)
			}
			if diff := cmp.Diff(tt.want, string(got)); diff != "" {
				t.Errorf("RemoveOptions() diff (-want +got):\n%s", diff)
			}
		})
	}
}
