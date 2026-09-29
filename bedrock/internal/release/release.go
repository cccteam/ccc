// Package release is bedrock's own distribution: where a copy of the tool comes from,
// what it is made of, and how it is fetched and verified. An application's placement pins
// bedrock one of two ways.
//
// A release is a GitHub Release of github.com/cccteam/ccc tagged bedrock/vX.Y.Z, cut by
// release-please. ccc's release workflow uploads to it one static binary per platform the
// tool runs on (bedrock-linux-amd64, which the pipelines and the infrastructure workflows
// run; bedrock-linux-arm64 and bedrock-darwin-arm64 for developer machines) and
// checksums.txt, the SHA-256 of each in the format sha256sum reads. A release pin is the
// version and the linux/amd64 checksum; the rendered pipeline downloads that binary at its
// first step and the rendered infrastructure workflow at its own, and both verify it
// against the checksum before running it.
//
// A commit pin is the pseudo-version the Go module proxy gives a pushed commit of the
// bedrock module (v0.0.0-lab.1.0.20260928222237-58b211dce544), with no checksum: the
// pipeline and the infrastructure workflow build it with go install, and Go's checksum
// database verifies the module in place of a checksum in the placement. Resolve turns a
// commit or a branch into that version. URL and Page name a release's assets and page and
// are for release pins only. bedrock upgrade moves either pin through this package.
package release

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/go-playground/errors/v5"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
)

const (
	// Repository is the GitHub repository the releases are cut in.
	Repository = "cccteam/ccc"
	// Name is the tool's name: the release-please component, so the tags' prefix, and
	// the binaries' first word.
	Name = "bedrock"
	// Module is the tool's module path: what go install builds for a commit pin.
	Module = "github.com/cccteam/ccc/bedrock"
	// ChecksumsFile is the asset listing the SHA-256 of every binary of a release, one
	// line each, as sha256sum writes and reads them: <hex>  <name>.
	ChecksumsFile = "checksums.txt"
	// PipelineOS is the operating system the pipeline and the infrastructure workflow
	// run the tool on, and PipelineArch its architecture: the binary whose checksum the
	// placement pins.
	PipelineOS = "linux"
	// PipelineArch is the architecture beside PipelineOS.
	PipelineArch = "amd64"

	githubURL    = "https://github.com"
	githubAPIURL = "https://api.github.com"
	proxyURL     = "https://proxy.golang.org"
	tokenEnv     = "GITHUB_TOKEN"
	// proxyTextLimit bounds how much of the module proxy's refusal is read, to quote it.
	proxyTextLimit = 1 << 10
	// releasePages bounds the listing a search for the latest release reads: the
	// repository cuts releases for a dozen modules, so bedrock's latest is within the
	// first pages.
	releasePages = 10
	perPage      = 100
	// fetchTimeout bounds one download.
	fetchTimeout = 5 * time.Minute
)

// Tag is the release's tag for a version: bedrock/v0.4.0.
func Tag(version string) string {
	return Name + "/" + version
}

// Asset is the binary's name for a platform: bedrock-linux-amd64.
func Asset(goos, goarch string) string {
	return Name + "-" + goos + "-" + goarch
}

// PipelineAsset is the binary the pipeline and the infrastructure workflow run.
func PipelineAsset() string {
	return Asset(PipelineOS, PipelineArch)
}

// URL is where an asset of a version is downloaded from, on GitHub.
func URL(version, asset string) string {
	return downloadURL(githubURL, version, asset)
}

// Page is the release's page on GitHub.
func Page(version string) string {
	return githubURL + "/" + Repository + "/releases/tag/" + Tag(version)
}

func downloadURL(base, version, asset string) string {
	return base + "/" + Repository + "/releases/download/" + Tag(version) + "/" + asset
}

// IsVersion reports whether v names a release: a semantic version with its v, as the
// tags carry it (v0.4.0, or a pre-release such as v0.5.0-rc.1), and not the
// pseudo-version Go stamps on a build from an untagged commit, nor a dirty build.
func IsVersion(v string) bool {
	return semver.IsValid(v) && semver.Canonical(v) == v && !module.IsPseudoVersion(v)
}

