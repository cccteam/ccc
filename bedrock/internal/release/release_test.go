package release

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const zeros = "0000000000000000000000000000000000000000000000000000000000000000"

func TestNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		got  string
		want string
	}{
		{name: "tag", got: Tag("v0.4.0"), want: "bedrock/v0.4.0"},
		{name: "asset", got: Asset("darwin", "arm64"), want: "bedrock-darwin-arm64"},
		{name: "pipeline asset", got: PipelineAsset(), want: "bedrock-linux-amd64"},
		{name: "url", got: URL("v0.4.0", "checksums.txt"), want: "https://github.com/cccteam/ccc/releases/download/bedrock/v0.4.0/checksums.txt"},
		{name: "page", got: Page("v0.4.0"), want: "https://github.com/cccteam/ccc/releases/tag/bedrock/v0.4.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if tt.got != tt.want {
				t.Errorf("got %q, want %q", tt.got, tt.want)
			}
		})
	}
}

func TestIsVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		v    string
		want bool
	}{
		{name: "a release", v: "v0.4.0", want: true},
		{name: "a pre-release", v: "v0.0.0-lab.1", want: true},
		{name: "a pseudo-version", v: "v0.0.0-20260928051039-a306688f4d7c"},
		{name: "a dirty build", v: "v0.4.0+dirty"},
		{name: "devel", v: "(devel)"},
		{name: "no v", v: "0.4.0"},
		{name: "short", v: "v0.4"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := IsVersion(tt.v); got != tt.want {
				t.Errorf("IsVersion(%q) = %v, want %v", tt.v, got, tt.want)
			}
		})
	}
}

func TestParseChecksums(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		text    string
		want    Checksums
		wantErr string
	}{
		{
			name: "sha256sum's lines, binary mode too, blank lines skipped",
			text: zeros + "  bedrock-linux-amd64\n\n" + strings.Repeat("ab", 32) + " *bedrock-darwin-arm64\n",
			want: Checksums{"bedrock-linux-amd64": zeros, "bedrock-darwin-arm64": strings.Repeat("ab", 32)},
		},
		{name: "a short sum", text: "abc  bedrock-linux-amd64\n", wantErr: "is not <sha256>  <name>"},
		{name: "no name", text: zeros + "\n", wantErr: "is not <sha256>  <name>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseChecksums(strings.NewReader(tt.text))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("error = %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for asset, sum := range tt.want {
				if got[asset] != sum {
					t.Errorf("%s = %q, want %q", asset, got[asset], sum)
				}
			}
		})
	}
}

// server stands in for GitHub: the release listing under /repos and the assets of one
// release under /cccteam/ccc/releases/download.
func server(t *testing.T, releases string, assets map[string]string) *Source {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/cccteam/ccc/releases", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") != "1" {
			_, _ = w.Write([]byte("[]"))

			return
		}
		_, _ = w.Write([]byte(releases))
	})
	for name, content := range assets {
		mux.HandleFunc("/cccteam/ccc/releases/download/bedrock/v0.4.0/"+name, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(content))
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return &Source{Base: srv.URL, API: srv.URL, HTTP: srv.Client()}
}

