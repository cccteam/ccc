package shipped

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"

	"github.com/go-playground/errors/v5"
	"golang.org/x/mod/semver"
)

// Release is the release the program was built from, stamped at link time:
//
//	-ldflags "-X github.com/cccteam/ccc/resource/shipped.Release=1.5.0"
//
// bedrock's image build stamps it on every program the release ships. Empty in a build
// that stamps nothing, where OwnRelease falls back to the build information.
var Release string

// OwnRelease is the release this copy was built from: Release when it is stamped; else
// the main module's version from the build information when it is a release, which a
// copy installed with go install module@v1.5.0 carries; else empty, for a development
// copy, which never upgrades. A version Go stamps from a checkout, a pseudo-version
// (v0.0.0-20261027120000-abcdef123456) or one marked +dirty, is a development copy.
func OwnRelease() string {
	info, ok := debug.ReadBuildInfo()

	return ownRelease(Release, info, ok)
}

// pseudoVersionPattern is the shape of a pseudo-version, as the go command spells one
// for a commit no tag names: vX.0.0-<timestamp>-<commit>, or a tag's version followed
// by .0.<timestamp>-<commit>, each with optional build metadata.
var pseudoVersionPattern = regexp.MustCompile(`^v[0-9]+\.(0\.0-|\d+\.\d+-([^+]*\.)?0\.)\d{14}-[A-Za-z0-9]+(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$`)

// ownRelease resolves the copy's release from the stamped value and the build
// information.
func ownRelease(stamped string, info *debug.BuildInfo, ok bool) string {
	if stamped != "" {
		return stamped
	}
	if !ok || info == nil {
		return ""
	}
	version := info.Main.Version
	if !semver.IsValid(version) || semver.Build(version) != "" || pseudoVersionPattern.MatchString(version) {
		return ""
	}

	return version
}

// Follower is the running copy's side of the handshake: it watches the release the
// outlet's answers name, and upgrades the copy to the newest one seen.
type Follower struct {
	// Client is the client the copy already calls the outlet with: its transport adds
	// the bearer token, so the fetch carries the same token as every other call. Nil
	// is http.DefaultClient, which carries no token.
	Client *http.Client
	// Outlet is the outlet's base URL as the router mounts it,
	// https://app.example.com/machines; the programs are fetched under it.
	Outlet string
	// Name is the program's declared name, corral for cmd/corral.
	Name string
	// Release is the copy's own release; empty takes OwnRelease(). A copy whose release
	// is not a semantic version, a development copy, never sees a newer release.
	Release string
	// Executable is the path of the running executable, for tests; empty takes
	// os.Executable with symbolic links resolved.
	Executable string

	mu     sync.Mutex
	newest string
}

// Transport wraps the copy's transport: every answer that passes through it is read
// for the server's release in X-Api-Version, and the newest release seen is kept. The
// answers themselves pass through untouched. next nil wraps http.DefaultTransport.
func (f *Follower) Transport(next http.RoundTripper) http.RoundTripper {
	if next == nil {
		next = http.DefaultTransport
	}

	return roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		resp, err := next.RoundTrip(r)
		if err != nil {
			return nil, errors.Wrap(err, "RoundTrip()")
		}
		f.saw(resp.Header.Get(ReleaseHeader))

		return resp, nil
	})
}

// roundTripperFunc adapts a function to http.RoundTripper.
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

