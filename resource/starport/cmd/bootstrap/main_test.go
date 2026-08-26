package main

import (
	"context"
	"fmt"
	"testing"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/access"
	"github.com/cccteam/access/spannerstore"
	"github.com/cccteam/ccc/accesstypes"
	initiator "github.com/cccteam/db-initiator"
)

// TestBootstrap runs the full bootstrap against a Spanner emulator and asserts the
// provisioned access state end to end: generated collection → MigrateRoles → access
// engine → CheckUser, including domain partitioning of the demo user's grants.
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

	spannerClient, err := spanner.NewClient(ctx, databasePath())
	if err != nil {
		t.Fatalf("spanner.NewClient() error = %v", err)
	}
	t.Cleanup(spannerClient.Close)

	store, err := spannerstore.New(spannerClient)
	if err != nil {
		t.Fatalf("spannerstore.New() error = %v", err)
	}

	client, err := access.New(store)
	if err != nil {
		t.Fatalf("access.New() error = %v", err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Log(err)
		}
	})

	const demoUser = accesstypes.User("demo")

	tests := []struct {
		name     string
		scope    accesstypes.Scope
		perm     accesstypes.Permission
		resource accesstypes.Resource
		wantOK   bool
	}{
		{
			name:     "global role grants a global resource",
			scope:    accesstypes.GlobalScope(),
			perm:     accesstypes.List,
			resource: "Ships",
			wantOK:   true,
		},
		{
			name:     "global role grants a field resource",
			scope:    accesstypes.GlobalScope(),
			perm:     accesstypes.Read,
			resource: "Ships.registryCode",
			wantOK:   true,
		},
		{
			name:     "global role never received an update grant on an immutable field",
			scope:    accesstypes.GlobalScope(),
			perm:     accesstypes.Update,
			resource: "Ships.registryCode",
			wantOK:   false,
		},
		{
			name:     "global role grants list on a virtual resource",
			scope:    accesstypes.GlobalScope(),
			perm:     accesstypes.List,
			resource: "ShipCargoSummaries",
			wantOK:   true,
		},
		{
			name:     "global role grants a virtual field resource",
			scope:    accesstypes.GlobalScope(),
			perm:     accesstypes.List,
			resource: "ShipCargoSummaries.totalDeclaredValue",
			wantOK:   true,
		},
		{
			name:     "no grant exists on a virtual primary-key field; its visibility follows the resource grant",
			scope:    accesstypes.GlobalScope(),
			perm:     accesstypes.List,
			resource: "ShipCargoSummaries.shipId",
			wantOK:   false,
		},
		{
			name:     "station role grants execute in the assigned station",
			scope:    accesstypes.DomainScope("station-alpha"),
			perm:     accesstypes.Execute,
			resource: "AuthorizeDocking",
			wantOK:   true,
		},
		{
			name:     "station role grants a field resource in the assigned station",
			scope:    accesstypes.DomainScope("station-alpha"),
			perm:     accesstypes.List,
			resource: "Berths.occupied",
			wantOK:   true,
		},
		{
			name:     "the assignment does not exist in the other station",
			scope:    accesstypes.DomainScope("station-beta"),
			perm:     accesstypes.Execute,
			resource: "AuthorizeDocking",
			wantOK:   false,
		},
		{
			name:     "the global assignment does not bleed into a station",
			scope:    accesstypes.DomainScope("station-alpha"),
			perm:     accesstypes.List,
			resource: "Ships",
			wantOK:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			missing, err := client.CheckUserResources(ctx, demoUser, tt.scope, tt.perm, tt.resource)
			if err != nil {
				t.Fatalf("CheckUserResources() error = %v", err)
			}
			if ok := len(missing) == 0; ok != tt.wantOK {
				t.Errorf("CheckUserResources(%s, %s, %s, %s) = %v (missing %v), want %v", demoUser, tt.scope, tt.perm, tt.resource, ok, missing, tt.wantOK)
			}
		})
	}
}
