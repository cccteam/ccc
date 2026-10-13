package org

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestStepsMatchPipeline holds the images and the install environment the triggers'
// steps use together with the application pipeline's (internal/render/view.go), read
// off its source: bedrock names one Go image, one gcloud image and one OpenTofu image.
func TestStepsMatchPipeline(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile(filepath.Join("..", "render", "view.go"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	constant := func(name string) string {
		m := regexp.MustCompile(`(?m)^\s*` + name + `\s*=\s*"([^"]+)"`).FindSubmatch(source)
		if m == nil {
			t.Fatalf("render/view.go declares no %s", name)
		}

		return string(m[1])
	}
	tests := []struct {
		name string
		got  string
		want string
	}{
		{name: "the gcloud image", got: gcloudImage, want: constant("gcloudImage")},
		{name: "the Go image", got: goImage, want: constant("goImage")},
		{name: "the OpenTofu image", got: openTofuImage, want: constant("openTofuImage")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if tt.got != tt.want {
				t.Errorf("%s is %q here and %q in the pipeline", tt.name, tt.got, tt.want)
			}
		})
	}
	for _, e := range goInstallEnv {
		if !strings.Contains(string(source), `"`+e+`"`) {
			t.Errorf("the pipeline's go install environment lacks %q", e)
		}
	}
}

// TestFetchBedrockScript runs the FetchBedrock step's script as the trigger does, with
// python3 over a checkout holding placement.json: a release pin is downloaded from the
// host given, verified against bedrockSha256 and run; a commit pin on a branch of the
// repository given leaves the note the build step reads, with its branch named, and
// nothing else; a commit only a tag reaches, one the repository does not hold, a
// repository that cannot be read, no pin, a pin without its checksum and a download that
// does not match are refused, each saying what records the pin or what was not read, and
// a refusal leaves no note and no binary.
func TestFetchBedrockScript(t *testing.T) {
	t.Parallel()

	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("no python3 to run the step with")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git to run the step with")
	}
	repository, onBranch, onTag := testRepository(t)
	const stamp = "v0.0.0-lab.1.0.20260928222237-"
	const binary = "#!/bin/sh\necho \"bedrock v0.5.0 (a stand-in binary)\"\n"
	digest := sha256.Sum256([]byte(binary))
	sum := hex.EncodeToString(digest[:])
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cccteam/ccc/releases/download/bedrock/v0.5.0/bedrock-linux-amd64" {
			http.NotFound(w, r)

			return
		}
		_, _ = w.Write([]byte(binary))
	}))
	t.Cleanup(srv.Close)
	tests := []struct {
		name       string
		placement  string
		repository string
		wantOut    string
		wantErr    string
		wantNote   string
		wantRun    bool
	}{
		{
			name:      "a release pin: downloaded, verified and run",
			placement: `{"bedrockVersion": "v0.5.0", "bedrockSha256": "` + sum + `"}`,
			wantOut:   "bedrock v0.5.0 (a stand-in binary)\n",
			wantRun:   true,
		},
		{
			name:      "a commit pin on a branch: the note for the build step, with its branch",
			placement: `{"bedrockVersion": "` + stamp + onBranch[:12] + `"}`,
			wantOut:   "bedrock " + stamp + onBranch[:12] + " is a commit pin on main: the next step builds it with go install\n",
			wantNote:  stamp + onBranch[:12],
		},
		{
			name:      "a commit pin only a tag reaches",
			placement: `{"bedrockVersion": "` + stamp + onTag[:12] + `"}`,
			wantErr:   "bedrock " + stamp + onTag[:12] + " pins commit " + onTag[:12] + ", which no branch of " + repository + " reaches: the triggers build only a commit on a branch of github.com/cccteam/ccc, as bedrock upgrade accepts; move the pin to one, or to a release",
		},
		{
			name:      "a commit pin the repository does not hold",
			placement: `{"bedrockVersion": "` + stamp + `0123456789ab"}`,
			wantErr:   "pins commit 0123456789ab, which no branch of " + repository + " reaches",
		},
		{
			name:       "a commit pin, and a repository that cannot be read",
			placement:  `{"bedrockVersion": "` + stamp + onBranch[:12] + `"}`,
			repository: "file://" + filepath.Join(t.TempDir(), "missing"),
			wantErr:    "could not read the branches of file://",
		},
		{
			name:      "no pin",
			placement: `{"prefix": "imp"}`,
			wantErr:   "placement.json pins no bedrock (bedrockVersion): run bedrock upgrade in this repository, which records the pin and renders, and commit what it changed",
		},
		{
			name:      "a release pin without its checksum",
			placement: `{"bedrockVersion": "v0.5.0"}`,
			wantErr:   "placement.json pins bedrock v0.5.0 without the checksum of its linux/amd64 binary (bedrockSha256): bedrock upgrade v0.5.0 records it from the release's checksums.txt",
		},
		{
			name:      "a download that does not match",
			placement: `{"bedrockVersion": "v0.5.0", "bedrockSha256": "` + strings.Repeat("0", 64) + `"}`,
			wantErr:   "/cccteam/ccc/releases/download/bedrock/v0.5.0/bedrock-linux-amd64 does not match bedrockSha256 in placement.json: the file downloaded is not the release's binary",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			checkout, home := t.TempDir(), t.TempDir()
			if err := os.WriteFile(filepath.Join(checkout, "placement.json"), []byte(tt.placement), 0o600); err != nil {
				t.Fatal(err)
			}
			repo := tt.repository
			if repo == "" {
				repo = repository
			}
			cmd := exec.CommandContext(t.Context(), python, "-I", "-c", fetchBedrockScript, home, srv.URL, repo)
			cmd.Dir = checkout
			cmd.Env = append(os.Environ(), "TMPDIR="+t.TempDir())
			out, err := cmd.CombinedOutput()
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(string(out), tt.wantErr) {
					t.Fatalf("the step failed = %v, want the refusal %q; output:\n%s", err, tt.wantErr, out)
				}
				if _, err := os.Stat(filepath.Join(home, "bedrock")); !os.IsNotExist(err) {
					t.Errorf("the step left a binary after refusing: %v", err)
				}
				if note, err := os.ReadFile(filepath.Join(home, "bedrock-commit")); err == nil {
					t.Errorf("the step left a commit note after refusing: %q", note)
				}

				return
			}
			if err != nil {
				t.Fatalf("the step failed: %v\n%s", err, out)
			}
			if string(out) != tt.wantOut {
				t.Errorf("the step printed %q, want %q", out, tt.wantOut)
			}
			note, err := os.ReadFile(filepath.Join(home, "bedrock-commit"))
			if tt.wantNote == "" && err == nil {
				t.Errorf("the step left a commit note for a release pin: %q", note)
			}
			if tt.wantNote != "" && string(note) != tt.wantNote {
				t.Errorf("the note = %q (%v), want %q", note, err, tt.wantNote)
			}
			if _, err := os.Stat(filepath.Join(home, "bedrock")); (err == nil) != tt.wantRun {
				t.Errorf("a binary at the home = %v, want %t", err, tt.wantRun)
			}
		})
	}
}

