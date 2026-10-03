package filestore_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/filestore"
	perrors "github.com/go-playground/errors/v5"
	"github.com/google/go-cmp/cmp"
)

// keyFileVariable is the Google client libraries' key file variable, refused on Cloud Run.
const keyFileVariable = "GOOGLE_APPLICATION_CREDENTIALS"

// The schemes the table runs every store rule over.
type scheme struct {
	name string
	// open returns the URL to open; a bucket row skips under -short.
	url func(t *testing.T) string
	// located says the store knows its location: a directory and a bucket do, memory
	// does not, since every opening is a store of its own.
	located bool
}

func schemes() []scheme {
	return []scheme{
		{
			name: "mem",
			url: func(*testing.T) string {
				return "mem://"
			},
		},
		{
			name: "file",
			url: func(t *testing.T) string {
				t.Helper()

				return filestore.SchemeDir + "://" + t.TempDir()
			},
			located: true,
		},
		{name: "gs", url: bucketStoreURL, located: true},
	}
}

// newKey mints a key the way the upload frame does.
func newKey(t *testing.T) string {
	t.Helper()

	id, err := ccc.NewUUID()
	if err != nil {
		t.Fatalf("ccc.NewUUID() error = %v", err)
	}

	return id.String()
}

// put stores body under key.
func put(t *testing.T, store resource.FileStore, key, contentType, body string) {
	t.Helper()

	if err := store.Put(t.Context(), key, contentType, strings.NewReader(body)); err != nil {
		t.Fatalf("Put(%s) error = %v", key, err)
	}
}

// read opens key and reads the whole body.
func read(t *testing.T, store resource.FileStore, key string) (content *resource.Content, body string) {
	t.Helper()

	content, err := store.Open(t.Context(), key)
	if err != nil {
		t.Fatalf("Open(%s) error = %v", key, err)
	}
	defer content.Body.Close()
	data, err := io.ReadAll(content.Body)
	if err != nil {
		t.Fatalf("io.ReadAll() error = %v", err)
	}

	return content, string(data)
}

// keys lists the store's keys, sorted.
func keys(t *testing.T, store filestore.Store) []string {
	t.Helper()

	var got []string
	for obj, err := range store.Objects(t.Context()) {
		if err != nil {
			t.Fatalf("Objects() error = %v", err)
		}
		got = append(got, obj.Key)
	}
	slices.Sort(got)

	return got
}

// failingReader fails after some bytes, the way a client that goes away mid-upload
// does.
type failingReader struct {
	remaining int
}

func (r *failingReader) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		return 0, errors.New("the client went away")
	}
	n := min(len(p), r.remaining)
	for i := range n {
		p[i] = 'x'
	}
	r.remaining -= n

	return n, nil
}

