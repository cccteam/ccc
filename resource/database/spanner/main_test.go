package spanner_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/go-playground/errors/v5"

	initiator "github.com/cccteam/db-initiator"
)

// emulatorVersion pins the Spanner emulator image the driver's tests open against: the
// version the resource package's tests and Lodestar pin.
const emulatorVersion = "1.5.56"

// sharedEmulator is the one Spanner emulator container the package's tests share, started
// on first demand and terminated when the test binary exits, as the resource package's
// tests share theirs.
var sharedEmulator struct {
	once      sync.Once
	container *initiator.SpannerContainer
	err       error
}

func TestMain(m *testing.M) {
	code := m.Run()
	if c := sharedEmulator.container; c != nil {
		ctx := context.Background()
		if err := c.Terminate(ctx); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
		if err := c.Close(); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	}
	os.Exit(code)
}

// spannerEmulator returns the shared container, starting it on first demand and naming
// it in SPANNER_EMULATOR_HOST for the process, the way a development environment names
// the emulator for the Spanner client library the driver opens through. Under -short
// the calling test skips instead, so the unit run never needs a container runtime.
func spannerEmulator(t *testing.T) *initiator.SpannerContainer {
	t.Helper()

	if testing.Short() {
		t.Skip("requires the Spanner emulator")
	}
	sharedEmulator.once.Do(func() {
		sharedEmulator.container, sharedEmulator.err = startEmulator(context.Background())
	})
	if sharedEmulator.err != nil {
		t.Fatalf("initiator.NewSpannerContainer() error = %v", sharedEmulator.err)
	}

	return sharedEmulator.container
}

// startEmulator starts the emulator container and points SPANNER_EMULATOR_HOST at it.
func startEmulator(ctx context.Context) (*initiator.SpannerContainer, error) {
	container, err := initiator.NewSpannerContainer(ctx, emulatorVersion)
	if err != nil {
		return nil, errors.Wrap(err, "initiator.NewSpannerContainer()")
	}
	host, err := container.Host(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "testcontainers.Container.Host()")
	}
	port, err := container.MappedPort(ctx, "9010/tcp")
	if err != nil {
		return nil, errors.Wrap(err, "testcontainers.Container.MappedPort()")
	}
	if err := os.Setenv("SPANNER_EMULATOR_HOST", host+":"+port.Port()); err != nil {
		return nil, errors.Wrap(err, "os.Setenv()")
	}

	return container, nil
}
