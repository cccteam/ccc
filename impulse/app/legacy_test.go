package app_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/impulse/app"
)

func TestLegacyAuths(t *testing.T) {
	t.Parallel()

	program := func(auth string) string {
		return `package main

import (
	"context"

	"github.com/cccteam/ccc/resource/generation"
)

func newGenerator(ctx context.Context) (*generation.ResourceGenerator, error) {
	return generation.NewResourceGenerator(
		ctx,
		"pkg/resources",
		[]string{"file://schema/migrations"},
		generation.GenerateHandlers("app"),
		generation.GenerateRoutes("pkg/router", "api",
			` + auth + `
			generation.WebApp("/"),
		),
	)
}
`
	}
	tests := []struct {
		name string
		src  string
		want []app.LegacyAuth
	}{
		{
			name: "method declarations are not legacy",
			src:  program(`generation.Auth("example.com/acme/beacon/pkg/auth/staff", generation.PasswordMethod(), generation.GoogleMethod("google").OpenSignUp()),`),
		},
		{
			name: "the single-method identifier is legacy, with the call's position and the auth package",
			src:  program(`generation.Auth("example.com/acme/beacon/pkg/auth/members", generation.Password),`),
			want: []app.LegacyAuth{{Pos: "cmd/generate/resourcegenerator/generator.go:16", Package: "example.com/acme/beacon/pkg/auth/members", Text: "generation.Password"}},
		},
		{
			name: "two auths, one legacy: the legacy one alone",
			src:  program(`generation.Auth("example.com/acme/beacon/pkg/auth/staff", generation.PasswordMethod()), generation.Auth("example.com/acme/beacon/pkg/auth/members", generation.OIDCGoogle),`),
			want: []app.LegacyAuth{{Pos: "cmd/generate/resourcegenerator/generator.go:16", Package: "example.com/acme/beacon/pkg/auth/members", Text: "generation.OIDCGoogle"}},
		},
		{
			name: "no auth at all",
			src:  program(""),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			g, err := app.ParseGenerator("cmd/generate/resourcegenerator/generator.go", []byte(tt.src))
			if err != nil {
				t.Fatalf("ParseGenerator() error = %v", err)
			}
			if g == nil {
				t.Fatal("ParseGenerator() = nil, want the program")
			}
			if diff := cmp.Diff(tt.want, g.LegacyAuths()); diff != "" {
				t.Errorf("LegacyAuths() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