// saw records the release an answer named when it is a release newer than any seen.
// Releases compare as semantic versions, with the leading v added when the answer
// lacks it; an answer that names no release, dev for one, is ignored.
func (f *Follower) saw(release string) {
	release = withV(strings.TrimSpace(release))
	if !semver.IsValid(release) {
		return
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.newest == "" || semver.Compare(release, f.newest) > 0 {
		f.newest = release
	}
}

// Newer reports the newest release seen on the outlet's answers when it is newer than
// the copy's own, with the leading v; ok is false when no newer release has been seen
// or the copy's own release is not one.
func (f *Follower) Newer() (release string, ok bool) {
	own := withV(f.ownRelease())
	if !semver.IsValid(own) {
		return "", false
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.newest == "" || semver.Compare(f.newest, own) <= 0 {
		return "", false
	}

	return f.newest, true
}

// ownRelease is the copy's release as configured or as built.
func (f *Follower) ownRelease() string {
	if f.Release != "" {
		return f.Release
	}

	return OwnRelease()
}

// Upgrade replaces the running copy with the newest release seen: it fetches
// <Outlet>/programs/<Name>/<release>/<os>/<arch> for the platform it runs on, writes the
// file beside the running executable with mode 0755 and syncs it, verifies the SHA-256
// against the answer's ChecksumHeader (a mismatch removes the file and is reported),
// renames the running path to <executable>.old, replacing an older one, and renames the
// new file over the running path. The rename is atomic on one filesystem, and the
// running process keeps the old file open and runs on: Upgrade returns the release and
// never exits the process, so the caller logs the release and exits 0 for its service
// manager to start the new one. A copy that has seen no newer release is refused, as is
// a copy on Windows, where a running program cannot be renamed over.
func (f *Follower) Upgrade(ctx context.Context) (release string, err error) {
	if runtime.GOOS == "windows" {
		return "", errors.New("a running program cannot be replaced on windows: install the new release by hand")
	}
	release, ok := f.Newer()
	if !ok {
		return "", errors.Newf("no release newer than this copy's (%q) has been seen on the outlet's answers", f.ownRelease())
	}
	executable, err := f.executable()
	if err != nil {
		return "", err
	}

	fetchURL := strings.TrimSuffix(f.Outlet, "/") + "/" + routeSegment + "/" + url.PathEscape(f.Name) + "/" + release + "/" + runtime.GOOS + "/" + runtime.GOARCH
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fetchURL, http.NoBody)
	if err != nil {
		return "", errors.Wrap(err, "http.NewRequestWithContext()")
	}
	client := f.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", errors.Wrap(err, "fetching the release")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))

		return "", errors.Newf("fetching %s answered %d: %s", fetchURL, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	want := resp.Header.Get(ChecksumHeader)
	if want == "" {
		return "", errors.Newf("fetching %s answered without %s: the answer is not a shipped program", fetchURL, ChecksumHeader)
	}

	if err := writeBeside(executable, resp.Body, want); err != nil {
		return "", err
	}

	return release, nil
}

// executable is the path of the running executable with symbolic links resolved, or the
// override.
func (f *Follower) executable() (string, error) {
	if f.Executable != "" {
		return f.Executable, nil
	}
	executable, err := os.Executable()
	if err != nil {
		return "", errors.Wrap(err, "os.Executable()")
	}
	resolved, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return "", errors.Wrap(err, "filepath.EvalSymlinks()")
	}

	return resolved, nil
}

// writeBeside writes body to a new file in the executable's directory, verifies it
// against the checksum, keeps the running file as <executable>.old and renames the new
// file over the running path. The new file is removed on every failure.
func writeBeside(executable string, body io.Reader, checksum string) (err error) {
	dir, base := filepath.Dir(executable), filepath.Base(executable)
	file, err := os.CreateTemp(dir, base+".new-*")
	if err != nil {
		return errors.Wrap(err, "os.CreateTemp()")
	}
	defer func() {
		if err != nil {
			_ = file.Close()
			_ = os.Remove(file.Name())
		}
	}()

	digest := sha256.New()
	if _, err := io.Copy(io.MultiWriter(file, digest), body); err != nil {
		return errors.Wrap(err, "writing the new file")
	}
	if got := hex.EncodeToString(digest.Sum(nil)); got != checksum {
		return errors.Newf("the fetched file's SHA-256 %s is not the %s the answer carried, %s: the file is removed", got, ChecksumHeader, checksum)
	}
	if err := file.Chmod(0o755); err != nil {
		return errors.Wrap(err, "Chmod()")
	}
	if err := file.Sync(); err != nil {
		return errors.Wrap(err, "Sync()")
	}
	if err := file.Close(); err != nil {
		return errors.Wrap(err, "Close()")
	}

	if err := os.Rename(executable, filepath.Join(dir, base+".old")); err != nil {
		return errors.Wrap(err, "keeping the running file as .old")
	}
	if err := os.Rename(file.Name(), executable); err != nil {
		return errors.Wrap(err, "renaming the new file over the running path")
	}

	return nil
}
