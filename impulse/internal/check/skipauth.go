package check

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/internal/app"
)

// skipAuth verifies that the simulated directory stays where it belongs. An auth whose
// people sign in through a directory (the OIDC flavors) is signed in to in development and
// in the tests under the session library's skipAuth build tag, which fabricates the login
// from APP_USERNAME and APP_ROLES. Only the library reads those variables, and only a
// development or test build carries the tag: a deployable build that did would accept any
// name as a login.
type skipAuth struct{}

// skipAuthName is the check's name.
const skipAuthName = "skipauth"

func (skipAuth) Name() string { return skipAuthName }

func (skipAuth) Describe() string {
	return "the simulated directory (the session library's skipAuth build tag) is confined to development and tests"
}

// simulatedVarRE matches the simulated directory's variables in Go source.
var simulatedVarRE = regexp.MustCompile(`"APP_(?:USERNAME|ROLES)"`)

// buildFileRE matches the files a deployable build is described in.
var buildFileRE = regexp.MustCompile(`^(?:Dockerfile.*|.*\.Dockerfile|cloudbuild.*\.ya?ml|Makefile|Taskfile.*\.ya?ml|skaffold.*\.ya?ml)$`)

// skipped are the directories a build-file walk never enters.
var skipped = map[string]bool{".git": true, "node_modules": true, ".angular": true, distDir: true, ".yalc": true}

// distDir is a browser app's build output directory.
const distDir = "dist"

func (c skipAuth) Run(_ context.Context, env *Env) Result {
	a := env.App
	oidc := 0
	for i := range a.Auths {
		if a.Auths[i].Flavor == app.FlavorOIDCAzure || a.Auths[i].Flavor == app.FlavorOIDCGoogle {
			oidc++
		}
	}
	if oidc == 0 {
		return skip(c.Name(), "no auth signs in through a directory")
	}

	var details []string
	for _, rel := range a.GoFiles() {
		src, err := os.ReadFile(a.Abs(rel))
		if err != nil {
			continue
		}
		if m := simulatedVarRE.Find(src); m != nil {
			details = append(details, fmt.Sprintf("%s: reads %s, the simulated directory's variable; only the session library's skipAuth build reads it, and only in development and tests", rel, strings.Trim(string(m), `"`)))
		}
	}

	buildFiles, err := c.buildFiles(a)
	if err != nil {
		return fail(c.Name(), err.Error())
	}
	for _, rel := range buildFiles {
		src, err := os.ReadFile(a.Abs(rel))
		if err != nil {
			continue
		}
		if strings.Contains(string(src), "skipAuth") {
			details = append(details, fmt.Sprintf("%s: a deployable build carries the skipAuth tag, so the built application would accept any name as a directory login; the tag belongs to the Procfile and the test command only", rel))
		}
	}

	if len(details) > 0 {
		return fail(c.Name(), fmt.Sprintf("%d simulated-directory problem(s)", len(details)), details...)
	}

	return pass(c.Name(), fmt.Sprintf("%d directory auth(s); the simulated directory is confined to development and tests", oidc))
}

// buildFiles lists the build descriptions in the tree.
func (skipAuth) buildFiles(a *app.App) ([]string, error) {
	var files []string
	err := filepath.WalkDir(a.Root, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if skipped[d.Name()] && p != a.Root {
				return filepath.SkipDir
			}

			return nil
		}
		if buildFileRE.MatchString(d.Name()) {
			rel, err := filepath.Rel(a.Root, p)
			if err != nil {
				return errors.Wrap(err, "filepath.Rel()")
			}
			files = append(files, filepath.ToSlash(rel))
		}

		return nil
	})
	if err != nil {
		return nil, errors.Wrap(err, "filepath.WalkDir()")
	}

	return files, nil
}