// testRepository makes a repository with one commit on its one branch, main, and one only
// a tag reaches, and returns its clone URL and the two commits.
func testRepository(t *testing.T) (repository, onBranch, onTag string) {
	t.Helper()

	dir := t.TempDir()
	git := func(args ...string) string {
		t.Helper()

		cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir, "-c", "user.name=bedrock", "-c", "user.email=bedrock@example.com", "-c", "commit.gpgsign=false"}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}

		return strings.TrimSpace(string(out))
	}
	git("init", "--quiet", "--initial-branch=main")
	git("config", "uploadpack.allowFilter", "true")
	git("commit", "--quiet", "--allow-empty", "-m", "one")
	onBranch = git("rev-parse", "HEAD")
	git("checkout", "--quiet", "-b", "side")
	git("commit", "--quiet", "--allow-empty", "-m", "two")
	onTag = git("rev-parse", "HEAD")
	git("tag", "only-a-tag")
	git("checkout", "--quiet", "main")
	git("branch", "--quiet", "-D", "side")

	return "file://" + dir, onBranch, onTag
}

// TestBuildBedrockScript runs the BuildBedrock step's script as the trigger does (the
// $$ Cloud Build reads as $ substituted) for a release pin, where it has nothing to do;
// the commit pin's go install is the script's one other line.
func TestBuildBedrockScript(t *testing.T) {
	t.Parallel()

	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh to run the step with")
	}
	script := strings.ReplaceAll(strings.ReplaceAll(buildBedrockScript, commitPinNote, filepath.Join(t.TempDir(), "bedrock-commit")), "$$", "$")
	cmd := exec.CommandContext(t.Context(), sh, "-c", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the step failed: %v\n%s", err, out)
	}
	if string(out) != "a release pin: nothing to build\n" {
		t.Errorf("the step printed %q", out)
	}
	if !strings.Contains(buildBedrockScript, `go install -ldflags="-s -w" github.com/cccteam/ccc/bedrock@"$$(cat `+commitPinNote+`)"`) {
		t.Errorf("the script does not build the commit pin with go install:\n%s", buildBedrockScript)
	}
	if strings.Contains(fetchBedrockScript, "$") {
		t.Errorf("the fetch script carries a $, which Cloud Build reads as a substitution")
	}
}
