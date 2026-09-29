package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/cccteam/ccc/bedrock/internal/derive"
	"github.com/cccteam/ccc/bedrock/internal/release"
)

// standIn is the script every fake bedrock binary is: it echoes its arguments.
const standIn = "#!/bin/sh\necho \"bedrock stand-in $@\"\n"

// The commits the fake proxy knows: head, at the head of feature/abac-implementation,
// which go install can build; tagged, which the release tag bedrock/v0.4.0 names;
// replaced, whose go.mod has a replace directive.
const (
	headCommit     = "58b211dce54409b23306e30f73f3444796ed561b"
	headVersion    = "v0.0.0-lab.1.0.20260928222237-58b211dce544"
	taggedCommit   = "32ae32fb975d683d12d8d070cb66ee77a68263f2"
	replacedCommit = "6f6f7795969d3e5a6b2c1d0e9f8a7b6c5d4e3f2a"
	replaced       = "v0.0.0-20260928182105-6f6f7795969d"
)

// releaseServer stands in for GitHub with one bedrock release, v0.4.0 (and a
// pre-release above it, which the latest skips), whose binaries are the stand-in script,
// and for the Go module proxy with the commits above; it answers the source and the
// binaries' checksum.
func releaseServer(t *testing.T) (src *release.Source, sum string) {
	t.Helper()

	digest := sha256.Sum256([]byte(standIn))
	sum = hex.EncodeToString(digest[:])
	assets := map[string]string{release.PipelineAsset(): standIn, release.Asset(runtime.GOOS, runtime.GOARCH): standIn}
	var checksums strings.Builder
	for name := range assets {
		fmt.Fprintf(&checksums, "%s  %s\n", sum, name)
	}
	assets[release.ChecksumsFile] = checksums.String()
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/cccteam/ccc/releases", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") != "1" {
			_, _ = w.Write([]byte("[]"))

			return
		}
		_, _ = w.Write([]byte(`[{"tag_name":"bedrock/v0.5.0-rc.1","prerelease":true},{"tag_name":"bedrock/v0.4.0"},{"tag_name":"resource/v0.11.0"}]`))
	})
	for name, content := range assets {
		mux.HandleFunc("/cccteam/ccc/releases/download/bedrock/v0.4.0/"+name, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(content))
		})
	}
	proxyRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return &release.Source{Base: srv.URL, API: srv.URL, Proxy: srv.URL, HTTP: srv.Client()}, sum
}

// proxyRoutes add the Go module proxy's answers for the bedrock module (.info and .mod)
// and the GitHub API's branch heads to the fake: 404 for anything they do not know.
func proxyRoutes(mux *http.ServeMux) {
	infos := map[string]string{
		headCommit: headVersion, headCommit[:12]: headVersion, taggedCommit[:8]: "v0.4.0", replacedCommit[:8]: replaced,
	}
	mods := map[string]string{
		headVersion: "module github.com/cccteam/ccc/bedrock\n\ngo 1.26.6\n",
		replaced:    "module github.com/cccteam/ccc/bedrock\n\ngo 1.26.6\n\nreplace github.com/cccteam/ccc/impulse => ../impulse\n",
	}
	mux.HandleFunc("/github.com/cccteam/ccc/bedrock/@v/", func(w http.ResponseWriter, r *http.Request) {
		file := strings.TrimPrefix(r.URL.Path, "/github.com/cccteam/ccc/bedrock/@v/")
		if rev, ok := strings.CutSuffix(file, ".info"); ok && infos[rev] != "" {
			_, _ = fmt.Fprintf(w, `{"Version":%q}`, infos[rev])

			return
		}
		if version, ok := strings.CutSuffix(file, ".mod"); ok && mods[version] != "" {
			_, _ = w.Write([]byte(mods[version]))

			return
		}
		http.Error(w, "not found: unknown revision", http.StatusNotFound)
	})
	mux.HandleFunc("/repos/cccteam/ccc/git/ref/heads/feature/abac-implementation", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, `{"object":{"sha":%q}}`, headCommit)
	})
}

// installs is a fake go install: it puts the stand-in script where go install would put
// bedrock, fetching it as the release's pipeline binary from the fake (whose checksum is
// sum), and records each version it installed.
type installs struct {
	src      *release.Source
	sum      string
	versions []string
}

func (i *installs) install(ctx context.Context, version, dir string) error {
	i.versions = append(i.versions, version)

	return i.src.Fetch(ctx, "v0.4.0", release.PipelineAsset(), i.sum, filepath.Join(dir, release.Name))
}

// pinnedStack is a stack directory holding the fixture placement, pinned as given.
func pinnedStack(t *testing.T, version, sum string) string {
	t.Helper()

	p, err := derive.ReadPlacement(placement)
	if err != nil {
		t.Fatalf("ReadPlacement() error = %v", err)
	}
	p.BedrockVersion, p.BedrockSHA256 = version, sum
	dir := filepath.Join(t.TempDir(), "stack")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := derive.WritePlacement(filepath.Join(dir, placementFile), p); err != nil {
		t.Fatalf("WritePlacement() error = %v", err)
	}

	return dir
}

