package app

import (
	"path"
	"strings"
)

// RunsGenerator reports whether a //go:generate directive runs the generator program:
// `go run` of the program's directory, relative to the directive's file or as an import
// path under the module.
func (a *App) RunsGenerator(g *Generator) (Directive, bool) {
	want := path.Dir(g.File)
	for _, d := range a.GoGenerate {
		fields := strings.Fields(d.Command)
		if len(fields) < 3 || fields[0] != "go" || fields[1] != "run" {
			continue
		}
		target := fields[2]
		if strings.HasSuffix(target, ".go") {
			target = path.Dir(target)
		}
		switch {
		case target == "." || strings.HasPrefix(target, "./") || strings.HasPrefix(target, "../"):
			target = path.Join(path.Dir(d.File), target)
		case a.GoMod != nil && a.GoMod.Module != nil && strings.HasPrefix(target, a.GoMod.Module.Mod.Path+"/"):
			target = strings.TrimPrefix(target, a.GoMod.Module.Mod.Path+"/")
		default:
			continue // another module's program, or a bare name go run would not accept
		}
		if path.Clean(target) == want {
			return d, true
		}
	}

	return Directive{}, false
}