func TestLatest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		releases string
		want     string
		wantErr  string
	}{
		{
			name:     "the newest bedrock release, other modules' and pre-releases and drafts skipped",
			releases: `[{"tag_name":"resource/v0.11.0"},{"tag_name":"bedrock/v0.5.0-rc.1","prerelease":true},{"tag_name":"bedrock/v0.5.0","draft":true},{"tag_name":"bedrock/v0.4.0"},{"tag_name":"bedrock/v0.3.0"}]`,
			want:     "v0.4.0",
		},
		{name: "no release yet", releases: `[{"tag_name":"resource/v0.11.0"}]`, wantErr: "no bedrock release on cccteam/ccc yet"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := server(t, tt.releases, nil).Latest(context.Background())
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("error = %v", err)
			}
			if got != tt.want {
				t.Errorf("Latest() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFetch(t *testing.T) {
	t.Parallel()

	content := "#!/bin/sh\necho bedrock\n"
	digest := sha256.Sum256([]byte(content))
	sum := hex.EncodeToString(digest[:])
	tests := []struct {
		name    string
		version string
		sum     string
		wantErr string
	}{
		{name: "verified, executable", version: "v0.4.0", sum: sum},
		{name: "a checksum that does not verify leaves nothing", version: "v0.4.0", sum: zeros, wantErr: "does not verify"},
		{name: "a release that is not there", version: "v0.9.0", sum: sum, wantErr: "no bedrock-linux-amd64 of bedrock v0.9.0 at"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			src := server(t, "[]", map[string]string{"bedrock-linux-amd64": content, ChecksumsFile: sum + "  bedrock-linux-amd64\n"})
			dst := filepath.Join(t.TempDir(), "cache", "bedrock")
			err := src.Fetch(context.Background(), tt.version, "bedrock-linux-amd64", tt.sum, dst)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want %q", err, tt.wantErr)
				}
				if _, statErr := os.Stat(dst); statErr == nil {
					t.Errorf("%s exists after a fetch that did not verify", dst)
				}

				return
			}
			if err != nil {
				t.Fatalf("error = %v", err)
			}
			info, err := os.Stat(dst)
			if err != nil {
				t.Fatalf("os.Stat() error = %v", err)
			}
			if info.Mode().Perm()&0o111 == 0 {
				t.Errorf("%s is not executable: %v", dst, info.Mode())
			}
			ok, err := Verify(dst, sum)
			if err != nil || !ok {
				t.Errorf("Verify() = %v, %v; want true", ok, err)
			}
			sums, err := src.Checksums(context.Background(), "v0.4.0")
			if err != nil || sums["bedrock-linux-amd64"] != sum {
				t.Errorf("Checksums() = %v, %v", sums, err)
			}
		})
	}
}

func TestIsCommitPin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		v    string
		want bool
	}{
		{name: "a commit before any tag", v: "v0.0.0-20260928182105-6f6f7795969d", want: true},
		{name: "a commit after a pre-release tag", v: "v0.0.0-lab.1.0.20260928222237-58b211dce544", want: true},
		{name: "a commit after a release", v: "v0.1.1-0.20261001120000-0123456789ab", want: true},
		{name: "a dirty build", v: "v0.0.0-lab.1.0.20260928222237-58b211dce544+dirty"},
		{name: "a release", v: "v0.4.0"},
		{name: "a pre-release", v: "v0.0.0-lab.1"},
		{name: "devel", v: "(devel)"},
		{name: "empty", v: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := IsCommitPin(tt.v); got != tt.want {
				t.Errorf("IsCommitPin(%q) = %v, want %v", tt.v, got, tt.want)
			}
		})
	}
}

// The commits the proxy fake knows: head, at the head of feature/abac-implementation,
// whose go.mod go install can build; tagged, which carries the release tag
// bedrock/v0.0.0-lab.1; replaced, at the head of master, whose go.mod has a replace
// directive.
const (
	headCommit      = "58b211dce54409b23306e30f73f3444796ed561b"
	headVersion     = "v0.0.0-lab.1.0.20260928222237-58b211dce544"
	taggedCommit    = "32ae32fb975d683d12d8d070cb66ee77a68263f2"
	replacedCommit  = "6f6f7795969d3e5a6b2c1d0e9f8a7b6c5d4e3f2a"
	replacedVersion = "v0.0.0-20260928182105-6f6f7795969d"
)

