package filestore_test

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"cloud.google.com/go/storage"
	"github.com/cccteam/ccc/resource/filestore"
	"github.com/go-playground/errors/v5"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// fakeImage pins the fake Cloud Storage server the bucket rows run against. The Go
// client ships no test server and Google documents no emulator; this third-party fake
// speaks the JSON API the client uses, create-only writes and resumable uploads
// included.
const fakeImage = "docker.io/fsouza/fake-gcs-server:1.52.2"

// fakePort is the port the fake listens on inside its container.
const fakePort = "4443"

// liveStoreURL names an environment variable that, when set to a gs:// URL, points the
// bucket rows at a real bucket under the process's own identity instead of the fake:
// how the lab proves the store against its bucket, keyless, and the 403 path with the
// grant removed.
const liveStoreURL = "FILESTORE_TEST_URL"

// fakeProject is the project the fake's buckets are created under; any name serves.
const fakeProject = "filestore-test"

// sharedFake is the one fake container the package's tests share, started before the
// tests when they are not short, since the client reads STORAGE_EMULATOR_HOST at
// construction and the variable must be set before the first bucket store opens.
var sharedFake struct {
	stop func()
	host string
}

// bucketSeq numbers the buckets one run creates on the fake.
var bucketSeq atomic.Int64

func TestMain(m *testing.M) {
	flag.Parse()
	if !testing.Short() && os.Getenv(liveStoreURL) == "" {
		host, stop, err := startFake()
		if err != nil {
			fmt.Fprintf(os.Stderr, "the fake Cloud Storage server did not start: %v\n", err)
			os.Exit(1)
		}
		sharedFake.host, sharedFake.stop = host, stop
		if err := os.Setenv("STORAGE_EMULATOR_HOST", host); err != nil {
			fmt.Fprintf(os.Stderr, "os.Setenv() error = %v\n", err)
			os.Exit(1)
		}
	}
	code := m.Run()
	if sharedFake.stop != nil {
		sharedFake.stop()
	}
	os.Exit(code)
}

// startFake runs the fake in a container on a host port chosen first, since the fake
// must be told its public address at start (resumable uploads and media links carry
// it), and waits until it answers.
func startFake() (host string, stop func(), err error) {
	ctx := context.Background()
	port, err := freePort(ctx)
	if err != nil {
		return "", nil, err
	}
	host = net.JoinHostPort("127.0.0.1", strconv.Itoa(port))

	fake, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		Started: true,
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        fakeImage,
			Cmd:          []string{"-scheme", "http", "-port", fakePort, "-public-host", host, "-external-url", "http://" + host},
			ExposedPorts: []string{fakePort + "/tcp"},
			HostConfigModifier: func(hc *container.HostConfig) {
				hc.PortBindings = network.PortMap{network.MustParsePort(fakePort + "/tcp"): {{HostIP: netip.MustParseAddr("127.0.0.1"), HostPort: strconv.Itoa(port)}}}
			},
			WaitingFor: wait.ForHTTP("/storage/v1/b").WithPort(fakePort + "/tcp").WithStartupTimeout(2 * time.Minute),
		},
	})
	if err != nil {
		return "", nil, errors.Wrap(err, "testcontainers.GenericContainer()")
	}
	stop = func() {
		if err := fake.Terminate(context.Background()); err != nil {
			fmt.Fprintf(os.Stderr, "the fake Cloud Storage container was not terminated: %v\n", err)
		}
	}

	return host, stop, nil
}

// freePort asks the kernel for a free TCP port and releases it.
func freePort(ctx context.Context) (int, error) {
	l, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return 0, errors.Wrap(err, "net.ListenConfig.Listen()")
	}
	defer l.Close()
	addr, ok := l.Addr().(*net.TCPAddr)
	if !ok {
		return 0, errors.Newf("net.Listener.Addr() = %T, want *net.TCPAddr", l.Addr())
	}

	return addr.Port, nil
}

// bucketStoreURL is the URL the bucket rows open: the live store named in the
// environment, or a bucket of its own created on the fake. Under -short the caller
// skips.
func bucketStoreURL(t *testing.T) string {
	t.Helper()

	if testing.Short() {
		t.Skip("runs against the fake Cloud Storage server in a container")
	}
	if u := os.Getenv(liveStoreURL); u != "" {
		return u
	}
	name := fmt.Sprintf("filestore-%d-%d", os.Getpid(), bucketSeq.Add(1))
	createBucket(t, name)

	return filestore.SchemeBucket + "://" + name
}

// createBucket creates a bucket on the fake through the client itself.
func createBucket(t *testing.T, name string) {
	t.Helper()

	client, err := storage.NewClient(t.Context())
	if err != nil {
		t.Fatalf("storage.NewClient() error = %v", err)
	}
	defer client.Close()
	if err := client.Bucket(name).Create(t.Context(), fakeProject, nil); err != nil {
		t.Fatalf("storage.BucketHandle.Create(%s) error = %v", name, err)
	}
}

// openStore opens the URL and closes the store when the test ends.
func openStore(t *testing.T, rawURL string) filestore.Store {
	t.Helper()

	store, err := filestore.Open(t.Context(), rawURL)
	if err != nil {
		t.Fatalf("filestore.Open(%s) error = %v", rawURL, err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})

	return store
}
