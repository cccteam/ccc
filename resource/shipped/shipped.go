// Package shipped is the handshake between a machine outlet and the installed copies of
// the programs a release ships through it: the route the outlet serves the compiled
// programs at, the header that carries each file's checksum, and the release header
// every answer of the outlet already carries. It is the one surface that never
// changes, so a copy of any release can fetch its next release from a server of any
// release.
//
// On the server, the generated router mounts Handler under the outlet's prefix, behind
// the outlet's token check, at <prefix>/programs: GET
// <prefix>/programs/<program>/<release>/<os>/<arch> answers the compiled file the image
// carries in the directory DirVariable names, with its SHA-256 in ChecksumHeader. A
// request the server cannot answer is refused with 404 and one sentence saying why: the
// program or platform is not declared, the release is not the server's (the sentence
// names which release the server is), the image carries no programs, or the file is
// missing.
//
// In the running copy, a Follower watches the release every answer of the outlet names
// in X-Api-Version through the transport it wraps, says when a newer release has been
// seen, and upgrades the copy: it fetches the matching file with the token the copy
// already carries, writes it beside the running executable, verifies the checksum,
// keeps the old copy as <executable>.old and renames the new file over the running
// path. It never exits the process: the caller logs the new release and exits 0, so its
// service manager starts the new release.
package shipped

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/go-playground/errors/v5"
	"golang.org/x/mod/semver"
)

const (
	// DirVariable names the directory the image carries the compiled programs in; the
	// image sets it (bedrock's Dockerfile sets APP_PROGRAMS=/programs). Each file is
	// named <name>-<os>-<arch>, corral-linux-amd64. Unset, the server carries no
	// programs and refuses every fetch saying so.
	DirVariable = "APP_PROGRAMS"
	// ChecksumHeader carries the hex SHA-256 of the file an answer carries. A copy
	// verifies what it wrote against it before anything runs the file.
	ChecksumHeader = "X-Program-Sha256"
	// ReleaseHeader is the header every answer of an outlet names the server's release
	// in, the same header the browser application sends its own release in. A copy
	// learns a newer release from it on any answer, and reads which release the server
	// is from a refused fetch.
	ReleaseHeader = "X-Api-Version"
	// routeSegment is the path segment under the outlet's prefix the programs are
	// served at.
	routeSegment = "programs"
)

// Program is one program a machine outlet ships: its name (the base of its main
// package's directory, corral for cmd/corral) and the platforms the release compiled it
// for, each <os>/<arch>.
type Program struct {
	Name      string
	Platforms []string
}

// Handler serves the programs the release ships: GET /<program>/<release>/<os>/<arch>,
// the path relative to where the router mounts it, answers the compiled file with
// Content-Type application/octet-stream, its length, and its SHA-256 in ChecksumHeader;
// HEAD answers the headers alone. release is the server's own release, what every
// answer of the outlet names in X-Api-Version: a fetch for any other release is refused
// naming this one, so a copy asks again for the release it read. The files are read from
// the directory DirVariable names when the handler is built; each file's checksum is
// computed once and kept, since a file never changes within a build. A method other
// than GET and HEAD answers 405.
func Handler(release string, programs ...Program) http.Handler {
	return newHandler(os.Getenv(DirVariable), release, programs)
}

// handler is the server side: the directory, the release, the file each declared
// program and platform is served from, and the checksums computed so far by file path.
type handler struct {
	dir     string
	release string
	// paths is the file of each declared program on each of its platforms, keyed by
	// <name>/<os>/<arch>: a fetch is answered only from a path declared here, never
	// from one spelled by the request.
	paths map[string]string

	mu    sync.Mutex
	files map[string]servedFile
}

// servedFile is what the handler keeps of a file after reading it once.
type servedFile struct {
	sha256 string
	size   int64
}

// newHandler builds the handler over dir, the directory the programs are read from;
// empty when the image carries none.
func newHandler(dir, release string, programs []Program) *handler {
	h := &handler{dir: dir, release: release, paths: make(map[string]string), files: make(map[string]servedFile)}
	for _, program := range programs {
		for _, platform := range program.Platforms {
			h.paths[program.Name+"/"+platform] = filepath.Join(dir, program.Name+"-"+strings.ReplaceAll(platform, "/", "-"))
		}
	}

	return h
}

// ServeHTTP answers one fetch.
func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "a program is fetched with GET", http.StatusMethodNotAllowed)

		return
	}

	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 4 || hasEmpty(parts) {
		http.Error(w, "the path names no program: a program is fetched at <program>/<release>/<os>/<arch>", http.StatusNotFound)

		return
	}
	name, release, goos, goarch := parts[0], parts[1], parts[2], parts[3]
	platform := goos + "/" + goarch
	path, ok := h.paths[name+"/"+platform]
	if !ok {
		http.Error(w, fmt.Sprintf("this release ships no %s for %s", name, platform), http.StatusNotFound)

		return
	}
	if !sameRelease(release, h.release) {
		http.Error(w, fmt.Sprintf("this server is release %s, not %s: read its release from %s and ask again", h.release, release, ReleaseHeader), http.StatusNotFound)

		return
	}
	if h.dir == "" {
		http.Error(w, fmt.Sprintf("this build carries no programs: %s is unset", DirVariable), http.StatusNotFound)

		return
	}

	file, err := h.file(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		http.Error(w, fmt.Sprintf("this build carries no %s for %s: %s is missing", name, platform, path), http.StatusNotFound)

		return
	case err != nil:
		http.Error(w, fmt.Sprintf("reading %s for %s failed: %v", name, platform, err), http.StatusInternalServerError)

		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(file.size, 10))
	w.Header().Set(ChecksumHeader, file.sha256)
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)

		return
	}

	f, err := os.Open(path)
	if err != nil {
		http.Error(w, fmt.Sprintf("reading %s for %s failed: %v", name, platform, err), http.StatusInternalServerError)

		return
	}
	defer f.Close()

	w.WriteHeader(http.StatusOK)
	// The connection is the copy's to retry: nothing is left to say once the body
	// has started.
	_, _ = io.Copy(w, f)
}

// file returns what the handler keeps of the file at path, reading and hashing it the
// first time; a missing file is an error wrapping os.ErrNotExist.
func (h *handler) file(path string) (servedFile, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if file, ok := h.files[path]; ok {
		return file, nil
	}

	f, err := os.Open(path)
	if err != nil {
		return servedFile{}, errors.Wrap(err, "os.Open()")
	}
	defer f.Close()

	digest := sha256.New()
	size, err := io.Copy(digest, f)
	if err != nil {
		return servedFile{}, errors.Wrap(err, "io.Copy()")
	}
	file := servedFile{sha256: hex.EncodeToString(digest.Sum(nil)), size: size}
	h.files[path] = file

	return file, nil
}

// hasEmpty reports whether any part is empty.
func hasEmpty(parts []string) bool {
	for _, part := range parts {
		if part == "" {
			return true
		}
	}

	return false
}

// sameRelease reports whether two releases are the same: spelled the same, or the same
// semantic version with or without the leading v (1.5.0 and v1.5.0).
func sameRelease(a, b string) bool {
	if a == b {
		return true
	}
	a, b = withV(a), withV(b)

	return semver.IsValid(a) && semver.IsValid(b) && semver.Compare(a, b) == 0
}

// withV returns the release with the leading v semantic versions carry.
func withV(release string) string {
	if strings.HasPrefix(release, "v") {
		return release
	}

	return "v" + release
}