// proxyServer stands in for the Go module proxy (the bedrock module's .info and .mod
// files) and for the GitHub API's branch heads, as Resolve reads them. The proxy answers
// 410 for a revision starting fedcba98 and 404 for any other it does not know.
func proxyServer(t *testing.T) *Source {
	t.Helper()

	infos := map[string]string{
		headCommit: headVersion, headCommit[:12]: headVersion, headVersion: headVersion,
		taggedCommit[:8]: "v0.0.0-lab.1", replacedCommit: replacedVersion, replacedCommit[:8]: replacedVersion,
	}
	mods := map[string]string{
		headVersion:     "module github.com/cccteam/ccc/bedrock\n\ngo 1.26.6\n",
		replacedVersion: "module github.com/cccteam/ccc/bedrock\n\ngo 1.26.6\n\nreplace github.com/cccteam/ccc/impulse => ../impulse\n",
	}
	branches := map[string]string{"feature/abac-implementation": headCommit, "master": replacedCommit}
	mux := http.NewServeMux()
	mux.HandleFunc("/github.com/cccteam/ccc/bedrock/@v/", func(w http.ResponseWriter, r *http.Request) {
		file := strings.TrimPrefix(r.URL.Path, "/github.com/cccteam/ccc/bedrock/@v/")
		if rev, ok := strings.CutSuffix(file, ".info"); ok && infos[rev] != "" {
			_, _ = fmt.Fprintf(w, `{"Version":%q,"Time":"2026-09-28T22:22:37Z"}`, infos[rev])

			return
		}
		if version, ok := strings.CutSuffix(file, ".mod"); ok && mods[version] != "" {
			_, _ = w.Write([]byte(mods[version]))

			return
		}
		if strings.HasPrefix(file, "fedcba98") {
			http.Error(w, "gone: github.com/cccteam/ccc/bedrock@"+file, http.StatusGone)

			return
		}
		http.Error(w, "not found: github.com/cccteam/ccc/bedrock@"+file+": invalid version: unknown revision", http.StatusNotFound)
	})
	mux.HandleFunc("/repos/cccteam/ccc/git/ref/heads/", func(w http.ResponseWriter, r *http.Request) {
		head, ok := branches[strings.TrimPrefix(r.URL.Path, "/repos/cccteam/ccc/git/ref/heads/")]
		if !ok {
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)

			return
		}
		_, _ = fmt.Fprintf(w, `{"object":{"type":"commit","sha":%q}}`, head)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return &Source{Base: srv.URL, API: srv.URL, Proxy: srv.URL, HTTP: srv.Client()}
}

func TestResolve(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		rev     string
		want    string
		wantErr string
	}{
		{name: "a full commit", rev: headCommit, want: headVersion},
		{name: "a 12-character hash", rev: headCommit[:12], want: headVersion},
		{name: "a branch with a slash, through the API", rev: "feature/abac-implementation", want: headVersion},
		{name: "a commit that carries a release tag answers the release", rev: taggedCommit[:8], want: "v0.0.0-lab.1"},
		{name: "a version given as is", rev: headVersion, want: headVersion},
		{
			name: "a commit the proxy does not know", rev: "0123456789ab",
			wantErr: "the module proxy does not know 0123456789ab: push it first, then retry; a lookup made before the push is remembered for about 30 minutes (the proxy says: not found:",
		},
		{name: "a commit the proxy calls gone", rev: "fedcba987654", wantErr: "the module proxy does not know fedcba987654: push it first"},
		{name: "an unknown branch", rev: "no-such-branch", wantErr: `"no-such-branch" is neither a commit, a version nor a branch of cccteam/ccc`},
		{name: "a go.mod with a replace directive", rev: replacedCommit[:8], wantErr: "bedrock " + replacedVersion + " cannot be installed with go install: its go.mod has a replace directive"},
		{name: "a branch whose head has a replace directive", rev: "master", wantErr: "its go.mod has a replace directive"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := proxyServer(t).Resolve(context.Background(), tt.rev)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("error = %v", err)
			}
			if got != tt.want {
				t.Errorf("Resolve(%q) = %q, want %q", tt.rev, got, tt.want)
			}
		})
	}
}
