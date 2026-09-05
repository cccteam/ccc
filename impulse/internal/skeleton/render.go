package skeleton

import (
	"go/format"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/go-playground/errors/v5"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
)

// Options describe one rendering of an embedded template.
type Options struct {
	// Candidate names the embedded skeleton to render.
	Candidate string
	// Dir is the directory to render into. It must not exist or must be empty.
	Dir string
	// ModulePath is the rendered application's module path. Every import and the go.mod
	// files are rewritten from the template's placeholder path to it.
	ModulePath string
	// DevRoot, when set, is a directory holding cccteam checkouts laid out by repository
	// (DevRoot/ccc/resource, DevRoot/session, ...). The rendering then writes a go.work
	// that uses every framework module go.mod requires directly and that has a checkout
	// under DevRoot, so the application builds against local framework work instead of
	// the pins. Indirect requirements stay pinned: a local checkout of a library's own
	// dependency can lag what the library needs, and pulling it in would break the build
	// for work the developer is not doing.
	DevRoot string
}

// Rendered reports what a rendering produced.
type Rendered struct {
	// Files counts the files written, go.work excluded.
	Files int
	// Workspace is the go.work path written for DevRoot, or empty.
	Workspace string
	// DevUsed lists the framework modules the workspace uses, by module path.
	DevUsed []string
	// DevMissing lists the framework modules go.mod requires directly that have no
	// checkout under DevRoot; the pins stay in force for those.
	DevMissing []string
}

// frameworkPrefix is the module path prefix of the cccteam libraries a --dev workspace
// may point at local checkouts of.
const frameworkPrefix = "github.com/cccteam/"

const goExt = ".go"

// Render copies an embedded template into opts.Dir under opts.ModulePath: go.mod.tmpl
// becomes go.mod, the placeholder module path is rewritten everywhere it appears, Go
// files are reformatted (the rewrite changes line lengths), and shell scripts regain
// their execute bit, which embedding drops.
func Render(opts Options) (*Rendered, error) {
	if err := module.CheckPath(opts.ModulePath); err != nil {
		return nil, errors.Wrapf(err, "module path %q", opts.ModulePath)
	}
	candidate, err := candidateByName(opts.Candidate)
	if err != nil {
		return nil, err
	}
	if err := ensureEmptyDir(opts.Dir); err != nil {
		return nil, err
	}
	sub, err := FS(candidate.Name)
	if err != nil {
		return nil, err
	}
	// Every write goes through a root scoped to the target directory, so no template
	// path, however named, can land outside it.
	target, err := os.OpenRoot(opts.Dir)
	if err != nil {
		return nil, errors.Wrap(err, "os.OpenRoot()")
	}
	defer target.Close()

	rendered := &Rendered{}
	err = fs.WalkDir(sub, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return errors.Wrap(err, "fs.WalkDir()")
		}
		if d.IsDir() {
			return nil
		}
		if err := renderFile(sub, target, p, opts.ModulePath, candidate.ModulePath); err != nil {
			return err
		}
		rendered.Files++

		return nil
	})
	if err != nil {
		return nil, errors.Wrap(err, "fs.WalkDir()")
	}

	if opts.DevRoot != "" {
		if err := writeWorkspace(opts, target, rendered); err != nil {
			return nil, err
		}
	}

	return rendered, nil
}

// renderFile writes one template file into the target tree. The rendered tree is a
// project a person works in, so it gets the conventional modes: 0755 directories, 0644
// files, 0755 shell scripts.
func renderFile(sub fs.FS, target *os.Root, p, modulePath, placeholder string) error {
	src, err := fs.ReadFile(sub, p)
	if err != nil {
		return errors.Wrapf(err, "fs.ReadFile(): %s", p)
	}

	rel := p
	if path.Base(p) == ModFile {
		rel = path.Join(path.Dir(p), "go.mod")
	}

	out := src
	if utf8.Valid(src) {
		out = []byte(strings.ReplaceAll(string(src), placeholder, modulePath))
		if path.Ext(rel) == goExt {
			formatted, err := format.Source(out)
			if err != nil {
				return errors.Wrapf(err, "format.Source(): %s", rel)
			}
			out = formatted
		}
	}

	dst := filepath.FromSlash(rel)
	if dir := filepath.Dir(dst); dir != "." {
		if err := target.MkdirAll(dir, 0o755); err != nil {
			return errors.Wrap(err, "os.Root.MkdirAll()")
		}
	}
	mode := os.FileMode(0o644)
	if path.Ext(rel) == ".sh" {
		mode = 0o755
	}
	if err := target.WriteFile(dst, out, mode); err != nil {
		return errors.Wrap(err, "os.Root.WriteFile()")
	}

	return nil
}

