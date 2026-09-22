// Package skeleton carries the application templates impulse renders: the candidate
// skeletons under _candidates, embedded in the binary so new and add work offline and
// ship exactly the tree the release was tested with.
//
// While they sit here the templates are not Go modules. Each carries its go.mod as
// go.mod.tmpl, so the directory is not a module boundary (go:embed excludes everything
// under a go.mod), and the leading underscore keeps the tree out of ./... patterns so
// go build and go vet never try to compile it. Rendering writes go.mod back under the
// target module path and rewrites every import to match. The templates are validated
// by rendering them, not in place.
package skeleton

import (
	"bufio"
	"embed"
	"io/fs"
	"strings"

	"github.com/go-playground/errors/v5"
)

// root is the embedded template tree. The all: prefix keeps the dotfiles every template
// needs (.gitignore, .golangci.yml, .envrc.template, .prettierrc, .prettierignore).
//
//go:embed all:_candidates
var root embed.FS

const (
	dir = "_candidates"

	// ModFile is the name a template's go.mod carries while embedded.
	ModFile = "go.mod.tmpl"
)

// Candidate is one embedded skeleton.
type Candidate struct {
	// Name is the directory name under _candidates and the template's identity.
	Name string
	// ModulePath is the placeholder module path the template's imports use. Rendering
	// replaces it with the target module path.
	ModulePath string
}

// Candidates lists the embedded skeletons in directory order.
func Candidates() ([]Candidate, error) {
	entries, err := fs.ReadDir(root, dir)
	if err != nil {
		return nil, errors.Wrap(err, "fs.ReadDir()")
	}

	candidates := make([]Candidate, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		modulePath, err := modulePath(entry.Name())
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, Candidate{Name: entry.Name(), ModulePath: modulePath})
	}

	return candidates, nil
}

// FS returns one candidate's template tree, rooted at the template's top level.
func FS(name string) (fs.FS, error) {
	sub, err := fs.Sub(root, dir+"/"+name)
	if err != nil {
		return nil, errors.Wrapf(err, "fs.Sub(): candidate %q", name)
	}
	if _, err := fs.Stat(sub, ModFile); err != nil {
		return nil, errors.Wrapf(err, "candidate %q: no %s", name, ModFile)
	}

	return sub, nil
}

// modulePath reads the module directive from a candidate's embedded go.mod.
func modulePath(name string) (string, error) {
	f, err := root.Open(dir + "/" + name + "/" + ModFile)
	if err != nil {
		return "", errors.Wrapf(err, "embed.FS.Open(): candidate %q", name)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if path, ok := strings.CutPrefix(strings.TrimSpace(scanner.Text()), "module "); ok {
			return strings.TrimSpace(path), nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", errors.Wrapf(err, "bufio.Scanner.Scan(): candidate %q", name)
	}

	return "", errors.Newf("candidate %q: %s has no module directive", name, ModFile)
}
