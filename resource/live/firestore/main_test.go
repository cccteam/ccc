package firestore_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cccteam/ccc/resource/live"
	livefirestore "github.com/cccteam/ccc/resource/live/firestore"
	"github.com/go-playground/errors/v5"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// emulatorImage pins the Cloud SDK emulators image the package's tests start the
// Firestore emulator from, by the SDK version as the Spanner emulator is pinned by
// its version; its manifest at pinning was
// sha256:806d1fc8b431d40955a5eb645436ff108f2291b7e59b6d83afe05f7d15ab2148.
const emulatorImage = "gcr.io/google.com/cloudsdktool/google-cloud-cli:562.0.0-emulators"

// The database the tests use: any project id serves the emulator, and a named
// database proves the service addresses one.
const (
	testProject  = "live-test"
	testDatabase = "live"
)

// sharedEmulator is the one Firestore emulator container the package's tests share,
// started on first demand and stopped when the test binary exits, the way the
// Spanner tests share theirs.
var sharedEmulator struct {
	once sync.Once
	host string
	stop func()
	err  error
}

func TestMain(m *testing.M) {
	code := m.Run()
	if stop := sharedEmulator.stop; stop != nil {
		stop()
	}
	os.Exit(code)
}

// firestoreEmulator returns the shared emulator's host:port, starting it on first
// demand. Under -short the calling test skips instead, so the unit run never needs a
// container runtime; the container is started through testcontainers, the way
// db-initiator starts the Spanner emulator, so Docker and podman both serve.
func firestoreEmulator(t *testing.T) string {
	t.Helper()

	if testing.Short() {
		t.Skip("requires the Firestore emulator")
	}
	sharedEmulator.once.Do(func() {
		sharedEmulator.host, sharedEmulator.stop, sharedEmulator.err = startEmulator()
	})
	if sharedEmulator.err != nil {
		t.Fatalf("startEmulator() error = %v", sharedEmulator.err)
	}

	return sharedEmulator.host
}

// emulatorPort is the port the emulator listens on inside its container; the host side
// is whatever the runtime maps it to.
const emulatorPort = "8080/tcp"

// startEmulator runs the emulator in a container on a mapped local port with the
// package's rules copied in, waits until its root answers, and returns its host:port and
// how to stop it.
func startEmulator() (host string, stop func(), err error) {
	rules, err := os.ReadFile("firestore.rules")
	if err != nil {
		return "", nil, errors.Wrap(err, "os.ReadFile()")
	}

	ctx := context.Background()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		Started: true,
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        emulatorImage,
			Cmd:          []string{"gcloud", "emulators", "firestore", "start", "--host-port=0.0.0.0:8080", "--rules=/firestore.rules"},
			ExposedPorts: []string{emulatorPort},
			Files: []testcontainers.ContainerFile{{
				Reader:            bytes.NewReader(rules),
				ContainerFilePath: "/firestore.rules",
				FileMode:          0o644,
			}},
			WaitingFor: wait.ForHTTP("/").WithPort(emulatorPort).WithStartupTimeout(2 * time.Minute),
		},
	})
	if err != nil {
		return "", nil, errors.Wrap(err, "testcontainers.GenericContainer()")
	}
	stop = func() {
		if err := container.Terminate(context.Background()); err != nil {
			fmt.Fprintf(os.Stderr, "the Firestore emulator container was not terminated: %v\n", err)
		}
	}

	h, err := container.Host(ctx)
	if err != nil {
		stop()

		return "", nil, errors.Wrap(err, "testcontainers.Container.Host()")
	}
	p, err := container.MappedPort(ctx, emulatorPort)
	if err != nil {
		stop()

		return "", nil, errors.Wrap(err, "testcontainers.Container.MappedPort()")
	}

	return net.JoinHostPort(h, p.Port()), stop, nil
}

// newService opens a service on the shared emulator's test database with the given
// clock, closed when the test ends.
func newService(t *testing.T, now func() time.Time) *livefirestore.Service {
	t.Helper()

	return newServiceIn(t, testDatabase, livefirestore.WithClock(now))
}

// newServiceIn opens a service on the named database of the shared emulator, closed
// when the test ends. The signals tests each take a database of their own
// (databaseFor), since an application has one signals document and parallel tests on
// one database would wake each other.
func newServiceIn(t *testing.T, database string, opts ...livefirestore.Option) *livefirestore.Service {
	t.Helper()

	svc, err := livefirestore.New(t.Context(), livefirestore.Config{
		ProjectID:    testProject,
		DatabaseID:   database,
		EmulatorHost: firestoreEmulator(t),
	}, opts...)
	if err != nil {
		t.Fatalf("firestore.New() error = %v", err)
	}
	t.Cleanup(func() {
		if err := svc.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})

	return svc
}

// databaseFor names a database after the test and its ask: a database id is lowercase letters,
// digits and hyphens opening with a letter, so the name is hashed.
func databaseFor(t *testing.T) string {
	t.Helper()

	// The sequence keeps a repeated run (-count) and two calls in one test apart: a
	// database carries its signals document over, and a subscription made before the
	// listener's first snapshot is woken once for what it holds.
	sum := sha256.Sum256(fmt.Appendf(nil, "%s/%d", t.Name(), databaseSeq.Add(1)))

	return "live-" + hex.EncodeToString(sum[:])[:12]
}

// databaseSeq numbers the databases one run asks for.
var databaseSeq atomic.Int64

// fixedClock pins a clock to one instant.
func fixedClock(at time.Time) func() time.Time {
	return func() time.Time {
		return at
	}
}

// register writes the subscriptions, each live until far after now unless its expiry is
// set.
func register(t *testing.T, svc *livefirestore.Service, now time.Time, subs ...live.Subscription) {
	t.Helper()

	for i := range subs {
		if subs[i].Expiry.IsZero() {
			subs[i].Expiry = now.Add(live.SubscriptionTTL)
		}
	}
	if err := svc.Register(t.Context(), subs); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
}