// writeWorkspace writes the --dev go.work: the rendered module plus every framework
// module go.mod requires directly that has a checkout under DevRoot. Use paths are
// relative when the application lives under the dev root, so the workspace survives a
// move of the whole tree, and absolute otherwise.
func writeWorkspace(opts Options, target *os.Root, rendered *Rendered) error {
	data, err := target.ReadFile("go.mod")
	if err != nil {
		return errors.Wrap(err, "os.Root.ReadFile()")
	}
	goModPath := filepath.Join(opts.Dir, "go.mod")
	mod, err := modfile.Parse(goModPath, data, nil)
	if err != nil {
		return errors.Wrap(err, "modfile.Parse()")
	}
	if mod.Go == nil {
		return errors.Newf("%s has no go directive", goModPath)
	}
	// Checkouts are looked up inside the dev root, so a module path can only ever name
	// a directory beneath it.
	devRoot, err := os.OpenRoot(opts.DevRoot)
	if err != nil {
		return errors.Wrapf(err, "dev root %s is not a directory of checkouts", opts.DevRoot)
	}
	defer devRoot.Close()

	relative := underDir(opts.DevRoot, opts.Dir)
	uses := []string{"."}
	for _, req := range mod.Require {
		modPath := req.Mod.Path
		if req.Indirect || !strings.HasPrefix(modPath, frameworkPrefix) {
			continue
		}
		checkoutRel := filepath.FromSlash(strings.TrimPrefix(modPath, frameworkPrefix))
		if _, err := devRoot.Stat(filepath.Join(checkoutRel, "go.mod")); err != nil {
			rendered.DevMissing = append(rendered.DevMissing, modPath)

			continue
		}
		use := filepath.Join(opts.DevRoot, checkoutRel)
		if relative {
			if rel, err := filepath.Rel(opts.Dir, use); err == nil {
				use = rel
			}
		}
		uses = append(uses, filepath.ToSlash(use))
		rendered.DevUsed = append(rendered.DevUsed, modPath)
	}
	sort.Strings(rendered.DevMissing)

	var b strings.Builder
	b.WriteString("// Development workspace written by impulse --dev: the application builds against\n")
	b.WriteString("// these local framework checkouts instead of the pins in go.mod. Do not commit it.\n")
	b.WriteString("go " + mod.Go.Version + "\n\nuse (\n")
	for _, use := range uses {
		b.WriteString("\t" + use + "\n")
	}
	b.WriteString(")\n")

	rendered.Workspace = filepath.Join(opts.Dir, "go.work")
	if err := target.WriteFile("go.work", []byte(b.String()), 0o644); err != nil {
		return errors.Wrap(err, "os.Root.WriteFile()")
	}

	return nil
}

// underDir reports whether dir lies inside root (after making both absolute).
func underDir(root, dir string) bool {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absRoot, absDir)
	if err != nil {
		return false
	}

	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// candidateByName finds one embedded skeleton.
func candidateByName(name string) (Candidate, error) {
	candidates, err := Candidates()
	if err != nil {
		return Candidate{}, err
	}
	for _, c := range candidates {
		if c.Name == name {
			return c, nil
		}
	}
	names := make([]string, 0, len(candidates))
	for _, c := range candidates {
		names = append(names, c.Name)
	}

	return Candidate{}, errors.Newf("unknown candidate %q: choose one of %s", name, strings.Join(names, ", "))
}

// ensureEmptyDir creates dir when absent and refuses a non-empty one, so a rendering
// never writes over an existing application.
func ensureEmptyDir(dir string) error {
	entries, err := os.ReadDir(dir)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return errors.Wrap(err, "os.MkdirAll()")
		}

		return nil
	case err != nil:
		return errors.Wrap(err, "os.ReadDir()")
	case len(entries) > 0:
		return errors.Newf("%s is not empty: render into a new or empty directory", dir)
	default:
		return nil
	}
}
