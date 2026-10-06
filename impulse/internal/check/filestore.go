package check

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/app"
)

// fileStore verifies the file store's wiring: every file store variable the code
// declares (APP_FILE_STORE, APP_FILE_STORE_<NAME>) names, in the development environment
// template, a store the framework opens (file://<dir>, gs://<bucket> or mem://), a
// directory store's directory is gitignored, and a resource or method recording files
// (@file, @upload) has a store to keep them in.
type fileStore struct{}

func (fileStore) Name() string { return "file-store" }

func (fileStore) Describe() string {
	return "every file store variable names a store the framework opens in the development environment template, a directory store is gitignored, and a resource recording files has a store wired"
}

// fileStoreSchemes are the URL schemes resource/filestore opens, as its constants name
// them; filestore_test.go pins the agreement.
var fileStoreSchemes = []string{"file", "gs", "mem"}

// The scheme a directory store is written under, and the prefix of a named store's
// variable.
const (
	dirScheme           = "file"
	fileStorePrefix     = app.FileStoreVariable + "_"
	fileStoreGitignore  = ".gitignore"
	fileStoreAddCommand = "impulse add files"
)

// fileAnnotationRE finds a @file or @upload annotation line in a Go source file.
var fileAnnotationRE = regexp.MustCompile(`(?m)^\s*//\s*@(file|upload)\b`)

func (c fileStore) Run(_ context.Context, env *Env) Result {
	a := env.App
	var tags []app.EnvTag
	for _, t := range a.EnvTags {
		if t.Name == app.FileStoreVariable || strings.HasPrefix(t.Name, fileStorePrefix) {
			tags = append(tags, t)
		}
	}
	recorders, err := c.fileRecorders(a)
	if err != nil {
		return fail(c.Name(), err.Error())
	}
	if len(tags) == 0 {
		if len(recorders) == 0 {
			return skip(c.Name(), "no file store variable, and no resource records files")
		}
		details := make([]string, 0, len(recorders))
		for _, r := range recorders {
			details = append(details, fmt.Sprintf("%s: declares %s, and no file store is wired (no env tag %s): run %s", r.pos, r.annotation, app.FileStoreVariable, fileStoreAddCommand))
		}

		return fail(c.Name(), fmt.Sprintf("%d file annotation(s) without a file store", len(recorders)), details...)
	}
	if a.EnvTemplate == "" {
		return skip(c.Name(), "no .envrc.template (or .env.template, .env.example) at the application root")
	}
	template, err := os.ReadFile(a.Abs(a.EnvTemplate))
	if err != nil {
		return fail(c.Name(), errors.Wrap(err, "os.ReadFile()").Error())
	}
	gitignore, err := os.ReadFile(a.Abs(fileStoreGitignore))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fail(c.Name(), errors.Wrap(err, "os.ReadFile()").Error())
	}

	var details, unignored []string
	seen := map[string]bool{}
	for _, t := range tags {
		if seen[t.Name] {
			continue
		}
		seen[t.Name] = true
		value, ok := templateValue(string(template), t.Name)
		if !ok {
			// The env-template check reports a variable the template lacks.
			continue
		}
		detail, dir := c.judge(t, value, a.EnvTemplate)
		if detail != "" {
			details = append(details, detail)

			continue
		}
		if dir != "" && !ignoresDirectory(string(gitignore), dir) {
			unignored = append(unignored, dir)
			details = append(details, fmt.Sprintf("%s: %s names the directory %s, which %s does not ignore, so the development files would be committed (--fix adds %s/)", a.EnvTemplate, t.Name, value, fileStoreGitignore, dir))
		}
	}
	if len(details) == 0 {
		return pass(c.Name(), fmt.Sprintf("%d file store variable(s) name stores the framework opens", len(seen)))
	}
	if env.Fix && len(unignored) == len(details) {
		lines := make([]string, 0, len(unignored))
		for _, dir := range unignored {
			lines = append(lines, dir+"/")
		}
		if err := appendLines(a.Abs(fileStoreGitignore), "", lines); err != nil {
			return fail(c.Name(), err.Error())
		}

		return passWithDetails(c.Name(), fmt.Sprintf("%d directory store(s) added to %s", len(lines), fileStoreGitignore), details...)
	}

	return fail(c.Name(), fmt.Sprintf("%d file store problem(s)", len(details)), details...)
}

