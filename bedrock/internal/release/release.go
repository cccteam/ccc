// Package release is bedrock's own distribution: where a release of the tool is
// published, what it is made of, and how a copy is fetched and verified.
//
// A release is a GitHub Release of github.com/cccteam/ccc tagged bedrock/vX.Y.Z, cut by
// release-please. ccc's release workflow uploads to it one static binary per platform the
// tool runs on (bedrock-linux-amd64, which the pipelines and the infrastructure workflows
// run; bedrock-linux-arm64 and bedrock-darwin-arm64 for developer machines) and
// checksums.txt, the SHA-256 of each in the format sha256sum reads. An application's
// placement pins a version and the linux/amd64 checksum; the rendered pipeline downloads
// that binary at its first step and the rendered infrastructure workflow at its own, and
// both verify it against the checksum before running it. bedrock upgrade moves the pin
// through this package.
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
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-playground/errors/v5"
	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
)

const (
	// Repository is the GitHub repository the releases are cut in.
	Repository = "cccteam/ccc"
	// Name is the tool's name: the release-please component, so the tags' prefix, and
	// the binaries' first word.
	Name = "bedrock"
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
	tokenEnv     = "GITHUB_TOKEN"
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

// Source is where releases are read from: GitHub, or a test server standing in for it.
type Source struct {
	// Base stands in for https://github.com (the downloads) and API for
	// https://api.github.com (the release listing); empty means GitHub.
	Base string
	API  string
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
		url := fmt.Sprintf("%s/repos/%s/releases?per_page=%d&page=%d", s.api(), Repository, perPage, page)
		if err := s.getJSON(ctx, url, &releases); err != nil {
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
func (s *Source) get(ctx context.Context, url, what string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, errors.Wrap(err, "http.NewRequestWithContext()")
	}
	resp, err := s.client().Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "http.Client.Do()")
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()

		return nil, errors.Newf("no %s at %s: %s", what, url, resp.Status)
	}

	return resp.Body, nil
}

// getJSON reads one API answer, with the token when there is one.
func (s *Source) getJSON(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
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
		return errors.Newf("GET %s: %s", url, resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return errors.Wrap(err, "json.Decoder.Decode()")
	}

	return nil
}
