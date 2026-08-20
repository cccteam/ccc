package main

import (
	"context"
	"fmt"
	"testing"

	"github.com/cccteam/access"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource/starport/pkg/stations"
	initiator "github.com/cccteam/db-initiator"
)

// TestBootstrap runs the full bootstrap against a Spanner emulator and asserts the
// provisioned access state end to end: generated collection → MigrateRoles → access
// engine → RequireResources, including domain partitioning of the demo user's grants.
func TestBootstrap(t *testing.T) {
	if testing.Short() {
		t.Skip("bootstrap requires the Spanner emulator")
	}

	ctx := t.Context()

	container, err := initiator.NewSpannerContainer(ctx, "1.5.55")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := container.Terminate(context.Background()); err != nil {
			t.Log(err)
		}
		if err := container.Close(); err != nil {
			t.Log(err)
		}
	})

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatal(err)
	}
	port, err := container.MappedPort(ctx, "9010/tcp")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SPANNER_EMULATOR_HOST", fmt.Sprintf("%s:%s", host, port.Port()))
	t.Chdir("../..") // the bootstrap resolves schema/config paths from the module root

	if err := run(ctx); err != nil {
		t.Fatalf("run() error = %v", err)
	}

	client, err := access.New(stations.NewDirectory(), access.NewSpannerAdapter(databasePath(), accessPoliciesTable))
	if err != nil {
		t.Fatalf("access.New() error = %v", err)
	}

	const demoUser = accesstypes.User("demo")

	tests := []struct {
		name     string
		domain   accesstypes.Domain
		perm     accesstypes.Permission
		resource accesstypes.Resource
		wantOK   bool
	}{
		{
			name:     "global role grants a global resource",
			domain:   accesstypes.GlobalDomain,
			perm:     accesstypes.List,
			resource: "Ships",
			wantOK:   true,
		},
		{
			name:     "global role grants a field resource",
			domain:   accesstypes.GlobalDomain,
			perm:     accesstypes.Read,
			resource: "Ships.registryCode",
			wantOK:   true,
		},
		{
			name:     "global role never received an update grant on an immutable field",
			domain:   accesstypes.GlobalDomain,
			perm:     accesstypes.Update,
			resource: "Ships.registryCode",
			wantOK:   false,
		},
		{
			name:     "station role grants execute in the assigned station",
			domain:   "station-alpha",
			perm:     accesstypes.Execute,
			resource: "AuthorizeDocking",
			wantOK:   true,
		},
		{
			name:     "station role grants a field resource in the assigned station",
			domain:   "station-alpha",
			perm:     accesstypes.List,
			resource: "Berths.occupied",
			wantOK:   true,
		},
		{
			name:     "the assignment does not exist in the other station",
			domain:   "station-beta",
			perm:     accesstypes.Execute,
			resource: "AuthorizeDocking",
			wantOK:   false,
		},
		{
			name:     "the global assignment does not bleed into a station",
			domain:   "station-alpha",
			perm:     accesstypes.List,
			resource: "Ships",
			wantOK:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, missing, err := client.RequireResources(ctx, demoUser, tt.domain, tt.perm, tt.resource)
			if err != nil {
				t.Fatalf("RequireResources() error = %v", err)
			}
			if ok != tt.wantOK {
				t.Errorf("RequireResources(%s, %s, %s, %s) = %v (missing %v), want %v", demoUser, tt.domain, tt.perm, tt.resource, ok, missing, tt.wantOK)
			}
		})
	}
}
