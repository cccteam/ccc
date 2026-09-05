// Package app discovers the shape of an Impulse application from its code.
//
// There is no manifest: everything the tool needs is read from files that are already
// load-bearing for the application itself — the generator program, go.mod, the browser
// apps' angular.json, the Procfile, the test harnesses, and the config struct tags.
package app

import (
	"os"
	"path/filepath"

	"github.com/go-playground/errors/v5"
	"golang.org/x/mod/modfile"
)

// App is the discovered shape of one Impulse application rooted at a Go module.
type App struct {
	// Root is the absolute path of the application root (the directory holding go.mod).
	Root string
	// GoMod is the parsed root go.mod.
	GoMod *modfile.File
	// Generators are the resource generator programs found in the tree, one per site
	// plus the shared generator in a multi-site application.
	Generators []*Generator
	// WebApps are the browser applications: every directory holding an angular.json.
	WebApps []WebApp
	// EmulatorImages are the Spanner emulator image tags named by process files
	// (Procfile, process-compose.yaml).
	EmulatorImages []EmulatorRef
	// EmulatorHarnesses are the Spanner emulator versions requested by test harnesses
	// through initiator.NewSpannerContainer.
	EmulatorHarnesses []EmulatorRef
	// EnvTags are the env struct tags declared by the application's Go code.
	EnvTags []EnvTag
	// EnvTemplate is the root-relative path of the development environment template
	// (.envrc.template or similar), or empty when the application has none.
	EnvTemplate string
}

// WebApp is one browser application.
type WebApp struct {
	// Dir is the root-relative directory holding angular.json.
	Dir string
}

// EmulatorRef is one place a Spanner emulator version is named.
type EmulatorRef struct {
	File    string
	Line    int
	Version string
}

// EnvTag is one env struct tag declared in the application's Go code.
type EnvTag struct {
	File       string
	Line       int
	Name       string
	Required   bool
	HasDefault bool
}

// Discover reads the application rooted at dir. dir must hold a go.mod.
func Discover(dir string) (*App, error) {
	root, err := filepath.Abs(dir)
	if err != nil {
		return nil, errors.Wrap(err, "filepath.Abs()")
	}

	modPath := filepath.Join(root, "go.mod")
	modData, err := os.ReadFile(modPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, errors.Newf("no go.mod at %s: run from the application root or pass --app", root)
		}

		return nil, errors.Wrap(err, "os.ReadFile()")
	}

	goMod, err := modfile.Parse(modPath, modData, nil)
	if err != nil {
		return nil, errors.Wrap(err, "modfile.Parse()")
	}

	a := &App{Root: root, GoMod: goMod}
	if err := a.scan(); err != nil {
		return nil, err
	}

	for _, name := range envTemplateNames {
		if _, err := os.Stat(filepath.Join(root, name)); err == nil {
			a.EnvTemplate = name

			break
		}
	}

	return a, nil
}

// envTemplateNames are the development environment templates recognized at the
// application root, in order of preference.
var envTemplateNames = []string{".envrc.template", ".env.template", ".env.example"}

// SiteGenerators returns the generators that emit handlers: one per site.
func (a *App) SiteGenerators() []*Generator {
	var sites []*Generator
	for _, g := range a.Generators {
		if g.HandlersDir() != "" {
			sites = append(sites, g)
		}
	}

	return sites
}

// SharedGenerators returns the generators that emit no handlers: the shared resource
// generators of a multi-site application.
func (a *App) SharedGenerators() []*Generator {
	var shared []*Generator
	for _, g := range a.Generators {
		if g.HandlersDir() == "" {
			shared = append(shared, g)
		}
	}

	return shared
}

// WebAppFor returns the browser application whose directory contains the root-relative
// path, or false when no browser application contains it.
func (a *App) WebAppFor(rel string) (WebApp, bool) {
	rel = filepath.ToSlash(filepath.Clean(rel))
	best, found := WebApp{}, false
	for _, w := range a.WebApps {
		if !within(w.Dir, rel) {
			continue
		}
		if !found || len(w.Dir) > len(best.Dir) {
			best, found = w, true
		}
	}

	return best, found
}

// within reports whether rel (a slash-separated root-relative path) is dir or lies under it.
func within(dir, rel string) bool {
	if dir == "." {
		return true
	}

	return rel == dir || len(rel) > len(dir) && rel[:len(dir)] == dir && rel[len(dir)] == '/'
}

// Rel returns the root-relative, slash-separated form of an absolute path under Root.
func (a *App) Rel(abs string) string {
	rel, err := filepath.Rel(a.Root, abs)
	if err != nil {
		return filepath.ToSlash(abs)
	}

	return filepath.ToSlash(rel)
}

// Abs returns the absolute path of a root-relative path.
func (a *App) Abs(rel string) string {
	return filepath.Join(a.Root, filepath.FromSlash(rel))
}
