package check

import (
	"context"
	"fmt"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/go-playground/errors/v5"
)

// eslintIgnore verifies that each browser app receiving generated TypeScript excludes it
// from eslint. With few resources the generated client holds empty interfaces and other
// shapes a stylistic rule rejects, and the generator output is not the developer's to
// change, so lint has nothing to say about it. The .prettierignore check is the same
// rule for the formatter.
type eslintIgnore struct{}

func (eslintIgnore) Name() string { return "eslint-ignore" }

func (eslintIgnore) Describe() string {
	return "each browser app's eslint configuration ignores the generated TypeScript"
}

// eslintConfigs are the flat-config file names eslint looks for, in its own order.
var eslintConfigs = []string{"eslint.config.js", "eslint.config.mjs", "eslint.config.cjs", "eslint.config.ts", "eslint.config.mts", "eslint.config.cts"}

// ignoresArrays finds every `ignores: [...]` array in a flat config; quoted extracts the
// string literals inside one.
var (
	ignoresArrays = regexp.MustCompile(`ignores\s*:\s*\[([^\]]*)\]`)
	quoted        = regexp.MustCompile("'([^']*)'|\"([^\"]*)\"|`([^`]*)`")
)

func (c eslintIgnore) Run(_ context.Context, env *Env) Result {
	a := env.App

	// Group the TypeScript targets by the browser app that receives them; a target
	// outside any browser app is prettier-ignore's finding, not this check's.
	targets := map[string][]string{} // web app dir -> target dirs relative to it
	for _, g := range a.Generators {
		for _, t := range g.TypescriptTargets() {
			w, ok := a.WebAppFor(t.Dir)
			if !ok {
				continue
			}
			targets[w.Dir] = append(targets[w.Dir], strings.TrimPrefix(strings.TrimPrefix(t.Dir, w.Dir), "/"))
		}
	}
	if len(targets) == 0 {
		return skip(c.Name(), "no GenerateTypescript target inside a browser app")
	}

	webDirs := make([]string, 0, len(targets))
	for dir, rels := range targets {
		webDirs = append(webDirs, dir)
		sort.Strings(rels)
	}
	sort.Strings(webDirs)

	var details []string
	checked, unconfigured := 0, 0
	for _, webDir := range webDirs {
		config, patterns, err := eslintPatterns(a.Abs(webDir))
		if err != nil {
			return fail(c.Name(), err.Error())
		}
		if config == "" {
			unconfigured++
			details = append(details, fmt.Sprintf("%s: no eslint configuration, nothing to check", webDir))

			continue
		}
		checked++
		for _, relTarget := range targets[webDir] {
			for _, probe := range probeFiles {
				if !ignored(patterns, path.Join(relTarget, probe)) {
					details = append(details, fmt.Sprintf("%s does not ignore %s (add ignores: ['**/zz_gen_*.ts'])", path.Join(webDir, config), path.Join(relTarget, "zz_gen_*.ts")))

					break
				}
			}
		}
	}

	failures := len(details) - unconfigured
	switch {
	case failures > 0:
		return fail(c.Name(), fmt.Sprintf("%d TypeScript target(s) not ignored by eslint", failures), details...)
	case checked == 0:
		return skip(c.Name(), "no browser app receiving generated TypeScript has an eslint configuration")
	default:
		return passWithDetails(c.Name(), fmt.Sprintf("%d browser app(s) ignore the generated TypeScript", checked), details...)
	}
}

// eslintPatterns returns the ignore patterns a browser app's eslint setup applies to
// the app's own files: every `ignores` array of its flat config plus a legacy
// .eslintignore, and the config file name found, or "" when the app has none.
func eslintPatterns(webAbs string) (config string, patterns []string, err error) {
	for _, name := range eslintConfigs {
		data, err := os.ReadFile(path.Join(webAbs, name))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", nil, errors.Wrap(err, "os.ReadFile()")
		}
		config = name
		for _, array := range ignoresArrays.FindAllStringSubmatch(string(data), -1) {
			for _, lit := range quoted.FindAllStringSubmatch(array[1], -1) {
				patterns = append(patterns, lit[1]+lit[2]+lit[3])
			}
		}

		break
	}

	legacy, err := readIgnore(path.Join(webAbs, ".eslintignore"))
	if err != nil {
		return "", nil, err
	}
	if legacy != nil && config == "" {
		config = ".eslintignore"
	}

	return config, append(patterns, legacy...), nil
}