// TestStores runs the store rules over every scheme: the round trip with the type and
// size, a missing key, a deleted key, create-only writes, a failed write leaving no
// object (across several upload chunks for the bucket), the key rules, the listing, and
// the frame's ranges, validators and not-found over the store's body.
func TestStores(t *testing.T) {
	t.Parallel()

	for _, sc := range schemes() {
		t.Run(sc.name, func(t *testing.T) {
			t.Parallel()

			store := openStore(t, sc.url(t))
			if located, ok := store.(resource.LocatedFileStore); ok != sc.located || (ok && !strings.HasPrefix(located.Location(), sc.name+"://")) {
				t.Errorf("located = %v, want %v", ok, sc.located)
			}

			t.Run("round trip", func(t *testing.T) {
				t.Parallel()

				key := newKey(t)
				put(t, store, key, "text/plain; charset=utf-8", "hello")
				content, body := read(t, store, key)
				if body != "hello" || content.Size != 5 {
					t.Errorf("Open() = %q (size %d), want hello (size 5)", body, content.Size)
				}
				if sc.name != "file" && content.ContentType != "text/plain; charset=utf-8" {
					t.Errorf("ContentType = %q, want the declared type", content.ContentType)
				}
				if content.ModTime.IsZero() {
					t.Error("ModTime is zero, want the object's time")
				}
			})

			t.Run("a missing key is not found, and deleting it succeeds", func(t *testing.T) {
				t.Parallel()

				key := newKey(t)
				if _, err := store.Open(t.Context(), key); !errors.Is(err, resource.ErrFileNotFound) {
					t.Errorf("Open() of a missing key error = %v, want resource.ErrFileNotFound", err)
				}
				if err := store.Delete(t.Context(), []string{key}); err != nil {
					t.Errorf("Delete() of a missing key error = %v, want nil", err)
				}
			})

			t.Run("delete removes several at once", func(t *testing.T) {
				t.Parallel()

				first, second := newKey(t), newKey(t)
				put(t, store, first, "text/plain", "1")
				put(t, store, second, "text/plain", "2")
				if err := store.Delete(t.Context(), []string{first, second, newKey(t)}); err != nil {
					t.Fatalf("Delete() error = %v", err)
				}
				for _, key := range []string{first, second} {
					if _, err := store.Open(t.Context(), key); !errors.Is(err, resource.ErrFileNotFound) {
						t.Errorf("Open(%s) after Delete error = %v, want resource.ErrFileNotFound", key, err)
					}
				}
			})

			t.Run("a key is written once", func(t *testing.T) {
				t.Parallel()

				key := newKey(t)
				put(t, store, key, "text/plain", "first")
				if err := store.Put(t.Context(), key, "text/plain", strings.NewReader("second")); err == nil {
					t.Error("Put() of a stored key succeeded, want a refusal")
				}
				if _, body := read(t, store, key); body != "first" {
					t.Errorf("body after the refused write = %q, want first", body)
				}
			})

			t.Run("a failed write leaves no object", func(t *testing.T) {
				t.Parallel()

				// Two and a half upload chunks, so the bucket's write has sent a chunk
				// before the reader fails.
				key := newKey(t)
				if err := store.Put(t.Context(), key, "application/octet-stream", &failingReader{remaining: 5 << 19}); err == nil {
					t.Fatal("Put() with a failing reader succeeded, want an error")
				}
				if _, err := store.Open(t.Context(), key); !errors.Is(err, resource.ErrFileNotFound) {
					t.Errorf("Open() after the failed write error = %v, want resource.ErrFileNotFound", err)
				}
				if slices.Contains(keys(t, store), key) {
					t.Error("the failed write's key is listed")
				}
			})

			t.Run("a type that does not parse is recorded as octet-stream", func(t *testing.T) {
				t.Parallel()
				if sc.name == "file" {
					t.Skip("a directory keeps no media type")
				}

				key := newKey(t)
				put(t, store, key, "not a type", "x")
				if content, _ := read(t, store, key); sc.name == "gs" && content.ContentType != "application/octet-stream" {
					t.Errorf("ContentType = %q, want application/octet-stream", content.ContentType)
				}
			})

			t.Run("the key rules", func(t *testing.T) {
				t.Parallel()

				for _, bad := range []string{"", "/" + newKey(t), "a//b", "./x", "a/../b", "a\\b", "a\x01b", strings.Repeat("k", 1025)} {
					if err := store.Put(t.Context(), bad, "text/plain", strings.NewReader("x")); err == nil {
						t.Errorf("Put(%q) succeeded, want a refusal", bad)
					}
					if _, err := store.Open(t.Context(), bad); err == nil || errors.Is(err, resource.ErrFileNotFound) {
						t.Errorf("Open(%q) error = %v, want a refusal that is not not-found", bad, err)
					}
					if err := store.Delete(t.Context(), []string{bad}); err == nil {
						t.Errorf("Delete(%q) succeeded, want a refusal", bad)
					}
				}
				// An adopter's own names are keys too.
				nested := "sponsors/logos/" + newKey(t)
				put(t, store, nested, "image/png", "png")
				if _, body := read(t, store, nested); body != "png" {
					t.Errorf("Open(%s) = %q, want png", nested, body)
				}
				if !slices.Contains(keys(t, store), nested) {
					t.Errorf("the nested key is not listed: %v", keys(t, store))
				}
			})

			t.Run("the frame serves ranges, validators and not-found over the body", func(t *testing.T) {
				t.Parallel()

				key := newKey(t)
				put(t, store, key, "text/plain", "0123456789")
				serve := func(t *testing.T, header http.Header) *httptest.ResponseRecorder {
					t.Helper()
					req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/documents/1/content", http.NoBody)
					copyHeaders(req.Header, header)
					rr := httptest.NewRecorder()
					err := resource.ServeStoredFile(t.Context(), rr, req, store, resource.StoredFile{Key: key, Name: "digits.txt", ContentType: "text/plain"}, "content", "Document", 1)
					if err != nil {
						t.Fatalf("ServeStoredFile() error = %v", err)
					}

					return rr
				}
				whole := serve(t, nil)
				if whole.Code != http.StatusOK || whole.Body.String() != "0123456789" {
					t.Errorf("GET = %d %q, want 200 and the digits", whole.Code, whole.Body.String())
				}
				partial := serve(t, http.Header{"Range": {"bytes=3-5"}})
				if partial.Code != http.StatusPartialContent || partial.Body.String() != "345" {
					t.Errorf("ranged GET = %d %q, want 206 and 345", partial.Code, partial.Body.String())
				}
				tail := serve(t, http.Header{"Range": {"bytes=-2"}})
				if tail.Code != http.StatusPartialContent || tail.Body.String() != "89" {
					t.Errorf("tail GET = %d %q, want 206 and 89", tail.Code, tail.Body.String())
				}
				cached := serve(t, http.Header{"If-None-Match": {whole.Header().Get("ETag")}})
				if cached.Code != http.StatusNotModified {
					t.Errorf("conditional GET = %d, want 304", cached.Code)
				}
				req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/documents/1/content", http.NoBody)
				rr := httptest.NewRecorder()
				err := resource.ServeStoredFile(t.Context(), rr, req, store, resource.StoredFile{Key: newKey(t)}, "content", "Document", 1)
				if err == nil || !strings.Contains(err.Error(), "was not found in the store") {
					t.Errorf("ServeStoredFile() of a missing key error = %v, want the frame's not-found", err)
				}
			})
		})
	}
}

