package ledger

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/internal/check"
)

const (
	sessionModule = "github.com/cccteam/session"
	tracerModule  = "github.com/cccteam/ccc/tracer"
)

// fakeMods answers go.mod files by "path@version"; a version it lacks is a NoGoModError,
// and one named by broken is a read error.
type fakeMods struct {
	files  map[string]string
	broken string
}

func (f fakeMods) GoMod(_ context.Context, path, version string) ([]byte, error) {
	key := path + "@" + version
	if key == f.broken {
		return nil, errors.New("disk read failed")
	}
	data, ok := f.files[key]
	if !ok {
		return nil, &NoGoModError{Reason: "not cached"}
	}

	return []byte(data), nil
}

// goMod writes a module's go.mod requiring the siblings given, "path version" each.
func goMod(path string, requires ...string) string {
	var b strings.Builder
	b.WriteString("module " + path + "\n\ngo 1.26\n\n")
	for _, r := range requires {
		b.WriteString("require " + r + "\n")
	}

	return b.String()
}

// TestConsistent holds a step's pins to what the pinned modules require: a sibling pinned
// at or beyond what a pin requires passes, one pinned below it is refused, a sibling the
// step does not pin is not held, a pin at a pushed commit is not read, a module neither
// the cache nor the network answers is skipped with a warning, and a go.mod that cannot
// be read or parsed is an error.
func TestConsistent(t *testing.T) {
	t.Parallel()

	mods := fakeMods{
		files: map[string]string{
			resourceModule + "@v0.15.0":            goMod(resourceModule, sessionModule+" v0.13.1", accessModule+" v0.11.1", "github.com/cccteam/ccc/cache v0.2.1"),
			resourceModule + "@v0.14.0":            goMod(resourceModule, sessionModule+" v0.12.2", tracerModule+" v0.3.0"),
			sessionModule + "@v0.13.1":             goMod(sessionModule, accessModule+" v0.11.1"),
			sessionModule + "@v0.13.0":             goMod(sessionModule),
			accessModule + "@v0.11.1":              goMod(accessModule),
			accessModule + "@v0.11.0":              goMod(accessModule),
			"github.com/cccteam/ccc/broken@v0.1.0": "module \"unterminated\n",
		},
		broken: tracerModule + "@v0.3.0",
	}
	tests := []struct {
		name     string
		steps    []Step
		wantErr  string
		wantWarn string
	}{
		{
			name:  "pins at or beyond what the pinned modules require",
			steps: []Step{{Pins: map[string]string{resourceModule: "v0.15.0", sessionModule: "v0.13.1", accessModule: "v0.11.1"}, Note: "n"}},
		},
		{
			name:    "a sibling pinned below what a pin requires is refused",
			steps:   []Step{{Pins: map[string]string{resourceModule: "v0.15.0", sessionModule: "v0.13.0", accessModule: "v0.11.1"}, Note: "n"}},
			wantErr: "step 1 pins github.com/cccteam/session at v0.13.0, and github.com/cccteam/ccc/resource v0.15.0 requires it at v0.13.1: no build list holds the step's pins together, so the walk's go get would refuse the step; pin session at v0.13.1 or later, or ccc/resource at a version that requires no more than the step pins",
		},
		{
			name:    "the step is named by its position",
			steps:   []Step{{Pins: map[string]string{accessModule: "v0.11.0"}, Note: "n"}, {Pins: map[string]string{sessionModule: "v0.13.1", accessModule: "v0.11.0"}, Note: "n"}},
			wantErr: "step 2 pins github.com/cccteam/access at v0.11.0, and github.com/cccteam/session v0.13.1 requires it at v0.11.1",
		},
		{
			name:  "a sibling the step does not pin is not held",
			steps: []Step{{Pins: map[string]string{resourceModule: "v0.15.0", sessionModule: "v0.13.1"}, Note: "n"}},
		},
		{
			name:  "a pin at a pushed commit is not read",
			steps: []Step{{Pins: map[string]string{resourceModule: "v0.15.1-0.20261021000000-0123456789ab", sessionModule: "v0.13.0"}, Note: "n", Pending: true}},
		},
		{
			name:     "a module neither the cache nor the network answers is skipped with a warning",
			steps:    []Step{{Pins: map[string]string{resourceModule: "v0.16.0", sessionModule: "v0.13.0"}, Note: "n"}},
			wantWarn: "step 1: github.com/cccteam/ccc/resource v0.16.0 is not read (the module cache lacks it and the network does not answer: not cached); what it requires is not held to the step's pins\n",
		},
		{
			name:    "a go.mod that cannot be read is an error",
			steps:   []Step{{Pins: map[string]string{tracerModule: "v0.3.0"}, Note: "n"}},
			wantErr: "step 1: github.com/cccteam/ccc/tracer v0.3.0",
		},
		{
			name:    "a go.mod that does not parse is an error",
			steps:   []Step{{Pins: map[string]string{"github.com/cccteam/ccc/broken": "v0.1.0"}, Note: "n"}},
			wantErr: "step 1: github.com/cccteam/ccc/broken v0.1.0: its go.mod does not parse",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var warn strings.Builder
			err := Consistent(t.Context(), tt.steps, mods, &warn)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Consistent() error = %v, want %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("Consistent() error = %v", err)
			}
			if warn.String() != tt.wantWarn {
				t.Errorf("Consistent() warned %q, want %q", warn.String(), tt.wantWarn)
			}
		})
	}
}

