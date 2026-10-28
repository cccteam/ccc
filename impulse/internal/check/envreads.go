package check

import (
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/app"
)

// envReads verifies that no Go file outside the config package reads the environment.
// The config package holds the application's env tags, and bedrock derives the stack's
// variables from them, so a read anywhere else names a variable the stack never learns of
// and no deployment sets. Two places are exempt: the main package of a shipped program
// (ShipsProgram on an outlet), which runs on the machine it is installed on and reads its
// own settings there, and test files, which set up the environment they read.
type envReads struct{}

func (envReads) Name() string { return "env-reads" }

func (envReads) Describe() string {
	return "no environment read outside the config package, except inside a declared shipped program's main package"
}

func (envReads) Meaning() string {
	return "The application's configuration is read in one place: the config package (the skeletons' `pkg/config`), where every variable is a tagged field of the level the process constructs (`env:\"APP_PUBLIC_URL,required\"`), read with go-envconfig when the level opens. bedrock derives the stack's variables from those tags, so a read anywhere else (`os.Getenv`, `os.LookupEnv`, `os.Environ`, `os.ExpandEnv`, `syscall.Getenv`, go-envconfig's `Process`, `ProcessWith` or `MustProcess`) names a variable the stack never learns of and no deployment sets, and the process runs with it empty. A shipped program (`ShipsProgram(\"cmd/<name>\", Platforms(...))` on a machine outlet) runs on the machine it is installed on, not in the application's processes, so its main package reads its own settings the same way, through a tagged struct bedrock leaves out; a test file sets up the environment it reads. Each line under the check names the file and line, the call, and the two places a read may live: move the read into the config package, as a tagged field of the level the process constructs, and have the process take the value from its configuration."
}

// environmentReader is a package whose functions read the environment: its import path,
// the name a file refers to it by when it imports it without an alias, and the functions.
type environmentReader struct {
	path  string
	name  string
	funcs []string
}

// environmentReaders are the reads the check looks for, resolved by import path so an
// alias does not escape them: the standard library's lookups, and go-envconfig's
// processing of a tagged struct.
var environmentReaders = []environmentReader{
	{path: "os", name: "os", funcs: []string{"Getenv", "LookupEnv", "Environ", "ExpandEnv"}},
	{path: "syscall", name: "syscall", funcs: []string{"Getenv"}},
	{path: "github.com/sethvargo/go-envconfig", name: "envconfig", funcs: []string{"Process", "ProcessWith", "MustProcess"}},
}

// environmentRead is one read: where it is, and the call as the file writes it, with the
// package's import path when the file imports the package under another name.
type environmentRead struct {
	file string
	line int
	call string
}

func (c envReads) Run(_ context.Context, env *Env) Result {
	a := env.App
	programs := declaredProgramDirs(a)
	configDirs := configPackageDirs(a, programs)
	switch {
	case len(configDirs) == 0:
		return skip(c.Name(), "no env tags outside a shipped program's main package: no config package to hold the reads")
	case len(configDirs) > 1:
		return fail(c.Name(), fmt.Sprintf("env tags in more than one package (%s): the config package holds the application's, and only the main package of a shipped program the generator program declares (ShipsProgram) holds its own, so the config package cannot be told", strings.Join(configDirs, ", ")))
	}
	configPackage := "the config package (" + configDirs[0] + ")"

	var details []string
	allowed := 0
	for _, rel := range a.GoFiles() {
		reads, err := environmentReadsOf(a.Abs(rel), rel)
		if err != nil {
			return fail(c.Name(), err.Error())
		}
		dir := path.Dir(rel)
		if slices.Contains(configDirs, dir) || slices.Contains(programs, dir) {
			allowed += len(reads)

			continue
		}
		for _, r := range reads {
			details = append(details, fmt.Sprintf("%s:%d: %s reads the environment outside %s: a read lives there, or in the main package of a shipped program the generator program declares (ShipsProgram)", r.file, r.line, r.call, configPackage))
		}
	}
	switch {
	case len(details) > 0:
		return fail(c.Name(), fmt.Sprintf("%d environment read(s) outside the config package", len(details)), details...)
	case allowed == 0:
		return pass(c.Name(), "no Go file reads the environment")
	case len(programs) > 0:
		return pass(c.Name(), fmt.Sprintf("%d environment read(s), every one in %s or a declared shipped program's main package (%s)", allowed, configPackage, strings.Join(programs, ", ")))
	default:
		return pass(c.Name(), fmt.Sprintf("%d environment read(s), every one in %s", allowed, configPackage))
	}
}

// declaredProgramDirs lists the main packages of the programs the generator programs
// declare on their outlets (ShipsProgram), sorted and without repeats.
func declaredProgramDirs(a *app.App) []string {
	p := a.Profile()
	var dirs []string
	for i := range p.Sites {
		outlets := p.Sites[i].AllOutlets()
		for j := range outlets {
			for k := range outlets[j].Programs {
				if !slices.Contains(dirs, outlets[j].Programs[k].Dir) {
					dirs = append(dirs, outlets[j].Programs[k].Dir)
				}
			}
		}
	}
	slices.Sort(dirs)

	return dirs
}

// configPackageDirs lists the directories holding the application's env tags, the
// declared programs' main packages left out, sorted and without repeats: the config
// package, where the application's environment is read. More than one is an application
// bedrock refuses, and the check cannot tell which is the config package.
func configPackageDirs(a *app.App, programs []string) []string {
	var dirs []string
	for i := range a.EnvTags {
		dir := path.Dir(a.EnvTags[i].File)
		if !slices.Contains(programs, dir) && !slices.Contains(dirs, dir) {
			dirs = append(dirs, dir)
		}
	}
	slices.Sort(dirs)

	return dirs
}

// environmentReadsOf lists the environment reads of one Go file, in source order: every
// reference to a reading function of a package the file imports by one of the watched
// import paths, under whatever name the file gives it. A file importing none of the
// packages is not parsed.
func environmentReadsOf(abs, rel string) ([]environmentRead, error) {
	src, err := os.ReadFile(abs)
	if err != nil {
		return nil, errors.Wrap(err, "os.ReadFile()")
	}
	if !importsReader(src) {
		return nil, nil
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, errors.Wrap(err, "parser.ParseFile()")
	}
	readers := readersImported(f)
	if len(readers) == 0 {
		return nil, nil
	}
	var reads []environmentRead
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		r, ok := readers[pkg.Name]
		if !ok || !slices.Contains(r.funcs, sel.Sel.Name) {
			return true
		}
		call := pkg.Name + "." + sel.Sel.Name
		if pkg.Name != r.name {
			call += " (" + r.path + ")"
		}
		reads = append(reads, environmentRead{file: rel, line: fset.Position(sel.Pos()).Line, call: call})

		return true
	})

	return reads, nil
}

// importsReader reports whether the source quotes a watched import path.
func importsReader(src []byte) bool {
	for i := range environmentReaders {
		if bytes.Contains(src, []byte(strconv.Quote(environmentReaders[i].path))) {
			return true
		}
	}

	return false
}

// readersImported maps the name a file refers to each watched package by, its alias or
// the package's own name, to the package. A blank or dot import binds no name.
func readersImported(f *ast.File) map[string]*environmentReader {
	imported := map[string]*environmentReader{}
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		for i := range environmentReaders {
			r := &environmentReaders[i]
			if r.path != p {
				continue
			}
			local := r.name
			if imp.Name != nil {
				local = imp.Name.Name
			}
			if local == "_" || local == "." {
				continue
			}
			imported[local] = r
		}
	}

	return imported
}