// copyHeaders copies headers onto a request's.
func copyHeaders(dst, src http.Header) {
	for k, v := range src {
		dst[k] = v
	}
}

// TestObjects pins the listing: every object with its size and a creation time, and a
// listing that stops when the consumer does.
func TestObjects(t *testing.T) {
	t.Parallel()

	for _, sc := range schemes() {
		t.Run(sc.name, func(t *testing.T) {
			t.Parallel()

			store := openStore(t, sc.url(t))
			if sc.name == "gs" && !strings.HasPrefix(sc.url(t), "gs://filestore-") {
				t.Skip("a live bucket's listing is not the test's to assert")
			}
			want := map[string]int64{}
			for i, body := range []string{"a", "bb", "ccc"} {
				key := newKey(t)
				put(t, store, key, "text/plain", body)
				want[key] = int64(i + 1)
			}
			got := map[string]int64{}
			for obj, err := range store.Objects(t.Context()) {
				if err != nil {
					t.Fatalf("Objects() error = %v", err)
				}
				if obj.Created.IsZero() {
					t.Errorf("%s: Created is zero", obj.Key)
				}
				got[obj.Key] = obj.Size
			}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("Objects() mismatch (-want +got):\n%s", diff)
			}
			var seen int
			for _, err := range store.Objects(t.Context()) {
				if err != nil {
					t.Fatalf("Objects() error = %v", err)
				}
				seen++

				break
			}
			if seen != 1 {
				t.Errorf("a stopped listing yielded %d, want 1", seen)
			}
		})
	}
}

