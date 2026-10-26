package check

import (
	"bytes"
	"context"
	"fmt"
	"go/format"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-playground/errors/v5"
)

// gofmtCheck verifies that every Go file the application's author owns is gofmt-formatted,
// test files included, since the rendered CI's lint gate refuses an unformatted file: a
// handoff's edit or a recipe's rewrite that leaves one is caught here, before the walk
// commits the step, instead of on the pull request after every step has walked. The
// generated files (zz_gen_*) are the generator's, formatted as it writes them, and are
// left out; so are the directories the other checks skip and the ones Go ignores
// (vendor, testdata, names starting with . or _).
type gofmtCheck struct{}

func (gofmtCheck) Name() string { return "gofmt" }

func (gofmtCheck) Describe() string {
	return "every Go file the author owns is gofmt-formatted, test files included (--fix formats them)"
}

func (gofmtCheck) Meaning() string {
	return "The rendered CI's lint gate runs gofmt over the application, so an unformatted file fails the pull request; format the files it names (gofmt -w, or impulse check --only gofmt --fix) and change nothing else in them."
}

func (c gofmtCheck) Run(_ context.Context, env *Env) Result {
	a := env.App
	files, err := c.ownedGoFiles(a.Root)
	if err != nil {
		return fail(c.Name(), err.Error())
	}
	// The files are read and written through a root opened on the application, so a
	// path can only name a file under it.
	root, err := os.OpenRoot(a.Root)
	if err != nil {
		return fail(c.Name(), errors.Wrap(err, "os.OpenRoot()").Error())
	}
	defer root.Close()
	var details, fixed []string
	for _, rel := range files {
		src, err := root.ReadFile(filepath.FromSlash(rel))
		if err != nil {
			return fail(c.Name(), errors.Wrap(err, "os.Root.ReadFile()").Error())
		}
		formatted, err := format.Source(src)
		if err != nil {
			// A file that does not parse is the compiler's to refuse (gowork-off), not this check's.
			continue
		}
		if bytes.Equal(src, formatted) {
			continue
		}
		if env.Fix {
			if err := root.WriteFile(filepath.FromSlash(rel), formatted, 0o600); err != nil {
				return fail(c.Name(), errors.Wrap(err, "os.Root.WriteFile()").Error())
			}
			fixed = append(fixed, rel)

			continue
		}
		details = append(details, rel+": not gofmt-formatted")
	}
	switch {
	case len(details) > 0:
		return fail(c.Name(), fmt.Sprintf("%d Go file(s) not gofmt-formatted (--fix formats them)", len(details)), details...)
	case len(fixed) > 0:
		return passWithDetails(c.Name(), fmt.Sprintf("%d Go file(s) formatted; %d checked", len(fixed), len(files)), fixed...)
	default:
		return pass(c.Name(), fmt.Sprintf("%d Go file(s) gofmt-formatted", len(files)))
	}
}

// ownedGoFiles lists the Go files the author owns, root-relative with forward slashes:
// every .go file under the root but the generated ones, outside the skipped directories
// and the ones Go ignores.
func (gofmtCheck) ownedGoFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		name := d.Name()
		if d.IsDir() {
			if p != root && (skipped[name] || name == "vendor" || name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")) {
				return filepath.SkipDir
			}

			return nil
		}
		if !strings.HasSuffix(name, ".go") || strings.HasPrefix(name, "zz_gen_") {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return errors.Wrap(err, "filepath.Rel()")
		}
		files = append(files, filepath.ToSlash(rel))

		return nil
	})
	if err != nil {
		return nil, errors.Wrap(err, "filepath.WalkDir()")
	}

	return files, nil
}
