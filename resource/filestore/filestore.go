// Package filestore holds the framework's file stores: the stores an application's
// uploaded files live in, opened from one URL per store. gs://<bucket> is a Cloud
// Storage bucket, file://<dir> a directory confined by os.Root, and mem:// memory, for
// tests. The same code path selects each, so development keeps files in a directory,
// Cloud Run keeps them in a bucket, and switching is a change of URL: APP_FILE_STORE
// for the default store, APP_FILE_STORE_<NAME> for a named one (resource.Store).
//
// Every store follows the rules the frame relies on (resource.FileStore): a failed Put
// leaves no object, deleting a missing key succeeds, a key is any safe relative object
// name, and a missing object on Open is resource.ErrFileNotFound while a refused
// permission is an error of its own. Every store also lists its objects, which the
// orphaned-file cleanup (Cleanup) reads a store's unclaimed objects from, knows the
// location it was opened at, so two stores on one location are refused where the client
// is built, and answers a readiness check at start.
package filestore

import (
	"context"
	"iter"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/cccteam/ccc/resource"
	"github.com/go-playground/errors/v5"
)

// The URL schemes the package opens.
const (
	// SchemeBucket is a Cloud Storage bucket: gs://<bucket>, the bucket name alone.
	SchemeBucket = "gs"
	// SchemeDir is a directory: file://<dir>, everything after the scheme the
	// directory, relative to the working directory or absolute.
	SchemeDir = "file"
	// SchemeMem is memory: mem://, a store of its own at each opening.
	SchemeMem = "mem"
)

// The environment the package reads beside the URL.
const (
	// cloudRunService and cloudRunJob are set by Cloud Run in a service and a job; the
	// package recognizes Cloud Run by either and refuses what has no place there.
	cloudRunService = "K_SERVICE"
	cloudRunJob     = "CLOUD_RUN_JOB"
	// emulatorHost is the Cloud Storage client's emulator override: every request goes
	// unauthenticated over plain HTTP to the host it names, which is right for the
	// development fake and never for Cloud Run.
	emulatorHost = "STORAGE_EMULATOR_HOST"
	// keyFileVariable points the Google client libraries at a key file; on Cloud Run
	// the service's identity is the credential, and a key file is refused.
	keyFileVariable = "GOOGLE_APPLICATION_CREDENTIALS"
)

// maxKeyBytes bounds a key: the Cloud Storage object name limit.
const maxKeyBytes = 1024

// Store is a file store the package opened: the frame's three methods, a listing of its
// objects for the orphaned-file cleanup, a readiness check, and Close.
type Store interface {
	resource.FileStore
	Lister
	// Check probes the store: a listing of one object. It returns an error when the
	// store is not reachable or the bucket does not exist, and nil otherwise; a
	// bucket store whose permission is refused logs and returns nil (see Bucket).
	Check(ctx context.Context) error
	// Close releases what the store holds.
	Close() error
}

// Lister lists a store's objects, which the orphaned-file cleanup needs. Every store
// this package opens is one.
type Lister interface {
	// Objects yields every object in the store, each with its key, size and creation
	// time, in no particular order. The listing stops at the first error.
	Objects(ctx context.Context) iter.Seq2[Object, error]
}

// Object is one stored object as a listing reports it.
type Object struct {
	Key     string
	Size    int64
	Created time.Time
}

// Open opens the store a URL names and checks it: gs://<bucket>, file://<dir> or
// mem://. A bad URL, an unknown scheme and a bucket that does not exist are errors, so
// a process with a wrong store does not start. On Cloud Run (K_SERVICE or CLOUD_RUN_JOB
// set) file:// and mem:// are refused, and so are STORAGE_EMULATOR_HOST and
// GOOGLE_APPLICATION_CREDENTIALS in the environment, since each would send the
// service's files somewhere other than its bucket under its own identity.
func Open(ctx context.Context, rawURL string) (Store, error) {
	loc, err := parseURL(rawURL)
	if err != nil {
		return nil, err
	}
	if err := refuseOnCloudRun(loc); err != nil {
		return nil, err
	}

	var store Store
	switch loc.scheme {
	case SchemeBucket:
		store, err = openBucket(ctx, loc.target)
	case SchemeDir:
		store, err = OpenDir(loc.target)
	case SchemeMem:
		store = NewMem()
	}
	if err != nil {
		return nil, err
	}
	if err := store.Check(ctx); err != nil {
		_ = store.Close()

		return nil, errors.Wrapf(err, "the file store %s is not usable", rawURL)
	}

	return store, nil
}

