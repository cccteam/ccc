package resource

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"

	initiator "github.com/cccteam/db-initiator"
)

// spannerEmulatorVersion pins the emulator image the package's database-backed
// tests run against: the version Lodestar pins.
const spannerEmulatorVersion = "1.5.56"

// sharedEmulator is the one Spanner emulator container the package's
// database-backed tests share, started on first demand and terminated when the
// test binary exits.
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

// spannerEmulator returns the shared container, starting it on first demand.
// Under -short the calling test skips instead, so the unit run never needs a
// container runtime; TestLodestarApp skips the same way.
func spannerEmulator(t *testing.T) *initiator.SpannerContainer {
	t.Helper()

	if testing.Short() {
		t.Skip("requires the Spanner emulator")
	}
	sharedEmulator.once.Do(func() {
		sharedEmulator.container, sharedEmulator.err = initiator.NewSpannerContainer(context.Background(), spannerEmulatorVersion)
	})
	if sharedEmulator.err != nil {
		t.Fatalf("initiator.NewSpannerContainer() error = %v", sharedEmulator.err)
	}

	return sharedEmulator.container
}