// TestConsistentRecorded holds the recorded ledger together, each pinned release's go.mod
// read from the module cache, or downloaded into it when the cache lacks it. A module
// neither answers for is a warning, and the test is skipped on one, saying so, rather than
// failed: the check holds where it can be run and never fails a build for want of the
// network.
func TestConsistentRecorded(t *testing.T) {
	t.Parallel()

	var warn strings.Builder
	if err := Consistent(t.Context(), Steps, &ModCache{Exec: check.OSExec{}}, &warn); err != nil {
		t.Fatalf("Consistent() error = %v", err)
	}
	if warn.Len() > 0 {
		t.Skipf("the recorded ledger was held together only in part; neither the module cache nor the network answered for:\n%s", warn.String())
	}
}

// downloadExec answers go env GOMODCACHE with the cache given and go mod download -json
// as go does: the module's go.mod written into the cache and named in the answer, or an
// Error in the answer for a version it does not know, or a failure for want of the
// network. Every command is recorded.
type downloadExec struct {
	cache   string
	known   map[string]string
	offline bool
	calls   []string
}

func (d *downloadExec) Run(_ context.Context, dir string, _ []string, name string, args ...string) ([]byte, error) {
	line := name + " " + strings.Join(args, " ")
	d.calls = append(d.calls, line)
	switch {
	case line == "go env GOMODCACHE":
		return []byte(d.cache + "\n"), nil
	case !strings.HasPrefix(line, "go mod download -json "):
		return nil, errors.Newf("unexpected command %q", line)
	case dir == "":
		return nil, errors.New("the download ran in the working directory")
	case d.offline:
		return []byte("go: module github.com/cccteam/ccc/resource: Get \"https://proxy.golang.org/...\": dial tcp: lookup proxy.golang.org: no such host\n"), errors.New("exit status 1")
	}
	path, version, _ := strings.Cut(args[len(args)-1], "@")
	data, ok := d.known[path+"@"+version]
	if !ok {
		return []byte(`{"Path":"` + path + `","Version":"` + version + `","Error":"` + path + "@" + version + `: invalid version: unknown revision ` + version + `"}` + "\n"), errors.New("exit status 1")
	}
	file := filepath.Join(d.cache, "cache", "download", filepath.FromSlash(path), "@v", version+".mod")
	if err := os.MkdirAll(filepath.Dir(file), 0o750); err != nil {
		return nil, errors.Wrap(err, "os.MkdirAll()")
	}
	if err := os.WriteFile(file, []byte(data), 0o600); err != nil {
		return nil, errors.Wrap(err, "os.WriteFile()")
	}

	return []byte("go: " + path + "@" + version + " requires go >= 1.26.9; switching to go1.26.10\n{\n\t\"Path\": \"" + path + "\",\n\t\"Version\": \"" + version + "\",\n\t\"GoMod\": \"" + file + "\"\n}\n"), nil
}

// TestModCache reads a module version's go.mod from the cache when it is there, downloads
// it into the cache when it is not (reading the answer past a toolchain's line before the
// JSON), answers a NoGoModError for a version the proxy does not know and for a download
// without the network, and escapes a path with capitals as the cache does.
func TestModCache(t *testing.T) {
	t.Parallel()

	cached := goMod(sessionModule, accessModule+" v0.11.1")
	downloadable := goMod(resourceModule, sessionModule+" v0.13.1")
	tests := []struct {
		name      string
		dir       bool
		offline   bool
		path      string
		version   string
		want      string
		wantErr   bool
		wantCalls []string
	}{
		{name: "in the cache", dir: true, path: sessionModule, version: "v0.13.1", want: cached},
		{name: "in the cache, under the escaped path", dir: true, path: "github.com/cccteam/Mixed", version: "v0.1.0", want: "module github.com/cccteam/Mixed\n"},
		{name: "downloaded into the cache", dir: true, path: resourceModule, version: "v0.15.0", want: downloadable, wantCalls: []string{"go mod download -json " + resourceModule + "@v0.15.0"}},
		{name: "the cache read from go env", path: sessionModule, version: "v0.13.1", want: cached, wantCalls: []string{"go env GOMODCACHE"}},
		{name: "a version the proxy does not know", dir: true, path: resourceModule, version: "v0.99.0", wantErr: true, wantCalls: []string{"go mod download -json " + resourceModule + "@v0.99.0"}},
		{name: "without the network", dir: true, offline: true, path: resourceModule, version: "v0.15.0", wantErr: true, wantCalls: []string{"go mod download -json " + resourceModule + "@v0.15.0"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cache := t.TempDir()
			for rel, content := range map[string]string{
				"cache/download/github.com/cccteam/session/@v/v0.13.1.mod":      cached,
				"cache/download/github.com/cccteam/!mixed/@v/v0.1.0.mod":        "module github.com/cccteam/Mixed\n",
				"cache/download/github.com/cccteam/ccc/resource/@v/v0.14.0.mod": goMod(resourceModule),
			} {
				if err := os.MkdirAll(filepath.Dir(filepath.Join(cache, rel)), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(cache, rel), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			exec := &downloadExec{cache: cache, known: map[string]string{resourceModule + "@v0.15.0": downloadable}, offline: tt.offline}
			c := &ModCache{Exec: exec}
			if tt.dir {
				c.Dir = cache
			}
			got, err := c.GoMod(t.Context(), tt.path, tt.version)
			var unread *NoGoModError
			if tt.wantErr {
				if !errors.As(err, &unread) {
					t.Fatalf("GoMod() error = %v, want a NoGoModError", err)
				}
			} else if err != nil {
				t.Fatalf("GoMod() error = %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("GoMod() = %q, want %q", got, tt.want)
			}
			if strings.Join(exec.calls, "\n") != strings.Join(tt.wantCalls, "\n") {
				t.Errorf("commands = %q, want %q", exec.calls, tt.wantCalls)
			}
		})
	}
}
