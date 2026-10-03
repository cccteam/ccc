package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestRolePolicyScan(t *testing.T) {
	t.Parallel()

	const auth = `package staff

import (
	_ "embed"

	"github.com/cccteam/access"
)

//go:embed roles.json
var roleFile access.RoleFile

func Roles() access.RoleFile { return roleFile }

func New(store access.Store, collection access.PermissionCollection) (*access.Client, error) {
	return access.New(store, access.WithDefaultRoles(collection, Roles()))
}
`
	const data = `package config

import (
	"github.com/cccteam/access"

	crewauth "example.com/harbor/pkg/auth/crew"
	"example.com/harbor/pkg/router"
)

func engines(store access.Store) error {
	if _, err := access.New(store, access.WithDefaultRoles(router.Collection(), crewauth.Roles())); err != nil {
		return err
	}
	if _, err := access.New(store, access.WithDefaultRoles(router.Collection(), loadRoles())); err != nil {
		return err
	}
	_, err := access.New(store, access.WithDefaultRoles(router.Collection()))

	return err
}
`
	const deploy = `package deploy

import (
	"context"

	"github.com/cccteam/access"
)

func CheckRoles(ctx context.Context, client *access.Client) error {
	_, err := client.CheckPolicy(ctx)

	return err
}
`
	const migrate = `package main

import "context"

func run(ctx context.Context, data *Data) error {
	if _, err := data.Staff().Access().CheckPolicy(ctx); err != nil {
		return err
	}

	return nil
}
`
	tests := []struct {
		name       string
		files      map[string]string
		wantRoles  []DefaultRoles
		wantChecks []PolicyCheck
	}{
		{
			name:  "the role file handed over unqualified, qualified, and in other shapes",
			files: map[string]string{"pkg/auth/staff/staff.go": auth, "pkg/config/data.go": data},
			wantRoles: []DefaultRoles{
				{File: "pkg/auth/staff/staff.go", Line: 15, RolesPackage: "example.com/harbor/pkg/auth/staff"},
				{File: "pkg/config/data.go", Line: 11, RolesPackage: "example.com/harbor/pkg/auth/crew"},
				{File: "pkg/config/data.go", Line: 14},
				{File: "pkg/config/data.go", Line: 17},
			},
		},
		{
			name:  "the policy checks, through the client and through an accessor",
			files: map[string]string{"pkg/deploy/deploy.go": deploy, "cmd/deployment/migrate/main.go": migrate},
			wantChecks: []PolicyCheck{
				{File: "cmd/deployment/migrate/main.go", Line: 6},
				{File: "pkg/deploy/deploy.go", Line: 10},
			},
		},
		{
			name:  "test files are not read",
			files: map[string]string{"pkg/auth/staff/staff_test.go": auth, "pkg/deploy/deploy_test.go": deploy},
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
			if diff := cmp.Diff(tt.wantRoles, a.DefaultRoles); diff != "" {
				t.Errorf("DefaultRoles mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantChecks, a.PolicyChecks); diff != "" {
				t.Errorf("PolicyChecks mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
