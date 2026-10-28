package shipped

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestHandler pins the server side of the handshake: the file with its checksum, HEAD
// with the headers alone, and each refusal with its sentence: a program or platform the
// release does not ship, another release naming the server's, the directory unset, the
// file missing, a path of another shape, and a method other than GET.
func TestHandler(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	content := []byte("#!/bin/sh\necho corral 2.0.0\n")
	writeFile(t, filepath.Join(dir, "corral-linux-amd64"), content)
	sum := sha256.Sum256(content)
	checksum := hex.EncodeToString(sum[:])
	programs := []Program{{Name: "corral", Platforms: []string{"linux/amd64", "linux/arm64"}}}
	fileHeaders := map[string]string{"Content-Type": "application/octet-stream", "Content-Length": strconv.Itoa(len(content)), ChecksumHeader: checksum}

	tests := []struct {
		name     string
		dir      string
		release  string
		method   string
		path     string
		wantCode int
		// wantBody is the exact body of an answered fetch, or a fragment of a refusal's
		// sentence.
		wantBody    string
		wantHeaders map[string]string
	}{
		{name: "the file with its checksum", dir: dir, release: "2.0.0", method: http.MethodGet, path: "/corral/2.0.0/linux/amd64", wantCode: http.StatusOK, wantBody: string(content), wantHeaders: fileHeaders},
		{name: "the release with a leading v", dir: dir, release: "2.0.0", method: http.MethodGet, path: "/corral/v2.0.0/linux/amd64", wantCode: http.StatusOK, wantBody: string(content), wantHeaders: fileHeaders},
		{name: "a server release with a leading v", dir: dir, release: "v2.0.0", method: http.MethodGet, path: "/corral/2.0.0/linux/amd64", wantCode: http.StatusOK, wantBody: string(content), wantHeaders: fileHeaders},
		{name: "HEAD answers the headers alone", dir: dir, release: "2.0.0", method: http.MethodHead, path: "/corral/2.0.0/linux/amd64", wantCode: http.StatusOK, wantBody: "", wantHeaders: fileHeaders},
		{name: "a program the release does not ship", dir: dir, release: "2.0.0", method: http.MethodGet, path: "/beacon/2.0.0/linux/amd64", wantCode: http.StatusNotFound, wantBody: "this release ships no beacon for linux/amd64"},
		{name: "a platform the release does not ship", dir: dir, release: "2.0.0", method: http.MethodGet, path: "/corral/2.0.0/darwin/arm64", wantCode: http.StatusNotFound, wantBody: "this release ships no corral for darwin/arm64"},
		{name: "another release names the server's", dir: dir, release: "2.0.0", method: http.MethodGet, path: "/corral/1.9.0/linux/amd64", wantCode: http.StatusNotFound, wantBody: "this server is release 2.0.0, not 1.9.0: read its release from X-Api-Version and ask again"},
		{name: "a server that is not a release", dir: dir, release: "dev", method: http.MethodGet, path: "/corral/2.0.0/linux/amd64", wantCode: http.StatusNotFound, wantBody: "this server is release dev, not 2.0.0"},
		{name: "the directory unset", dir: "", release: "2.0.0", method: http.MethodGet, path: "/corral/2.0.0/linux/amd64", wantCode: http.StatusNotFound, wantBody: "this build carries no programs: APP_PROGRAMS is unset"},
		{name: "the file missing", dir: dir, release: "2.0.0", method: http.MethodGet, path: "/corral/2.0.0/linux/arm64", wantCode: http.StatusNotFound, wantBody: "this build carries no corral for linux/arm64: " + filepath.Join(dir, "corral-linux-arm64") + " is missing"},
		{name: "a path of another shape", dir: dir, release: "2.0.0", method: http.MethodGet, path: "/corral/2.0.0/linux", wantCode: http.StatusNotFound, wantBody: "the path names no program: a program is fetched at <program>/<release>/<os>/<arch>"},
		{name: "an empty segment", dir: dir, release: "2.0.0", method: http.MethodGet, path: "/corral//linux/amd64", wantCode: http.StatusNotFound, wantBody: "the path names no program"},
		{name: "a fifth segment", dir: dir, release: "2.0.0", method: http.MethodGet, path: "/corral/2.0.0/linux/amd64/extra", wantCode: http.StatusNotFound, wantBody: "the path names no program"},
		{name: "a method other than GET", dir: dir, release: "2.0.0", method: http.MethodPost, path: "/corral/2.0.0/linux/amd64", wantCode: http.StatusMethodNotAllowed, wantBody: "a program is fetched with GET", wantHeaders: map[string]string{"Allow": "GET, HEAD"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newHandler(tt.dir, tt.release, programs)
			req := httptest.NewRequestWithContext(t.Context(), tt.method, tt.path, http.NoBody)
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)

			if rr.Code != tt.wantCode {
				t.Fatalf("Code = %d, want %d; body %q", rr.Code, tt.wantCode, rr.Body.String())
			}
			if tt.wantCode == http.StatusOK {
				if got := rr.Body.String(); got != tt.wantBody {
					t.Errorf("body = %q, want %q", got, tt.wantBody)
				}
			} else if got := rr.Body.String(); !strings.Contains(got, tt.wantBody) {
				t.Errorf("body = %q, want it to carry %q", got, tt.wantBody)
			}
			for name, want := range tt.wantHeaders {
				if got := rr.Header().Get(name); got != want {
					t.Errorf("%s = %q, want %q", name, got, want)
				}
			}
		})
	}
}

