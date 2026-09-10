package app

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
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

	if err := a.followRoleWrappers(); err != nil {
		return err
	}

	return a.authPackages()
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

	generated := strings.HasPrefix(filepath.Base(rel), "zz_gen_")
	if bytes.Contains(data, []byte(`env:"`)) && !generated {
		tags, err := parseEnvTags(rel, data)
		if err != nil {
			return err
		}
		a.EnvTags = append(a.EnvTags, tags...)
	}

	if (bytes.Contains(data, []byte("@"+permissionScopeKeyword)) || bytes.Contains(data, []byte("@"+outletKeyword))) && !generated {
		docs, err := parseStructDocs(rel, data)
		if err != nil {
			return err
		}
		for _, d := range docs {
			if domainScopeRE.MatchString(d.Doc) {
				a.DomainResources = append(a.DomainResources, DomainResource{File: rel, Line: d.Line, Name: d.Name})
			}
			for _, m := range outletRE.FindAllStringSubmatch(d.Doc, -1) {
				member := OutletMember{File: rel, Line: d.Line, Name: d.Name}
				for _, name := range strings.Split(m[1], ",") {
					if name = strings.TrimSpace(name); name != "" {
						member.Outlets = append(member.Outlets, name)
					}
				}
				a.OutletMembers = append(a.OutletMembers, member)
			}
		}
	}

	if bytes.Contains(data, []byte("//go:generate")) {
		a.GoGenerate = append(a.GoGenerate, findDirectives(rel, data)...)
	}

	if bytes.HasPrefix(bytes.TrimSpace(stripLeadingComments(data)), []byte("package main")) {
		dir := path.Dir(rel)
		if len(a.MainPackages) == 0 || a.MainPackages[len(a.MainPackages)-1] != dir {
			a.MainPackages = append(a.MainPackages, dir)
		}
	}

	if bytes.Contains(data, []byte(sessionImportPath)) {
		auths, err := parseAuths(rel, data)
		if err != nil {
			return err
		}
		a.Auths = append(a.Auths, auths...)
	}

	a.goFiles = append(a.goFiles, rel)

	return a.scanRoleMigrations(rel, data)
}

// scanRoleMigrations records the file's access.MigrateRoles calls and wrappers.
func (a *App) scanRoleMigrations(rel string, data []byte) error {
	if !bytes.Contains(data, []byte("MigrateRoles(")) {
		return nil
	}
	calls, wrappers, err := parseRoleMigrations(rel, data)
	if err != nil {
		return err
	}
	a.RoleMigrations = append(a.RoleMigrations, calls...)
	for _, w := range wrappers {
		w.Pkg = a.packagePath(path.Dir(rel))
		a.roleWrappers = append(a.roleWrappers, w)
	}

	return nil
}

// packagePath is the import path of a root-relative directory, or empty without a module
// directive.
func (a *App) packagePath(dir string) string {
	if a.GoMod == nil || a.GoMod.Module == nil {
		return ""
	}
	if dir == "." {
		return a.GoMod.Module.Mod.Path
	}

	return a.GoMod.Module.Mod.Path + "/" + dir
}

// followRoleWrappers records the calls to the application's MigrateRoles wrappers as role
// migrations, so the domains an application passes are checked where it passes them and
// not only inside the wrapper that forwards them.
func (a *App) followRoleWrappers() error {
	if len(a.roleWrappers) == 0 {
		return nil
	}
	for _, rel := range a.goFiles {
		data, err := os.ReadFile(a.Abs(rel))
		if err != nil {
			return errors.Wrap(err, "os.ReadFile()")
		}
		for _, w := range a.roleWrappers {
			if w.Pkg == "" || !bytes.Contains(data, []byte(w.Func+"(")) {
				continue
			}
			calls, err := parseWrapperCalls(rel, data, w)
			if err != nil {
				return err
			}
			a.RoleMigrations = append(a.RoleMigrations, calls...)
		}
	}

	return nil
}

// parseWrapperCalls returns the file's calls to the wrapper, through its import.
func parseWrapperCalls(rel string, src []byte, w roleWrapper) ([]RoleMigration, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, errors.Wrap(err, "parser.ParseFile()")
	}
	local := localImportName(f, w.Pkg)
	if local == "" {
		return nil, nil
	}
	var calls []RoleMigration
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || !isQualified(call.Fun, local, w.Func) {
			return true
		}
		domains := len(call.Args) - w.Fixed
		if call.Ellipsis.IsValid() {
			domains--
		}
		calls = append(calls, RoleMigration{
			File:    rel,
			Line:    fset.Position(call.Pos()).Line,
			Domains: max(domains, 0),
			Spread:  call.Ellipsis.IsValid(),
			Via:     local + "." + w.Func,
		})

		return true
	})

	return calls, nil
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

// goGenerateRE matches a //go:generate directive at the start of a line.
var goGenerateRE = regexp.MustCompile(`^//go:generate\s+(.+?)\s*$`)

