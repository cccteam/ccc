package app

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"

	"github.com/go-playground/errors/v5"
)

// skippedDirs are never descended into: they hold dependencies or build output, not the
// application's own declarations.
var skippedDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
	"dist":         true,
	".angular":     true,
}

var (
	emulatorImageRE   = regexp.MustCompile(`cloud-spanner-emulator/emulator:(\d[A-Za-z0-9.\-]*)`)
	emulatorHarnessRE = regexp.MustCompile(`NewSpannerContainer\(\s*[^,()]+,\s*"([^"]+)"`)
	processFileRE     = regexp.MustCompile(`^(Procfile.*|process-compose.*\.ya?ml)$`)
)

// scan walks the tree once and records every declaration the checks read.
func (a *App) scan() error {
	err := filepath.WalkDir(a.Root, func(abs string, d fs.DirEntry, err error) error {
		if err != nil {
			return errors.Wrap(err, "filepath.WalkDir()")
		}
		if d.IsDir() {
			if abs != a.Root && skippedDirs[d.Name()] {
				return filepath.SkipDir
			}

			return nil
		}

		return a.scanFile(abs, d.Name())
	})
	if err != nil {
		return errors.Wrap(err, "filepath.WalkDir()")
	}

	return nil
}

func (a *App) scanFile(abs, name string) error {
	rel := a.Rel(abs)
	switch {
	case name == "angular.json":
		a.WebApps = append(a.WebApps, WebApp{Dir: filepath.ToSlash(filepath.Dir(rel))})
	case processFileRE.MatchString(name):
		data, err := os.ReadFile(abs)
		if err != nil {
			return errors.Wrap(err, "os.ReadFile()")
		}
		a.EmulatorImages = append(a.EmulatorImages, findRefs(rel, data, emulatorImageRE)...)
	case strings.HasSuffix(name, "_test.go"):
		data, err := os.ReadFile(abs)
		if err != nil {
			return errors.Wrap(err, "os.ReadFile()")
		}
		a.EmulatorHarnesses = append(a.EmulatorHarnesses, findRefs(rel, data, emulatorHarnessRE)...)
	case strings.HasSuffix(name, ".go"):
		return a.scanGoFile(abs, rel)
	}

	return nil
}

// scanGoFile reads one non-test Go file for a generator program and for env tags.
func (a *App) scanGoFile(abs, rel string) error {
	data, err := os.ReadFile(abs)
	if err != nil {
		return errors.Wrap(err, "os.ReadFile()")
	}

	if bytes.Contains(data, []byte("NewResourceGenerator")) {
		g, err := parseGenerator(rel, data)
		if err != nil {
			return err
		}
		if g != nil {
			a.Generators = append(a.Generators, g)
		}
	}

	if bytes.Contains(data, []byte(`env:"`)) && !strings.HasPrefix(filepath.Base(rel), "zz_gen_") {
		tags, err := parseEnvTags(rel, data)
		if err != nil {
			return err
		}
		a.EnvTags = append(a.EnvTags, tags...)
	}

	return nil
}

// findRefs returns one EmulatorRef per line of data matching re, whose first group is the
// version.
func findRefs(rel string, data []byte, re *regexp.Regexp) []EmulatorRef {
	var refs []EmulatorRef
	line := 0
	for l := range bytes.Lines(data) {
		line++
		for _, m := range re.FindAllSubmatch(l, -1) {
			refs = append(refs, EmulatorRef{File: rel, Line: line, Version: string(m[1])})
		}
	}

	return refs
}

// parseEnvTags returns every env struct tag in the file. Tags without a name (such as
// the prefix-only tags on embedded structs) are skipped.
func parseEnvTags(rel string, src []byte) ([]EnvTag, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, errors.Wrap(err, "parser.ParseFile()")
	}

	var tags []EnvTag
	ast.Inspect(f, func(n ast.Node) bool {
		field, ok := n.(*ast.Field)
		if !ok || field.Tag == nil {
			return true
		}
		raw := strings.Trim(field.Tag.Value, "`")
		value, ok := reflect.StructTag(raw).Lookup("env")
		if !ok {
			return true
		}
		tag, ok := parseEnvTag(value)
		if !ok {
			return true
		}
		tag.File = rel
		tag.Line = fset.Position(field.Pos()).Line
		tags = append(tags, tag)

		return true
	})

	return tags, nil
}

// parseEnvTag interprets an envconfig tag value: NAME[,required][,default=VALUE][,...].
func parseEnvTag(value string) (EnvTag, bool) {
	parts := strings.Split(value, ",")
	name := strings.TrimSpace(parts[0])
	if name == "" {
		return EnvTag{}, false
	}
	tag := EnvTag{Name: name}
	for _, opt := range parts[1:] {
		opt = strings.TrimSpace(opt)
		switch {
		case opt == "required":
			tag.Required = true
		case strings.HasPrefix(opt, "default="):
			tag.HasDefault = true
		}
	}

	return tag, true
}