// IsCommitPin reports whether v names a commit of the bedrock module the way the Go module
// proxy does: a pseudo-version (v0.0.0-20260928182105-6f6f7795969d, or one built on a tag
// before the commit, v0.0.0-lab.1.0.20260928222237-58b211dce544), in canonical form, so
// not a dirty build (+dirty) nor anything else with a build suffix, which no proxy serves.
func IsCommitPin(v string) bool {
	return semver.IsValid(v) && semver.Canonical(v) == v && module.IsPseudoVersion(v)
}

// hashRE reads a commit hash, abbreviated as git abbreviates it or in full.
var hashRE = regexp.MustCompile(`^[0-9a-f]{7,40}$`)

// Source is where releases and commits are read from: GitHub and the Go module proxy, or
// a test server standing in for them.
type Source struct {
	// Base stands in for https://github.com (the downloads), API for
	// https://api.github.com (the release listing and the branches) and Proxy for
	// https://proxy.golang.org (the commits); empty means the real one.
	Base  string
	API   string
	Proxy string
	// Token authenticates the listing when set, which raises its rate limit; the
	// downloads need none.
	Token string
	// HTTP is the client; nil means one with a timeout fit for a binary.
	HTTP *http.Client
}

// GitHub is the source the tool reads: GitHub, with GITHUB_TOKEN when the environment
// holds one.
func GitHub() *Source {
	return &Source{Token: os.Getenv(tokenEnv)}
}

// Checksums are the SHA-256 of a release's binaries by asset name.
type Checksums map[string]string

// Latest is the newest release that is neither a draft nor a pre-release, as GitHub
// lists them: what bedrock upgrade moves to when no version is named. A pre-release
// is moved to by name only.
func (s *Source) Latest(ctx context.Context) (string, error) {
	for page := 1; page <= releasePages; page++ {
		var releases []struct {
			Tag        string `json:"tag_name"`
			Draft      bool   `json:"draft"`
			Prerelease bool   `json:"prerelease"`
		}
		listing := fmt.Sprintf("%s/repos/%s/releases?per_page=%d&page=%d", s.api(), Repository, perPage, page)
		if err := s.getJSON(ctx, listing, &releases); err != nil {
			return "", err
		}
		for _, r := range releases {
			version, ok := strings.CutPrefix(r.Tag, Name+"/")
			if ok && !r.Draft && !r.Prerelease && IsVersion(version) {
				return version, nil
			}
		}
		if len(releases) < perPage {
			break
		}
	}

	return "", errors.Newf("no bedrock release on %s yet: name the version to move to", Repository)
}

// Checksums reads a release's checksums file.
func (s *Source) Checksums(ctx context.Context, version string) (Checksums, error) {
	body, err := s.get(ctx, downloadURL(s.base(), version, ChecksumsFile), "checksums of bedrock "+version)
	if err != nil {
		return nil, err
	}
	defer body.Close()

	return ParseChecksums(body)
}

// Resolve is the version the Go module proxy gives a revision of the bedrock module. A
// commit hash, in full or abbreviated, or a version is asked of the proxy as it is;
// anything else is a branch of github.com/cccteam/ccc, whose head commit is read from the
// GitHub API first (the proxy takes no name with a slash, and most branches have one). A
// commit that carries a release tag answers with that release, which the caller treats as
// a release pin. Any other answer is a commit pin, and its go.mod must let go install
// build it: no replace or exclude directive. A revision the proxy does not know is refused
// with the cause it almost always has: the commit is not pushed yet.
func (s *Source) Resolve(ctx context.Context, rev string) (string, error) {
	commit := rev
	if !hashRE.MatchString(rev) && !semver.IsValid(rev) {
		head, err := s.branchHead(ctx, rev)
		if err != nil {
			return "", err
		}
		commit = head
	}
	version, err := s.proxyVersion(ctx, commit)
	if err != nil {
		return "", err
	}
	if IsVersion(version) {
		return version, nil
	}
	if !IsCommitPin(version) {
		return "", errors.Newf("the module proxy answers %q for %s, which is neither a release nor a commit's pseudo-version", version, rev)
	}
	if err := s.installable(ctx, version); err != nil {
		return "", err
	}

	return version, nil
}

