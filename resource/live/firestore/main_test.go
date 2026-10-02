package firestore_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cccteam/ccc/resource/live"
	livefirestore "github.com/cccteam/ccc/resource/live/firestore"
	"github.com/go-playground/errors/v5"
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
// demand. Under -short, or without podman to start it, the calling test skips instead,
// so the unit run never needs a container runtime.
func firestoreEmulator(t *testing.T) string {
	t.Helper()

	if testing.Short() {
		t.Skip("requires the Firestore emulator")
	}
	podman, err := exec.LookPath("podman")
	if err != nil {
		t.Skip("podman is not available to start the Firestore emulator")
	}
	sharedEmulator.once.Do(func() {
		sharedEmulator.host, sharedEmulator.stop, sharedEmulator.err = startEmulator(podman)
	})
	if sharedEmulator.err != nil {
		t.Fatalf("startEmulator() error = %v", sharedEmulator.err)
	}

	return sharedEmulator.host
}

// startEmulator runs the emulator in a container on a random local port with the
// package's rules, waits for it to answer, and returns its host and how to stop it.
func startEmulator(podman string) (host string, stop func(), err error) {
	rulesDir, err := os.MkdirTemp("", "live-firestore-rules-*")
	if err != nil {
		return "", nil, errors.Wrap(err, "os.MkdirTemp()")
	}
	rules, err := os.ReadFile("firestore.rules")
	if err != nil {
		return "", nil, errors.Wrap(err, "os.ReadFile()")
	}
	if err := os.WriteFile(filepath.Join(rulesDir, "firestore.rules"), rules, 0o644); err != nil {
		return "", nil, errors.Wrap(err, "os.WriteFile()")
	}

	ctx := context.Background()
	out, err := exec.CommandContext(ctx, podman, "run", "-d", "--rm",
		"-p", "127.0.0.1::8080",
		"-v", rulesDir+":/rules:ro,Z",
		emulatorImage,
		"gcloud", "emulators", "firestore", "start", "--host-port=0.0.0.0:8080", "--rules=/rules/firestore.rules",
	).CombinedOutput()
	if err != nil {
		return "", nil, errors.Wrapf(err, "podman run: %s", out)
	}
	id := strings.TrimSpace(string(out))
	stop = func() {
		if out, err := exec.CommandContext(ctx, podman, "stop", "-t", "2", id).CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "podman stop: %v: %s\n", err, out)
		}
		_ = os.RemoveAll(rulesDir)
	}

	out, err = exec.CommandContext(ctx, podman, "port", id, "8080/tcp").Output()
	if err != nil {
		stop()

		return "", nil, errors.Wrap(err, "podman port")
	}
	host = strings.TrimSpace(strings.Split(string(out), "\n")[0])
	if err := waitForEmulator(host); err != nil {
		logs, _ := exec.CommandContext(ctx, podman, "logs", id).CombinedOutput()
		stop()

		return "", nil, errors.Wrapf(err, "the Firestore emulator did not come up: %s", logs)
	}

	return host, stop, nil
}

// waitForEmulator polls the emulator's root, which answers Ok once it serves.
func waitForEmulator(host string) error {
	deadline := time.Now().Add(2 * time.Minute)
	client := &http.Client{Timeout: 2 * time.Second}
	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+host+"/", http.NoBody)
		if err != nil {
			return errors.Wrap(err, "http.NewRequestWithContext()")
		}
		resp, err := client.Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(250 * time.Millisecond)
	}

	return errors.New("timed out")
}

// newService opens a service on the shared emulator with the given clock, closed when
// the test ends.
func newService(t *testing.T, now func() time.Time) *livefirestore.Service {
	t.Helper()

	svc, err := livefirestore.New(t.Context(), livefirestore.Config{
		ProjectID:    testProject,
		DatabaseID:   testDatabase,
		EmulatorHost: firestoreEmulator(t),
	}, livefirestore.WithClock(now))
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
