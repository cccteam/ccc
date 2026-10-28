package shipped

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
)

// TestOwnRelease pins how a copy learns its release: the stamped value first, else the
// main module's version when it is a release, else nothing for a development copy.
func TestOwnRelease(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		stamped string
		version string
		noInfo  bool
		want    string
	}{
		{name: "stamped at link time", stamped: "1.5.0", version: "(devel)", want: "1.5.0"},
		{name: "stamped wins over the build information", stamped: "1.5.0", version: "v1.4.0", want: "1.5.0"},
		{name: "installed with go install", version: "v1.5.0", want: "v1.5.0"},
		{name: "a pre-release installed with go install", version: "v2.0.0-rc1", want: "v2.0.0-rc1"},
		{name: "a development build", version: "(devel)", want: ""},
		{name: "no build information", noInfo: true, want: ""},
		{name: "a pseudo-version of an untagged commit", version: "v0.0.0-20261027120000-abcdef123456", want: ""},
		{name: "a pseudo-version after a tag", version: "v1.5.1-0.20261027120000-abcdef123456", want: ""},
		{name: "a tag built from a dirty checkout", version: "v1.5.0+dirty", want: ""},
		{name: "an empty version", version: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			info := &debug.BuildInfo{}
			info.Main.Version = tt.version
			ok := true
			if tt.noInfo {
				info, ok = nil, false
			}
			if got := ownRelease(tt.stamped, info, ok); got != tt.want {
				t.Errorf("ownRelease() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestFollower_Newer pins what the transport records from the outlet's answers and what
// Newer reports: the newest release seen, with the leading v, when it is newer than the
// copy's own; nothing when no answer named a newer release, when an answer named no
// release, or when the copy is a development copy.
func TestFollower_Newer(t *testing.T) {
	t.Parallel()

	// The server names in X-Api-Version whatever release the request asks it to, so one
	// server stands in for a sequence of releases.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if release := r.Header.Get("X-Test-Release"); release != "" {
			w.Header().Set(ReleaseHeader, release)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	tests := []struct {
		name    string
		own     string
		answers []string
		want    string
		wantOK  bool
	}{
		{name: "no answer yet", own: "1.5.0"},
		{name: "the copy's own release", own: "1.5.0", answers: []string{"1.5.0"}},
		{name: "an older release", own: "1.5.0", answers: []string{"1.4.0"}},
		{name: "a newer release", own: "1.5.0", answers: []string{"1.6.0"}, want: "v1.6.0", wantOK: true},
		{name: "the newest of several", own: "1.5.0", answers: []string{"1.6.0", "1.7.0", "1.6.5"}, want: "v1.7.0", wantOK: true},
		{name: "releases with the leading v", own: "v1.5.0", answers: []string{"v1.6.0"}, want: "v1.6.0", wantOK: true},
		{name: "an answer that names no release", own: "1.5.0", answers: []string{"dev", ""}},
		{name: "a release among answers that name none", own: "1.5.0", answers: []string{"dev", "1.6.0", ""}, want: "v1.6.0", wantOK: true},
		{name: "a development copy", own: "dev", answers: []string{"1.6.0"}},
		{name: "a pre-release below the release", own: "1.6.0", answers: []string{"1.6.0-rc1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := &Follower{Outlet: srv.URL, Name: "corral", Release: tt.own}
			client := &http.Client{Transport: f.Transport(srv.Client().Transport)}
			for _, answer := range tt.answers {
				req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/ping", http.NoBody)
				if err != nil {
					t.Fatalf("http.NewRequestWithContext() error = %v", err)
				}
				req.Header.Set("X-Test-Release", answer)
				resp, err := client.Do(req)
				if err != nil {
					t.Fatalf("client.Do() error = %v", err)
				}
				if err := resp.Body.Close(); err != nil {
					t.Fatalf("Body.Close() error = %v", err)
				}
			}

			got, ok := f.Newer()
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("Newer() = %q, %v; want %q, %v", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

// TestFollower_Upgrade pins the self-upgrade against a server built from the handler:
// the copy is replaced by the fetched file, verified, with the running file kept as
// .old; a checksum mismatch, a server that moved on, a server that ships no file, and
// no newer release each leave the copy as it was.
func TestFollower_Upgrade(t *testing.T) {
	t.Parallel()

	platform := runtime.GOOS + "/" + runtime.GOARCH
	fileName := "corral-" + runtime.GOOS + "-" + runtime.GOARCH
	oldContent, newContent := []byte("corral 1.5.0"), []byte("corral 1.6.0")
	programs := []Program{{Name: "corral", Platforms: []string{platform}}}

	tests := []struct {
		name string
		// server is the release every answer of the outlet names.
		server string
		// handler builds the programs route over dir, the directory the server's file is
		// written to; nil mounts the handler at the server's release.
		handler func(dir string) http.Handler
		// noFile leaves the server's directory empty; oldCopy writes an older .old
		// beside the executable first.
		noFile  bool
		oldCopy bool
		want    string
		wantErr string
	}{
		{name: "the copy is replaced", server: "1.6.0", want: "v1.6.0"},
		{name: "an older .old is replaced", server: "1.6.0", oldCopy: true, want: "v1.6.0"},
		{
			name:   "a checksum mismatch",
			server: "1.6.0",
			handler: func(string) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set(ChecksumHeader, "0000")
					_, _ = w.Write(newContent)
				})
			},
			wantErr: "is not the X-Program-Sha256 the answer carried, 0000: the file is removed",
		},
		{
			name:   "an answer without the checksum",
			server: "1.6.0",
			handler: func(string) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					_, _ = w.Write(newContent)
				})
			},
			wantErr: "answered without X-Program-Sha256: the answer is not a shipped program",
		},
		{
			name:   "the server moved on",
			server: "1.6.0",
			handler: func(dir string) http.Handler {
				return newHandler(dir, "1.7.0", programs)
			},
			wantErr: "answered 404: this server is release 1.7.0, not v1.6.0: read its release from X-Api-Version and ask again",
		},
		{name: "the server ships no file", server: "1.6.0", noFile: true, wantErr: "answered 404: this build carries no corral for " + platform},
		{name: "no newer release", server: "1.5.0", wantErr: `no release newer than this copy's ("1.5.0") has been seen on the outlet's answers`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			serverDir := t.TempDir()
			if !tt.noFile {
				writeFile(t, filepath.Join(serverDir, fileName), newContent)
			}
			var route http.Handler = newHandler(serverDir, tt.server, programs)
			if tt.handler != nil {
				route = tt.handler(serverDir)
			}
			mux := http.NewServeMux()
			mux.HandleFunc("/machines/ping", func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			})
			mux.Handle("/machines/programs/", http.StripPrefix("/machines/programs", route))
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set(ReleaseHeader, tt.server)
				mux.ServeHTTP(w, r)
			}))
			t.Cleanup(srv.Close)

			copyDir := t.TempDir()
			executable := filepath.Join(copyDir, "corral")
			writeFile(t, executable, oldContent)
			if tt.oldCopy {
				writeFile(t, executable+".old", []byte("corral 1.4.0"))
			}

			f := &Follower{Outlet: srv.URL + "/machines/", Name: "corral", Release: "1.5.0", Executable: executable}
			f.Client = &http.Client{Transport: f.Transport(srv.Client().Transport)}
			ping(t, f.Client, srv.URL+"/machines/ping")

			got, err := f.Upgrade(t.Context())
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Upgrade() error = %v, want it to contain %q", err, tt.wantErr)
				}
				if content := readFile(t, executable); !bytes.Equal(content, oldContent) {
					t.Errorf("the running file = %q, want it untouched", content)
				}
				wantNames := "corral"
				if tt.oldCopy {
					wantNames = "corral,corral.old"
				}
				if names := entries(t, copyDir); strings.Join(names, ",") != wantNames {
					t.Errorf("directory = %v, want nothing written beside the copy", names)
				}

				return
			}
			if err != nil {
				t.Fatalf("Upgrade() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("Upgrade() = %q, want %q", got, tt.want)
			}
			if content := readFile(t, executable); !bytes.Equal(content, newContent) {
				t.Errorf("the running file = %q, want the fetched %q", content, newContent)
			}
			info, err := os.Stat(executable)
			if err != nil {
				t.Fatalf("os.Stat() error = %v", err)
			}
			if mode := info.Mode().Perm(); mode != 0o755 {
				t.Errorf("mode = %o, want 755", mode)
			}
			if content := readFile(t, executable+".old"); !bytes.Equal(content, oldContent) {
				t.Errorf("the .old file = %q, want the replaced %q", content, oldContent)
			}
			if names := entries(t, copyDir); strings.Join(names, ",") != "corral,corral.old" {
				t.Errorf("directory = %v, want the copy and its .old alone", names)
			}
			want := sha256.Sum256(newContent)
			if sum := sha256.Sum256(readFile(t, executable)); hex.EncodeToString(sum[:]) != hex.EncodeToString(want[:]) {
				t.Errorf("the running file's checksum differs from the fetched file's")
			}
		})
	}
}

// ping makes one call through the follower's client, so the transport sees the
// server's release.
func ping(t *testing.T, client *http.Client, url string) {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, http.NoBody)
	if err != nil {
		t.Fatalf("http.NewRequestWithContext() error = %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("client.Do() error = %v", err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatalf("Body.Close() error = %v", err)
	}
}

// readFile reads one file.
func readFile(t *testing.T, path string) []byte {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile(%s) error = %v", path, err)
	}

	return content
}

// entries lists a directory's names in order.
func entries(t *testing.T, dir string) []string {
	t.Helper()

	list, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("os.ReadDir(%s) error = %v", dir, err)
	}
	names := make([]string, 0, len(list))
	for _, entry := range list {
		names = append(names, entry.Name())
	}

	return names
}
