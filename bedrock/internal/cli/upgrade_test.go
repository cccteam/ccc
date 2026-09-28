package cli

import (
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

// releaseServer stands in for GitHub with one bedrock release, v0.4.0 (and a
// pre-release above it, which the latest skips), whose binaries are a script that
// echoes its arguments; it answers the source and the binaries' checksum.
func releaseServer(t *testing.T) (src *release.Source, sum string) {
	t.Helper()

	script := "#!/bin/sh\necho \"bedrock stand-in $@\"\n"
	digest := sha256.Sum256([]byte(script))
	sum = hex.EncodeToString(digest[:])
	assets := map[string]string{release.PipelineAsset(): script, release.Asset(runtime.GOOS, runtime.GOARCH): script}
	var checksums strings.Builder
	for name := range assets {
		fmt.Fprintf(&checksums, "%s  %s\n", sum, name)
	}
	assets[release.ChecksumsFile] = checksums.String()
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/cccteam/ccc/releases", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") != "1" {
			fmt.Fprint(w, "[]")

			return
		}
		fmt.Fprint(w, `[{"tag_name":"bedrock/v0.5.0-rc.1","prerelease":true},{"tag_name":"bedrock/v0.4.0"},{"tag_name":"resource/v0.11.0"}]`)
	})
	for name, content := range assets {
		mux.HandleFunc("/cccteam/ccc/releases/download/bedrock/v0.4.0/"+name, func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(w, content)
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return &release.Source{Base: srv.URL, API: srv.URL, HTTP: srv.Client()}, sum
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

func TestUpgrade(t *testing.T) {
	t.Parallel()

	const zeros = "0000000000000000000000000000000000000000000000000000000000000000"
	tests := []struct {
		name string
		// pinned is the placement's pin before; args follow "upgrade".
		pinnedVersion, pinnedSum string
		args                     []string
		wantVersion              string
		wantOut                  []string
		wantErr                  string
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
		{name: "not a version", pinnedVersion: "v0.1.0", pinnedSum: zeros, args: []string{"0.4.0"}, wantVersion: "v0.1.0", wantErr: `"0.4.0" is not a release version`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			src, sum := releaseServer(t)
			pinnedSum := tt.pinnedSum
			if tt.pinnedVersion == "v0.4.0" {
				pinnedSum = sum
			}
			stack := pinnedStack(t, tt.pinnedVersion, pinnedSum)
			app := copyRepo(t, fixtureApp)
			d := deps{interactive: never, releases: func() *release.Source { return src }, cacheDir: t.TempDir()}
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
		})
	}
}

func TestPinGuard(t *testing.T) {
	t.Parallel()

	const zeros = "0000000000000000000000000000000000000000000000000000000000000000"
	tests := []struct {
		name          string
		running       string
		pinnedVersion string
		wantErr       string
	}{
		{name: "a build from a checkout renders any pin", running: "v0.0.0-20260928051039-a306688f4d7c", pinnedVersion: "v0.1.0"},
		{name: "devel renders any pin", running: "(devel)", pinnedVersion: "v0.1.0"},
		{name: "the pinned release renders", running: "v0.1.0", pinnedVersion: "v0.1.0"},
		{name: "another release is refused", running: "v0.2.0", pinnedVersion: "v0.1.0", wantErr: "pins bedrock v0.1.0 and this is bedrock v0.2.0"},
		{name: "no pin is refused", running: "(devel)", wantErr: "pins no bedrock (bedrockVersion, bedrockSha256): run bedrock upgrade"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sum := ""
			if tt.pinnedVersion != "" {
				sum = zeros
			}
			stack := pinnedStack(t, tt.pinnedVersion, sum)
			app := copyRepo(t, fixtureApp)
			d := deps{interactive: never, version: func() string { return tt.running }}
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