// TestUpgrade runs alone, its cases one after another: each writes a binary and runs it,
// and a fork by a parallel test between the two makes the run fail with "text file busy".
func TestUpgrade(t *testing.T) {
	const zeros = "0000000000000000000000000000000000000000000000000000000000000000"
	tests := []struct {
		name string
		// pinned is the placement's pin before; args follow "upgrade".
		pinnedVersion, pinnedSum string
		args                     []string
		wantVersion              string
		// wantInstalled is the version go install built, empty for none.
		wantInstalled string
		wantOut       []string
		wantErr       string
	}{
		{
			name: "the latest release, by the listing", pinnedVersion: "v0.1.0", pinnedSum: zeros,
			wantVersion: "v0.4.0", wantOut: []string{"from bedrock v0.1.0 to bedrock v0.4.0", "bedrock stand-in render --app", "Commit "},
		},
		{
			name: "a release by name", pinnedVersion: "v0.1.0", pinnedSum: zeros, args: []string{"v0.4.0"},
			wantVersion: "v0.4.0", wantOut: []string{"to bedrock v0.4.0"},
		},
		{
			name: "an unpinned placement takes its first pin", args: []string{"v0.4.0"},
			wantVersion: "v0.4.0", wantOut: []string{"from no bedrock to bedrock v0.4.0"},
		},
		{name: "already there", pinnedVersion: "v0.4.0", pinnedSum: "", args: []string{"v0.4.0"}, wantVersion: "v0.4.0", wantOut: []string{"already pins bedrock v0.4.0"}},
		{name: "a release that is not there", pinnedVersion: "v0.1.0", pinnedSum: zeros, args: []string{"v0.9.0"}, wantVersion: "v0.1.0", wantErr: "no checksums of bedrock v0.9.0 at"},
		{name: "not a version", pinnedVersion: "v0.1.0", pinnedSum: zeros, args: []string{"0.4.0"}, wantVersion: "v0.1.0", wantErr: `"0.4.0" is not a version: did you mean v0.4.0?`},
		{
			name: "a release to a commit clears the checksum", pinnedVersion: "v0.1.0", pinnedSum: zeros, args: []string{headCommit[:12]},
			wantVersion: headVersion, wantInstalled: headVersion,
			wantOut: []string{"from bedrock v0.1.0 to bedrock " + headVersion + ", a commit pin: the pipeline builds it with go install", "bedrock stand-in render --app", "build bedrock " + headVersion + " with go install"},
		},
		{
			name: "a commit to a release takes the checksum", pinnedVersion: headVersion, args: []string{"v0.4.0"},
			wantVersion: "v0.4.0", wantOut: []string{"from bedrock " + headVersion + " to bedrock v0.4.0 (bedrock-linux-amd64 "},
		},
		{
			name: "a branch with a slash, through the API", pinnedVersion: "v0.1.0", pinnedSum: zeros, args: []string{"feature/abac-implementation"},
			wantVersion: headVersion, wantInstalled: headVersion, wantOut: []string{"to bedrock " + headVersion + ", a commit pin"},
		},
		{
			name: "already at the commit installs nothing", pinnedVersion: headVersion, args: []string{headCommit},
			wantVersion: headVersion, wantOut: []string{"already pins bedrock " + headVersion},
		},
		{
			name: "an unknown commit leaves the placement", pinnedVersion: "v0.1.0", pinnedSum: zeros, args: []string{"0123456789ab"},
			wantVersion: "v0.1.0", wantErr: "the module proxy does not know 0123456789ab: push it first",
		},
		{
			name: "a commit a release tag names moves to the release", pinnedVersion: "v0.1.0", pinnedSum: zeros, args: []string{taggedCommit[:8]},
			wantVersion: "v0.4.0", wantOut: []string{"to bedrock v0.4.0 (bedrock-linux-amd64 "},
		},
		{
			name: "a commit go install cannot build leaves the placement", pinnedVersion: "v0.1.0", pinnedSum: zeros, args: []string{replacedCommit[:8]},
			wantVersion: "v0.1.0", wantErr: "bedrock " + replaced + " cannot be installed with go install: its go.mod has a replace directive",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src, sum := releaseServer(t)
			pinnedSum := tt.pinnedSum
			if tt.pinnedVersion == "v0.4.0" {
				pinnedSum = sum
			}
			stack := pinnedStack(t, tt.pinnedVersion, pinnedSum)
			app := copyRepo(t, fixtureApp)
			fake := &installs{src: src, sum: sum}
			d := deps{
				interactive: never,
				releases: func() *release.Source {
					return src
				},
				cacheDir: t.TempDir(),
				install:  fake.install,
			}
			args := append([]string{upgradeCommand}, tt.args...)
			args = append(args, "--app", app, "--dir", stack)
			out, err := execute(d, "", args...)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want %q; output:\n%s", err, tt.wantErr, out)
				}
			} else if err != nil {
				t.Fatalf("error = %v; output:\n%s", err, out)
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out, want) {
					t.Errorf("output lacks %q:\n%s", want, out)
				}
			}
			p, err := derive.ReadPlacement(filepath.Join(stack, placementFile))
			if err != nil {
				t.Fatalf("ReadPlacement() after: %v", err)
			}
			if p.BedrockVersion != tt.wantVersion {
				t.Errorf("bedrockVersion = %q, want %q", p.BedrockVersion, tt.wantVersion)
			}
			if tt.wantVersion == "v0.4.0" && p.BedrockSHA256 != sum {
				t.Errorf("bedrockSha256 = %q, want the release's %q", p.BedrockSHA256, sum)
			}
			if release.IsCommitPin(tt.wantVersion) && p.BedrockSHA256 != "" {
				t.Errorf("bedrockSha256 = %q beside a commit pin, want none", p.BedrockSHA256)
			}
			if got := strings.Join(fake.versions, " "); got != tt.wantInstalled {
				t.Errorf("installed %q, want %q", got, tt.wantInstalled)
			}
		})
	}
}

