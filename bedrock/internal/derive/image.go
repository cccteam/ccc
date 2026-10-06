// image.go reads what the seeded Dockerfile copies into its build stages by name: the
// directories holding the application's Go packages, and the files each browser
// workspace's package install reads.

package derive

import (
	"os"
	"path"
	"slices"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/app"
)

// ImageInputs is what the seeded Dockerfile copies by name, read from the tree at render
// time. The Go stage copies go.mod and go.sum, the root package's Go files and each
// top-level directory holding Go packages, whole: a copy layer is keyed on the files it
// copies, so the pipeline's per-build files, the git directory and the provider
// downloads are inputs of no layer. A package directory added later needs a line of its
// own in the application's Dockerfile; a missing one fails the compile within seconds,
// naming the package. Each browser workspace's install stage copies the files bun
// install reads and nothing else, so the stage's layers hold the installed packages
// alone.
type ImageInputs struct {
	// RootGo reports Go files in the root package (main.go), test files aside.
	RootGo bool
	// GoDirs are the top-level directories holding Go packages, test files aside, in
	// name order. A directory go build never reads as a package (testdata, a name
	// starting with . or _) and a directory that is a module of its own are left out.
	GoDirs []string
	// Workspaces are the browser workspaces, every directory holding an angular.json, in
	// the order found, each with its install files.
	Workspaces []WorkspaceInputs
}

// WorkspaceInputs is one browser workspace's install: its root-relative directory and
// the root-relative files bun install reads there.
type WorkspaceInputs struct {
	Dir string
	// Files are the manifest and the lockfile (package.json, bun.lock) always, and the
	// install's configuration files (bunfig.toml, .npmrc) when the workspace has them.
	Files []string
}

// The files a workspace's package install reads: the two every install has, and the two
// configuration files an install reads when they are there (bunfig.toml sets how peers
// install and which registry serves a scope, so an install without it resolves
// differently and a frozen lockfile refuses it).
var (
	installFiles         = []string{"package.json", "bun.lock"}
	optionalInstallFiles = []string{"bunfig.toml", ".npmrc"}
)

// WorkspaceInputs returns the workspace's install files, or the two every install has
// for a workspace the tree does not hold (the render of a fixture without one).
func (in *ImageInputs) WorkspaceInputs(dir string) []string {
	for i := range in.Workspaces {
		if in.Workspaces[i].Dir == dir {
			return in.Workspaces[i].Files
		}
	}
	files := make([]string, 0, len(installFiles))
	for _, name := range installFiles {
		files = append(files, path.Join(dir, name))
	}

	return files
}

// imageInputs reads the Go directories off the non-test Go files and each workspace's
// install files off the tree.
func (m *Model) imageInputs(a *app.App) error {
	dirs := map[string]bool{}
	for _, file := range a.GoFiles() {
		top, _, nested := strings.Cut(file, "/")
		if !nested {
			m.ImageInputs.RootGo = true

			continue
		}
		if dirs[top] || ignoredByGo(top) {
			continue
		}
		own, err := exists(a.Abs(path.Join(top, "go.mod")))
		if err != nil {
			return err
		}
		dirs[top] = !own
	}
	for dir, in := range dirs {
		if in {
			m.ImageInputs.GoDirs = append(m.ImageInputs.GoDirs, dir)
		}
	}
	slices.Sort(m.ImageInputs.GoDirs)
	for _, w := range a.WebApps {
		files := make([]string, 0, len(installFiles)+len(optionalInstallFiles))
		for _, name := range installFiles {
			files = append(files, path.Join(w.Dir, name))
		}
		for _, name := range optionalInstallFiles {
			has, err := exists(a.Abs(path.Join(w.Dir, name)))
			if err != nil {
				return err
			}
			if has {
				files = append(files, path.Join(w.Dir, name))
			}
		}
		m.ImageInputs.Workspaces = append(m.ImageInputs.Workspaces, WorkspaceInputs{Dir: w.Dir, Files: files})
	}

	return nil
}

// ignoredByGo reports a top-level directory go build never reads as a package
// directory: testdata, and a name starting with . or _.
func ignoredByGo(dir string) bool {
	return dir == "testdata" || strings.HasPrefix(dir, ".") || strings.HasPrefix(dir, "_")
}

// exists reports whether the file is there.
func exists(abs string) (bool, error) {
	_, err := os.Stat(abs)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, os.ErrNotExist):
		return false, nil
	default:
		return false, errors.Wrap(err, "os.Stat()")
	}
}
