package ledger

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-playground/errors/v5"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"

	"github.com/cccteam/ccc/impulse/internal/check"
)

// ModReader answers a module version's own go.mod, for the consistency check.
type ModReader interface {
	// GoMod reads the go.mod of the module at the version. A *NoGoModError says there is
	// none to read: the module cache lacks it and the network does not answer.
	GoMod(ctx context.Context, path, version string) ([]byte, error)
}

// NoGoModError says a module version's go.mod could not be read from anywhere: the module
// cache lacks it and the network does not answer, for the Reason given. The consistency
// check skips the module with a warning, never an error, so a build never fails for want
// of the network.
type NoGoModError struct {
	Reason string
}

func (e *NoGoModError) Error() string {
	return "the module cache lacks it and the network does not answer: " + e.Reason
}

// ModCache reads a module version's go.mod from the module cache
// ($GOMODCACHE/cache/download/<path>/@v/<version>.mod, the path and version escaped as the
// cache escapes them) and, when the cache lacks it, has go download it (go mod download
// -json), which answers from the proxy and fills the cache. Dir is the module cache, read
// from go env when "". The download runs from an empty directory, outside any module, so
// it records nothing in a go.sum.
type ModCache struct {
	Dir  string
	Exec check.Execer
}

// GoMod implements ModReader.
func (c *ModCache) GoMod(ctx context.Context, path, version string) ([]byte, error) {
	dir, err := c.dir(ctx)
	if err != nil {
		return nil, err
	}
	escapedPath, err := module.EscapePath(path)
	if err != nil {
		return nil, errors.Wrap(err, "module.EscapePath()")
	}
	escapedVersion, err := module.EscapeVersion(version)
	if err != nil {
		return nil, errors.Wrap(err, "module.EscapeVersion()")
	}
	file := filepath.Join(dir, "cache", "download", filepath.FromSlash(escapedPath), "@v", escapedVersion+".mod")
	data, err := os.ReadFile(file)
	switch {
	case err == nil:
		return data, nil
	case !errors.Is(err, fs.ErrNotExist):
		return nil, errors.Wrap(err, "os.ReadFile()")
	}

	return c.download(ctx, path, version)
}

// dir is the module cache: Dir, or what go env answers.
func (c *ModCache) dir(ctx context.Context) (string, error) {
	if c.Dir != "" {
		return c.Dir, nil
	}
	out, err := c.Exec.Run(ctx, "", nil, "go", "env", "GOMODCACHE")
	if err != nil {
		return "", &NoGoModError{Reason: "go env GOMODCACHE: " + lastLine(out)}
	}
	dir := strings.TrimSpace(string(out))
	if dir == "" {
		return "", &NoGoModError{Reason: "go env GOMODCACHE answers nothing"}
	}

	return dir, nil
}

// download has go fetch the module version into the cache and reads its go.mod from where
// go says it is. A download that fails, for want of the network or of the version, is a
// NoGoModError with go's reason.
func (c *ModCache) download(ctx context.Context, path, version string) ([]byte, error) {
	scratch, err := os.MkdirTemp("", "impulse-ledger-")
	if err != nil {
		return nil, errors.Wrap(err, "os.MkdirTemp()")
	}
	defer os.RemoveAll(scratch)
	out, err := c.Exec.Run(ctx, scratch, nil, "go", "mod", "download", "-json", path+"@"+version)
	var answer struct {
		GoMod string
		Error string
	}
	// The toolchain may print a line before the JSON (a toolchain switch), so the object
	// is read from its first brace.
	if i := bytes.IndexByte(out, '{'); i >= 0 {
		if jsonErr := json.Unmarshal(out[i:], &answer); jsonErr != nil {
			return nil, &NoGoModError{Reason: fmt.Sprintf("go mod download -json %s@%s answered %q", path, version, lastLine(out))}
		}
	}
	switch {
	case answer.Error != "":
		return nil, &NoGoModError{Reason: "go mod download: " + answer.Error}
	case err != nil:
		return nil, &NoGoModError{Reason: fmt.Sprintf("go mod download -json %s@%s: %s", path, version, lastLine(out))}
	case answer.GoMod == "":
		return nil, &NoGoModError{Reason: fmt.Sprintf("go mod download -json %s@%s named no go.mod", path, version)}
	}
	data, err := os.ReadFile(answer.GoMod)
	if err != nil {
		return nil, errors.Wrap(err, "os.ReadFile()")
	}

	return data, nil
}

// lastLine is the last non-empty line of a command's output, for a reason.
func lastLine(out []byte) string {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")

	return strings.TrimSpace(lines[len(lines)-1])
}

// Consistent holds each step's pins together: for every pin at a release, the module
// version's own go.mod must require no sibling the step pins beyond the step's pin for it.
// Go builds one list from every requirement, so a step asking for less of a sibling than a
// module it pins requires has no list that holds it: the walk's go get refuses the set,
// and, moved one pin at a time, it would move the module off the step instead. A pin at a
// pushed commit is not read (the step is pending, and the repin reads the tags). A module
// version neither the module cache nor the network answers is skipped with a line on
// warn, never an error, so the check runs where it can and fails a build nowhere it cannot.
// A sibling the step does not pin is not held: the step says nothing about it.
func Consistent(ctx context.Context, steps []Step, mods ModReader, warn io.Writer) error {
	for i := range steps {
		s := &steps[i]
		at := fmt.Sprintf("step %d", i+1)
		for _, name := range s.PinNames() {
			version := s.Pins[name]
			if !IsRelease(version) {
				continue
			}
			data, err := mods.GoMod(ctx, name, version)
			var unread *NoGoModError
			switch {
			case errors.As(err, &unread):
				fmt.Fprintf(warn, "%s: %s %s is not read (%s); what it requires is not held to the step's pins\n", at, name, version, unread.Error())

				continue
			case err != nil:
				return errors.Wrapf(err, "%s: %s %s", at, name, version)
			}
			if err := consistentWith(s, at, name, version, data); err != nil {
				return err
			}
		}
	}

	return nil
}

// consistentWith holds a step's pins to what one pinned module version requires.
func consistentWith(s *Step, at, name, version string, data []byte) error {
	mod, err := modfile.ParseLax(name+"@"+version+"/go.mod", data, nil)
	if err != nil {
		return errors.Wrapf(err, "%s: %s %s: its go.mod does not parse", at, name, version)
	}
	for _, r := range mod.Require {
		pin, ok := s.Pins[r.Mod.Path]
		if !ok || semver.Compare(r.Mod.Version, pin) <= 0 {
			continue
		}

		return errors.Newf("%s pins %s at %s, and %s %s requires it at %s: no build list holds the step's pins together, so the walk's go get would refuse the step; pin %s at %s or later, or %s at a version that requires no more than the step pins", at, r.Mod.Path, pin, name, version, r.Mod.Version, Short(r.Mod.Path), r.Mod.Version, Short(name))
	}

	return nil
}
