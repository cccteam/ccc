package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"testing"

	"github.com/cccteam/ccc/resource/lodestar/pkg/config"
	initiator "github.com/cccteam/db-initiator"
)

// The container's own instance: db-initiator creates it when the emulator starts.
const (
	containerProjectID  = "unit-testing"
	containerInstanceID = "test-instance"
)

// TestMain starts one Spanner emulator for the package and publishes it in the
// environment, the way the Procfile does for the bootstrap: the bootstrap reaches its
// target through SPANNER_EMULATOR_HOST alone.
func TestMain(m *testing.M) {
	ctx := context.Background()

	c, err := initiator.NewSpannerContainer(ctx, "1.5.56")
	if err != nil {
		log.Fatal(err)
	}
	host, err := c.Host(ctx)
	if err != nil {
		log.Fatal(err)
	}
	port, err := c.MappedPort(ctx, "9010/tcp")
	if err != nil {
		log.Fatal(err)
	}
	if err := os.Setenv("SPANNER_EMULATOR_HOST", fmt.Sprintf("%s:%s", host, port.Port())); err != nil {
		log.Fatal(err)
	}

	exitCode := m.Run()

	if err := c.Terminate(ctx); err != nil {
		fmt.Println(err)
	}
	if err := c.Close(); err != nil {
		fmt.Println(err)
	}

	os.Exit(exitCode)
}

func TestTargetOf(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		emulatorHost string
		want         target
	}{
		{name: "an emulator host means the emulator", emulatorHost: "127.0.0.1:9010", want: emulatorTarget},
		{name: "no emulator host means the real project", emulatorHost: "", want: projectTarget},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := targetOf(tt.emulatorHost); got != tt.want {
				t.Errorf("targetOf(%q) = %q, want %q", tt.emulatorHost, got, tt.want)
			}
		})
	}
}

// TestEnsureInstance pins the emulator-only step on both sides: the instance the
// container already created is found and left alone, and a missing one is created.
//
// Demonstrates: bootstrap.target.
func TestEnsureInstance(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		instanceID string
	}{
		{name: "an existing instance is left alone", instanceID: containerInstanceID},
		{name: "a missing instance is created", instanceID: "bootstrap-created"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			settings := config.SpannerSettings{ProjectID: containerProjectID, InstanceID: tt.instanceID, DatabaseName: "unused"}
			if err := ensureInstance(t.Context(), settings); err != nil {
				t.Fatalf("ensureInstance() error = %v", err)
			}
			// The step is idempotent: a second run finds what the first left or made.
			if err := ensureInstance(t.Context(), settings); err != nil {
				t.Fatalf("ensureInstance() second run error = %v", err)
			}
		})
	}
}

// TestEnsureDatabase pins the create-where-missing step and its report: the first run
// creates the database and says it was not there, the second finds it and says it was.
//
// Demonstrates: bootstrap.target.
func TestEnsureDatabase(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	settings := config.SpannerSettings{ProjectID: containerProjectID, InstanceID: containerInstanceID, DatabaseName: "bootstrap-target"}

	existed, err := ensureDatabase(ctx, settings)
	if err != nil {
		t.Fatalf("ensureDatabase() first run error = %v", err)
	}
	if existed {
		t.Error("ensureDatabase() first run reported the database existed; it was created")
	}

	existed, err = ensureDatabase(ctx, settings)
	if err != nil {
		t.Fatalf("ensureDatabase() second run error = %v", err)
	}
	if !existed {
		t.Error("ensureDatabase() second run reported the database missing; the first run created it")
	}
}
