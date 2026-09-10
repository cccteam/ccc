package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestRoleMigrationsFollowWrappers(t *testing.T) {
	t.Parallel()

	const wrapper = `package deploy

import (
	"context"

	"github.com/cccteam/access"
	"github.com/cccteam/ccc/accesstypes"
)

func MigrateRoles(ctx context.Context, manager access.UserManager, domains ...accesstypes.Domain) error {
	return access.MigrateRoles(ctx, manager, nil, nil, domains...)
}

// Seed is not a wrapper: it spreads a slice of its own making.
func Seed(ctx context.Context, manager access.UserManager) error {
	fixed := []accesstypes.Domain{"north"}

	return access.MigrateRoles(ctx, manager, nil, nil, fixed...)
}
`
	const caller = `package main

import (
	"context"

	"github.com/cccteam/access"

	dep "example.com/harbor/pkg/deploy"
)

func run(ctx context.Context, manager access.UserManager) error {
	if err := dep.MigrateRoles(ctx, manager); err != nil {
		return err
	}
	if err := dep.Seed(ctx, manager); err != nil {
		return err
	}

	return dep.MigrateRoles(ctx, manager, "north", "south")
}
`
	tests := []struct {
		name  string
		files map[string]string
		want  []RoleMigration
	}{
		{
			name:  "a wrapper and its callers",
			files: map[string]string{"pkg/deploy/deploy.go": wrapper, "cmd/bootstrap/main.go": caller},
			// The direct calls come from the walk; the callers follow it.
			want: []RoleMigration{
				{File: "pkg/deploy/deploy.go", Line: 11, Domains: 0, Spread: true},
				{File: "pkg/deploy/deploy.go", Line: 18, Domains: 0, Spread: true},
				{File: "cmd/bootstrap/main.go", Line: 12, Domains: 0, Via: "dep.MigrateRoles"},
				{File: "cmd/bootstrap/main.go", Line: 19, Domains: 2, Via: "dep.MigrateRoles"},
			},
		},
		{
			name:  "no wrapper, callers are nothing",
			files: map[string]string{"cmd/bootstrap/main.go": caller},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			files := map[string]string{"go.mod": "module example.com/harbor\n\ngo 1.26\n"}
			for rel, content := range tt.files {
				files[rel] = content
			}
			for rel, content := range files {
				abs := filepath.Join(root, filepath.FromSlash(rel))
				if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(abs, []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			a, err := Discover(root)
			if err != nil {
				t.Fatalf("Discover() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, a.RoleMigrations); diff != "" {
				t.Errorf("RoleMigrations mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