func TestPinGuard(t *testing.T) {
	t.Parallel()

	const zeros = "0000000000000000000000000000000000000000000000000000000000000000"
	const (
		commit = "v0.0.0-lab.1.0.20260928222237-58b211dce544"
		other  = "v0.0.0-lab.1.0.20260929101500-e55f4db4a1b2"
	)
	tests := []struct {
		name          string
		running       build
		pinnedVersion string
		wantErr       string
	}{
		{name: "a checkout build renders any pin", running: build{version: "v0.0.0-20260928051039-a306688f4d7c", kind: buildCheckout}, pinnedVersion: "v0.1.0"},
		{name: "a dirty checkout build renders any pin", running: build{version: other + "+dirty", kind: buildCheckout}, pinnedVersion: commit},
		{name: "a checkout build renders a commit pin", running: build{version: other, kind: buildCheckout}, pinnedVersion: commit},
		{name: "devel renders any pin", running: build{version: "(devel)", kind: buildDevel}, pinnedVersion: "v0.1.0"},
		{name: "the pinned release renders", running: build{version: "v0.1.0", kind: buildStamped}, pinnedVersion: "v0.1.0"},
		{
			name: "another release is refused", running: build{version: "v0.2.0", kind: buildStamped}, pinnedVersion: "v0.1.0",
			wantErr: "pins bedrock v0.1.0 and this is bedrock v0.2.0: install the pinned one (https://github.com/cccteam/ccc/releases/tag/bedrock/v0.1.0)",
		},
		{
			name: "a checkout at another release's tag is refused", running: build{version: "v0.2.0", kind: buildCheckout}, pinnedVersion: "v0.1.0",
			wantErr: "pins bedrock v0.1.0 and this is bedrock v0.2.0",
		},
		{name: "installed at the pinned commit renders", running: build{version: commit, kind: buildInstalled}, pinnedVersion: commit},
		{
			name: "installed at another commit is refused", running: build{version: other, kind: buildInstalled}, pinnedVersion: commit,
			wantErr: "pins bedrock " + commit + " and this is bedrock " + other + ": install the pinned one (go install github.com/cccteam/ccc/bedrock@" + commit + ") or move the pin (bedrock upgrade)",
		},
		{
			name: "an installed commit against a release pin is refused", running: build{version: commit, kind: buildInstalled}, pinnedVersion: "v0.1.0",
			wantErr: "pins bedrock v0.1.0 and this is bedrock " + commit + ": install the pinned one (https://github.com/cccteam/ccc/releases/tag/bedrock/v0.1.0)",
		},
		{
			name: "a release against a commit pin is refused", running: build{version: "v0.1.0", kind: buildStamped}, pinnedVersion: commit,
			wantErr: "pins bedrock " + commit + " and this is bedrock v0.1.0: install the pinned one (go install github.com/cccteam/ccc/bedrock@" + commit + ")",
		},
		{name: "no pin is refused", running: build{version: "(devel)", kind: buildDevel}, wantErr: "pins no bedrock (bedrockVersion): run bedrock upgrade"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sum := ""
			if release.IsVersion(tt.pinnedVersion) {
				sum = zeros
			}
			stack := pinnedStack(t, tt.pinnedVersion, sum)
			app := copyRepo(t, fixtureApp)
			d := deps{
				interactive: never,
				version: func() build {
					return tt.running
				},
			}
			out, err := execute(d, "", renderCommand, "--app", app, "--out", stack)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want %q; output:\n%s", err, tt.wantErr, out)
				}

				return
			}
			if err != nil || !strings.Contains(out, "Rendered the harbor stack") {
				t.Fatalf("error = %v; output:\n%s", err, out)
			}
		})
	}
}