// branchHead is the commit at the head of a branch of the repository.
func (s *Source) branchHead(ctx context.Context, branch string) (string, error) {
	segments := strings.Split(branch, "/")
	for i, segment := range segments {
		segments[i] = url.PathEscape(segment)
	}
	var ref struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	err := s.getJSON(ctx, fmt.Sprintf("%s/repos/%s/git/ref/heads/%s", s.api(), Repository, strings.Join(segments, "/")), &ref)
	var status *statusError
	if errors.As(err, &status) && status.code == http.StatusNotFound {
		return "", errors.Newf("%q is neither a commit, a version nor a branch of %s", branch, Repository)
	}
	if err != nil {
		return "", err
	}
	if !hashRE.MatchString(ref.Object.SHA) {
		return "", errors.Newf("branch %s of %s: the GitHub API names no commit at its head", branch, Repository)
	}

	return ref.Object.SHA, nil
}

// proxyVersion asks the module proxy which version it gives a commit or a version of the
// module.
func (s *Source) proxyVersion(ctx context.Context, rev string) (string, error) {
	escaped, err := module.EscapeVersion(rev)
	if err != nil {
		return "", errors.Newf("%q is not a revision the module proxy can be asked about: %v", rev, err)
	}
	body, err := s.proxyGet(ctx, rev, escaped+".info")
	if err != nil {
		return "", err
	}
	var info struct {
		Version string `json:"Version"`
	}
	if err := json.Unmarshal(body, &info); err != nil {
		return "", errors.Wrap(err, "json.Unmarshal()")
	}

	return info.Version, nil
}

// installable refuses a version whose go.mod go install would refuse: go install builds a
// module at a version only when its go.mod has no replace and no exclude directive. The
// file is parsed as a main module's (Parse, not ParseLax, which skips both directives).
func (s *Source) installable(ctx context.Context, version string) error {
	body, err := s.proxyGet(ctx, version, version+".mod")
	if err != nil {
		return err
	}
	f, err := modfile.Parse("go.mod", body, nil)
	if err != nil {
		return errors.Wrapf(err, "modfile.Parse(): the go.mod of bedrock %s", version)
	}
	if len(f.Replace) > 0 {
		return errors.Newf("bedrock %s cannot be installed with go install: its go.mod has a replace directive", version)
	}
	if len(f.Exclude) > 0 {
		return errors.Newf("bedrock %s cannot be installed with go install: its go.mod has an exclude directive", version)
	}

	return nil
}

// proxyGet reads one file of the module from the proxy (<version>.info, <version>.mod);
// rev is what the caller named, for the refusal. The proxy answers 404 or 410 for a
// revision it does not know, which for a commit is almost always one not pushed yet, and it
// remembers that answer for a while.
func (s *Source) proxyGet(ctx context.Context, rev, file string) ([]byte, error) {
	path, err := module.EscapePath(Module)
	if err != nil {
		return nil, errors.Wrap(err, "module.EscapePath()")
	}
	u := s.proxy() + "/" + path + "/@v/" + file
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, http.NoBody)
	if err != nil {
		return nil, errors.Wrap(err, "http.NewRequestWithContext()")
	}
	resp, err := s.client().Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "http.Client.Do()")
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		text, _ := io.ReadAll(io.LimitReader(resp.Body, proxyTextLimit))

		return nil, errors.Newf("the module proxy does not know %s: push it first, then retry; a lookup made before the push is remembered for about 30 minutes (the proxy says: %s)", rev, strings.TrimSpace(string(text)))
	}
	if resp.StatusCode != http.StatusOK {
		return nil, errors.Newf("GET %s: %s", u, resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, "io.ReadAll()")
	}

	return body, nil
}