// findDirectives returns the //go:generate directives in the file.
func findDirectives(rel string, data []byte) []Directive {
	var directives []Directive
	line := 0
	for l := range bytes.Lines(data) {
		line++
		if m := goGenerateRE.FindSubmatch(bytes.TrimRight(l, "\r\n")); m != nil {
			directives = append(directives, Directive{File: rel, Line: line, Command: string(m[1])})
		}
	}

	return directives
}

// stripLeadingComments drops the comment lines and blank lines before a file's package
// clause, so the clause can be read without a full parse.
func stripLeadingComments(data []byte) []byte {
	for {
		trimmed := bytes.TrimLeft(data, " \t\r\n")
		switch {
		case bytes.HasPrefix(trimmed, []byte("//")):
			i := bytes.IndexByte(trimmed, '\n')
			if i < 0 {
				return nil
			}
			data = trimmed[i+1:]
		case bytes.HasPrefix(trimmed, []byte("/*")):
			i := bytes.Index(trimmed, []byte("*/"))
			if i < 0 {
				return nil
			}
			data = trimmed[i+2:]
		default:
			return trimmed
		}
	}
}

// The annotations and import the scan reads.
const (
	permissionScopeKeyword = "permissionScope"
	outletKeyword          = "outlet"
	accessImportPath       = "github.com/cccteam/access"
	// migrateRolesFixedArgs is how many arguments access.MigrateRoles takes before the
	// domains: ctx, manager, collection, roles.
	migrateRolesFixedArgs = 4
)

// domainScopeRE matches the @permissionScope(domain) struct annotation; outletRE
// captures the names an @outlet(...) annotation lists.
var (
	domainScopeRE = regexp.MustCompile(`@` + permissionScopeKeyword + `\(\s*domain\s*\)`)
	outletRE      = regexp.MustCompile(`@` + outletKeyword + `\(([^)]*)\)`)
)

// structDoc is one struct type declaration with its doc comment.
type structDoc struct {
	Name string
	Line int
	Doc  string
}

// parseStructDocs returns every struct type in the file that has a doc comment.
func parseStructDocs(rel string, src []byte) ([]structDoc, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil, errors.Wrap(err, "parser.ParseFile()")
	}

	var docs []structDoc
	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			if _, isStruct := ts.Type.(*ast.StructType); !isStruct {
				continue
			}
			doc := ts.Doc
			if doc == nil && len(gen.Specs) == 1 {
				doc = gen.Doc
			}
			if doc == nil {
				continue
			}
			docs = append(docs, structDoc{Name: ts.Name.Name, Line: fset.Position(ts.Pos()).Line, Doc: doc.Text()})
		}
	}

	return docs, nil
}

// parseRoleMigrations returns every access.MigrateRoles call in the file, and the
// functions that wrap it by passing their own variadic domains through, whose callers
// are role migrations as well.
func parseRoleMigrations(rel string, src []byte) (calls []RoleMigration, wrappers []roleWrapper, err error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, nil, errors.Wrap(err, "parser.ParseFile()")
	}
	pkg := localImportName(f, accessImportPath)
	if pkg == "" {
		return nil, nil, nil
	}

	for _, decl := range f.Decls {
		fd, _ := decl.(*ast.FuncDecl)
		ast.Inspect(decl, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || !isQualified(call.Fun, pkg, "MigrateRoles") {
				return true
			}
			domains := len(call.Args) - migrateRolesFixedArgs
			if call.Ellipsis.IsValid() {
				domains-- // the spread slice is not a domain
			}
			calls = append(calls, RoleMigration{
				File:    rel,
				Line:    fset.Position(call.Pos()).Line,
				Domains: max(domains, 0),
				Spread:  call.Ellipsis.IsValid(),
			})
			if w, ok := wrapperOf(fd, call); ok {
				wrappers = append(wrappers, w)
			}

			return true
		})
	}

	return calls, wrappers, nil
}

// wrapperOf reports whether the call spreads the enclosing top-level function's own
// variadic parameter, which makes that function a MigrateRoles wrapper.
func wrapperOf(fd *ast.FuncDecl, call *ast.CallExpr) (roleWrapper, bool) {
	if fd == nil || fd.Recv != nil || !call.Ellipsis.IsValid() || len(call.Args) == 0 || fd.Type.Params == nil {
		return roleWrapper{}, false
	}
	params := fd.Type.Params.List
	if len(params) == 0 {
		return roleWrapper{}, false
	}
	last := params[len(params)-1]
	if _, variadic := last.Type.(*ast.Ellipsis); !variadic || len(last.Names) != 1 {
		return roleWrapper{}, false
	}
	spread, ok := call.Args[len(call.Args)-1].(*ast.Ident)
	if !ok || spread.Name != last.Names[0].Name {
		return roleWrapper{}, false
	}
	fixed := 0
	for _, p := range params[:len(params)-1] {
		fixed += max(len(p.Names), 1)
	}

	return roleWrapper{Func: fd.Name.Name, Fixed: fixed}, true
}

// localImportName returns the name the file imports the path under, or empty when the
// file does not import it.
func localImportName(f *ast.File, importPath string) string {
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil || p != importPath {
			continue
		}
		if imp.Name != nil {
			return imp.Name.Name
		}

		return path.Base(p)
	}

	return ""
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
