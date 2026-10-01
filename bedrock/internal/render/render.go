// Package render writes an application stack from the derived model: every file the
// stack is made of, rendered from an embedded template, and which of them the tool owns.
//
// A file has a tier. An owned file is rewritten on every render: the stack's .tf files
// and its README say what the code declares and nothing a person keeps by hand, and so
// do the pipeline files Cloud Build reads at the application root (cloudbuild.yaml,
// cloudbuild-sweep.yaml). A seeded file is written once, when absent, and never again:
// terraform.tfvars holds the placement values a person fills in per environment, the
// stack's .gitignore keeps the per-environment backend caches and saved plans out of
// the repository, and the Dockerfile at the application root is the image build a
// person takes over from its first shape. Files the tool never writes (the lock file,
// the placement) have no tier here.
package render

import (
	"bytes"
	"embed"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"github.com/go-playground/errors/v5"
	"github.com/hashicorp/hcl/v2/hclwrite"

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
	// Path is the file's path relative to the stack directory, or to the application
	// root when Root is set.
	Path string
	Tier Tier
	// Root is true for a file that lives at the application root (the directory
	// holding go.mod, the repository's root), where Cloud Build and the image build
	// read it, rather than in the stack directory.
	Root bool
	// Content is the rendered text.
	Content []byte
}

// templates are the stack's files, one template per file, embedded so the tool renders
// offline and ships exactly what it was tested with. The root directory holds the files
// that go to the application root.
//
//go:embed templates/*.tmpl templates/root/*.tmpl
var templates embed.FS

const (
	templateDir     = "templates"
	rootTemplateDir = "templates/root"
	templateExt     = ".tmpl"
	// The files the tool writes once: the placement values per environment, the
	// stack's ignore rules, and the image build.
	tfvarsFile = "terraform.tfvars"
	ignoreFile = ".gitignore"
	// generateFile is bedrock's generate-time step, rendered beside the application's
	// //go:generate directive.
	generateFile   = "bedrock.go"
	dockerfileFile = "Dockerfile"
)

// seeded are the files the tool writes once.
var seeded = map[string]bool{
	tfvarsFile:     true,
	ignoreFile:     true,
	dockerfileFile: true,
}

// placed are the root files whose place depends on the application: the generate-time
// step goes beside the directive that runs the site generator, in a file that sorts
// before the application's own; an application with no such directive gets none.
var placed = map[string]func(v *view) string{
	generateFile: func(v *view) string {
		if v.Schema.GenerateDir == "" {
			return ""
		}

		return path.Join(v.Schema.GenerateDir, generateFile)
	},
	// The GitHub workflows, under .github/workflows: the infrastructure check, which
	// gets the bedrock the placement pins and checks with it, and the release
	// workflow.
	infrastructureWorkflow: func(*view) string {
		return path.Join(workflowsDir, infrastructureWorkflow)
	},
	releaseWorkflow: func(*view) string {
		return path.Join(workflowsDir, releaseWorkflow)
	},
	OperationsWorkflow: func(*view) string {
		return path.Join(workflowsDir, OperationsWorkflow)
	},
}

// The GitHub workflows bedrock renders at the application root: the infrastructure
// check, the release workflow, and the operations workflow, which starts a restore of an
// environment (bedrock restore dispatches it by this name).
const (
	workflowsDir           = ".github/workflows"
	infrastructureWorkflow = "infrastructure.yml"
	releaseWorkflow        = "release-please.yml"
	OperationsWorkflow     = "operations.yml"
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
	stack, err := renderDir(templateDir, false, v)
	if err != nil {
		return nil, err
	}
	root, err := renderDir(rootTemplateDir, true, v)
	if err != nil {
		return nil, err
	}
	files := make([]File, 0, len(stack)+len(root))
	files = append(files, stack...)
	files = append(files, root...)
	sort.Slice(files, func(i, j int) bool {
		if files[i].Root != files[j].Root {
			return !files[i].Root
		}

		return files[i].Path < files[j].Path
	})

	return files, nil
}

// renderDir renders every template of one embedded directory; root says where the
// files go.
func renderDir(dir string, root bool, v *view) ([]File, error) {
	entries, err := fs.ReadDir(templates, dir)
	if err != nil {
		return nil, errors.Wrapf(err, "fs.ReadDir(): %s", dir)
	}
	files := make([]File, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), templateExt)
		if place, ok := placed[name]; ok {
			if name = place(v); name == "" {
				continue
			}
		}
		content, err := renderOne(dir, entry.Name(), v)
		if err != nil {
			return nil, err
		}
		tier := Owned
		if seeded[name] {
			tier = Seeded
		}
		files = append(files, File{Path: name, Tier: tier, Root: root, Content: Formatted(name, content)})
	}

	return files, nil
}

// Formatted returns content as tofu fmt writes it when name is an HCL file (.tf or
// .tfvars), so that a rendered file passes the infrastructure check's tofu fmt -check
// whatever a template's own spacing: a block's trailing comments align on its longest
// line, which a value of varying width (an environment list, a release actor) moves.
// Any other file is returned as it is.
func Formatted(name string, content []byte) []byte {
	if ext := path.Ext(name); ext != ".tf" && ext != ".tfvars" {
		return content
	}

	return hclwrite.Format(content)
}

// renderOne executes one embedded template.
func renderOne(dir, name string, v *view) ([]byte, error) {
	src, err := templates.ReadFile(dir + "/" + name)
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
	// Owned counts the owned files written into the stack directory.
	Owned int
	// Pipeline lists the owned files written at the application root.
	Pipeline []string
	// Seeded lists the seeded files created, and Kept the ones left as they were.
	Seeded []string
	Kept   []string
}

// Write puts the files into dir, creating it when absent, and the root files into
// appDir, which exists: owned files always, seeded files only when absent.
func Write(files []File, dir, appDir string) (*Written, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, errors.Wrap(err, "os.MkdirAll()")
	}
	stack, err := os.OpenRoot(dir)
	if err != nil {
		return nil, errors.Wrap(err, "os.OpenRoot()")
	}
	defer stack.Close()
	app, err := os.OpenRoot(appDir)
	if err != nil {
		return nil, errors.Wrap(err, "os.OpenRoot()")
	}
	defer app.Close()

	w := &Written{}
	for _, f := range files {
		root := stack
		if f.Root {
			root = app
		}
		dst := filepath.FromSlash(f.Path)
		if parent := filepath.Dir(dst); parent != "." {
			if err := root.MkdirAll(parent, 0o755); err != nil {
				return nil, errors.Wrapf(err, "os.Root.MkdirAll(): %s", parent)
			}
		}
		switch {
		case f.Tier == Seeded:
			if _, err := root.Stat(dst); err == nil {
				w.Kept = append(w.Kept, f.Path)

				continue
			}
			w.Seeded = append(w.Seeded, f.Path)
		case f.Root:
			w.Pipeline = append(w.Pipeline, f.Path)
		default:
			w.Owned++
		}
		if err := root.WriteFile(dst, f.Content, 0o644); err != nil {
			return nil, errors.Wrapf(err, "os.Root.WriteFile(): %s", f.Path)
		}
	}

	return w, nil
}