// ParseChecksums reads sha256sum's format: <hex>  <name> per line, the name with a
// leading * in binary mode.
func ParseChecksums(r io.Reader) (Checksums, error) {
	sums := Checksums{}
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 {
			continue
		}
		if len(fields) != 2 || len(fields[0]) != hex.EncodedLen(sha256.Size) {
			return nil, errors.Newf("%s: line %q is not <sha256>  <name>", ChecksumsFile, scanner.Text())
		}
		sums[strings.TrimPrefix(fields[1], "*")] = fields[0]
	}
	if err := scanner.Err(); err != nil {
		return nil, errors.Wrap(err, "bufio.Scanner.Err()")
	}

	return sums, nil
}

// Fetch downloads an asset of a release to dst, verifies it against sum (the SHA-256
// in hex) and makes it executable; nothing lands at dst that did not verify.
func (s *Source) Fetch(ctx context.Context, version, asset, sum, dst string) error {
	body, err := s.get(ctx, downloadURL(s.base(), version, asset), asset+" of bedrock "+version)
	if err != nil {
		return err
	}
	defer body.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return errors.Wrap(err, "os.MkdirAll()")
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+"-*")
	if err != nil {
		return errors.Wrap(err, "os.CreateTemp()")
	}
	defer os.Remove(tmp.Name())
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, h), body); err != nil {
		_ = tmp.Close()

		return errors.Wrap(err, "io.Copy()")
	}
	if err := tmp.Close(); err != nil {
		return errors.Wrap(err, "os.File.Close()")
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != sum {
		return errors.Newf("%s of bedrock %s does not verify: its SHA-256 is %s, the checksum says %s", asset, version, got, sum)
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		return errors.Wrap(err, "os.Chmod()")
	}
	if err := os.Rename(tmp.Name(), dst); err != nil {
		return errors.Wrap(err, "os.Rename()")
	}

	return nil
}

// Verify reports whether the file at path has the SHA-256 sum; a missing file has not.
func Verify(path, sum string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}

		return false, errors.Wrap(err, "os.Open()")
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false, errors.Wrap(err, "io.Copy()")
	}

	return hex.EncodeToString(h.Sum(nil)) == sum, nil
}

func (s *Source) base() string {
	if s.Base != "" {
		return s.Base
	}

	return githubURL
}

func (s *Source) proxy() string {
	if s.Proxy != "" {
		return s.Proxy
	}

	return proxyURL
}

func (s *Source) api() string {
	if s.API != "" {
		return s.API
	}

	return githubAPIURL
}

func (s *Source) client() *http.Client {
	if s.HTTP != nil {
		return s.HTTP
	}

	return &http.Client{Timeout: fetchTimeout}
}

// get answers the body of a successful GET of what (named in a refusal); the caller
// closes it.
func (s *Source) get(ctx context.Context, address, what string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, http.NoBody)
	if err != nil {
		return nil, errors.Wrap(err, "http.NewRequestWithContext()")
	}
	resp, err := s.client().Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "http.Client.Do()")
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()

		return nil, errors.Newf("no %s at %s: %s", what, address, resp.Status)
	}

	return resp.Body, nil
}

// statusError is an API answer other than 200 OK.
type statusError struct {
	url  string
	code int
	text string
}

func (e *statusError) Error() string {
	return fmt.Sprintf("GET %s: %s", e.url, e.text)
}

// getJSON reads one API answer, with the token when there is one.
func (s *Source) getJSON(ctx context.Context, address string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, http.NoBody)
	if err != nil {
		return errors.Wrap(err, "http.NewRequestWithContext()")
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if s.Token != "" {
		req.Header.Set("Authorization", "Bearer "+s.Token)
	}
	resp, err := s.client().Do(req)
	if err != nil {
		return errors.Wrap(err, "http.Client.Do()")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return &statusError{url: address, code: resp.StatusCode, text: resp.Status}
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return errors.Wrap(err, "json.Decoder.Decode()")
	}

	return nil
}