// judge reads one variable's template value: the detail of what is wrong with it, or
// the directory it names when it is a directory store the gitignore must cover.
func (fileStore) judge(t app.EnvTag, value, template string) (detail, dir string) {
	if value == "" {
		return fmt.Sprintf("%s: %s names no store; development keeps files in a directory, file://uploads", template, t.Name), ""
	}
	scheme, rest, ok := strings.Cut(value, "://")
	if !ok || scheme == "" {
		return fmt.Sprintf("%s: %s=%s has no scheme; a store is file://<dir>, gs://<bucket> or mem://", template, t.Name, value), ""
	}
	if !slices.Contains(fileStoreSchemes, scheme) {
		return fmt.Sprintf("%s: %s=%s has the scheme %s, which the framework's store (resource/filestore) does not open; a store is file://<dir>, gs://<bucket> or mem://", template, t.Name, value, scheme), ""
	}
	if scheme != dirScheme {
		return "", ""
	}
	dir = strings.TrimSuffix(rest, "/")
	switch {
	case dir == "":
		return fmt.Sprintf("%s: %s=%s names no directory; a directory store is file://<dir>", template, t.Name, value), ""
	case path.IsAbs(dir) || strings.Contains(dir, "$") || strings.HasPrefix(dir, "..") || strings.HasPrefix(dir, "./"):
		// A directory outside the tree, or one the shell computes, is not the
		// repository's to ignore.
		return "", ""
	}

	return "", dir
}

// templateAssignRE finds an uncommented assignment of a variable in the template.
func templateAssignRE(name string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^\s*(?:export\s+)?` + regexp.QuoteMeta(name) + `=(.*)$`)
}

// templateValue reads the value a template assigns a variable, unquoted; false when the
// template assigns it nowhere outside a comment.
func templateValue(template, name string) (string, bool) {
	m := templateAssignRE(name).FindStringSubmatch(template)
	if m == nil {
		return "", false
	}
	value := strings.TrimSpace(m[1])
	if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
		value = value[1 : len(value)-1]
	}

	return value, true
}

// ignoresDirectory reports whether a .gitignore's lines ignore the directory, written
// any of the ways git reads as the directory at the root or anywhere.
func ignoresDirectory(gitignore, dir string) bool {
	for line := range strings.Lines(gitignore) {
		switch strings.TrimSpace(line) {
		case dir, dir + "/", "/" + dir, "/" + dir + "/":
			return true
		}
	}

	return false
}

// fileRecorder is one @file or @upload annotation in the application's own code.
type fileRecorder struct {
	pos        string
	annotation string
}

// fileRecorders finds the @file and @upload annotations in the resource and rpc packages
// the generators read.
func (fileStore) fileRecorders(a *app.App) ([]fileRecorder, error) {
	var dirs []string
	for _, g := range a.Generators {
		for _, dir := range append([]string{g.ResourcePackageDir}, g.RPCDir()) {
			if dir != "" && !slices.Contains(dirs, dir) {
				dirs = append(dirs, dir)
			}
		}
	}
	var found []fileRecorder
	for _, dir := range dirs {
		files, err := filepath.Glob(filepath.Join(a.Abs(dir), "*.go"))
		if err != nil {
			return nil, errors.Wrap(err, "filepath.Glob()")
		}
		for _, f := range files {
			name := filepath.Base(f)
			if strings.HasSuffix(name, "_test.go") || strings.HasPrefix(name, "zz_gen_") {
				continue
			}
			data, err := os.ReadFile(f)
			if err != nil {
				return nil, errors.Wrap(err, "os.ReadFile()")
			}
			rel := path.Join(dir, name)
			for _, loc := range fileAnnotationRE.FindAllSubmatchIndex(data, -1) {
				line := 1 + strings.Count(string(data[:loc[0]]), "\n")
				found = append(found, fileRecorder{pos: fmt.Sprintf("%s:%d", rel, line), annotation: "@" + string(data[loc[2]:loc[3]])})
			}
		}
	}

	return found, nil
}