// TestHandler_checksumIsKept pins that a file is hashed once: the checksum a later
// answer carries is the first one computed, since a file never changes within a build.
func TestHandler_checksumIsKept(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "corral-linux-amd64")
	first := []byte("first")
	writeFile(t, path, first)
	sum := sha256.Sum256(first)
	want := hex.EncodeToString(sum[:])
	h := newHandler(dir, "2.0.0", []Program{{Name: "corral", Platforms: []string{"linux/amd64"}}})

	for i, content := range [][]byte{first, []byte("second")} {
		writeFile(t, path, content)
		req := httptest.NewRequestWithContext(t.Context(), http.MethodHead, "/corral/2.0.0/linux/amd64", http.NoBody)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("answer %d: Code = %d, want 200", i, rr.Code)
		}
		if got := rr.Header().Get(ChecksumHeader); got != want {
			t.Errorf("answer %d: %s = %q, want the first checksum %q", i, ChecksumHeader, got, want)
		}
	}
}

// TestHandler_mounted pins the route as the generated router mounts it: under the
// outlet's prefix at /programs, the prefix stripped before the handler reads the path.
func TestHandler_mounted(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	content := []byte("corral")
	writeFile(t, filepath.Join(dir, "corral-linux-amd64"), content)
	mux := http.NewServeMux()
	mux.Handle("/machines/programs/", http.StripPrefix("/machines/programs", newHandler(dir, "2.0.0", []Program{{Name: "corral", Platforms: []string{"linux/amd64"}}})))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/machines/programs/corral/2.0.0/linux/amd64", http.NoBody)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || rr.Body.String() != string(content) {
		t.Fatalf("Code = %d, body = %q; want 200 with the file", rr.Code, rr.Body.String())
	}
}

// TestHandler_readsDirVariable pins that Handler reads the directory from DirVariable
// when it is built: set, the file is served; unset, every fetch is refused saying so.
func TestHandler_readsDirVariable(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "corral-linux-amd64"), []byte("corral"))
	program := Program{Name: "corral", Platforms: []string{"linux/amd64"}}

	tests := []struct {
		name     string
		value    string
		wantCode int
	}{
		{name: "set", value: dir, wantCode: http.StatusOK},
		{name: "unset", value: "", wantCode: http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(DirVariable, tt.value)

			h := Handler("2.0.0", program)
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/corral/2.0.0/linux/amd64", http.NoBody)
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code != tt.wantCode {
				t.Fatalf("Code = %d, want %d; body %q", rr.Code, tt.wantCode, rr.Body.String())
			}
		})
	}
}

// writeFile writes content to path, for a program the test's directory carries.
func writeFile(t *testing.T, path string, content []byte) {
	t.Helper()

	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("os.WriteFile(%s) error = %v", path, err)
	}
}
