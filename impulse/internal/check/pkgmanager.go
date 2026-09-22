package check

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/internal/app"
)

// packageManager verifies that one JavaScript package manager runs the whole
// application: every browser app carries the same kind of lockfile, and the process files
// and package scripts invoke that tool and no other. Two tools in one repository means
// two lockfiles drifting apart and install steps that disagree with the pipeline.
type packageManager struct{}

func (packageManager) Name() string { return "package-manager" }

func (packageManager) Describe() string {
	return "the browser apps share one package manager, and the process files and scripts use it"
}

// The package managers this check tells apart.
const (
	bun  = "bun"
	npm  = "npm"
	yarn = "yarn"
	pnpm = "pnpm"
)

// lockfiles maps each lockfile name to the package manager that writes it.
var lockfiles = map[string]string{
	"bun.lock": bun, "bun.lockb": bun,
	"package-lock.json": npm, "npm-shrinkwrap.json": npm,
	"yarn.lock":      yarn,
	"pnpm-lock.yaml": pnpm,
}

// toolWord finds a package manager invocation in a command line; runners map to the
// manager that ships them.
var (
	toolWord = regexp.MustCompile(`(?:^|[\s;&|(])(npm|npx|bun|bunx|yarn|pnpm)(?:\s|$)`)
	runners  = map[string]string{"npx": npm, "bunx": bun}
)

func (c packageManager) Run(_ context.Context, env *Env) Result {
	a := env.App
	if len(a.WebApps) == 0 {
		return skip(c.Name(), "no browser apps")
	}

	var details []string
	byTool := map[string][]string{} // tool -> web app dirs locked with it
	for _, w := range a.WebApps {
		var found []string
		for name, tool := range lockfiles {
			if _, err := os.Stat(a.Abs(path.Join(w.Dir, name))); err == nil {
				found = append(found, tool+" ("+name+")")
				byTool[tool] = append(byTool[tool], w.Dir)
			}
		}
		sort.Strings(found)
		switch len(found) {
		case 0:
			details = append(details, w.Dir+": no lockfile")
		case 1:
		default:
			details = append(details, fmt.Sprintf("%s: %d lockfiles: %s", w.Dir, len(found), strings.Join(found, ", ")))
		}
	}
	if len(byTool) == 0 {
		return skip(c.Name(), "no browser app has a lockfile")
	}

	tools := make([]string, 0, len(byTool))
	for tool := range byTool {
		tools = append(tools, tool)
	}
	sort.Strings(tools)
	if len(tools) > 1 {
		for _, tool := range tools {
			dirs := byTool[tool]
			sort.Strings(dirs)
			details = append(details, fmt.Sprintf("%s: %s", tool, strings.Join(dirs, ", ")))
		}

		return fail(c.Name(), fmt.Sprintf("browser apps are locked by %d different package managers", len(tools)), details...)
	}
	tool := tools[0]

	mismatches, err := c.foreignInvocations(a.Root, a.WebApps, tool)
	if err != nil {
		return fail(c.Name(), err.Error())
	}
	details = append(details, mismatches...)

	if len(details) > 0 {
		return fail(c.Name(), fmt.Sprintf("%d place(s) disagree with %s, the package manager the lockfiles name", len(details), tool), details...)
	}

	return pass(c.Name(), fmt.Sprintf("%d browser app(s) on %s", len(a.WebApps), tool))
}

// foreignInvocations lists the process-file lines and package scripts that invoke a
// package manager other than tool.
func (packageManager) foreignInvocations(root string, webApps []app.WebApp, tool string) ([]string, error) {
	var details []string

	processFiles, err := processFiles(root)
	if err != nil {
		return nil, err
	}
	for _, file := range processFiles {
		data, err := os.ReadFile(filepath.Join(root, file))
		if err != nil {
			return nil, errors.Wrap(err, "os.ReadFile()")
		}
		for i, line := range strings.Split(string(data), "\n") {
			if other := foreignTool(line, tool); other != "" {
				details = append(details, fmt.Sprintf("%s:%d runs %s", file, i+1, other))
			}
		}
	}

	for _, w := range webApps {
		pkgPath := path.Join(w.Dir, "package.json")
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(pkgPath)))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, errors.Wrap(err, "os.ReadFile()")
		}
		var pkg struct {
			Scripts map[string]string `json:"scripts"`
		}
		if err := json.Unmarshal(data, &pkg); err != nil {
			return nil, errors.Wrapf(err, "json.Unmarshal(): %s", pkgPath)
		}
		names := make([]string, 0, len(pkg.Scripts))
		for name := range pkg.Scripts {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if other := foreignTool(pkg.Scripts[name], tool); other != "" {
				details = append(details, fmt.Sprintf("%s script %q runs %s", pkgPath, name, other))
			}
		}
	}

	return details, nil
}

// foreignTool returns the package manager a command line invokes when it is not tool,
// or "" when the line invokes tool or none.
func foreignTool(line, tool string) string {
	for _, m := range toolWord.FindAllStringSubmatch(line, -1) {
		word := m[1]
		if manager, ok := runners[word]; ok {
			word = manager
		}
		if word != tool {
			return word
		}
	}

	return ""
}

// processFiles lists the process files at the application root: Procfile variants and
// process-compose files, relative to root.
func processFiles(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, errors.Wrap(err, "os.ReadDir()")
	}
	var files []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			continue
		}
		if strings.HasPrefix(name, "Procfile") || (strings.HasPrefix(name, "process-compose") && (strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml"))) {
			files = append(files, name)
		}
	}
	sort.Strings(files)

	return files, nil
}