// TestOpen_refusals pins Open's own refusals: the URL shapes it does not accept, with
// the fix in the message.
func TestOpen_refusals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		url     string
		wantErr string
	}{
		{name: "no scheme", url: "uploads", wantErr: `the file store URL "uploads" has no scheme; a store is gs://<bucket>, file://<dir> or mem://`},
		{name: "an unknown scheme", url: "s3://files", wantErr: `has the scheme s3, which this package does not open`},
		{name: "a bucket with a path", url: "gs://files/sub", wantErr: "names more than a bucket; a bucket store is gs://<bucket>, with no path, query, user information, port or fragment"},
		{name: "a bucket with a query", url: "gs://files?x=1", wantErr: "names more than a bucket"},
		{name: "a bucket with user information", url: "gs://me@files", wantErr: "names more than a bucket"},
		{name: "a bucket with a fragment", url: "gs://files#frag", wantErr: "names more than a bucket"},
		{name: "a bucket with a port", url: "gs://files:443", wantErr: "names more than a bucket"},
		{name: "a bucket alone", url: "gs://", wantErr: "names more than a bucket"},
		{name: "a directory alone", url: "file://", wantErr: "names no directory; a directory store is file://<dir>"},
		{name: "memory with a name", url: "mem://documents", wantErr: "names something after mem://; a memory store is mem:// alone"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := filestore.Open(t.Context(), tt.url)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Open(%s) error = %v, want containing %q", tt.url, err, tt.wantErr)
			}
		})
	}
}

// TestOpen_missingBucket pins the start-up refusal: a bucket that does not exist does
// not start.
func TestOpen_missingBucket(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("runs against the fake Cloud Storage server in a container")
	}

	_, err := filestore.Open(t.Context(), "gs://filestore-no-such-bucket-"+strings.ToLower(newKey(t)))
	if err == nil || !strings.Contains(err.Error(), "is not usable") {
		t.Fatalf("Open() of a missing bucket error = %v, want the start-up refusal", err)
	}
}

// TestOpen_cloudRun pins what the store refuses on Cloud Run: a directory, memory, the
// emulator variable and a key file. The tests set the environment, so they run in
// sequence.
func TestOpen_cloudRun(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		url     string
		wantErr string
	}{
		{name: "a directory in a service", env: map[string]string{"K_SERVICE": "harbor"}, url: "file://uploads", wantErr: "the file store file:// is refused on Cloud Run; a service or job keeps its files in a bucket, gs://<bucket>"},
		{name: "memory in a job", env: map[string]string{"CLOUD_RUN_JOB": "harbor-jobs"}, url: "mem://", wantErr: "the file store mem:// is refused on Cloud Run"},
		{name: "the emulator variable", env: map[string]string{"K_SERVICE": "harbor", "STORAGE_EMULATOR_HOST": "fake:4443"}, url: "gs://harbor-files", wantErr: `STORAGE_EMULATOR_HOST is set to "fake:4443", which is refused on Cloud Run; the variable sends every file request unauthenticated over plain HTTP to the host it names`},
		{name: "a key file", env: map[string]string{"K_SERVICE": "harbor", keyFileVariable: "/secrets/key.json"}, url: "gs://harbor-files", wantErr: `GOOGLE_APPLICATION_CREDENTIALS is set to "/secrets/key.json", which is refused on Cloud Run; the service's own identity is its credential, and a key file is never one`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, name := range []string{"K_SERVICE", "CLOUD_RUN_JOB", "STORAGE_EMULATOR_HOST", "GOOGLE_APPLICATION_CREDENTIALS"} {
				t.Setenv(name, tt.env[name])
			}
			_, err := filestore.Open(t.Context(), tt.url)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Open(%s) error = %v, want containing %q", tt.url, err, tt.wantErr)
			}
		})
	}
}

// TestDir_confinement pins the directory store's confinement: a key resolving outside
// the directory never reaches it, and a symbolic link inside pointing out is not
// followed.
func TestDir_confinement(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := openStore(t, "file://"+dir)
	if err := store.Put(t.Context(), "../escape", "text/plain", strings.NewReader("x")); err == nil {
		t.Error("Put(../escape) succeeded, want a refusal")
	}
	var buf bytes.Buffer
	buf.WriteString("x")
	if err := store.Put(t.Context(), "nested/../../escape", "text/plain", &buf); err == nil {
		t.Error("Put(nested/../../escape) succeeded, want a refusal")
	}
}