// location is a parsed store URL: the scheme and what it names, a bucket or a
// directory, empty for memory.
type location struct {
	scheme string
	target string
}

// parseURL reads a store URL. gs:// takes a bucket name alone, with no path, query,
// user information or fragment, so a URL can never switch the store to anonymous access
// or a key file; file:// takes everything after the scheme as the directory; mem://
// takes nothing.
func parseURL(rawURL string) (location, error) {
	scheme, rest, ok := strings.Cut(rawURL, "://")
	if !ok || scheme == "" {
		return location{}, errors.Newf("the file store URL %q has no scheme; a store is gs://<bucket>, file://<dir> or mem://", rawURL)
	}
	switch scheme {
	case SchemeBucket:
		u, err := url.Parse(rawURL)
		if err != nil {
			return location{}, errors.Wrapf(err, "the file store URL %q does not parse", rawURL)
		}
		if u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || strings.Contains(u.Host, ":") {
			return location{}, errors.Newf("the file store URL %q names more than a bucket; a bucket store is gs://<bucket>, with no path, query, user information, port or fragment", rawURL)
		}

		return location{scheme: scheme, target: u.Host}, nil
	case SchemeDir:
		if rest == "" {
			return location{}, errors.Newf("the file store URL %q names no directory; a directory store is file://<dir>", rawURL)
		}

		return location{scheme: scheme, target: rest}, nil
	case SchemeMem:
		if rest != "" {
			return location{}, errors.Newf("the file store URL %q names something after mem://; a memory store is mem:// alone", rawURL)
		}

		return location{scheme: scheme}, nil
	default:
		return location{}, errors.Newf("the file store URL %q has the scheme %s, which this package does not open; a store is gs://<bucket>, file://<dir> or mem://", rawURL, scheme)
	}
}

// onCloudRun reports whether the process runs on Cloud Run: in a service or a job.
func onCloudRun() bool {
	return os.Getenv(cloudRunService) != "" || os.Getenv(cloudRunJob) != ""
}

// refuseOnCloudRun refuses, on Cloud Run, a store that is not a bucket and an
// environment that would redirect the bucket store or its credential.
func refuseOnCloudRun(loc location) error {
	if !onCloudRun() {
		return nil
	}
	if loc.scheme != SchemeBucket {
		return errors.Newf("the file store %s:// is refused on Cloud Run; a service or job keeps its files in a bucket, gs://<bucket>", loc.scheme)
	}
	if host := os.Getenv(emulatorHost); host != "" {
		return errors.Newf("%s is set to %q, which is refused on Cloud Run; the variable sends every file request unauthenticated over plain HTTP to the host it names", emulatorHost, host)
	}
	if file := os.Getenv(keyFileVariable); file != "" {
		return errors.Newf("%s is set to %q, which is refused on Cloud Run; the service's own identity is its credential, and a key file is never one", keyFileVariable, file)
	}

	return nil
}

// checkKey admits a safe relative object name: non-empty, at most 1024 bytes, no
// leading slash, no empty, dot or dot-dot segment, and no backslash or control
// character. The upload frame mints UUIDs, which is a fact about the frame and not a
// check here, so an adopter with existing files may pass its rows' names as keys.
func checkKey(key string) error {
	switch {
	case key == "":
		return errors.New("the file key is empty")
	case len(key) > maxKeyBytes:
		return errors.Newf("the file key is %d bytes long; a key is at most %d", len(key), maxKeyBytes)
	case strings.HasPrefix(key, "/"):
		return errors.Newf("the file key %q begins with a slash; a key is a relative name", key)
	}
	for _, r := range key {
		if r == '\\' || r < 0x20 || r == 0x7f {
			return errors.Newf("the file key %q carries a backslash or a control character", key)
		}
	}
	for segment := range strings.SplitSeq(key, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return errors.Newf("the file key %q carries an empty, dot or dot-dot segment", key)
		}
	}

	return nil
}
