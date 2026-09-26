// Package render writes an application stack from the derived model: every file the
// stack is made of, rendered from an embedded template, and which of them the tool owns.
//
// A file has a tier. An owned file is rewritten on every render: the stack's .tf files
// and its README say what the code declares and nothing a person keeps by hand. A
// seeded file is written once, when absent, and never again: terraform.tfvars holds the
// placement values a person fills in per environment. Files the tool never writes (the
// lock file, the placement) have no tier here.
package render

import (
	"bytes"
	"embed"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/derive"
)

// Tier says whether the tool owns a file or only seeds it.
type Tier int

// The tiers.
const (
	// Owned files are rewritten on every render.
	Owned Tier = iota
	// Seeded files are written once and then belong to the person.
	Seeded
)

func (t Tier) String() string {
	switch t {
	case Owned:
		return "owned"
	case Seeded:
		return "seeded"
	default:
		return "unknown"
	}
}

// File is one rendered file of the stack.
type File struct {
	// Path is the file's path relative to the stack directory.
	Path string
	Tier Tier
	// Content is the rendered text.
	Content []byte
}

// templates are the stack's files, one template per file, embedded so the tool renders
// offline and ships exactly what it was tested with.
//
//go:embed templates/*.tmpl
var templates embed.FS

const (
	templateDir = "templates"
	templateExt = ".tmpl"
	// seededFile is the one file the tool writes once: the placement values per
	// environment.
	seededFile = "terraform.tfvars"
)

// Render renders every file of the application's stack.
func Render(m *derive.Model) ([]File, error) {
	if m.Migrate == nil {
		return nil, errors.Newf("the stack runs the migration as a Cloud Run job; %s has no migrate command (cmd/deployment/migrate)", m.App)
	}
	v, err := newView(m)
	if err != nil {
		return nil, err
	}
	entries, err := fs.ReadDir(templates, templateDir)
	if err != nil {
		return nil, errors.Wrap(err, "fs.ReadDir()")
	}

	files := make([]File, 0, len(entries))
	for _, entry := range entries {
		name := strings.TrimSuffix(entry.Name(), templateExt)
		content, err := renderOne(entry.Name(), v)
		if err != nil {
			return nil, err
		}
		tier := Owned
		if name == seededFile {
			tier = Seeded
		}
		files = append(files, File{Path: name, Tier: tier, Content: content})
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})

	return files, nil
}

// renderOne executes one embedded template.
func renderOne(name string, v *view) ([]byte, error) {
	src, err := templates.ReadFile(templateDir + "/" + name)
	if err != nil {
		return nil, errors.Wrapf(err, "embed.FS.ReadFile(): %s", name)
	}
	t, err := template.New(name).Option("missingkey=error").Parse(string(src))
	if err != nil {
		return nil, errors.Wrapf(err, "template.Parse(): %s", name)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, v); err != nil {
		return nil, errors.Wrapf(err, "template.Execute(): %s", name)
	}

	return buf.Bytes(), nil
}

// Written reports what a write did.
type Written struct {
	// Owned counts the owned files written.
	Owned int
	// Seeded lists the seeded files created, and Kept the ones left as they were.
	Seeded []string
	Kept   []string
}

// Write puts the files into dir, creating it when absent: owned files always, seeded
// files only when absent.
func Write(files []File, dir string) (*Written, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, errors.Wrap(err, "os.MkdirAll()")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, errors.Wrap(err, "os.OpenRoot()")
	}
	defer root.Close()

	w := &Written{}
	for _, f := range files {
		dst := filepath.FromSlash(f.Path)
		if f.Tier == Seeded {
			if _, err := root.Stat(dst); err == nil {
				w.Kept = append(w.Kept, f.Path)

				continue
			}
			w.Seeded = append(w.Seeded, f.Path)
		} else {
			w.Owned++
		}
		if err := root.WriteFile(dst, f.Content, 0o644); err != nil {
			return nil, errors.Wrapf(err, "os.Root.WriteFile(): %s", f.Path)
		}
	}

	return w, nil
}