// TestBucket_refusedPermission pins the 403 path against a server answering 403: the
// store starts, every file operation answers 503 through the frame, and once the
// server grants, the next operation runs. The test sets the emulator variable, so it
// runs in sequence.
func TestBucket_refusedPermission(t *testing.T) {
	var granted atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if !granted.Load() {
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"error":{"code":403,"message":"the caller does not have storage.objects.list access","errors":[{"message":"forbidden","domain":"global","reason":"forbidden"}]}}`)

			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/o") || strings.HasSuffix(r.URL.Path, "/o/"):
			_, _ = io.WriteString(w, `{"kind":"storage#objects","items":[]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error":{"code":404,"message":"No such object"}}`)
		}
	}))
	t.Cleanup(server.Close)
	t.Setenv("STORAGE_EMULATOR_HOST", strings.TrimPrefix(server.URL, "http://"))

	store, err := filestore.Open(t.Context(), "gs://harbor-files")
	if err != nil {
		t.Fatalf("Open() under a refused permission error = %v, want the store to start", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	serve := func(t *testing.T) error {
		t.Helper()
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/documents/1/content", http.NoBody)
		rr := httptest.NewRecorder()
		if err := resource.ServeStoredFile(t.Context(), rr, req, store, resource.StoredFile{Key: newKey(t)}, "content", "Document", 1); err != nil {
			return perrors.Wrap(err, "resource.ServeStoredFile()")
		}

		return nil
	}
	if err := serve(t); err == nil || !strings.Contains(err.Error(), "the file store is unavailable while its permission is refused") {
		t.Fatalf("ServeStoredFile() under a refused permission error = %v, want the 503 message", err)
	}
	if err := store.Put(t.Context(), newKey(t), "text/plain", strings.NewReader("x")); err == nil || !strings.Contains(err.Error(), "unavailable while its permission is refused") {
		t.Errorf("Put() under a refused permission error = %v, want the 503 message", err)
	}
	if err := store.Delete(t.Context(), []string{newKey(t)}); err == nil || !strings.Contains(err.Error(), "unavailable while its permission is refused") {
		t.Errorf("Delete() under a refused permission error = %v, want the 503 message", err)
	}

	granted.Store(true)
	if err := serve(t); err == nil || !strings.Contains(err.Error(), "was not found in the store") {
		t.Fatalf("ServeStoredFile() once granted error = %v, want the store probed again and the frame's not-found", err)
	}
}

// TestBucket_abortedWrite pins the cancel-before-close rule across upload chunks: a
// writer whose context is canceled mid-copy saves nothing, where closing it would save
// a truncated object.
func TestBucket_abortedWrite(t *testing.T) {
	t.Parallel()

	store := openStore(t, bucketStoreURL(t))
	ctx, cancel := context.WithCancel(t.Context())
	key := newKey(t)
	// The reader cancels the context once the first chunk is past, then fails.
	var sent int
	r := io.MultiReader(bytes.NewReader(bytes.Repeat([]byte("y"), 3<<19)), readerFunc(func([]byte) (int, error) {
		sent++
		cancel()

		return 0, context.Canceled
	}))
	err := store.Put(ctx, key, "application/octet-stream", r)
	if err == nil {
		t.Fatal("Put() with a canceled copy succeeded, want an error")
	}
	if sent == 0 {
		t.Fatalf("the canceling reader was never reached; Put() error = %v", err)
	}
	if _, err := store.Open(t.Context(), key); !errors.Is(err, resource.ErrFileNotFound) {
		t.Errorf("Open() after the aborted write error = %v, want resource.ErrFileNotFound", err)
	}
}

// readerFunc adapts a function to io.Reader.
type readerFunc func(p []byte) (int, error)

func (f readerFunc) Read(p []byte) (int, error) {
	return f(p)
}
